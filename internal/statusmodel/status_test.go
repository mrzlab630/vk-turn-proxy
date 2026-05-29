package statusmodel

import (
	"strings"
	"testing"
	"time"
)

func TestNewSnapshotDefaultsVLESSSidecarState(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	snapshot := NewSnapshot(BuilderInput{
		ServiceName: "vkturn-test",
		Now:         now,
		StartedAt:   now.Add(-time.Minute),
		ListenAddr:  "127.0.0.1:56000",
		BackendAddr: "127.0.0.1:10001",
		VLESSMode:   true,
	})

	if snapshot.SchemaVersion != "status.v1" {
		t.Fatalf("schema = %q", snapshot.SchemaVersion)
	}
	if snapshot.Overall != StateReady {
		t.Fatalf("overall = %q, want ready", snapshot.Overall)
	}
	if snapshot.Mode != "vless" {
		t.Fatalf("mode = %q, want vless", snapshot.Mode)
	}
	if snapshot.Listener.State != StateListening || snapshot.Listener.Network != "udp" {
		t.Fatalf("listener = %#v", snapshot.Listener)
	}
	if snapshot.Backend.Network != "tcp" || snapshot.Backend.Address != "127.0.0.1:10001" {
		t.Fatalf("backend = %#v", snapshot.Backend)
	}
	if snapshot.Provider.Name != ProviderNone || snapshot.Provider.State != StateDisabled {
		t.Fatalf("provider = %#v", snapshot.Provider)
	}
	if snapshot.Metrics.StartedAtUnix != now.Add(-time.Minute).Unix() {
		t.Fatalf("started_at_unix = %d", snapshot.Metrics.StartedAtUnix)
	}
}

func TestNewSnapshotMapsErrorCodesToComponentStates(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	snapshot := NewSnapshot(BuilderInput{
		Now:         now,
		ListenAddr:  "127.0.0.1:56000",
		BackendAddr: "127.0.0.1:10001",
		VLESSMode:   true,
		LastError:   NewError(CodeBackendDial, "backend failed", now),
	})

	if snapshot.Overall != StateDegraded {
		t.Fatalf("overall = %q, want degraded", snapshot.Overall)
	}
	if snapshot.Backend.State != StateError {
		t.Fatalf("backend state = %q, want error", snapshot.Backend.State)
	}
	if snapshot.LastError == nil || snapshot.LastError.Code != CodeBackendDial {
		t.Fatalf("last error = %#v", snapshot.LastError)
	}
}

func TestNewSnapshotUsesExplicitProviderState(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	snapshot := NewSnapshot(BuilderInput{
		Now:           now,
		Provider:      ProviderVK,
		ProviderState: StateCaptchaRequired,
	})

	if snapshot.Provider.Name != ProviderVK || snapshot.Provider.State != StateCaptchaRequired {
		t.Fatalf("provider = %#v", snapshot.Provider)
	}
}

func TestNewSnapshotMarksFatalListenFailure(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	snapshot := NewSnapshot(BuilderInput{
		Now:         now,
		ListenAddr:  "0.0.0.0:56000",
		BackendAddr: "127.0.0.1:10001",
		LastError:   NewError(CodeListenFailed, "listen failed", now),
	})

	if snapshot.Overall != StateError {
		t.Fatalf("overall = %q, want error", snapshot.Overall)
	}
	if snapshot.Listener.State != StateError {
		t.Fatalf("listener state = %q, want error", snapshot.Listener.State)
	}
}

func TestRedactRemovesKnownSecretShapes(t *testing.T) {
	input := strings.Join([]string{
		"access_token=abc",
		"password=secret",
		"credential=turn-pass",
		"session_token=session-secret",
		"session_key=session-key",
		"success_token=success-secret",
		"captcha_sid=captcha-secret",
		"captcha_key=captcha-key",
		"map[captcha_sid:map-secret credential:map-credential]",
		`{"session_token":"json-session","success_token":"json-success"}`,
		"vk1.a.b",
		"https://vk.com/call/join/foo",
		"https://telemost.yandex.ru/j/bar",
	}, " ")
	got := Redact(input)
	for _, leak := range []string{"abc", "secret", "turn-pass", "session-key", "success-secret", "captcha-key", "map-secret", "map-credential", "json-session", "json-success", "vk1.a.b", "call/join/foo", "telemost.yandex.ru/j/bar"} {
		if strings.Contains(got, leak) {
			t.Fatalf("redacted output leaks %q: %s", leak, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("redacted output missing marker: %s", got)
	}
}

func TestIsLocalAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080"} {
		if !IsLocalAddress(address) {
			t.Fatalf("%s should be local", address)
		}
	}
	for _, address := range []string{"0.0.0.0:8080", "192.168.1.10:8080", "example.com:8080"} {
		if IsLocalAddress(address) {
			t.Fatalf("%s should not be local", address)
		}
	}
}
