package tcputil

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
)

func TestNewKCPOverDTLSRoundTrip(t *testing.T) {
	listener := startTestDTLSServer(t)
	defer func() { _ = listener.Close() }()

	serverDone := make(chan error, 1)
	releaseServer := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		dtlsConn, ok := conn.(*dtls.Conn)
		if !ok {
			_ = conn.Close()
			serverDone <- errors.New("accepted non-DTLS connection")
			return
		}
		defer func() { _ = dtlsConn.Close() }()

		sess, err := NewKCPOverDTLS(dtlsConn, true)
		if err != nil {
			serverDone <- err
			return
		}
		defer func() { _ = sess.Close() }()

		if err := sess.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			serverDone <- err
			return
		}
		got := make([]byte, len("client-to-server"))
		if _, err := io.ReadFull(sess, got); err != nil {
			serverDone <- err
			return
		}
		if !bytes.Equal(got, []byte("client-to-server")) {
			serverDone <- errors.New("unexpected client payload")
			return
		}
		if _, err := sess.Write([]byte("server-to-client")); err != nil {
			serverDone <- err
			return
		}
		select {
		case <-releaseServer:
		case <-time.After(2 * time.Second):
			serverDone <- errors.New("client did not release server session")
			return
		}
		serverDone <- nil
	}()

	clientConn := dialTestDTLSClient(t, listener.Addr().String())
	defer func() { _ = clientConn.Close() }()

	clientSession, err := NewKCPOverDTLS(clientConn, false)
	if err != nil {
		t.Fatalf("create client KCP session: %v", err)
	}
	defer func() { _ = clientSession.Close() }()

	writeKCPPayload(t, clientSession, []byte("client-to-server"))
	readKCPPayload(t, clientSession, []byte("server-to-client"))
	close(releaseServer)

	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("server KCP round trip: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("server KCP session did not complete")
	}
}

func writeKCPPayload(t *testing.T, writer io.Writer, payload []byte) {
	t.Helper()

	writeErr := make(chan error, 1)
	go func() {
		_, err := writer.Write(payload)
		writeErr <- err
	}()

	select {
	case err := <-writeErr:
		if err != nil {
			t.Fatalf("write KCP payload: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("write KCP payload did not complete")
	}
}

func readKCPPayload(t *testing.T, reader interface {
	io.Reader
	SetReadDeadline(time.Time) error
}, payload []byte) {
	t.Helper()

	if err := reader.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatalf("read KCP payload: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %q, want %q", string(got), string(payload))
	}
}

func startTestDTLSServer(t *testing.T) net.Listener {
	t.Helper()

	certificate, err := selfsign.GenerateSelfSigned()
	if err != nil {
		t.Fatalf("generate server certificate: %v", err)
	}
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve DTLS server addr: %v", err)
	}
	listener, err := dtls.ListenWithOptions(
		"udp",
		addr,
		dtls.WithCertificates(certificate),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
		dtls.WithCipherSuites(dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256),
		dtls.WithConnectionIDGenerator(dtls.RandomCIDGenerator(8)),
	)
	if err != nil {
		t.Fatalf("listen DTLS server: %v", err)
	}
	return listener
}

func dialTestDTLSClient(t *testing.T, addr string) *dtls.Conn {
	t.Helper()

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		t.Fatalf("resolve DTLS client addr: %v", err)
	}
	certificate, err := selfsign.GenerateSelfSigned()
	if err != nil {
		t.Fatalf("generate client certificate: %v", err)
	}
	conn, err := dtls.DialWithOptions(
		"udp",
		udpAddr,
		dtls.WithCertificates(certificate),
		dtls.WithInsecureSkipVerify(true),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
		dtls.WithCipherSuites(dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256),
		dtls.WithConnectionIDGenerator(dtls.OnlySendCIDGenerator()),
	)
	if err != nil {
		t.Fatalf("dial DTLS client: %v", err)
	}
	return conn
}
