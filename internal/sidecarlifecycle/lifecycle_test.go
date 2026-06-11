package sidecarlifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cacggghp/vk-turn-proxy/internal/sidecarinstall"
	"github.com/cacggghp/vk-turn-proxy/internal/xrayplan"
)

func TestLifecycleReportsMissingUnit(t *testing.T) {
	result, err := Status(Options{Root: t.TempDir(), DryRun: true})
	if !errors.Is(err, ErrMissingUnit) {
		t.Fatalf("err = %v, want ErrMissingUnit", err)
	}
	if result.State != StateMissing {
		t.Fatalf("state = %q, want missing", result.State)
	}
	if !strings.Contains(result.Message, "missing") {
		t.Fatalf("message = %q, want missing context", result.Message)
	}
}

func TestLifecycleStartStatusStopRestartWithDryRunRoot(t *testing.T) {
	root := installFixture(t)
	now := time.Unix(1700000000, 0).UTC()

	status, err := Status(Options{Root: root, DryRun: true, Now: now})
	if err != nil {
		t.Fatalf("initial Status: %v", err)
	}
	if status.State != StateStopped {
		t.Fatalf("initial state = %q, want stopped", status.State)
	}

	started, err := Start(Options{Root: root, DryRun: true, Now: now})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if started.State != StateRunning || !started.Changed {
		t.Fatalf("start result = %#v, want running changed", started)
	}

	status, err = Status(Options{Root: root, DryRun: true, Now: now})
	if err != nil {
		t.Fatalf("running Status: %v", err)
	}
	if status.State != StateRunning {
		t.Fatalf("state = %q, want running", status.State)
	}

	restarted, err := Restart(Options{Root: root, DryRun: true, Now: now.Add(time.Second)})
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if restarted.State != StateRunning || !restarted.Changed {
		t.Fatalf("restart result = %#v, want running changed", restarted)
	}

	stopped, err := Stop(Options{Root: root, DryRun: true, Now: now.Add(2 * time.Second)})
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.State != StateStopped || !stopped.Changed {
		t.Fatalf("stop result = %#v, want stopped changed", stopped)
	}

	journal, err := os.ReadFile(filepath.Join(root, "run", "vkturn", "lifecycle.log"))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	stateInfo, err := os.Stat(filepath.Join(root, "run", "vkturn", "server-state.json"))
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if got := stateInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("state file mode = %o, want 0600", got)
	}
	journalInfo, err := os.Stat(filepath.Join(root, "run", "vkturn", "lifecycle.log"))
	if err != nil {
		t.Fatalf("stat journal file: %v", err)
	}
	if got := journalInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("journal file mode = %o, want 0600", got)
	}
	for _, want := range []string{"action=start", "action=restart", "action=stop"} {
		if !strings.Contains(string(journal), want) {
			t.Fatalf("journal missing %q:\n%s", want, string(journal))
		}
	}

	assertFileMode(t, filepath.Join(root, "run", "vkturn"), 0o700)
	assertFileMode(t, filepath.Join(root, "run", "vkturn", "server-state.json"), 0o600)
	assertFileMode(t, filepath.Join(root, "run", "vkturn", "lifecycle.log"), 0o600)
}

func TestLifecycleLogsReadsTail(t *testing.T) {
	root := installFixture(t)
	logPath := filepath.Join(root, "var", "log", "vk-turn-proxy", "server.log")
	if err := os.WriteFile(logPath, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	result, err := Logs(Options{Root: root, DryRun: true, LogLines: 2})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if got := strings.Join(result.Logs, ","); got != "two,three" {
		t.Fatalf("logs = %q, want two,three", got)
	}
	if !strings.Contains(result.Message, "read 2 log line") {
		t.Fatalf("message = %q, want log count", result.Message)
	}
}

func TestLifecycleRejectsLogStreamPath(t *testing.T) {
	root := installFixture(t)
	_, err := Logs(Options{Root: root, DryRun: true, LogStream: "../secret.log"})
	if err == nil {
		t.Fatalf("Logs accepted path traversal log stream")
	}
	if !strings.Contains(err.Error(), "file name") {
		t.Fatalf("error = %v, want file name context", err)
	}

	_, err = Logs(Options{Root: root, DryRun: true, LogStream: "/tmp/server.log"})
	if err == nil {
		t.Fatalf("Logs accepted absolute log stream")
	}
}

func TestLifecycleRequiresDryRun(t *testing.T) {
	_, err := Start(Options{Root: t.TempDir()})
	if err == nil {
		t.Fatalf("Start succeeded without dry-run")
	}
	if !strings.Contains(err.Error(), "requires --dry-run") {
		t.Fatalf("error = %v, want dry-run context", err)
	}
}

func installFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	install, err := sidecarinstall.BuildDryRun(sidecarinstall.Options{
		Root:   root,
		DryRun: true,
		Plan: xrayplan.Plan{
			ReadOnly:        true,
			XrayConfigPath:  "dev/fixtures/xray/vless-tcp.json",
			XrayServiceName: "xray.service",
			BackendAddress:  "127.0.0.1:10001",
			SidecarAddress:  "0.0.0.0:56000",
		},
	})
	if err != nil {
		t.Fatalf("BuildDryRun: %v", err)
	}
	if err := sidecarinstall.WriteDryRunArtifacts(install); err != nil {
		t.Fatalf("WriteDryRunArtifacts: %v", err)
	}
	return root
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode %s = %03o, want %03o", path, got, want)
	}
}
