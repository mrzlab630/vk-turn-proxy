package sidecarinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cacggghp/vk-turn-proxy/internal/xrayplan"
)

func TestBuildDryRunRendersSidecarArtifacts(t *testing.T) {
	result, err := BuildDryRun(Options{
		Root:        "/tmp/vkturn-dry-run",
		DryRun:      true,
		Plan:        testPlan(),
		GeneratedAt: time.Unix(1700000000, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("BuildDryRun: %v", err)
	}

	if !result.DryRun {
		t.Fatalf("result.DryRun = false, want true")
	}
	assertArtifactContains(t, result, DefaultConfigPath, `"listen_addr": "0.0.0.0:56000"`)
	assertArtifactContains(t, result, DefaultConfigPath, `"connect_addr": "127.0.0.1:10001"`)
	assertArtifactContains(t, result, DefaultUnitPath, "ExecStart=/opt/vk-turn-proxy/vk-turn-proxy-server -config /etc/vkturn/server.json")
	assertArtifactContains(t, result, DefaultUnitPath, "After=network-online.target xray.service")
	assertArtifactContains(t, result, DefaultEnvPath, "VKTURN_CONFIG='/etc/vkturn/server.json'")
	assertArtifactContains(t, result, DefaultManifest, `"xray_config_path": "dev/fixtures/xray/vless-tcp.json"`)
	assertContains(t, result.WillNotTouch, "dev/fixtures/xray/vless-tcp.json")
	assertContains(t, result.WillNotTouch, "xray.service")
	assertContains(t, result.SkippedApply, "restart or reload Xray")
}

func TestBuildDryRunRefusesHostRoot(t *testing.T) {
	_, err := BuildDryRun(Options{Root: "/", DryRun: true, Plan: testPlan()})
	if err == nil {
		t.Fatalf("BuildDryRun accepted host root")
	}
	if !strings.Contains(err.Error(), "--root") {
		t.Fatalf("error = %v, want root context", err)
	}
}

func TestBuildDryRunRequiresDryRun(t *testing.T) {
	_, err := BuildDryRun(Options{Root: "/tmp/vkturn-dry-run", Plan: testPlan()})
	if err == nil {
		t.Fatalf("BuildDryRun accepted non dry-run install")
	}
	if !strings.Contains(err.Error(), "only dry-run") {
		t.Fatalf("error = %v, want dry-run context", err)
	}
}

func TestWriteDryRunArtifactsWritesOnlyUnderRoot(t *testing.T) {
	root := t.TempDir()
	result, err := BuildDryRun(Options{
		Root:        root,
		DryRun:      true,
		Plan:        testPlan(),
		GeneratedAt: time.Unix(1700000000, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("BuildDryRun: %v", err)
	}
	if err := WriteDryRunArtifacts(result); err != nil {
		t.Fatalf("WriteDryRunArtifacts: %v", err)
	}

	serverConfig := filepath.Join(root, "etc", "vkturn", "server.json")
	data, err := os.ReadFile(serverConfig)
	if err != nil {
		t.Fatalf("read server config: %v", err)
	}
	if !strings.Contains(string(data), `"connect_addr": "127.0.0.1:10001"`) {
		t.Fatalf("server config missing backend:\n%s", string(data))
	}

	xrayConfig := filepath.Join(root, "etc", "xray", "config.json")
	if _, err := os.Stat(xrayConfig); !os.IsNotExist(err) {
		t.Fatalf("dry-run unexpectedly created Xray config at %s", xrayConfig)
	}

	unit := filepath.Join(root, "etc", "systemd", "system", "vkturn-server.service")
	if _, err := os.Stat(unit); err != nil {
		t.Fatalf("unit not written: %v", err)
	}
}

func testPlan() xrayplan.Plan {
	return xrayplan.Plan{
		ReadOnly:        true,
		XrayConfigPath:  "dev/fixtures/xray/vless-tcp.json",
		XrayServiceName: "xray.service",
		BackendAddress:  "127.0.0.1:10001",
		SidecarListen:   "0.0.0.0",
		SidecarPort:     56000,
		SidecarAddress:  "0.0.0.0:56000",
	}
}

func assertArtifactContains(t *testing.T, result Result, path, want string) {
	t.Helper()
	for _, artifact := range result.Artifacts {
		if artifact.Path == path {
			if !strings.Contains(artifact.Content, want) {
				t.Fatalf("artifact %s missing %q:\n%s", path, want, artifact.Content)
			}
			return
		}
	}
	t.Fatalf("artifact %s not found", path)
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
