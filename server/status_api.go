package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cacggghp/vk-turn-proxy/internal/statusmodel"
)

type RuntimeStatus struct {
	cfg       ServerConfig
	startedAt time.Time
	events    []statusmodel.Event
	logs      []string
	provider  statusmodel.ProviderStatus
	lastError atomic.Pointer[statusmodel.StatusError]
	nextEvent atomic.Uint64

	acceptedConnections atomic.Uint64
	activeConnections   atomic.Int64
	backendDialFailures atomic.Uint64
	handshakeFailures   atomic.Uint64

	mu sync.Mutex
}

func NewRuntimeStatus(cfg ServerConfig) *RuntimeStatus {
	now := time.Now().UTC()
	runtime := &RuntimeStatus{
		cfg:       cfg,
		startedAt: now,
		provider:  statusmodel.ProviderStatus{Name: statusmodel.ProviderNone, State: statusmodel.StateDisabled},
	}
	runtime.RecordEvent("server", statusmodel.StateInitializing, statusmodel.CodeNone, "server status initialized")
	return runtime
}

func (r *RuntimeStatus) Snapshot() statusmodel.Snapshot {
	var lastError *statusmodel.StatusError
	if ptr := r.lastError.Load(); ptr != nil {
		copy := *ptr
		lastError = &copy
	}
	provider := r.ProviderStatus()
	return statusmodel.NewSnapshot(statusmodel.BuilderInput{
		ServiceName:   r.cfg.ServiceName,
		Now:           time.Now().UTC(),
		StartedAt:     r.startedAt,
		ListenAddr:    r.cfg.ListenAddr,
		BackendAddr:   r.cfg.ConnectAddr,
		BackendNet:    r.cfg.BackendNetwork,
		VLESSMode:     r.cfg.VLESSMode,
		Provider:      provider.Name,
		ProviderState: provider.State,
		LastError:     lastError,
		Metrics: statusmodel.Metrics{
			AcceptedConnections: r.acceptedConnections.Load(),
			ActiveConnections:   r.activeConnections.Load(),
			BackendDialFailures: r.backendDialFailures.Load(),
			HandshakeFailures:   r.handshakeFailures.Load(),
		},
	})
}

func (r *RuntimeStatus) RecordProviderState(provider statusmodel.ProviderName, state statusmodel.ComponentState, code statusmodel.ErrorCode, message string) {
	if provider == "" {
		provider = statusmodel.ProviderNone
	}
	if state == "" {
		state = statusmodel.StateUnknown
	}
	r.mu.Lock()
	r.provider = statusmodel.ProviderStatus{Name: provider, State: state}
	r.mu.Unlock()
	r.RecordEvent("provider", state, code, message)
}

func (r *RuntimeStatus) ProviderStatus() statusmodel.ProviderStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.provider
}

func (r *RuntimeStatus) RecordAcceptedConnection() {
	r.acceptedConnections.Add(1)
	r.activeConnections.Add(1)
	r.RecordEvent("listener", statusmodel.StateConnected, statusmodel.CodeNone, "accepted DTLS connection")
}

func (r *RuntimeStatus) RecordConnectionClosed() {
	r.activeConnections.Add(-1)
}

func (r *RuntimeStatus) RecordError(component string, code statusmodel.ErrorCode, message string) {
	err := statusmodel.NewError(code, message, time.Now().UTC())
	r.lastError.Store(err)
	switch code {
	case statusmodel.CodeBackendDial, statusmodel.CodeBackendDown:
		r.backendDialFailures.Add(1)
	case statusmodel.CodeDTLSHandshake:
		r.handshakeFailures.Add(1)
	}
	r.RecordEvent(component, statusmodel.StateError, code, message)
}

func (r *RuntimeStatus) RecordEvent(component string, state statusmodel.ComponentState, code statusmodel.ErrorCode, message string) {
	if code == "" {
		code = statusmodel.CodeNone
	}
	event := statusmodel.Event{
		ID:        r.nextEvent.Add(1),
		At:        time.Now().UTC(),
		Component: component,
		State:     state,
		Code:      code,
		Message:   statusmodel.Redact(message),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	if len(r.events) > 200 {
		r.events = r.events[len(r.events)-200:]
	}
}

func (r *RuntimeStatus) AppendLog(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, statusmodel.Redact(line))
	if len(r.logs) > 500 {
		r.logs = r.logs[len(r.logs)-500:]
	}
}

func (r *RuntimeStatus) Events() []statusmodel.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]statusmodel.Event(nil), r.events...)
}

func (r *RuntimeStatus) Logs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.logs...)
}

func startStatusAPI(ctx context.Context, addr string, runtime *RuntimeStatus) (net.Addr, error) {
	if strings.TrimSpace(addr) == "" {
		return nil, nil
	}
	if !statusmodel.IsLocalAddress(addr) {
		return nil, fmt.Errorf("status API must bind to a loopback address: %s", addr)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen status API: %w", err)
	}
	mux := http.NewServeMux()
	registerStatusHandlers(mux, runtime)
	server := &http.Server{Handler: loopbackOnly(mux)}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			runtime.RecordError("status_api", statusmodel.CodeListenFailed, err.Error())
		}
	}()
	return listener.Addr(), nil
}

func registerStatusHandlers(mux *http.ServeMux, runtime *RuntimeStatus) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		snapshot := runtime.Snapshot()
		writeJSON(w, map[string]string{"status": string(snapshot.Overall), "schema_version": snapshot.SchemaVersion})
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, runtime.Snapshot())
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, map[string]any{"events": runtime.Events()})
	})
	mux.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, map[string]any{"logs": runtime.Logs()})
	})
	mux.HandleFunc("/restart-sidecar", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		runtime.RecordEvent("server", statusmodel.StateReady, statusmodel.CodeNone, "restart requested through status API")
		writeJSON(w, map[string]string{"status": "accepted", "action": "restart-sidecar"})
	})
}

func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.Error(w, "status API accepts loopback clients only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
