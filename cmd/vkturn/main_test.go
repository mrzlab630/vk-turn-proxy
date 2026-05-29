package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerPlanPrintsReadOnlyHumanPlan(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"server",
		"plan",
		"--xray-config", "../../dev/fixtures/xray/vless-tcp.json",
		"--sidecar-port-start", "56000",
		"--sidecar-port-end", "56010",
		"--no-port-probe",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v, stderr: %s", err, stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"mode: read-only",
		"Selected inbound: tag=vless-in protocol=vless network=tcp security=none listen=0.0.0.0 port=10001 clients=1",
		"Backend target for sidecar: 127.0.0.1:10001",
		"Selected sidecar UDP listen: 0.0.0.0:56000",
		"vk-turn-proxy-server -listen 0.0.0.0:56000 -connect 127.0.0.1:10001 -vless",
		"Will not touch:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestDoctorPrintsReadOnlyHumanReport(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"doctor",
		"--root", "../../dev/fixtures/linux-root",
		"--skip-host-commands",
		"--no-port-probe",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v, stderr: %s", err, stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"vkturn doctor",
		"mode: read-only",
		"Xray service candidates:",
		"xray.service status=ok confidence=high",
		"Selected Xray config: ../../dev/fixtures/linux-root/etc/xray/config.json",
		"VLESS TCP inbound: tag=vless-in",
		"Sidecar port checks:",
		"status=skipped",
		"Summary:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestDoctorPrintsJSONReport(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"doctor",
		"--root", "../../dev/fixtures/linux-root",
		"--skip-host-commands",
		"--no-port-probe",
		"--json",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v, stderr: %s", err, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, `"read_only": true`) {
		t.Fatalf("JSON output missing read_only:\n%s", output)
	}
	if !strings.Contains(output, `"selected_config": "../../dev/fixtures/linux-root/etc/xray/config.json"`) {
		t.Fatalf("JSON output missing selected_config:\n%s", output)
	}
}

func TestServerPlanPrintsJSONPlan(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"server",
		"plan",
		"--xray-config", "../../dev/fixtures/xray/vless-tcp.json",
		"--no-port-probe",
		"--json",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v, stderr: %s", err, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, `"read_only": true`) {
		t.Fatalf("JSON output missing read_only:\n%s", output)
	}
	if !strings.Contains(output, `"backend_address": "127.0.0.1:10001"`) {
		t.Fatalf("JSON output missing backend_address:\n%s", output)
	}
}

func TestServerPlanRequiresXrayConfig(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"server", "plan"}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run succeeded without --xray-config")
	}
	if !strings.Contains(err.Error(), "--xray-config is required") {
		t.Fatalf("error = %v, want --xray-config context", err)
	}
}

func TestServerInstallDryRunPrintsArtifacts(t *testing.T) {
	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"server",
		"install",
		"--dry-run",
		"--root", root,
		"--xray-config", "../../dev/fixtures/xray/vless-tcp.json",
		"--no-port-probe",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v, stderr: %s", err, stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"vkturn server install",
		"mode: dry-run",
		"Artifacts planned:",
		"/etc/vkturn/server.json",
		"/etc/systemd/system/vkturn-server.service",
		"Will not touch:",
		"xray.service",
		"restart or reload Xray",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestServerInstallDryRunWriteCreatesArtifactsUnderRoot(t *testing.T) {
	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"server",
		"install",
		"--dry-run",
		"--write",
		"--root", root,
		"--xray-config", "../../dev/fixtures/xray/vless-tcp.json",
		"--no-port-probe",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v, stderr: %s", err, stderr.String())
	}

	serverConfig := filepath.Join(root, "etc", "vkturn", "server.json")
	data, err := os.ReadFile(serverConfig)
	if err != nil {
		t.Fatalf("read server config: %v", err)
	}
	if !strings.Contains(string(data), `"listen_addr": "0.0.0.0:56000"`) {
		t.Fatalf("server config missing listen addr:\n%s", string(data))
	}

	unitPath := filepath.Join(root, "etc", "systemd", "system", "vkturn-server.service")
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	if !strings.Contains(string(unit), "ExecStart=/opt/vk-turn-proxy/vk-turn-proxy-server -config /etc/vkturn/server.json") {
		t.Fatalf("unit missing ExecStart:\n%s", string(unit))
	}
}

func TestServerInstallRequiresDryRun(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"server",
		"install",
		"--root", t.TempDir(),
		"--xray-config", "../../dev/fixtures/xray/vless-tcp.json",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run succeeded without --dry-run")
	}
	if !strings.Contains(err.Error(), "requires --dry-run") {
		t.Fatalf("error = %v, want dry-run context", err)
	}
}

func TestServerLifecycleDryRunCommands(t *testing.T) {
	root := installDryRunFixture(t)

	statusOut := runCLI(t, []string{"server", "status", "--dry-run", "--root", root})
	assertOutputContains(t, statusOut, "vkturn server status", "State: stopped")

	startOut := runCLI(t, []string{"server", "start", "--dry-run", "--root", root})
	assertOutputContains(t, startOut, "vkturn server start", "State: running", "Changed: true")

	statusOut = runCLI(t, []string{"server", "status", "--dry-run", "--root", root})
	assertOutputContains(t, statusOut, "State: running")

	restartOut := runCLI(t, []string{"server", "restart", "--dry-run", "--root", root})
	assertOutputContains(t, restartOut, "vkturn server restart", "State: running")

	stopOut := runCLI(t, []string{"server", "stop", "--dry-run", "--root", root})
	assertOutputContains(t, stopOut, "vkturn server stop", "State: stopped")
}

func TestServerLifecycleLogsReadsDryRunLogs(t *testing.T) {
	root := installDryRunFixture(t)
	logPath := filepath.Join(root, "var", "log", "vk-turn-proxy", "server.log")
	if err := os.WriteFile(logPath, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	output := runCLI(t, []string{"server", "logs", "--dry-run", "--root", root, "--lines", "2"})
	assertOutputContains(t, output, "vkturn server logs", "- beta", "- gamma")
}

func TestServerLifecycleRequiresDryRun(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"server", "status", "--root", t.TempDir()}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run succeeded without --dry-run")
	}
	if !strings.Contains(err.Error(), "requires --dry-run") {
		t.Fatalf("error = %v, want dry-run context", err)
	}
}

func TestServerLifecycleReportsMissingUnit(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"server", "status", "--dry-run", "--root", t.TempDir()}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run succeeded without installed unit")
	}
	if !strings.Contains(err.Error(), "sidecar unit is not installed") {
		t.Fatalf("error = %v, want missing unit context", err)
	}
}

func TestServerUninstallDryRunPlansAndWritesRemovals(t *testing.T) {
	root := installDryRunFixture(t)
	xrayConfig := filepath.Join(root, "etc", "xray", "config.json")
	if err := os.MkdirAll(filepath.Dir(xrayConfig), 0o755); err != nil {
		t.Fatalf("create xray dir: %v", err)
	}
	if err := os.WriteFile(xrayConfig, []byte("xray stays\n"), 0o644); err != nil {
		t.Fatalf("write xray config: %v", err)
	}

	planOut := runCLI(t, []string{"server", "uninstall", "--dry-run", "--root", root})
	assertOutputContains(t, planOut, "vkturn server uninstall", "Removals planned:", "/etc/vkturn/server.json", "Will not touch:")
	if _, err := os.Stat(filepath.Join(root, "etc", "vkturn", "server.json")); err != nil {
		t.Fatalf("dry-run plan removed server config: %v", err)
	}

	writeOut := runCLI(t, []string{"server", "uninstall", "--dry-run", "--write", "--root", root})
	assertOutputContains(t, writeOut, "Applied: true", "Removals applied:")
	if _, err := os.Stat(filepath.Join(root, "etc", "vkturn", "server.json")); !os.IsNotExist(err) {
		t.Fatalf("server config still exists or unexpected stat error: %v", err)
	}
	if data, err := os.ReadFile(xrayConfig); err != nil || !strings.Contains(string(data), "xray stays") {
		t.Fatalf("xray config was touched, data=%q err=%v", string(data), err)
	}
}

func TestServerUninstallRequiresDryRun(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"server", "uninstall", "--root", t.TempDir()}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run succeeded without --dry-run")
	}
	if !strings.Contains(err.Error(), "requires --dry-run") {
		t.Fatalf("error = %v, want dry-run context", err)
	}
}

func TestServerUninstallReportsMissingManifest(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"server", "uninstall", "--dry-run", "--root", t.TempDir()}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run succeeded without manifest")
	}
	if !strings.Contains(err.Error(), "manifest is missing") {
		t.Fatalf("error = %v, want missing manifest context", err)
	}
}

func installDryRunFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runCLI(t, []string{
		"server",
		"install",
		"--dry-run",
		"--write",
		"--root", root,
		"--xray-config", "../../dev/fixtures/xray/vless-tcp.json",
		"--no-port-probe",
	})
	return root
}

func runCLI(t *testing.T, args []string) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run %v: %v, stderr: %s", args, err, stderr.String())
	}
	return stdout.String()
}

func assertOutputContains(t *testing.T, output string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}
