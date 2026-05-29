// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v5"
)

type connectedUDPConn struct {
	*net.UDPConn
}

func (c *connectedUDPConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	return c.Write(p)
}

type turnParams struct {
	host      string
	port      string
	link      string
	udp       bool
	devDirect bool
	getCreds  getCredsFunc
}

type turnRelaySession struct {
	relayConn      net.PacketConn
	client         *turn.Client
	closeTransport func() error
}

func (s *turnRelaySession) close() error {
	if s == nil {
		return nil
	}

	var errs []error
	if s.relayConn != nil {
		if err := s.relayConn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.client != nil {
		s.client.Close()
	}
	if s.closeTransport != nil {
		if err := s.closeTransport(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func createTURNRelaySession(ctx context.Context, tp *turnParams, peer *net.UDPAddr, streamID int) (*turnRelaySession, error) {
	user, pass, urlTarget, err := tp.getCreds(ctx, tp.link, streamID)
	if err != nil {
		return nil, fmt.Errorf("get TURN credentials: %w", err)
	}

	urlhost, urlport, err := net.SplitHostPort(urlTarget)
	if err != nil {
		return nil, fmt.Errorf("parse TURN server address: %w", err)
	}
	if tp.host != "" {
		urlhost = tp.host
	}
	if tp.port != "" {
		urlport = tp.port
	}

	turnServerAddr := net.JoinHostPort(urlhost, urlport)
	turnServerUDPAddr, err := net.ResolveUDPAddr("udp", turnServerAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve TURN server address: %w", err)
	}
	turnServerAddr = turnServerUDPAddr.String()
	fmt.Println(turnServerUDPAddr.IP)

	turnConn, closeTransport, err := dialTURNTransport(ctx, tp.udp, turnServerUDPAddr)
	if err != nil {
		return nil, err
	}
	session := &turnRelaySession{closeTransport: closeTransport}

	addrFamily := turn.RequestedAddressFamilyIPv6
	if peer.IP.To4() != nil {
		addrFamily = turn.RequestedAddressFamilyIPv4
	}

	cfg := &turn.ClientConfig{
		STUNServerAddr:         turnServerAddr,
		TURNServerAddr:         turnServerAddr,
		Conn:                   turnConn,
		Net:                    newDirectNet(),
		Username:               user,
		Password:               pass,
		RequestedAddressFamily: addrFamily,
		LoggerFactory:          logging.NewDefaultLoggerFactory(),
	}

	turnClient, err := turn.NewClient(cfg)
	if err != nil {
		_ = session.close()
		return nil, fmt.Errorf("create TURN client: %w", err)
	}
	session.client = turnClient

	if err = turnClient.Listen(); err != nil {
		_ = session.close()
		return nil, fmt.Errorf("TURN listen: %w", err)
	}

	relayConn, err := turnClient.Allocate()
	if err != nil {
		_ = session.close()
		return nil, fmt.Errorf("TURN allocate: %w", err)
	}
	session.relayConn = relayConn

	return session, nil
}

func dialTURNTransport(ctx context.Context, useUDP bool, turnServerUDPAddr *net.UDPAddr) (net.PacketConn, func() error, error) {
	if useUDP {
		conn, err := net.DialUDP("udp", nil, turnServerUDPAddr) // nolint: noctx
		if err != nil {
			return nil, nil, fmt.Errorf("connect to TURN server over UDP: %w", err)
		}
		return &connectedUDPConn{conn}, conn.Close, nil
	}

	ctx1, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx1, "tcp", turnServerUDPAddr.String())
	if err != nil {
		return nil, nil, fmt.Errorf("connect to TURN server over TCP: %w", err)
	}
	return turn.NewSTUNConn(conn), conn.Close, nil
}

func oneTurnConnection(ctx context.Context, turnParams *turnParams, peer *net.UDPAddr, conn2 net.PacketConn, streamID int, c chan<- error) {
	time.Sleep(time.Duration(secureIntn(400)+100) * time.Millisecond)
	var err error
	defer func() { c <- err }()
	turnSession, err1 := createTURNRelaySession(ctx, turnParams, peer, streamID)
	if err1 != nil {
		if isAuthError(err1) {
			handleAuthError(streamID)
		}
		err = err1
		return
	}
	relayConn := turnSession.relayConn

	// Reset error count on successful allocation.
	getStreamCache(streamID).errorCount.Store(0)

	// Safely track active streams globally.
	connectedStreams.Add(1)
	defer func() {
		connectedStreams.Add(-1)
		if err1 := turnSession.close(); err1 != nil {
			err = errors.Join(err, fmt.Errorf("close TURN relay session: %w", err1))
		}
	}()

	if isDebug {
		log.Printf("[STREAM %d] relayed-address=%s", streamID, relayConn.LocalAddr().String())
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	turnctx, turncancel := context.WithCancel(ctx)
	context.AfterFunc(turnctx, func() {
		if err := relayConn.SetDeadline(time.Now()); err != nil {
			log.Printf("Failed to set relay deadline: %s", err)
		}
		// Do not set conn2 deadline; it belongs to the DTLS packet pipe.
	})
	var internalPipeAddr atomic.Value

	go func() {
		defer turncancel()
		buf := make([]byte, 1600)
		for {
			if turnctx.Err() != nil {
				return
			}
			n, addr1, err1 := conn2.ReadFrom(buf)
			if err1 != nil {
				return
			}
			if turnctx.Err() != nil {
				return
			}

			internalPipeAddr.Store(addr1)

			_, err1 = relayConn.WriteTo(buf[:n], peer)
			if err1 != nil {
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		defer turncancel()
		buf := make([]byte, 1600)
		for {
			n, _, err1 := relayConn.ReadFrom(buf)
			if err1 != nil {
				return
			}
			addr1 := internalPipeAddr.Load()
			if addr1 == nil {
				continue
			}

			if addr, ok := addr1.(net.Addr); ok {
				if _, err := conn2.WriteTo(buf[:n], addr); err != nil {
					return
				}
			}
		}
	}()

	wg.Wait()
	if err := relayConn.SetDeadline(time.Time{}); err != nil {
		log.Printf("Failed to clear relay deadline: %s", err)
	}
}

func oneTurnConnectionLoop(ctx context.Context, turnParams *turnParams, peer *net.UDPAddr, connchan <-chan net.PacketConn, t <-chan time.Time, streamID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case conn2 := <-connchan:
			select {
			case <-t:
			case <-ctx.Done():
				return
			}
			c := make(chan error)
			go oneTurnConnection(ctx, turnParams, peer, conn2, streamID, c)

			if err := <-c; err != nil {
				if strings.Contains(err.Error(), "FATAL_CAPTCHA") {
					log.Printf("[STREAM %d] Fatal manual captcha error. Shutting down application.", streamID)
					if globalAppCancel != nil {
						globalAppCancel()
					}
					return
				}
				if strings.Contains(err.Error(), "CAPTCHA_WAIT_REQUIRED") {
					if !strings.Contains(err.Error(), "global lockout active") {
						log.Printf("[STREAM %d] Backing off for 60 seconds to avoid IP ban...", streamID)
						select {
						case <-ctx.Done():
							return
						case <-time.After(60 * time.Second):
						}
					} else {
						lockoutEnd := globalCaptchaLockout.Load()
						sleepDuration := time.Until(time.Unix(lockoutEnd, 0))
						if sleepDuration < 0 {
							sleepDuration = 5 * time.Second
						}
						select {
						case <-ctx.Done():
							return
						case <-time.After(sleepDuration):
						}
					}
				} else {
					log.Printf("[STREAM %d] %s", streamID, err)
					time.Sleep(2 * time.Second)
				}
			}
		}
	}
}
