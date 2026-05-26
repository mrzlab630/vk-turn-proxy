package main

import (
	"net/url"
	"testing"
)

func TestRewriteProxyRedirectLocation(t *testing.T) {
	t.Parallel()

	targetURL, err := url.Parse("https://id.vk.ru/captcha")
	if err != nil {
		t.Fatalf("failed to parse target URL: %v", err)
	}

	testCases := []struct {
		name     string
		location string
		want     string
		ok       bool
	}{
		{
			name:     "keeps safe relative path",
			location: "/captcha?step=2",
			want:     "/captcha?step=2",
			ok:       true,
		},
		{
			name:     "rewrites same-origin absolute URL",
			location: "https://id.vk.ru/captcha?step=2",
			want:     "http://localhost:8765/captcha?step=2",
			ok:       true,
		},
		{
			name:     "blocks scheme-relative redirect",
			location: "//evil.example/captcha",
			ok:       false,
		},
		{
			name:     "blocks slash-backslash redirect",
			location: `/\evil.example/captcha`,
			ok:       false,
		},
		{
			name:     "blocks lookalike absolute host",
			location: "https://id.vk.ru.evil.example/captcha",
			ok:       false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := rewriteProxyRedirectLocation(tc.location, targetURL)
			if ok != tc.ok {
				t.Fatalf("rewriteProxyRedirectLocation() ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("rewriteProxyRedirectLocation() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsSafeGenericProxyTarget(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		rawURL string
		want   bool
	}{
		{name: "allows public https host", rawURL: "https://id.vk.ru/captcha.js", want: true},
		{name: "allows public http host", rawURL: "http://vk.com/assets/app.js", want: true},
		{name: "blocks javascript scheme", rawURL: "javascript:alert(1)", want: false},
		{name: "blocks localhost", rawURL: "http://localhost:8080/admin", want: false},
		{name: "blocks localhost suffix", rawURL: "http://app.localhost:8080/admin", want: false},
		{name: "blocks loopback ip", rawURL: "http://127.0.0.1:8080/admin", want: false},
		{name: "blocks ipv4-mapped loopback ip", rawURL: "http://[::ffff:127.0.0.1]:8080/admin", want: false},
		{name: "blocks private ip", rawURL: "http://192.168.1.1/admin", want: false},
		{name: "blocks link local ip", rawURL: "http://169.254.1.1/admin", want: false},
		{name: "blocks unspecified ip", rawURL: "http://0.0.0.0/admin", want: false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := url.Parse(tc.rawURL)
			if err != nil {
				t.Fatalf("failed to parse test URL: %v", err)
			}
			if got := isSafeGenericProxyTarget(parsed); got != tc.want {
				t.Fatalf("isSafeGenericProxyTarget(%q) = %v, want %v", tc.rawURL, got, tc.want)
			}
		})
	}
}
