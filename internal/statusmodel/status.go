package statusmodel

import (
	"net"
	"regexp"
	"strings"
	"time"
)

type ComponentState string

const (
	StateUnknown         ComponentState = "unknown"
	StateDisabled        ComponentState = "disabled"
	StateInitializing    ComponentState = "initializing"
	StateReady           ComponentState = "ready"
	StateListening       ComponentState = "listening"
	StateConnected       ComponentState = "connected"
	StateDisconnected    ComponentState = "disconnected"
	StateDegraded        ComponentState = "degraded"
	StateError           ComponentState = "error"
	StateAuthRequired    ComponentState = "auth_required"
	StateCaptchaRequired ComponentState = "captcha_required"
	StateRateLimited     ComponentState = "rate_limited"
	StateProviderDown    ComponentState = "provider_down"
)

type ErrorCode string

const (
	CodeNone                ErrorCode = "none"
	CodeConfigInvalid       ErrorCode = "config_invalid"
	CodeBackendDown         ErrorCode = "backend_down"
	CodeListenFailed        ErrorCode = "listen_failed"
	CodeProviderAuth        ErrorCode = "provider_auth_required"
	CodeProviderCaptcha     ErrorCode = "provider_captcha_required"
	CodeProviderRateLimited ErrorCode = "provider_rate_limited"
	CodeProviderDown        ErrorCode = "provider_down"
	CodeTurnAllocation      ErrorCode = "turn_allocation_failed"
	CodeDTLSHandshake       ErrorCode = "dtls_handshake_failed"
	CodeKCPSession          ErrorCode = "kcp_session_failed"
	CodeSmuxSession         ErrorCode = "smux_session_failed"
	CodeBackendDial         ErrorCode = "backend_dial_failed"
	CodeUnknown             ErrorCode = "unknown"
)

type ProviderName string

const (
	ProviderNone   ProviderName = "none"
	ProviderVK     ProviderName = "vk"
	ProviderYandex ProviderName = "yandex"
	ProviderMAX    ProviderName = "max"
)

type Snapshot struct {
	SchemaVersion string          `json:"schema_version"`
	ServiceName   string          `json:"service_name"`
	Mode          string          `json:"mode"`
	GeneratedAt   time.Time       `json:"generated_at"`
	Overall       ComponentState  `json:"overall"`
	Provider      ProviderStatus  `json:"provider"`
	TURN          ComponentStatus `json:"turn"`
	DTLS          ComponentStatus `json:"dtls"`
	KCP           ComponentStatus `json:"kcp"`
	Smux          SmuxStatus      `json:"smux"`
	Listener      ListenerStatus  `json:"listener"`
	Backend       BackendStatus   `json:"backend"`
	LastError     *StatusError    `json:"last_error,omitempty"`
	Metrics       Metrics         `json:"metrics"`
	Warnings      []string        `json:"warnings,omitempty"`
}

type ProviderStatus struct {
	Name  ProviderName   `json:"name"`
	State ComponentState `json:"state"`
}

type ComponentStatus struct {
	State ComponentState `json:"state"`
}

type SmuxStatus struct {
	State          ComponentState `json:"state"`
	ActiveSessions int            `json:"active_sessions"`
	ActiveStreams  int            `json:"active_streams"`
}

type ListenerStatus struct {
	State   ComponentState `json:"state"`
	Address string         `json:"address"`
	Network string         `json:"network"`
}

type BackendStatus struct {
	State   ComponentState `json:"state"`
	Address string         `json:"address"`
	Network string         `json:"network"`
}

type StatusError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

type Metrics struct {
	AcceptedConnections uint64 `json:"accepted_connections"`
	ActiveConnections   int64  `json:"active_connections"`
	BackendDialFailures uint64 `json:"backend_dial_failures"`
	HandshakeFailures   uint64 `json:"handshake_failures"`
	StartedAtUnix       int64  `json:"started_at_unix"`
}

type Event struct {
	ID        uint64         `json:"id"`
	At        time.Time      `json:"at"`
	Component string         `json:"component"`
	State     ComponentState `json:"state"`
	Code      ErrorCode      `json:"code,omitempty"`
	Message   string         `json:"message"`
}

type BuilderInput struct {
	ServiceName   string
	Mode          string
	Now           time.Time
	StartedAt     time.Time
	ListenAddr    string
	BackendAddr   string
	BackendNet    string
	VLESSMode     bool
	Provider      ProviderName
	ProviderState ComponentState
	LastError     *StatusError
	Metrics       Metrics
	Warnings      []string
}

func NewSnapshot(input BuilderInput) Snapshot {
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	}
	if input.StartedAt.IsZero() {
		input.StartedAt = input.Now
	}
	if input.ServiceName == "" {
		input.ServiceName = "vkturn-server"
	}
	if input.Mode == "" {
		if input.VLESSMode {
			input.Mode = "vless"
		} else {
			input.Mode = "udp"
		}
	}
	if input.BackendNet == "" {
		if input.VLESSMode {
			input.BackendNet = "tcp"
		} else {
			input.BackendNet = "udp"
		}
	}
	if input.Provider == "" {
		input.Provider = ProviderNone
	}
	if input.ProviderState == "" {
		if input.Provider == ProviderNone {
			input.ProviderState = StateDisabled
		} else {
			input.ProviderState = StateUnknown
		}
	}
	input.Metrics.StartedAtUnix = input.StartedAt.Unix()

	overall := StateReady
	if input.LastError != nil && input.LastError.Code != CodeNone {
		overall = StateDegraded
		if isFatalCode(input.LastError.Code) {
			overall = StateError
		}
	}

	snapshot := Snapshot{
		SchemaVersion: "status.v1",
		ServiceName:   input.ServiceName,
		Mode:          input.Mode,
		GeneratedAt:   input.Now,
		Overall:       overall,
		Provider: ProviderStatus{
			Name:  input.Provider,
			State: input.ProviderState,
		},
		TURN: ComponentStatus{State: StateDisabled},
		DTLS: ComponentStatus{State: StateReady},
		KCP:  ComponentStatus{State: StateReady},
		Smux: SmuxStatus{
			State: StateReady,
		},
		Listener: ListenerStatus{
			State:   StateListening,
			Address: input.ListenAddr,
			Network: "udp",
		},
		Backend: BackendStatus{
			State:   StateReady,
			Address: input.BackendAddr,
			Network: input.BackendNet,
		},
		LastError: input.LastError,
		Metrics:   input.Metrics,
		Warnings:  input.Warnings,
	}

	if input.LastError != nil {
		snapshot.LastError = &StatusError{
			Code:    input.LastError.Code,
			Message: Redact(input.LastError.Message),
			At:      input.LastError.At,
		}
		applyErrorState(&snapshot, input.LastError.Code)
	}

	return snapshot
}

func Redact(value string) string {
	redacted := value
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?i)(access_token|token|password|passwd|pwd|secret|credential|turn_password|turn_username|username|session_token|session_key|success_token|captcha_sid|captcha_key)=([^\s&]+)`),
		regexp.MustCompile(`(?i)("(?:access_token|token|password|passwd|pwd|secret|credential|turn_password|turn_username|username|session_token|session_key|success_token|captcha_sid|captcha_key)"\s*:\s*")([^"]+)`),
		regexp.MustCompile(`(?i)((?:access_token|token|password|passwd|pwd|secret|credential|turn_password|turn_username|username|session_token|session_key|success_token|captcha_sid|captcha_key)\s*:\s*)([^\s,}\]]+)`),
		regexp.MustCompile(`(?i)(vk1\.[A-Za-z0-9._-]+)`),
		regexp.MustCompile(`(?i)(https://vk\.com/call/join/[^\s]+)`),
		regexp.MustCompile(`(?i)(https://telemost\.yandex\.ru/j/[^\s]+)`),
	} {
		redacted = re.ReplaceAllStringFunc(redacted, func(match string) string {
			if strings.Contains(match, "=") {
				parts := strings.SplitN(match, "=", 2)
				return parts[0] + "=[REDACTED]"
			}
			if strings.Contains(match, ":") {
				parts := strings.SplitN(match, ":", 2)
				if strings.HasPrefix(strings.TrimSpace(parts[1]), "\"") {
					return parts[0] + ":\"[REDACTED]"
				}
				return parts[0] + ":[REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return redacted
}

func NewError(code ErrorCode, message string, at time.Time) *StatusError {
	if code == "" {
		code = CodeUnknown
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return &StatusError{Code: code, Message: Redact(message), At: at}
}

func IsLocalAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func applyErrorState(snapshot *Snapshot, code ErrorCode) {
	switch code {
	case CodeBackendDown, CodeBackendDial:
		snapshot.Backend.State = StateError
	case CodeListenFailed:
		snapshot.Listener.State = StateError
	case CodeDTLSHandshake:
		snapshot.DTLS.State = StateError
	case CodeKCPSession:
		snapshot.KCP.State = StateError
	case CodeSmuxSession:
		snapshot.Smux.State = StateError
	case CodeProviderAuth:
		snapshot.Provider.State = StateAuthRequired
	case CodeProviderCaptcha:
		snapshot.Provider.State = StateCaptchaRequired
	case CodeProviderRateLimited:
		snapshot.Provider.State = StateRateLimited
	case CodeProviderDown:
		snapshot.Provider.State = StateProviderDown
	case CodeTurnAllocation:
		snapshot.TURN.State = StateError
	}
}

func isFatalCode(code ErrorCode) bool {
	switch code {
	case CodeConfigInvalid, CodeListenFailed:
		return true
	default:
		return false
	}
}
