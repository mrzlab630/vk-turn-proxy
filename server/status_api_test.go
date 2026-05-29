package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cacggghp/vk-turn-proxy/internal/statusmodel"
)

func TestStatusAPIExposesHealthStatusEventsAndLogs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime := NewRuntimeStatus(ServerConfig{
		ListenAddr:     "127.0.0.1:56000",
		ConnectAddr:    "127.0.0.1:10001",
		VLESSMode:      true,
		BackendNetwork: "tcp",
		ServiceName:    "vkturn-test",
	})
	runtime.AppendLog("access_token=secret https://vk.com/call/join/private")
	runtime.RecordError("backend", statusmodel.CodeBackendDial, "password=secret backend failed")

	addr, err := startStatusAPI(ctx, "127.0.0.1:0", runtime)
	if err != nil {
		t.Fatalf("startStatusAPI: %v", err)
	}
	baseURL := "http://" + addr.String()

	var health map[string]string
	getJSON(t, baseURL+"/health", &health)
	if health["schema_version"] != "status.v1" {
		t.Fatalf("health = %#v", health)
	}

	var snapshot statusmodel.Snapshot
	getJSON(t, baseURL+"/status", &snapshot)
	if snapshot.ServiceName != "vkturn-test" {
		t.Fatalf("service name = %q", snapshot.ServiceName)
	}
	if snapshot.Backend.State != statusmodel.StateError {
		t.Fatalf("backend state = %q, want error", snapshot.Backend.State)
	}
	if snapshot.LastError == nil || strings.Contains(snapshot.LastError.Message, "secret") {
		t.Fatalf("last error not redacted: %#v", snapshot.LastError)
	}

	var logs struct {
		Logs []string `json:"logs"`
	}
	getJSON(t, baseURL+"/logs", &logs)
	if len(logs.Logs) != 1 || strings.Contains(logs.Logs[0], "secret") || strings.Contains(logs.Logs[0], "call/join/private") {
		t.Fatalf("logs not redacted: %#v", logs.Logs)
	}

	var events struct {
		Events []statusmodel.Event `json:"events"`
	}
	getJSON(t, baseURL+"/events", &events)
	if len(events.Events) == 0 {
		t.Fatalf("events empty")
	}

	resp, err := http.Post(baseURL+"/restart-sidecar", "application/json", nil)
	if err != nil {
		t.Fatalf("restart-sidecar: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restart status = %d", resp.StatusCode)
	}
}

func TestStatusAPIExposesProviderStateTransitions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime := NewRuntimeStatus(ServerConfig{ServiceName: "vkturn-provider-test"})
	runtime.RecordProviderState(
		statusmodel.ProviderVK,
		statusmodel.StateCaptchaRequired,
		statusmodel.CodeProviderCaptcha,
		"captcha_sid=secret captcha required",
	)

	addr, err := startStatusAPI(ctx, "127.0.0.1:0", runtime)
	if err != nil {
		t.Fatalf("startStatusAPI: %v", err)
	}
	baseURL := "http://" + addr.String()

	var snapshot statusmodel.Snapshot
	getJSON(t, baseURL+"/status", &snapshot)
	if snapshot.Provider.Name != statusmodel.ProviderVK || snapshot.Provider.State != statusmodel.StateCaptchaRequired {
		t.Fatalf("provider = %#v", snapshot.Provider)
	}

	var events struct {
		Events []statusmodel.Event `json:"events"`
	}
	getJSON(t, baseURL+"/events", &events)
	if len(events.Events) == 0 {
		t.Fatalf("events empty")
	}
	last := events.Events[len(events.Events)-1]
	if last.Component != "provider" || last.State != statusmodel.StateCaptchaRequired || last.Code != statusmodel.CodeProviderCaptcha {
		t.Fatalf("last provider event = %#v", last)
	}
	if strings.Contains(last.Message, "secret") {
		t.Fatalf("provider event leaked secret: %q", last.Message)
	}
}

func TestStatusAPIRejectsNonLoopbackBind(t *testing.T) {
	_, err := startStatusAPI(context.Background(), "0.0.0.0:0", NewRuntimeStatus(ServerConfig{}))
	if err == nil {
		t.Fatalf("startStatusAPI accepted non-loopback bind")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("error = %v, want loopback context", err)
	}
}

func TestRunServerStartsStatusAPI(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunServer(ctx, ServerConfig{
			ListenAddr:     "127.0.0.1:0",
			ConnectAddr:    "127.0.0.1:1",
			VLESSMode:      true,
			BackendNetwork: "tcp",
			StatusAPIAddr:  "127.0.0.1:0",
			CheckBackend:   false,
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunServer returned early: %v", err)
		}
		fatalfNow(t, "RunServer returned early")
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunServer returned after cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RunServer did not stop")
	}
}

func getJSON(t *testing.T, url string, target any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func fatalfNow(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatalf(format, args...)
}
