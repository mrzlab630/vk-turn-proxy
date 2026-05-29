package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cacggghp/vk-turn-proxy/tcputil"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/xtaci/smux"
)

func TestPipeConnCopiesBothDirections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	leftLocal, leftRemote := net.Pipe()
	rightLocal, rightRemote := net.Pipe()
	done := make(chan struct{})

	go func() {
		pipeConn(ctx, leftLocal, rightLocal)
		close(done)
	}()

	assertPipeRoundTrip(t, leftRemote, rightRemote, []byte("left-to-right"))
	assertPipeRoundTrip(t, rightRemote, leftRemote, []byte("right-to-left"))

	if err := leftRemote.Close(); err != nil {
		t.Fatalf("close left remote: %v", err)
	}
	if err := rightRemote.Close(); err != nil {
		t.Fatalf("close right remote: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("pipeConn did not stop: %v", ctx.Err())
	}
}

func TestHandleUDPConnectionForwardsPacketsBothDirections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	backendAddr, backendPackets, backendDone := startUDPBackend(t, ctx)
	leftConn, rightConn := net.Pipe()
	done := make(chan struct{})

	go func() {
		handleUDPConnection(ctx, leftConn, backendAddr)
		close(done)
	}()

	if _, err := rightConn.Write([]byte("ping")); err != nil {
		t.Fatalf("write incoming UDP payload: %v", err)
	}

	select {
	case got := <-backendPackets:
		if !bytes.Equal(got, []byte("ping")) {
			t.Fatalf("backend got %q, want ping", string(got))
		}
	case <-ctx.Done():
		t.Fatalf("backend did not receive packet: %v", ctx.Err())
	}

	buf := make([]byte, 32)
	if err := rightConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	n, err := rightConn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if got := string(buf[:n]); got != "echo:ping" {
		t.Fatalf("response = %q, want echo:ping", got)
	}

	if err := rightConn.Close(); err != nil {
		t.Fatalf("close right conn: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("handleUDPConnection did not stop: %v", ctx.Err())
	}

	cancel()
	select {
	case <-backendDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("UDP backend did not stop")
	}
}

func TestHandleUDPConnectionReturnsOnInvalidBackendAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	leftConn, rightConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		handleUDPConnection(ctx, leftConn, "bad udp address")
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("handleUDPConnection did not return after invalid backend address: %v", ctx.Err())
	}

	if err := rightConn.Close(); err != nil {
		t.Fatalf("close right conn: %v", err)
	}
}

func TestHandleVLESSConnectionForwardsSmuxStreamsToTCPBackend(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	backendAddr, backendDone := startTCPBackend(t, ctx)
	listener := startDTLSServer(t)
	defer func() { _ = listener.Close() }()

	serverDone := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() == nil && !isClosedNetworkError(err) {
				t.Errorf("DTLS accept: %v", err)
			}
			close(serverDone)
			return
		}
		defer func() { _ = conn.Close() }()

		dtlsConn, ok := conn.(*dtls.Conn)
		if !ok {
			t.Errorf("accepted %T, want *dtls.Conn", conn)
			close(serverDone)
			return
		}
		handleVLESSConnection(ctx, dtlsConn, backendAddr, nil)
		close(serverDone)
	}()

	clientConn := dialDTLSClient(t, listener.Addr().String())
	kcpSess, err := tcputil.NewKCPOverDTLS(clientConn, false)
	if err != nil {
		t.Fatalf("client KCP session: %v", err)
	}
	smuxSess, err := smux.Client(kcpSess, tcputil.DefaultSmuxConfig())
	if err != nil {
		t.Fatalf("client smux session: %v", err)
	}

	stream, err := smuxSess.OpenStream()
	if err != nil {
		t.Fatalf("open smux stream: %v", err)
	}
	assertPipeRoundTrip(t, stream, stream, []byte("vless-payload"))

	if err := stream.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}
	if err := smuxSess.Close(); err != nil {
		t.Fatalf("close smux: %v", err)
	}
	if err := kcpSess.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("close kcp: %v", err)
	}
	if err := clientConn.Close(); err != nil {
		t.Fatalf("close DTLS client: %v", err)
	}

	cancel()
	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("handleVLESSConnection did not stop after cancel")
	}
	select {
	case <-backendDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("TCP backend did not stop")
	}
}

func TestHandleVLESSConnectionClosesStreamWhenBackendDialFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener := startDTLSServer(t)
	defer func() { _ = listener.Close() }()

	serverDone := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() == nil && !isClosedNetworkError(err) {
				t.Errorf("DTLS accept: %v", err)
			}
			close(serverDone)
			return
		}
		defer func() { _ = conn.Close() }()

		dtlsConn, ok := conn.(*dtls.Conn)
		if !ok {
			t.Errorf("accepted %T, want *dtls.Conn", conn)
			close(serverDone)
			return
		}
		handleVLESSConnection(ctx, dtlsConn, "127.0.0.1:1", nil)
		close(serverDone)
	}()

	clientConn := dialDTLSClient(t, listener.Addr().String())
	kcpSess, err := tcputil.NewKCPOverDTLS(clientConn, false)
	if err != nil {
		t.Fatalf("client KCP session: %v", err)
	}
	smuxSess, err := smux.Client(kcpSess, tcputil.DefaultSmuxConfig())
	if err != nil {
		t.Fatalf("client smux session: %v", err)
	}

	stream, err := smuxSess.OpenStream()
	if err != nil {
		t.Fatalf("open smux stream: %v", err)
	}
	if _, err := stream.Write([]byte("dial-fail")); err != nil {
		t.Fatalf("write stream: %v", err)
	}

	buf := make([]byte, 1)
	if err := stream.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set stream deadline: %v", err)
	}
	_, err = stream.Read(buf)
	if err == nil {
		t.Fatalf("stream read succeeded after backend dial failure")
	}

	if err := smuxSess.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("close smux: %v", err)
	}
	if err := kcpSess.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("close kcp: %v", err)
	}
	if err := clientConn.Close(); err != nil {
		t.Fatalf("close DTLS client: %v", err)
	}

	cancel()
	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("handleVLESSConnection did not stop after backend dial failure and cancel")
	}
}

func TestRunServerValidatesConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := RunServer(ctx, ServerConfig{ListenAddr: "127.0.0.1:0"}); err == nil {
		t.Fatalf("RunServer succeeded without connect address")
	}

	err := RunServer(ctx, ServerConfig{ListenAddr: "bad listen address", ConnectAddr: "127.0.0.1:1", CheckBackend: false})
	if err == nil {
		t.Fatalf("RunServer succeeded with invalid listen address")
	}
}

func TestRunServerStartsAndStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- RunServer(ctx, ServerConfig{
			ListenAddr:   "127.0.0.1:0",
			ConnectAddr:  "127.0.0.1:1",
			CheckBackend: false,
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunServer returned before cancel: %v", err)
		}
		t.Fatalf("RunServer returned before cancel")
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunServer returned error after cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RunServer did not stop after context cancel")
	}
}

func TestLoadServerConfigDefaultsAndJSON(t *testing.T) {
	cfg, err := LoadServerConfig("")
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if cfg.ListenAddr != "0.0.0.0:56000" {
		t.Fatalf("default listen = %q", cfg.ListenAddr)
	}
	if !cfg.CheckBackend {
		t.Fatalf("default config should check backend")
	}

	path := filepath.Join(t.TempDir(), "server.json")
	data := []byte(`{
  "listen_addr": "127.0.0.1:56099",
  "connect_addr": "127.0.0.1:443",
  "vless_mode": true,
  "log_level": "debug",
  "status_api_addr": "127.0.0.1:18080",
  "service_name": "vkturn-test",
  "check_backend": false
}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err = LoadServerConfig(path)
	if err != nil {
		t.Fatalf("load JSON config: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:56099" || cfg.ConnectAddr != "127.0.0.1:443" || !cfg.VLESSMode {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.LogLevel != "debug" || cfg.StatusAPIAddr != "127.0.0.1:18080" || cfg.ServiceName != "vkturn-test" {
		t.Fatalf("metadata fields not loaded: %+v", cfg)
	}
	if cfg.CheckBackend {
		t.Fatalf("check_backend false was not preserved")
	}
	if cfg.BackendNetwork != "" {
		t.Fatalf("backend network should be inferred at validation time, got %q", cfg.BackendNetwork)
	}
}

func TestServerConfigNormalizeAndValidate(t *testing.T) {
	cfg := ServerConfig{ConnectAddr: "127.0.0.1:443", VLESSMode: true}
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("normalize vless config: %v", err)
	}
	if cfg.ListenAddr != "0.0.0.0:56000" {
		t.Fatalf("listen default = %q", cfg.ListenAddr)
	}
	if cfg.BackendNetwork != "tcp" {
		t.Fatalf("vless backend network = %q, want tcp", cfg.BackendNetwork)
	}

	cfg = DefaultServerConfig()
	cfg.ConnectAddr = "127.0.0.1:443"
	cfg.VLESSMode = true
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("normalize default-backed vless config: %v", err)
	}
	if cfg.BackendNetwork != "tcp" {
		t.Fatalf("default-backed vless backend network = %q, want tcp", cfg.BackendNetwork)
	}

	cfg = ServerConfig{ConnectAddr: "127.0.0.1:51820"}
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("normalize UDP config: %v", err)
	}
	if cfg.BackendNetwork != "udp" {
		t.Fatalf("udp backend network = %q", cfg.BackendNetwork)
	}

	cfg = ServerConfig{ListenAddr: "127.0.0.1:0", ConnectAddr: "127.0.0.1:1", BackendNetwork: "icmp"}
	if err := cfg.NormalizeAndValidate(); err == nil {
		t.Fatalf("unsupported backend network was accepted")
	}
}

func TestApplyServerConfigOverridesOnlyUsesExplicitFlags(t *testing.T) {
	cfg := ServerConfig{
		ListenAddr:    "127.0.0.1:56000",
		ConnectAddr:   "127.0.0.1:443",
		VLESSMode:     true,
		CheckBackend:  true,
		LogLevel:      "info",
		StatusAPIAddr: "127.0.0.1:18080",
		ServiceName:   "from-config",
	}

	merged := ApplyServerConfigOverrides(cfg, ServerConfigOverrides{
		ListenAddr:   "127.0.0.1:56001",
		ConnectAddr:  "127.0.0.1:8443",
		VLESSMode:    false,
		CheckBackend: false,
		LogLevel:     "debug",
		ServiceName:  "from-flag",
		Set: map[string]bool{
			"connect":       true,
			"check-backend": true,
			"service-name":  true,
		},
	})

	if merged.ListenAddr != cfg.ListenAddr {
		t.Fatalf("listen should not be overridden without explicit flag: %+v", merged)
	}
	if merged.ConnectAddr != "127.0.0.1:8443" {
		t.Fatalf("connect override not applied: %+v", merged)
	}
	if !merged.VLESSMode {
		t.Fatalf("vless should not be disabled without explicit flag")
	}
	if merged.CheckBackend {
		t.Fatalf("check backend override not applied")
	}
	if merged.LogLevel != cfg.LogLevel {
		t.Fatalf("log level should not be overridden without explicit flag: %+v", merged)
	}
	if merged.ServiceName != "from-flag" {
		t.Fatalf("service name override not applied: %+v", merged)
	}
}

func TestCheckBackendTCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen backend: %v", err)
	}
	defer func() { _ = listener.Close() }()

	accepted := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
		close(accepted)
	}()

	if err := CheckBackend(ctx, ServerConfig{ConnectAddr: listener.Addr().String(), BackendNetwork: "tcp"}); err != nil {
		t.Fatalf("tcp backend check failed: %v", err)
	}
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatalf("backend check did not dial listener")
	}

	err = CheckBackend(ctx, ServerConfig{ConnectAddr: "127.0.0.1:1", BackendNetwork: "tcp"})
	if err == nil {
		t.Fatalf("tcp backend check succeeded for closed port")
	}

	if err := CheckBackend(ctx, ServerConfig{ConnectAddr: "127.0.0.1:1", BackendNetwork: "udp"}); err != nil {
		t.Fatalf("udp backend check should be non-blocking: %v", err)
	}
}

func TestIsExpectedCloseError(t *testing.T) {
	if !isExpectedCloseError(nil) {
		t.Fatalf("nil close error should be expected")
	}
	if !isExpectedCloseError(net.ErrClosed) {
		t.Fatalf("net.ErrClosed should be expected")
	}
	if !isExpectedCloseError(io.ErrClosedPipe) {
		t.Fatalf("io.ErrClosedPipe should be expected")
	}
	if isExpectedCloseError(errors.New("unexpected close failure")) {
		t.Fatalf("unexpected error should not be treated as expected close")
	}
}

func assertPipeRoundTrip(t *testing.T, writer io.Writer, reader io.Reader, payload []byte) {
	t.Helper()

	writeErr := make(chan error, 1)
	go func() {
		_, err := writer.Write(payload)
		writeErr <- err
	}()

	got := make([]byte, len(payload))
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %q, want %q", string(got), string(payload))
	}

	select {
	case err := <-writeErr:
		if err != nil {
			t.Fatalf("write payload: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("write did not complete")
	}
}

func startUDPBackend(t *testing.T, ctx context.Context) (string, <-chan []byte, <-chan struct{}) {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen UDP backend: %v", err)
	}

	packets := make(chan []byte, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = conn.Close() }()
		buf := make([]byte, 1600)
		for {
			if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
				return
			}
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				if ctx.Err() != nil || isTimeout(err) || isClosedNetworkError(err) {
					if ctx.Err() != nil || isClosedNetworkError(err) {
						return
					}
					continue
				}
				t.Errorf("UDP backend read: %v", err)
				return
			}
			payload := append([]byte(nil), buf[:n]...)
			select {
			case packets <- payload:
			default:
			}
			if _, err := conn.WriteTo(append([]byte("echo:"), payload...), addr); err != nil {
				t.Errorf("UDP backend write: %v", err)
				return
			}
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })

	return conn.LocalAddr().String(), packets, done
}

func startTCPBackend(t *testing.T, ctx context.Context) (string, <-chan struct{}) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen TCP backend: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = listener.Close() }()
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() == nil && !isClosedNetworkError(err) {
				t.Errorf("TCP backend accept: %v", err)
			}
			return
		}
		defer func() { _ = conn.Close() }()
		if _, err := io.Copy(conn, conn); err != nil && !isClosedNetworkError(err) {
			t.Errorf("TCP backend echo: %v", err)
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })

	return listener.Addr().String(), done
}

func startDTLSServer(t *testing.T) net.Listener {
	t.Helper()

	certificate, err := selfsign.GenerateSelfSigned()
	if err != nil {
		t.Fatalf("generate certificate: %v", err)
	}
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve DTLS addr: %v", err)
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
		t.Fatalf("listen DTLS: %v", err)
	}
	return listener
}

func dialDTLSClient(t *testing.T, addr string) *dtls.Conn {
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

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isClosedNetworkError(err error) bool {
	return errors.Is(err, net.ErrClosed)
}
