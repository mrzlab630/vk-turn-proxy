package xrayplan

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseConfigFileFindsVLESSTCPInbound(t *testing.T) {
	inbounds, err := ParseConfigFile("../../dev/fixtures/xray/vless-tcp.json")
	if err != nil {
		t.Fatalf("ParseConfigFile: %v", err)
	}
	if len(inbounds) != 1 {
		t.Fatalf("len(inbounds) = %d, want 1", len(inbounds))
	}

	got := inbounds[0]
	if got.Tag != "vless-in" {
		t.Fatalf("tag = %q, want vless-in", got.Tag)
	}
	if got.Port != 10001 {
		t.Fatalf("port = %d, want 10001", got.Port)
	}
	if got.Network != "tcp" {
		t.Fatalf("network = %q, want tcp", got.Network)
	}
	if got.Security != "none" {
		t.Fatalf("security = %q, want none", got.Security)
	}
	if got.ClientCount != 1 {
		t.Fatalf("client count = %d, want 1", got.ClientCount)
	}
	if got.BackendAddress() != "127.0.0.1:10001" {
		t.Fatalf("backend = %q, want 127.0.0.1:10001", got.BackendAddress())
	}
}

func TestParseConfigSkipsUnsupportedInboundTypes(t *testing.T) {
	inbounds, err := ParseConfigFile("../../dev/fixtures/xray/no-vless.json")
	if err != nil {
		t.Fatalf("ParseConfigFile: %v", err)
	}
	if len(inbounds) != 0 {
		t.Fatalf("len(inbounds) = %d, want 0", len(inbounds))
	}
}

func TestParseConfigFileRejectsMalformedJSON(t *testing.T) {
	_, err := ParseConfigFile("../../dev/fixtures/xray/malformed.json")
	if err == nil {
		t.Fatalf("ParseConfigFile succeeded for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse Xray config JSON") {
		t.Fatalf("error = %v, want parse context", err)
	}
}

func TestSelectUDPPortInRangeSkipsBusyPorts(t *testing.T) {
	busyPorts := map[int]bool{56000: true, 56001: true}
	got, err := SelectUDPPortInRange(56000, 56003, func(port int) (bool, error) {
		return busyPorts[port], nil
	})
	if err != nil {
		t.Fatalf("SelectUDPPortInRange: %v", err)
	}
	if got != 56002 {
		t.Fatalf("port = %d, want 56002", got)
	}
}

func TestSelectUDPPortInRangeReturnsErrorWhenAllBusy(t *testing.T) {
	_, err := SelectUDPPortInRange(56000, 56001, func(port int) (bool, error) {
		return true, nil
	})
	if err == nil {
		t.Fatalf("SelectUDPPortInRange succeeded when all ports are busy")
	}
	if !strings.Contains(err.Error(), "no free UDP port") {
		t.Fatalf("error = %v, want no free UDP port", err)
	}
}

func TestBuildPlanUsesFirstVLESSInboundAndReadOnlyArtifacts(t *testing.T) {
	plan, err := BuildPlan(PlanOptions{
		XrayConfigPath:  "../../dev/fixtures/xray/vless-tcp.json",
		XrayServiceName: "xray-prod-sim.service",
		SidecarListen:   "0.0.0.0",
		PortStart:       56000,
		PortEnd:         56010,
		PortBusy: func(port int) (bool, error) {
			return port == 56000, nil
		},
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	if !plan.ReadOnly {
		t.Fatalf("plan.ReadOnly = false, want true")
	}
	if plan.BackendAddress != "127.0.0.1:10001" {
		t.Fatalf("backend = %q, want 127.0.0.1:10001", plan.BackendAddress)
	}
	if plan.SidecarAddress != "0.0.0.0:56001" {
		t.Fatalf("sidecar = %q, want 0.0.0.0:56001", plan.SidecarAddress)
	}
	wantCommand := []string{"vk-turn-proxy-server", "-listen", "0.0.0.0:56001", "-connect", "127.0.0.1:10001", "-vless"}
	if !reflect.DeepEqual(plan.ServerCommand, wantCommand) {
		t.Fatalf("server command = %#v, want %#v", plan.ServerCommand, wantCommand)
	}
	assertContains(t, plan.WillCreate, "/etc/vkturn/server.json")
	assertContains(t, plan.WillNotTouch, "../../dev/fixtures/xray/vless-tcp.json")
	assertContains(t, plan.WillNotTouch, "xray-prod-sim.service")
}

func TestBuildPlanReturnsNoVLESSTCPInbound(t *testing.T) {
	_, err := BuildPlan(PlanOptions{
		XrayConfigPath: "../../dev/fixtures/xray/no-vless.json",
		PortBusy: func(port int) (bool, error) {
			return false, nil
		},
	})
	if !errors.Is(err, ErrNoVLESSTCPInbound) {
		t.Fatalf("error = %v, want ErrNoVLESSTCPInbound", err)
	}
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
