package xraydoctor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunDiscoversFixtureXrayServiceConfigAndProcess(t *testing.T) {
	report := Run(context.Background(), Options{
		Root:             "../../dev/fixtures/linux-root",
		Now:              time.Unix(1700000000, 0).UTC(),
		SkipHostCommands: true,
		NoPortProbe:      true,
	})

	if !report.ReadOnly {
		t.Fatalf("report.ReadOnly = false, want true")
	}
	if report.Root != "../../dev/fixtures/linux-root" {
		t.Fatalf("root = %q", report.Root)
	}
	if len(report.XrayServices) != 1 {
		t.Fatalf("services = %#v, want one xray service", report.XrayServices)
	}
	service := report.XrayServices[0]
	if service.Name != "xray.service" {
		t.Fatalf("service name = %q, want xray.service", service.Name)
	}
	if service.ConfigPath != "/etc/xray/config.json" {
		t.Fatalf("service config = %q, want /etc/xray/config.json", service.ConfigPath)
	}
	if service.Confidence != "high" {
		t.Fatalf("service confidence = %q, want high", service.Confidence)
	}

	if len(report.XrayProcesses) != 1 {
		t.Fatalf("processes = %#v, want one xray process", report.XrayProcesses)
	}
	if report.XrayProcesses[0].ConfigPath != "/etc/xray/config.json" {
		t.Fatalf("process config = %q", report.XrayProcesses[0].ConfigPath)
	}

	wantConfig := filepath.Clean("../../dev/fixtures/linux-root/etc/xray/config.json")
	if filepath.Clean(report.SelectedConfig) != wantConfig {
		t.Fatalf("selected config = %q, want %q", report.SelectedConfig, wantConfig)
	}
	if len(report.VLESSInbounds) != 1 {
		t.Fatalf("VLESS inbounds = %#v, want one", report.VLESSInbounds)
	}
	if report.VLESSInbounds[0].BackendAddress() != "127.0.0.1:10001" {
		t.Fatalf("backend = %q", report.VLESSInbounds[0].BackendAddress())
	}
	if len(report.PortChecks) != 1 || report.PortChecks[0].Status != "skipped" {
		t.Fatalf("port checks = %#v, want skipped", report.PortChecks)
	}
	assertSummaryContains(t, report.Summary, "found 1 Xray service candidate(s)")
	assertSummaryContains(t, report.Summary, "found 1 VLESS TCP inbound(s)")
}

func TestRunReportsMissingConfigWithoutMutatingHost(t *testing.T) {
	report := Run(context.Background(), Options{
		Root:             t.TempDir(),
		SkipHostCommands: true,
		NoPortProbe:      true,
	})

	if len(report.XrayServices) != 0 {
		t.Fatalf("services = %#v, want none", report.XrayServices)
	}
	if report.SelectedConfig != "" {
		t.Fatalf("selected config = %q, want empty", report.SelectedConfig)
	}
	assertSummaryContains(t, report.Summary, "no Xray systemd unit found under selected root")
	assertSummaryContains(t, report.Summary, "no usable Xray VLESS TCP config selected")
}

func TestRunChecksBackendReachabilityWhenEnabled(t *testing.T) {
	report := Run(context.Background(), Options{
		Root:                  "../../dev/fixtures/linux-root",
		SkipHostCommands:      true,
		NoPortProbe:           true,
		CheckBackendReachable: true,
	})

	if len(report.BackendChecks) != 1 {
		t.Fatalf("backend checks = %#v, want one", report.BackendChecks)
	}
	if report.BackendChecks[0].Address != "127.0.0.1:10001" {
		t.Fatalf("backend address = %q", report.BackendChecks[0].Address)
	}
	if report.BackendChecks[0].Status != StatusWarn && report.BackendChecks[0].Status != StatusOK {
		t.Fatalf("backend status = %q", report.BackendChecks[0].Status)
	}
}

func TestExtractConfigPathHandlesCommonForms(t *testing.T) {
	for _, test := range []struct {
		text string
		want string
	}{
		{"ExecStart=/usr/local/bin/xray run -config /etc/xray/config.json", "/etc/xray/config.json"},
		{"/usr/local/bin/xray run --config=/etc/xray/config.json", "/etc/xray/config.json"},
		{"xray -c '/tmp/xray.json'", "/tmp/xray.json"},
	} {
		if got := extractConfigPath(test.text); got != test.want {
			t.Fatalf("extractConfigPath(%q) = %q, want %q", test.text, got, test.want)
		}
	}
}

func assertSummaryContains(t *testing.T, summary []string, want string) {
	t.Helper()
	for _, item := range summary {
		if strings.Contains(item, want) {
			return
		}
	}
	t.Fatalf("summary %#v does not contain %q", summary, want)
}
