package providerstate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cacggghp/vk-turn-proxy/internal/statusmodel"
)

func TestClassifyError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		wantState statusmodel.ComponentState
		wantCode  statusmodel.ErrorCode
		retryable bool
	}{
		{
			name:      "nil is ready",
			err:       nil,
			wantState: statusmodel.StateReady,
			wantCode:  statusmodel.CodeNone,
		},
		{
			name:      "VK captcha lockout",
			err:       errors.New("CAPTCHA_WAIT_REQUIRED: global lockout active"),
			wantState: statusmodel.StateCaptchaRequired,
			wantCode:  statusmodel.CodeProviderCaptcha,
		},
		{
			name:      "VK raw captcha error",
			err:       errors.New("VK API error: map[error_code:14 captcha_sid:sid]"),
			wantState: statusmodel.StateCaptchaRequired,
			wantCode:  statusmodel.CodeProviderCaptcha,
		},
		{
			name:      "VK rate limit",
			err:       errors.New("VK API error: map[error_code:29 error_msg:Rate limit reached]"),
			wantState: statusmodel.StateRateLimited,
			wantCode:  statusmodel.CodeProviderRateLimited,
			retryable: true,
		},
		{
			name:      "TURN stale nonce auth",
			err:       errors.New("TURN allocate: stale nonce"),
			wantState: statusmodel.StateAuthRequired,
			wantCode:  statusmodel.CodeProviderAuth,
		},
		{
			name:      "provider timeout",
			err:       context.DeadlineExceeded,
			wantState: statusmodel.StateProviderDown,
			wantCode:  statusmodel.CodeProviderDown,
			retryable: true,
		},
		{
			name:      "provider 503",
			err:       errors.New("GetConference: status=503 body=maintenance"),
			wantState: statusmodel.StateProviderDown,
			wantCode:  statusmodel.CodeProviderDown,
			retryable: true,
		},
		{
			name:      "unknown parser drift",
			err:       errors.New("missing turn_server in response"),
			wantState: statusmodel.StateUnknown,
			wantCode:  statusmodel.CodeUnknown,
			retryable: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ClassifyError(statusmodel.ProviderVK, tt.err)
			if got.Provider != statusmodel.ProviderVK {
				t.Fatalf("provider = %q", got.Provider)
			}
			if got.State != tt.wantState || got.Code != tt.wantCode || got.Retryable != tt.retryable {
				t.Fatalf("diagnosis = %#v, want state=%q code=%q retryable=%v", got, tt.wantState, tt.wantCode, tt.retryable)
			}
		})
	}
}

func TestClassifyErrorRedactsMessage(t *testing.T) {
	t.Parallel()

	diagnosis := ClassifyError(statusmodel.ProviderVK, errors.New("access_token=secret https://vk.com/call/join/private captcha_sid=sid"))
	for _, leak := range []string{"secret", "call/join/private"} {
		if strings.Contains(diagnosis.Message, leak) {
			t.Fatalf("diagnosis message leaks %q: %s", leak, diagnosis.Message)
		}
	}
}
