package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptchaSolveModeForAttempt(t *testing.T) {
	t.Parallel()

	t.Run("default flow", func(t *testing.T) {
		t.Parallel()

		mode, ok := captchaSolveModeForAttempt(0, false, true)
		if !ok || mode != captchaSolveModeAuto {
			t.Fatalf("expected first attempt to use auto captcha, got mode=%v ok=%v", mode, ok)
		}

		mode, ok = captchaSolveModeForAttempt(1, false, true)
		if !ok || mode != captchaSolveModeSliderPOC {
			t.Fatalf("expected second attempt to use slider POC, got mode=%v ok=%v", mode, ok)
		}

		mode, ok = captchaSolveModeForAttempt(2, false, true)
		if !ok || mode != captchaSolveModeManual {
			t.Fatalf("expected third attempt to use manual captcha, got mode=%v ok=%v", mode, ok)
		}

		if _, ok = captchaSolveModeForAttempt(3, false, true); ok {
			t.Fatal("expected no fourth captcha attempt in default flow")
		}
	})

	t.Run("manual only flow", func(t *testing.T) {
		t.Parallel()

		mode, ok := captchaSolveModeForAttempt(0, true, true)
		if !ok || mode != captchaSolveModeManual {
			t.Fatalf("expected manual mode on first attempt, got mode=%v ok=%v", mode, ok)
		}

		if _, ok = captchaSolveModeForAttempt(1, true, true); ok {
			t.Fatal("expected only one manual captcha attempt when manual mode is forced")
		}
	})

	t.Run("flow without slider poc", func(t *testing.T) {
		t.Parallel()

		mode, ok := captchaSolveModeForAttempt(0, false, false)
		if !ok || mode != captchaSolveModeAuto {
			t.Fatalf("expected auto captcha first, got mode=%v ok=%v", mode, ok)
		}

		mode, ok = captchaSolveModeForAttempt(1, false, false)
		if !ok || mode != captchaSolveModeManual {
			t.Fatalf("expected manual captcha second when slider POC is disabled, got mode=%v ok=%v", mode, ok)
		}

		if _, ok = captchaSolveModeForAttempt(2, false, false); ok {
			t.Fatal("expected only two attempts when slider POC is disabled")
		}
	})
}

func TestParseVkCaptchaError(t *testing.T) {
	t.Parallel()

	t.Run("redirect captcha without image", func(t *testing.T) {
		t.Parallel()

		captchaErr := ParseVkCaptchaError(readJSONFixture(t, "captcha_redirect.json"))
		if captchaErr == nil {
			t.Fatal("expected captcha error to parse")
		}
		if !captchaErr.IsCaptchaError() {
			t.Fatal("expected redirect/session-token captcha to be solvable")
		}
		if captchaErr.SessionToken != "session-1" {
			t.Fatalf("expected session token session-1, got %q", captchaErr.SessionToken)
		}
		if captchaErr.CaptchaImg != "" {
			t.Fatalf("expected empty captcha image, got %q", captchaErr.CaptchaImg)
		}
	})

	t.Run("legacy image captcha is not auto solvable without redirect", func(t *testing.T) {
		t.Parallel()

		captchaErr := ParseVkCaptchaError(readJSONFixture(t, "captcha_image.json"))
		if captchaErr == nil {
			t.Fatal("expected legacy captcha error to parse")
		}
		if !captchaErr.IsCaptchaError() {
			t.Fatal("expected legacy image-only captcha to be handled by manual image fallback")
		}
		if captchaErr.RedirectURI != "" || captchaErr.SessionToken != "" {
			t.Fatalf("expected no redirect/session fields, got redirect=%q session=%q", captchaErr.RedirectURI, captchaErr.SessionToken)
		}
	})
}

func TestParseVKTurnServer(t *testing.T) {
	t.Parallel()

	t.Run("valid TURN response strips scheme and query", func(t *testing.T) {
		t.Parallel()

		user, pass, addr, err := parseVKTurnServer(readJSONFixture(t, "turn_success.json"))
		if err != nil {
			t.Fatalf("parseVKTurnServer returned error: %v", err)
		}
		if user != "turn-user" || pass != "turn-pass" || addr != "127.0.0.1:3478" {
			t.Fatalf("parsed user=%q pass=%q addr=%q", user, pass, addr)
		}
	})

	t.Run("valid TURNS response strips scheme", func(t *testing.T) {
		t.Parallel()

		_, _, addr, err := parseVKTurnServer(readJSONFixture(t, "turn_success_tls.json"))
		if err != nil {
			t.Fatalf("parseVKTurnServer returned error: %v", err)
		}
		if addr != "example.test:5349" {
			t.Fatalf("addr = %q", addr)
		}
	})

	t.Run("missing fields are explicit", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name    string
			fixture string
			want    string
		}{
			{name: "missing turn server", fixture: "turn_missing_server.json", want: "missing turn_server"},
			{name: "missing username", fixture: "turn_missing_username.json", want: "missing username"},
			{name: "missing credential", fixture: "turn_missing_credential.json", want: "missing credential"},
			{name: "empty urls", fixture: "turn_empty_urls.json", want: "missing or empty urls"},
			{name: "non string url", fixture: "turn_non_string_url.json", want: "turn server url is not a string"},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				_, _, _, err := parseVKTurnServer(readJSONFixture(t, tt.fixture))
				if err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("error = %v, want %q", err, tt.want)
				}
			})
		}
	})
}

func readJSONFixture(t *testing.T, name string) map[string]interface{} {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "vk", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var value map[string]interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return value
}

func TestIsAuthErrorUsesProviderClassification(t *testing.T) {
	t.Parallel()

	if !isAuthError(errors.New("TURN allocate: stale nonce")) {
		t.Fatalf("stale nonce should be classified as auth error")
	}
	if isAuthError(errors.New("CAPTCHA_WAIT_REQUIRED")) {
		t.Fatalf("captcha wait should not be classified as auth error")
	}
}

func TestVKCredentialsFromEnv(t *testing.T) {
	t.Setenv("VKTURN_VK_CREDENTIALS", "111:aaa, bad-entry, 222:bbb, :missing-id, 333:")

	credentials := vkCredentialsFromEnv()
	if len(credentials) != 2 {
		t.Fatalf("credentials length = %d, want 2: %#v", len(credentials), credentials)
	}
	if credentials[0] != (VKCredentials{ClientID: "111", ClientSecret: "aaa"}) {
		t.Fatalf("first credential = %#v", credentials[0])
	}
	if credentials[1] != (VKCredentials{ClientID: "222", ClientSecret: "bbb"}) {
		t.Fatalf("second credential = %#v", credentials[1])
	}
}
