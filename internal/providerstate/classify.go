package providerstate

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/cacggghp/vk-turn-proxy/internal/statusmodel"
)

type Diagnosis struct {
	Provider  statusmodel.ProviderName   `json:"provider"`
	State     statusmodel.ComponentState `json:"state"`
	Code      statusmodel.ErrorCode      `json:"code"`
	Message   string                     `json:"message,omitempty"`
	Retryable bool                       `json:"retryable"`
}

func ClassifyError(provider statusmodel.ProviderName, err error) Diagnosis {
	if err == nil {
		return Diagnosis{Provider: providerOrNone(provider), State: statusmodel.StateReady, Code: statusmodel.CodeNone}
	}

	message := err.Error()
	lower := strings.ToLower(message)

	switch {
	case isCaptcha(lower):
		return diagnosis(provider, statusmodel.StateCaptchaRequired, statusmodel.CodeProviderCaptcha, message, false)
	case isRateLimited(lower):
		return diagnosis(provider, statusmodel.StateRateLimited, statusmodel.CodeProviderRateLimited, message, true)
	case isAuthRequired(lower):
		return diagnosis(provider, statusmodel.StateAuthRequired, statusmodel.CodeProviderAuth, message, false)
	case isProviderDown(err, lower):
		return diagnosis(provider, statusmodel.StateProviderDown, statusmodel.CodeProviderDown, message, true)
	default:
		return diagnosis(provider, statusmodel.StateUnknown, statusmodel.CodeUnknown, message, true)
	}
}

func diagnosis(provider statusmodel.ProviderName, state statusmodel.ComponentState, code statusmodel.ErrorCode, message string, retryable bool) Diagnosis {
	return Diagnosis{
		Provider:  providerOrNone(provider),
		State:     state,
		Code:      code,
		Message:   statusmodel.Redact(message),
		Retryable: retryable,
	}
}

func providerOrNone(provider statusmodel.ProviderName) statusmodel.ProviderName {
	if provider == "" {
		return statusmodel.ProviderNone
	}
	return provider
}

func isCaptcha(lower string) bool {
	return containsAny(lower,
		"captcha_wait_required",
		"fatal_captcha",
		"captcha needed",
		"captcha_required",
		"captcha required",
		"error_code:14",
		"error_code: 14",
		"error_code=14",
		"captcha_sid",
	)
}

func isRateLimited(lower string) bool {
	return containsAny(lower,
		"rate limit",
		"rate_limited",
		"too many requests",
		"error_code:29",
		"error_code: 29",
		"error_code=29",
		"status=429",
		"status: 429",
		" 429 ",
	)
}

func isAuthRequired(lower string) bool {
	return containsAny(lower,
		"401",
		"403",
		"unauthorized",
		"forbidden",
		"authentication",
		"auth required",
		"invalid credential",
		"invalid credentials",
		"invalid access_token",
		"stale nonce",
		"access denied",
	)
}

func isProviderDown(err error, lower string) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return containsAny(lower,
		"context deadline exceeded",
		"i/o timeout",
		"timeout awaiting response",
		"temporary failure",
		"no such host",
		"connection refused",
		"connection reset",
		"server misbehaving",
		"status=500",
		"status=502",
		"status=503",
		"status=504",
		"status: 500",
		"status: 502",
		"status: 503",
		"status: 504",
		" 500 ",
		" 502 ",
		" 503 ",
		" 504 ",
	)
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
