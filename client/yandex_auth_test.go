package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseYandexConferenceResponse(t *testing.T) {
	t.Parallel()

	data, err := parseYandexConferenceResponse(strings.NewReader(readYandexFixture(t, "conference_success.json")))
	if err != nil {
		t.Fatalf("parseYandexConferenceResponse returned error: %v", err)
	}
	if data.ParticipantID != "peer-1" || data.RoomID != "room-1" || data.Credentials != "conf-credentials" || data.Wss != "wss://media.example.test/ws" {
		t.Fatalf("conference data = %#v", data)
	}

	_, err = parseYandexConferenceResponse(strings.NewReader(readYandexFixture(t, "conference_missing_wss.json")))
	if err == nil || !strings.Contains(err.Error(), "media_server_url") {
		t.Fatalf("error = %v, want media_server_url", err)
	}
}

func TestParseYandexTURNServerMessage(t *testing.T) {
	t.Parallel()

	t.Run("array urls skip tcp and strip query", func(t *testing.T) {
		t.Parallel()

		user, pass, addr, found, err := parseYandexTURNServerMessage([]byte(readYandexFixture(t, "wss_turn_success.json")))
		if err != nil {
			t.Fatalf("parseYandexTURNServerMessage returned error: %v", err)
		}
		if !found {
			t.Fatal("expected TURN server to be found")
		}
		if user != "turn-user" || pass != "turn-pass" || addr != "turn.example.test:3478" {
			t.Fatalf("parsed user=%q pass=%q addr=%q", user, pass, addr)
		}
	})

	t.Run("string urls and turns scheme", func(t *testing.T) {
		t.Parallel()

		user, pass, addr, found, err := parseYandexTURNServerMessage([]byte(readYandexFixture(t, "wss_turn_string_url.json")))
		if err != nil {
			t.Fatalf("parseYandexTURNServerMessage returned error: %v", err)
		}
		if !found {
			t.Fatal("expected TURN server to be found")
		}
		if user != "turns-user" || pass != "turns-pass" || addr != "turns.example.test:5349" {
			t.Fatalf("parsed user=%q pass=%q addr=%q", user, pass, addr)
		}
	})

	t.Run("ack is ignored", func(t *testing.T) {
		t.Parallel()

		_, _, _, found, err := parseYandexTURNServerMessage([]byte(readYandexFixture(t, "wss_ack.json")))
		if err != nil {
			t.Fatalf("ack should not be an error: %v", err)
		}
		if found {
			t.Fatal("ack should not contain TURN server")
		}
	})

	t.Run("tcp-only turn is ignored", func(t *testing.T) {
		t.Parallel()

		_, _, _, found, err := parseYandexTURNServerMessage([]byte(readYandexFixture(t, "wss_tcp_only.json")))
		if err != nil {
			t.Fatalf("TCP-only TURN should not be an error: %v", err)
		}
		if found {
			t.Fatal("TCP-only TURN URL should be ignored")
		}
	})

	t.Run("missing credential is explicit", func(t *testing.T) {
		t.Parallel()

		_, _, _, _, err := parseYandexTURNServerMessage([]byte(readYandexFixture(t, "wss_missing_credential.json")))
		if err == nil || !strings.Contains(err.Error(), "missing credential") {
			t.Fatalf("error = %v, want missing credential", err)
		}
	})

	t.Run("invalid serverHello urls type is explicit", func(t *testing.T) {
		t.Parallel()

		msg := []byte(readYandexFixture(t, "wss_invalid_urls_type.json"))
		_, _, _, found, err := parseYandexTURNServerMessage(msg)
		if found {
			t.Fatal("invalid serverHello should not be reported as found")
		}
		if err == nil || !strings.Contains(err.Error(), "decode Yandex serverHello") {
			t.Fatalf("error = %v, want decode Yandex serverHello", err)
		}
	})

	t.Run("unrelated json mentioning serverHello is ignored", func(t *testing.T) {
		t.Parallel()

		msg := []byte(`{"log":"serverHello appeared in text"}`)
		_, _, _, found, err := parseYandexTURNServerMessage(msg)
		if err != nil {
			t.Fatalf("unrelated JSON should not be an error: %v", err)
		}
		if found {
			t.Fatal("unrelated JSON should not contain TURN server")
		}
	})
}

func readYandexFixture(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "yandex", name))
	if err != nil {
		t.Fatalf("read Yandex fixture %s: %v", name, err)
	}
	return string(data)
}
