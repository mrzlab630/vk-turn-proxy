package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cacggghp/vk-turn-proxy/internal/statusmodel"
)

func TestProviderCredentialErrorClassifiesAndUnwraps(t *testing.T) {
	t.Parallel()

	cause := errors.New("VK API error: map[error_code:14 captcha_sid=sid access_token=secret]")
	err := newProviderCredentialError(statusmodel.ProviderVK, cause)
	if !errors.Is(err, cause) {
		t.Fatalf("provider credential error does not unwrap original cause")
	}

	diagnosis, ok := providerCredentialDiagnosis(err)
	if !ok {
		t.Fatalf("providerCredentialDiagnosis did not detect typed error")
	}
	if diagnosis.Provider != statusmodel.ProviderVK || diagnosis.State != statusmodel.StateCaptchaRequired || diagnosis.Code != statusmodel.CodeProviderCaptcha {
		t.Fatalf("diagnosis = %#v", diagnosis)
	}
	if strings.Contains(diagnosis.Message, "secret") || strings.Contains(diagnosis.Message, "captcha_sid=sid") {
		t.Fatalf("diagnosis leaked secret material: %s", diagnosis.Message)
	}
}

func TestCreateTURNRelaySessionWrapsCredentialErrors(t *testing.T) {
	t.Parallel()

	credentialErr := errors.New("GetConference: status=503 body=maintenance")
	tp := &turnParams{
		link:     "room-1",
		provider: statusmodel.ProviderYandex,
		getCreds: func(context.Context, string, int) (string, string, string, error) {
			return "", "", "", credentialErr
		},
	}

	_, err := createTURNRelaySession(context.Background(), tp, nil, 7)
	if err == nil {
		t.Fatalf("createTURNRelaySession succeeded")
	}
	if !errors.Is(err, credentialErr) {
		t.Fatalf("wrapped error does not expose credential cause: %v", err)
	}
	diagnosis, ok := providerCredentialDiagnosis(err)
	if !ok {
		t.Fatalf("expected provider credential diagnosis from %v", err)
	}
	if diagnosis.Provider != statusmodel.ProviderYandex || diagnosis.State != statusmodel.StateProviderDown || diagnosis.Code != statusmodel.CodeProviderDown || !diagnosis.Retryable {
		t.Fatalf("diagnosis = %#v", diagnosis)
	}
}
