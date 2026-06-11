package xraydoctor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/cacggghp/vk-turn-proxy/internal/xrayplan"
)

const (
	StatusOK      = "ok"
	StatusWarn    = "warn"
	StatusMissing = "missing"
	StatusError   = "error"
)

type Report struct {
	ReadOnly       bool                    `json:"read_only"`
	GeneratedAt    time.Time               `json:"generated_at"`
	Root           string                  `json:"root"`
	OS             OSInfo                  `json:"os"`
	Privileges     PrivilegeInfo           `json:"privileges"`
	Commands       []CommandCheck          `json:"commands"`
	XrayServices   []ServiceCandidate      `json:"xray_services"`
	XrayConfigs    []ConfigCandidate       `json:"xray_configs"`
	XrayProcesses  []ProcessCandidate      `json:"xray_processes"`
	SelectedConfig string                  `json:"selected_config,omitempty"`
	VLESSInbounds  []xrayplan.VLESSInbound `json:"vless_inbounds,omitempty"`
	BackendChecks  []EndpointCheck         `json:"backend_checks,omitempty"`
	PortChecks     []PortCheck             `json:"port_checks"`
	Docker         DockerInfo              `json:"docker"`
	Summary        []string                `json:"summary"`
}

type OSInfo struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
}

type PrivilegeInfo struct {
	UID    int  `json:"uid"`
	IsRoot bool `json:"is_root"`
}

type CommandCheck struct {
	Name   string `json:"name"`
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
}

type ServiceCandidate struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Status     string `json:"status"`
	Confidence string `json:"confidence"`
	ConfigPath string `json:"config_path,omitempty"`
	Reason     string `json:"reason"`
}

type ConfigCandidate struct {
	Path        string `json:"path"`
	Status      string `json:"status"`
	VLESSCount  int    `json:"vless_count"`
	Error       string `json:"error,omitempty"`
	Confidence  string `json:"confidence"`
	FromService string `json:"from_service,omitempty"`
}

type ProcessCandidate struct {
	PID        string `json:"pid,omitempty"`
	Command    string `json:"command"`
	ConfigPath string `json:"config_path,omitempty"`
	Status     string `json:"status"`
}

type EndpointCheck struct {
	Name    string `json:"name"`
	Network string `json:"network"`
	Address string `json:"address"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

type PortCheck struct {
	Network string `json:"network"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

type DockerInfo struct {
	Available bool   `json:"available"`
	Status    string `json:"status"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

type Options struct {
	Root                  string
	Now                   time.Time
	CommandNames          []string
	ServiceNames          []string
	ConfigPaths           []string
	ProcessSnapshotPath   string
	SidecarListen         string
	SidecarPortStart      int
	SidecarPortEnd        int
	NoPortProbe           bool
	CheckBackendReachable bool
	SkipHostCommands      bool
}

func DefaultOptions() Options {
	return Options{
		Root:             "/",
		CommandNames:     []string{"xray", "systemctl", "docker"},
		ServiceNames:     []string{"xray.service", "xray@.service", "v2ray.service", "v2raya.service"},
		ConfigPaths:      []string{"/etc/xray/config.json", "/usr/local/etc/xray/config.json", "/etc/v2ray/config.json", "/etc/v2raya/config.json"},
		SidecarListen:    xrayplan.DefaultSidecarListen,
		SidecarPortStart: xrayplan.DefaultSidecarPortStart,
		SidecarPortEnd:   xrayplan.DefaultSidecarPortEnd,
	}
}

func Run(ctx context.Context, opts Options) Report {
	defaults := DefaultOptions()
	if opts.Root == "" {
		opts.Root = defaults.Root
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if len(opts.CommandNames) == 0 {
		opts.CommandNames = defaults.CommandNames
	}
	if len(opts.ServiceNames) == 0 {
		opts.ServiceNames = defaults.ServiceNames
	}
	if len(opts.ConfigPaths) == 0 {
		opts.ConfigPaths = defaults.ConfigPaths
	}
	if opts.SidecarListen == "" {
		opts.SidecarListen = defaults.SidecarListen
	}
	if opts.SidecarPortStart == 0 {
		opts.SidecarPortStart = defaults.SidecarPortStart
	}
	if opts.SidecarPortEnd == 0 {
		opts.SidecarPortEnd = defaults.SidecarPortEnd
	}

	report := Report{
		ReadOnly:    true,
		GeneratedAt: opts.Now,
		Root:        opts.Root,
		OS: OSInfo{
			GOOS:   runtime.GOOS,
			GOARCH: runtime.GOARCH,
		},
		Privileges: PrivilegeInfo{
			UID:    os.Geteuid(),
			IsRoot: os.Geteuid() == 0,
		},
	}

	report.Commands = checkCommands(opts.CommandNames, opts.SkipHostCommands)
	report.XrayServices = discoverServices(opts.Root, opts.ServiceNames)
	report.XrayProcesses = discoverProcesses(opts.Root, opts.ProcessSnapshotPath)
	report.XrayConfigs = discoverConfigs(opts.Root, opts.ConfigPaths, report.XrayServices, report.XrayProcesses)
	report.SelectedConfig, report.VLESSInbounds = selectConfig(report.XrayConfigs)
	report.PortChecks = checkSidecarPort(opts)
	report.Docker = checkDocker(ctx, report.Commands)
	if opts.CheckBackendReachable {
		report.BackendChecks = checkBackendReachability(ctx, report.VLESSInbounds)
	}
	report.Summary = summarize(report)

	return report
}

func checkCommands(names []string, skipHostCommands bool) []CommandCheck {
	checks := make([]CommandCheck, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if skipHostCommands {
			checks = append(checks, CommandCheck{Name: name, Status: "skipped"})
			continue
		}
		path, err := exec.LookPath(name)
		check := CommandCheck{Name: name, Path: path, Status: StatusOK}
		if err != nil {
			check.Status = StatusMissing
		}
		checks = append(checks, check)
	}
	return checks
}

func discoverServices(root string, names []string) []ServiceCandidate {
	candidates := make([]ServiceCandidate, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		for _, dir := range []string{"/etc/systemd/system", "/lib/systemd/system", "/usr/lib/systemd/system"} {
			path := rootedPath(root, filepath.Join(dir, name))
			if seen[path] {
				continue
			}
			seen[path] = true
			// #nosec G304 -- service files are read-only probes under the operator-selected root.
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			service := ServiceCandidate{
				Name:       name,
				Path:       path,
				Status:     StatusOK,
				Confidence: serviceConfidence(name, string(data)),
				ConfigPath: extractConfigPath(string(data)),
				Reason:     "systemd unit file found under root",
			}
			candidates = append(candidates, service)
		}
	}
	return candidates
}

func discoverProcesses(root, explicitSnapshot string) []ProcessCandidate {
	snapshotPath := explicitSnapshot
	if snapshotPath == "" {
		snapshotPath = rootedPath(root, "/run/processes.txt")
	}
	// #nosec G304 -- process snapshot path is an explicit fixture/operator input for read-only diagnostics.
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		return nil
	}

	var candidates []ProcessCandidate
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(strings.ToLower(line), "xray") {
			continue
		}
		pid, command := splitPIDCommand(line)
		candidates = append(candidates, ProcessCandidate{
			PID:        pid,
			Command:    command,
			ConfigPath: extractConfigPath(command),
			Status:     StatusOK,
		})
	}
	return candidates
}

func discoverConfigs(root string, paths []string, services []ServiceCandidate, processes []ProcessCandidate) []ConfigCandidate {
	ordered := make([]string, 0, len(paths)+len(services)+len(processes))
	fromService := map[string]string{}
	appendPath := func(path string) {
		path = strings.TrimSpace(path)
		if path != "" {
			ordered = append(ordered, path)
		}
	}
	for _, service := range services {
		appendPath(service.ConfigPath)
		if service.ConfigPath != "" {
			fromService[service.ConfigPath] = service.Name
		}
	}
	for _, process := range processes {
		appendPath(process.ConfigPath)
	}
	for _, path := range paths {
		appendPath(path)
	}

	seen := map[string]bool{}
	var candidates []ConfigCandidate
	for _, path := range ordered {
		if seen[path] {
			continue
		}
		seen[path] = true
		rooted := rootedPath(root, path)
		candidate := ConfigCandidate{Path: rooted, Status: StatusOK, Confidence: "low", FromService: fromService[path]}
		inbounds, err := xrayplan.ParseConfigFile(rooted)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				candidate.Status = StatusMissing
			} else {
				candidate.Status = StatusError
				candidate.Error = err.Error()
			}
		} else {
			candidate.VLESSCount = len(inbounds)
			if len(inbounds) > 0 {
				candidate.Confidence = "high"
			} else {
				candidate.Confidence = "medium"
			}
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func selectConfig(configs []ConfigCandidate) (string, []xrayplan.VLESSInbound) {
	for _, cfg := range configs {
		if cfg.Status != StatusOK || cfg.VLESSCount == 0 {
			continue
		}
		inbounds, err := xrayplan.ParseConfigFile(cfg.Path)
		if err == nil {
			return cfg.Path, inbounds
		}
	}
	return "", nil
}

func checkSidecarPort(opts Options) []PortCheck {
	check := PortCheck{
		Network: "udp",
		Address: opts.SidecarListen,
		Port:    opts.SidecarPortStart,
		Status:  StatusOK,
	}
	if opts.NoPortProbe {
		check.Status = "skipped"
		return []PortCheck{check}
	}

	port, err := xrayplan.SelectUDPPortInRange(opts.SidecarPortStart, opts.SidecarPortEnd, xrayplan.UDPPortBusyChecker(opts.SidecarListen))
	if err != nil {
		check.Status = StatusError
		check.Error = err.Error()
		return []PortCheck{check}
	}
	check.Port = port
	return []PortCheck{check}
}

func checkDocker(ctx context.Context, commands []CommandCheck) DockerInfo {
	if !commandAvailable(commands, "docker") {
		return DockerInfo{Status: StatusMissing}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	output, err := cmd.Output()
	if err != nil {
		return DockerInfo{Status: StatusError, Error: strings.TrimSpace(err.Error())}
	}
	return DockerInfo{Available: true, Status: StatusOK, Version: strings.TrimSpace(string(output))}
}

func checkBackendReachability(ctx context.Context, inbounds []xrayplan.VLESSInbound) []EndpointCheck {
	checks := make([]EndpointCheck, 0, len(inbounds))
	for _, inbound := range inbounds {
		address := inbound.BackendAddress()
		check := EndpointCheck{Name: inbound.Tag, Network: "tcp", Address: address, Status: StatusOK}
		dialCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		dialer := net.Dialer{}
		conn, err := dialer.DialContext(dialCtx, "tcp", address)
		cancel()
		if err != nil {
			check.Status = StatusWarn
			check.Error = err.Error()
		} else {
			_ = conn.Close()
		}
		checks = append(checks, check)
	}
	return checks
}

func summarize(report Report) []string {
	var summary []string
	if len(report.XrayServices) == 0 {
		summary = append(summary, "no Xray systemd unit found under selected root")
	} else {
		summary = append(summary, fmt.Sprintf("found %d Xray service candidate(s)", len(report.XrayServices)))
	}
	if report.SelectedConfig == "" {
		summary = append(summary, "no usable Xray VLESS TCP config selected")
	} else {
		summary = append(summary, fmt.Sprintf("selected Xray config %s", report.SelectedConfig))
		summary = append(summary, fmt.Sprintf("found %d VLESS TCP inbound(s)", len(report.VLESSInbounds)))
	}
	for _, port := range report.PortChecks {
		if port.Status == StatusOK {
			summary = append(summary, fmt.Sprintf("sidecar UDP port %d is available on %s", port.Port, port.Address))
		} else if port.Status == "skipped" {
			summary = append(summary, "sidecar UDP port probe skipped")
		} else {
			summary = append(summary, fmt.Sprintf("sidecar UDP port check failed: %s", port.Error))
		}
	}
	return summary
}

func serviceConfidence(name, body string) string {
	lowerName := strings.ToLower(name)
	lowerBody := strings.ToLower(body)
	if strings.Contains(lowerName, "xray") && strings.Contains(lowerBody, "xray") {
		return "high"
	}
	if strings.Contains(lowerBody, "xray") {
		return "medium"
	}
	return "low"
}

func extractConfigPath(text string) string {
	fields := strings.Fields(text)
	for i, field := range fields {
		if (field == "-config" || field == "--config" || field == "-c") && i+1 < len(fields) {
			return strings.Trim(fields[i+1], "'\"")
		}
		for _, prefix := range []string{"-config=", "--config="} {
			if strings.HasPrefix(field, prefix) {
				return strings.Trim(strings.TrimPrefix(field, prefix), "'\"")
			}
		}
	}
	return ""
}

func splitPIDCommand(line string) (string, string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return "", ""
	}
	if _, err := strconv.Atoi(parts[0]); err == nil && len(parts) > 1 {
		return parts[0], strings.TrimSpace(strings.TrimPrefix(line, parts[0]))
	}
	return "", line
}

func rootedPath(root, path string) string {
	if root == "" || root == "/" {
		return filepath.Clean(path)
	}
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) {
		clean = strings.TrimPrefix(clean, string(os.PathSeparator))
	}
	return filepath.Join(root, clean)
}

func commandAvailable(commands []CommandCheck, name string) bool {
	for _, command := range commands {
		if command.Name == name && command.Status == StatusOK {
			return true
		}
	}
	return false
}
