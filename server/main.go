package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cacggghp/vk-turn-proxy/internal/statusmodel"
	"github.com/cacggghp/vk-turn-proxy/tcputil"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/xtaci/smux"
)

type ServerConfig struct {
	ListenAddr     string `json:"listen_addr"`
	ConnectAddr    string `json:"connect_addr"`
	VLESSMode      bool   `json:"vless_mode"`
	LogLevel       string `json:"log_level,omitempty"`
	StatusAPIAddr  string `json:"status_api_addr,omitempty"`
	ServiceName    string `json:"service_name,omitempty"`
	CheckBackend   bool   `json:"check_backend"`
	BackendNetwork string `json:"backend_network,omitempty"`
}

type ServerConfigOverrides struct {
	ListenAddr    string
	ConnectAddr   string
	VLESSMode     bool
	CheckBackend  bool
	LogLevel      string
	StatusAPIAddr string
	ServiceName   string
	Set           map[string]bool
}

func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		ListenAddr:   "0.0.0.0:56000",
		CheckBackend: true,
	}
}

func main() {
	configPath := flag.String("config", "", "path to server JSON config")
	listen := flag.String("listen", "", "listen on ip:port")
	connect := flag.String("connect", "", "connect to ip:port")
	vlessMode := flag.Bool("vless", false, "VLESS mode: forward TCP connections (for VLESS) instead of UDP packets")
	checkBackend := flag.Bool("check-backend", true, "check backend reachability before accepting sidecar traffic")
	logLevel := flag.String("log-level", "", "log level metadata for config/status output")
	statusAPIAddr := flag.String("status-api", "", "status API listen address metadata")
	serviceName := flag.String("service-name", "", "service name metadata")
	flag.Parse()

	cfg, err := LoadServerConfig(*configPath)
	if err != nil {
		log.Panicf("load config: %v", err)
	}
	setFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	cfg = ApplyServerConfigOverrides(cfg, ServerConfigOverrides{
		ListenAddr:    *listen,
		ConnectAddr:   *connect,
		VLESSMode:     *vlessMode,
		CheckBackend:  *checkBackend,
		LogLevel:      *logLevel,
		StatusAPIAddr: *statusAPIAddr,
		ServiceName:   *serviceName,
		Set:           setFlags,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-signalChan
		log.Printf("Terminating...\n")
		cancel()
		<-signalChan
		log.Fatalf("Exit...\n")
	}()

	if err := RunServer(ctx, cfg); err != nil {
		log.Panicf("server failed: %v", err)
	}
}

func ApplyServerConfigOverrides(cfg ServerConfig, overrides ServerConfigOverrides) ServerConfig {
	if overrides.Set["listen"] {
		cfg.ListenAddr = overrides.ListenAddr
	}
	if overrides.Set["connect"] {
		cfg.ConnectAddr = overrides.ConnectAddr
	}
	if overrides.Set["vless"] {
		cfg.VLESSMode = overrides.VLESSMode
	}
	if overrides.Set["check-backend"] {
		cfg.CheckBackend = overrides.CheckBackend
	}
	if overrides.Set["log-level"] {
		cfg.LogLevel = overrides.LogLevel
	}
	if overrides.Set["status-api"] {
		cfg.StatusAPIAddr = overrides.StatusAPIAddr
	}
	if overrides.Set["service-name"] {
		cfg.ServiceName = overrides.ServiceName
	}
	return cfg
}

func LoadServerConfig(path string) (ServerConfig, error) {
	cfg := DefaultServerConfig()
	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read server config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse server config: %w", err)
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = DefaultServerConfig().ListenAddr
	}
	return cfg, nil
}

func RunServer(ctx context.Context, cfg ServerConfig) error {
	if err := cfg.NormalizeAndValidate(); err != nil {
		return err
	}
	runtime := NewRuntimeStatus(cfg)
	if cfg.CheckBackend {
		if err := CheckBackend(ctx, cfg); err != nil {
			runtime.RecordError("backend", statusmodel.CodeBackendDown, err.Error())
			return err
		}
	}
	if _, err := startStatusAPI(ctx, cfg.StatusAPIAddr, runtime); err != nil {
		runtime.RecordError("status_api", statusmodel.CodeListenFailed, err.Error())
		return err
	}

	addr, err := net.ResolveUDPAddr("udp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("resolve listen address: %w", err)
	}
	// Generate a certificate and private key to secure the connection
	certificate, genErr := selfsign.GenerateSelfSigned()
	if genErr != nil {
		return fmt.Errorf("generate certificate: %w", genErr)
	}

	// Connect to a DTLS server
	listener, err := dtls.ListenWithOptions(
		"udp",
		addr,
		dtls.WithCertificates(certificate),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
		dtls.WithCipherSuites(dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256),
		dtls.WithConnectionIDGenerator(dtls.RandomCIDGenerator(8)),
	)
	if err != nil {
		runtime.RecordError("listener", statusmodel.CodeListenFailed, err.Error())
		return fmt.Errorf("listen DTLS: %w", err)
	}
	context.AfterFunc(ctx, func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			log.Printf("failed to close listener: %v", err)
		}
	})

	fmt.Println("Listening")

	wg1 := sync.WaitGroup{}
	for {
		select {
		case <-ctx.Done():
			wg1.Wait()
			return nil
		default:
		}
		// Wait for a connection.
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				wg1.Wait()
				return nil
			}
			log.Println(err)
			continue
		}
		wg1.Add(1)
		go func(conn net.Conn) {
			defer wg1.Done()
			runtime.RecordAcceptedConnection()
			defer runtime.RecordConnectionClosed()
			serveAcceptedConnection(ctx, conn, cfg, runtime)
		}(conn)
	}
}

func (cfg *ServerConfig) NormalizeAndValidate() error {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = DefaultServerConfig().ListenAddr
	}
	if cfg.BackendNetwork == "" {
		if cfg.VLESSMode {
			cfg.BackendNetwork = "tcp"
		} else {
			cfg.BackendNetwork = "udp"
		}
	}

	if cfg.ConnectAddr == "" {
		return fmt.Errorf("server address is required")
	}
	if _, err := net.ResolveUDPAddr("udp", cfg.ListenAddr); err != nil {
		return fmt.Errorf("resolve listen address: %w", err)
	}
	if cfg.BackendNetwork != "tcp" && cfg.BackendNetwork != "udp" {
		return fmt.Errorf("unsupported backend network: %s", cfg.BackendNetwork)
	}
	switch cfg.BackendNetwork {
	case "tcp":
		if _, err := net.ResolveTCPAddr("tcp", cfg.ConnectAddr); err != nil {
			return fmt.Errorf("resolve backend address: %w", err)
		}
	case "udp":
		if _, err := net.ResolveUDPAddr("udp", cfg.ConnectAddr); err != nil {
			return fmt.Errorf("resolve backend address: %w", err)
		}
	}
	return nil
}

func CheckBackend(ctx context.Context, cfg ServerConfig) error {
	if cfg.BackendNetwork == "tcp" {
		dialer := net.Dialer{Timeout: 2 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", cfg.ConnectAddr)
		if err != nil {
			return fmt.Errorf("backend tcp check failed: %w", err)
		}
		if err := conn.Close(); err != nil && !isExpectedCloseError(err) {
			return fmt.Errorf("close backend tcp check: %w", err)
		}
	}
	return nil
}

func serveAcceptedConnection(ctx context.Context, conn net.Conn, cfg ServerConfig, runtime *RuntimeStatus) {
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("failed to close incoming connection: %s", closeErr)
			if runtime != nil {
				runtime.AppendLog(closeErr.Error())
			}
		}
	}()
	log.Printf("Connection from %s\n", conn.RemoteAddr())
	if runtime != nil {
		runtime.AppendLog("connection from " + conn.RemoteAddr().String())
	}

	// Perform the handshake with a 30-second timeout.
	ctx1, cancel1 := context.WithTimeout(ctx, 30*time.Second)
	defer cancel1()

	dtlsConn, ok := conn.(*dtls.Conn)
	if !ok {
		log.Println("Type error: expected *dtls.Conn")
		if runtime != nil {
			runtime.RecordError("dtls", statusmodel.CodeDTLSHandshake, "type error: expected DTLS connection")
		}
		return
	}
	log.Println("Start handshake")
	if err := dtlsConn.HandshakeContext(ctx1); err != nil {
		log.Printf("Handshake failed: %v", err)
		if runtime != nil {
			runtime.RecordError("dtls", statusmodel.CodeDTLSHandshake, err.Error())
		}
		return
	}
	log.Println("Handshake done")
	if runtime != nil {
		runtime.RecordEvent("dtls", statusmodel.StateConnected, statusmodel.CodeNone, "DTLS handshake done")
	}

	if cfg.VLESSMode {
		handleVLESSConnection(ctx, dtlsConn, cfg.ConnectAddr, runtime)
	} else {
		handleUDPConnection(ctx, conn, cfg.ConnectAddr)
	}

	log.Printf("Connection closed: %s\n", conn.RemoteAddr())
	if runtime != nil {
		runtime.AppendLog("connection closed: " + conn.RemoteAddr().String())
	}
}

// handleUDPConnection forwards DTLS packets to a UDP backend (WireGuard).
func handleUDPConnection(ctx context.Context, conn net.Conn, connectAddr string) {
	serverConn, err := net.Dial("udp", connectAddr)
	if err != nil {
		log.Println(err)
		return
	}
	defer func() {
		if err = serverConn.Close(); err != nil {
			log.Printf("failed to close outgoing connection: %s", err)
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	ctx2, cancel2 := context.WithCancel(ctx)
	context.AfterFunc(ctx2, func() {
		if err := conn.SetDeadline(time.Now()); err != nil {
			log.Printf("failed to set incoming deadline: %s", err)
		}
		if err := serverConn.SetDeadline(time.Now()); err != nil {
			log.Printf("failed to set outgoing deadline: %s", err)
		}
	})
	go func() {
		defer wg.Done()
		defer cancel2()
		buf := make([]byte, 1600)
		for {
			select {
			case <-ctx2.Done():
				return
			default:
			}
			if err1 := conn.SetReadDeadline(time.Now().Add(time.Minute * 30)); err1 != nil {
				log.Printf("Failed: %s", err1)
				return
			}
			n, err1 := conn.Read(buf)
			if err1 != nil {
				log.Printf("Failed: %s", err1)
				return
			}

			if err1 = serverConn.SetWriteDeadline(time.Now().Add(time.Minute * 30)); err1 != nil {
				log.Printf("Failed: %s", err1)
				return
			}
			_, err1 = serverConn.Write(buf[:n])
			if err1 != nil {
				log.Printf("Failed: %s", err1)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		defer cancel2()
		buf := make([]byte, 1600)
		for {
			select {
			case <-ctx2.Done():
				return
			default:
			}
			if err1 := serverConn.SetReadDeadline(time.Now().Add(time.Minute * 30)); err1 != nil {
				log.Printf("Failed: %s", err1)
				return
			}
			n, err1 := serverConn.Read(buf)
			if err1 != nil {
				log.Printf("Failed: %s", err1)
				return
			}

			if err1 = conn.SetWriteDeadline(time.Now().Add(time.Minute * 30)); err1 != nil {
				log.Printf("Failed: %s", err1)
				return
			}
			_, err1 = conn.Write(buf[:n])
			if err1 != nil {
				log.Printf("Failed: %s", err1)
				return
			}
		}
	}()
	wg.Wait()
}

// handleVLESSConnection creates a KCP+smux session over DTLS and forwards
// each smux stream as a TCP connection to the backend (Xray/VLESS).
func handleVLESSConnection(ctx context.Context, dtlsConn net.Conn, connectAddr string, runtime *RuntimeStatus) {
	// 1. Create KCP session over DTLS
	kcpSess, err := tcputil.NewKCPOverDTLS(dtlsConn, true)
	if err != nil {
		log.Printf("KCP session error: %s", err)
		if runtime != nil {
			runtime.RecordError("kcp", statusmodel.CodeKCPSession, err.Error())
		}
		return
	}
	closeKCPSession := sync.OnceFunc(func() {
		if err := kcpSess.Close(); err != nil && !isExpectedCloseError(err) {
			log.Printf("failed to close KCP session: %v", err)
		}
	})
	defer closeKCPSession()
	context.AfterFunc(ctx, closeKCPSession)
	log.Printf("KCP session established (server)")
	if runtime != nil {
		runtime.RecordEvent("kcp", statusmodel.StateConnected, statusmodel.CodeNone, "KCP session established")
	}

	// 2. Create smux server session over KCP
	smuxSess, err := smux.Server(kcpSess, tcputil.DefaultSmuxConfig())
	if err != nil {
		log.Printf("smux server error: %s", err)
		if runtime != nil {
			runtime.RecordError("smux", statusmodel.CodeSmuxSession, err.Error())
		}
		return
	}
	closeSmuxSession := sync.OnceFunc(func() {
		if err := smuxSess.Close(); err != nil && !isExpectedCloseError(err) {
			log.Printf("failed to close smux session: %v", err)
		}
	})
	defer closeSmuxSession()
	context.AfterFunc(ctx, closeSmuxSession)
	log.Printf("smux session established (server)")
	if runtime != nil {
		runtime.RecordEvent("smux", statusmodel.StateConnected, statusmodel.CodeNone, "smux session established")
	}

	// 3. Accept smux streams and forward to backend via TCP
	var wg sync.WaitGroup
	for {
		stream, err := smuxSess.AcceptStream()
		if err != nil {
			select {
			case <-ctx.Done():
			default:
				log.Printf("smux accept error: %s", err)
			}
			break
		}

		wg.Add(1)
		go func(s *smux.Stream) {
			defer wg.Done()

			defer func() {
				if err := s.Close(); err != nil && err != smux.ErrGoAway && !isExpectedCloseError(err) {
					log.Printf("failed to close smux stream: %v", err)
				}
			}()

			// Connect to backend (Xray/VLESS)
			backendConn, err := net.DialTimeout("tcp", connectAddr, 10*time.Second)
			if err != nil {
				log.Printf("backend dial error: %s", err)
				if runtime != nil {
					runtime.RecordError("backend", statusmodel.CodeBackendDial, err.Error())
				}
				return
			}
			defer func() {
				if err := backendConn.Close(); err != nil {
					log.Printf("failed to close backend connection: %v", err)
				}
			}()

			// Bidirectional copy
			pipeConn(ctx, s, backendConn)
		}(stream)
	}
	wg.Wait()
}

// pipeConn copies data bidirectionally between two connections.
func pipeConn(ctx context.Context, c1, c2 net.Conn) {
	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()

	context.AfterFunc(ctx2, func() {
		if err := c1.SetDeadline(time.Now()); err != nil {
			log.Printf("pipeConn: failed to set deadline c1: %v", err)
		}
		if err := c2.SetDeadline(time.Now()); err != nil {
			log.Printf("pipeConn: failed to set deadline c2: %v", err)
		}
	})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer cancel()
		if _, err := io.Copy(c1, c2); err != nil {
			log.Printf("pipeConn: c1<-c2 copy error: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		defer cancel()
		if _, err := io.Copy(c2, c1); err != nil {
			log.Printf("pipeConn: c2<-c1 copy error: %v", err)
		}
	}()

	wg.Wait()

	// Reset deadlines
	_ = c1.SetDeadline(time.Time{})
	_ = c2.SetDeadline(time.Time{})
}

func isExpectedCloseError(err error) bool {
	if err == nil {
		return true
	}
	return errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.ErrClosedPipe) ||
		strings.Contains(err.Error(), "use of closed network connection")
}
