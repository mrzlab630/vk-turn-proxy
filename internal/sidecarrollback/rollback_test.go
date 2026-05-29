package sidecarrollback

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

func TestBuildDryRunPlansSidecarRemovalsFromManifest(t *testing.T) {
	root := installFixture(t)
	result, err := BuildDryRun(Options{Root: root, DryRun: true, Generated: time.Unix(1700000000, 0).UTC()})
	if err != nil {
		t.Fatalf("BuildDryRun: %v", err)
	}

	if !result.DryRun {
		t.Fatalf("result.DryRun = false, want true")
	}
	assertRemoval(t, result, sidecarinstall.DefaultUnitPath, true)
	assertRemoval(t, result, sidecarinstall.DefaultConfigPath, true)
	assertRemoval(t, result, sidecarinstall.DefaultBinaryPath, true)
	assertRemoval(t, result, sidecarinstall.DefaultManifest, true)
	assertContains(t, result.WillNotTouch, "dev/fixtures/xray/vless-tcp.json")
	assertContains(t, result.WillNotTouch, "xray.service")
	assertContains(t, result.SkippedApply, "restart or reload Xray")
}

func TestApplyDryRunRemovesOnlySidecarPaths(t *testing.T) {
	root := installFixture(t)
	xrayConfig := filepath.Join(root, "etc", "xray", "config.json")
	if err := os.MkdirAll(filepath.Dir(xrayConfig), 0o755); err != nil {
		t.Fatalf("create xray dir: %v", err)
	}
	if err := os.WriteFile(xrayConfig, []byte("xray config must stay\n"), 0o644); err != nil {
		t.Fatalf("write xray config: %v", err)
	}

	plan, err := BuildDryRun(Options{Root: root, DryRun: true})
	if err != nil {
		t.Fatalf("BuildDryRun: %v", err)
	}
	applied, err := ApplyDryRun(plan)
	if err != nil {
		t.Fatalf("ApplyDryRun: %v", err)
	}
	if !applied.Applied {
		t.Fatalf("applied.Applied = false, want true")
	}

	for _, path := range []string{
		filepath.Join(root, "etc", "systemd", "system", "vkturn-server.service"),
		filepath.Join(root, "etc", "vkturn", "server.json"),
		filepath.Join(root, "opt", "vk-turn-proxy", "vk-turn-proxy-server"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists or unexpected stat error: %v", path, err)
		}
	}
	if data, err := os.ReadFile(xrayConfig); err != nil || !strings.Contains(string(data), "must stay") {
		t.Fatalf("xray config was touched, data=%q err=%v", string(data), err)
	}
}

func TestBuildDryRunReportsMissingManifest(t *testing.T) {
	result, err := BuildDryRun(Options{Root: t.TempDir(), DryRun: true})
	if !errors.Is(err, ErrMissingManifest) {
		t.Fatalf("err = %v, want ErrMissingManifest", err)
	}
	if !strings.Contains(result.Message, "manifest is missing") {
		t.Fatalf("message = %q, want missing manifest context", result.Message)
	}
}

func TestBuildDryRunRequiresDryRun(t *testing.T) {
	_, err := BuildDryRun(Options{Root: t.TempDir()})
	if err == nil {
		t.Fatalf("BuildDryRun accepted non dry-run")
	}
	if !strings.Contains(err.Error(), "only dry-run") {
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
		t.Fatalf("BuildDryRun install: %v", err)
	}
	if err := sidecarinstall.WriteDryRunArtifacts(install); err != nil {
		t.Fatalf("WriteDryRunArtifacts: %v", err)
	}
	return root
}

func assertRemoval(t *testing.T, result Result, path string, exists bool) {
	t.Helper()
	for _, removal := range result.Removals {
		if removal.Path == path {
			if removal.Exists != exists {
				t.Fatalf("removal %s exists=%t, want %t", path, removal.Exists, exists)
			}
			return
		}
	}
	t.Fatalf("removal %s not found in %#v", path, result.Removals)
}

func assertContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("%#v does not contain %q", values, want)
}
