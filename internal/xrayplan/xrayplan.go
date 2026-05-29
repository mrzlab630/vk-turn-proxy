package xrayplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
)

const (
	DefaultSidecarListen    = "0.0.0.0"
	DefaultSidecarPortStart = 56000
	DefaultSidecarPortEnd   = 56100
	DefaultXrayServiceName  = "xray.service"
)

var ErrNoVLESSTCPInbound = errors.New("no VLESS TCP inbound found")

type VLESSInbound struct {
	Tag         string `json:"tag,omitempty"`
	Listen      string `json:"listen,omitempty"`
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	Network     string `json:"network"`
	Security    string `json:"security"`
	ClientCount int    `json:"client_count"`
}

func (in VLESSInbound) BackendAddress() string {
	host := normalizeBackendHost(in.Listen)
	return net.JoinHostPort(host, strconv.Itoa(in.Port))
}

type Plan struct {
	ReadOnly        bool         `json:"read_only"`
	XrayConfigPath  string       `json:"xray_config_path"`
	XrayServiceName string       `json:"xray_service_name"`
	SelectedInbound VLESSInbound `json:"selected_inbound"`
	BackendAddress  string       `json:"backend_address"`
	SidecarListen   string       `json:"sidecar_listen"`
	SidecarPort     int          `json:"sidecar_port"`
	SidecarAddress  string       `json:"sidecar_address"`
	ServerCommand   []string     `json:"server_command"`
	WillCreate      []string     `json:"will_create"`
	WillNotTouch    []string     `json:"will_not_touch"`
	Warnings        []string     `json:"warnings,omitempty"`
}

type PlanOptions struct {
	XrayConfigPath  string
	XrayServiceName string
	SidecarListen   string
	PortStart       int
	PortEnd         int
	PortBusy        UDPPortBusyFunc
}

type UDPPortBusyFunc func(port int) (bool, error)

func DefaultPlanOptions() PlanOptions {
	return PlanOptions{
		XrayServiceName: DefaultXrayServiceName,
		SidecarListen:   DefaultSidecarListen,
		PortStart:       DefaultSidecarPortStart,
		PortEnd:         DefaultSidecarPortEnd,
	}
}

func ParseConfigFile(path string) ([]VLESSInbound, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Xray config: %w", err)
	}
	return ParseConfig(data)
}

func ParseConfig(data []byte) ([]VLESSInbound, error) {
	var cfg rawConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse Xray config JSON: %w", err)
	}

	inbounds := make([]VLESSInbound, 0, len(cfg.Inbounds))
	for _, raw := range cfg.Inbounds {
		if !strings.EqualFold(raw.Protocol, "vless") {
			continue
		}

		network := strings.TrimSpace(raw.StreamSettings.Network)
		if network == "" {
			network = "tcp"
		}
		if !strings.EqualFold(network, "tcp") {
			continue
		}

		port, err := parseXrayPort(raw.Port)
		if err != nil {
			return nil, fmt.Errorf("parse inbound %s port: %w", inboundName(raw), err)
		}

		security := strings.TrimSpace(raw.StreamSettings.Security)
		if security == "" {
			security = "none"
		}

		inbounds = append(inbounds, VLESSInbound{
			Tag:         raw.Tag,
			Listen:      raw.Listen,
			Port:        port,
			Protocol:    strings.ToLower(raw.Protocol),
			Network:     strings.ToLower(network),
			Security:    strings.ToLower(security),
			ClientCount: len(raw.Settings.Clients),
		})
	}

	return inbounds, nil
}

func SelectUDPPortInRange(start, end int, isBusy UDPPortBusyFunc) (int, error) {
	if start <= 0 || end <= 0 || start > 65535 || end > 65535 || start > end {
		return 0, fmt.Errorf("invalid UDP port range: %d..%d", start, end)
	}
	if isBusy == nil {
		return 0, fmt.Errorf("UDP port busy checker is required")
	}

	for port := start; port <= end; port++ {
		busy, err := isBusy(port)
		if err != nil {
			return 0, fmt.Errorf("check UDP port %d: %w", port, err)
		}
		if !busy {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free UDP port in range %d..%d", start, end)
}

func UDPPortBusyChecker(listenHost string) UDPPortBusyFunc {
	host := strings.TrimSpace(listenHost)
	if host == "" {
		host = DefaultSidecarListen
	}

	return func(port int) (bool, error) {
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		conn, err := (&net.ListenConfig{}).ListenPacket(context.Background(), "udp", addr)
		if err != nil {
			if isAddressInUse(err) {
				return true, nil
			}
			return false, err
		}
		if err := conn.Close(); err != nil {
			return false, err
		}
		return false, nil
	}
}

func BuildPlan(opts PlanOptions) (Plan, error) {
	defaults := DefaultPlanOptions()
	if opts.XrayServiceName == "" {
		opts.XrayServiceName = defaults.XrayServiceName
	}
	if opts.SidecarListen == "" {
		opts.SidecarListen = defaults.SidecarListen
	}
	if opts.PortStart == 0 {
		opts.PortStart = defaults.PortStart
	}
	if opts.PortEnd == 0 {
		opts.PortEnd = defaults.PortEnd
	}
	if opts.PortBusy == nil {
		opts.PortBusy = UDPPortBusyChecker(opts.SidecarListen)
	}
	if strings.TrimSpace(opts.XrayConfigPath) == "" {
		return Plan{}, fmt.Errorf("Xray config path is required")
	}

	inbounds, err := ParseConfigFile(opts.XrayConfigPath)
	if err != nil {
		return Plan{}, err
	}
	if len(inbounds) == 0 {
		return Plan{}, ErrNoVLESSTCPInbound
	}

	port, err := SelectUDPPortInRange(opts.PortStart, opts.PortEnd, opts.PortBusy)
	if err != nil {
		return Plan{}, err
	}

	selected := inbounds[0]
	backendAddress := selected.BackendAddress()
	sidecarAddress := net.JoinHostPort(opts.SidecarListen, strconv.Itoa(port))
	plan := Plan{
		ReadOnly:        true,
		XrayConfigPath:  opts.XrayConfigPath,
		XrayServiceName: opts.XrayServiceName,
		SelectedInbound: selected,
		BackendAddress:  backendAddress,
		SidecarListen:   opts.SidecarListen,
		SidecarPort:     port,
		SidecarAddress:  sidecarAddress,
		ServerCommand: []string{
			"vk-turn-proxy-server",
			"-listen", sidecarAddress,
			"-connect", backendAddress,
			"-vless",
		},
		WillCreate: []string{
			"/etc/vkturn/server.json",
			"/opt/vk-turn-proxy/vk-turn-proxy-server",
			"/etc/systemd/system/vkturn-server.service",
			"/var/log/vk-turn-proxy/",
		},
		WillNotTouch: []string{
			opts.XrayConfigPath,
			opts.XrayServiceName,
			"Xray VLESS users, inbounds, routing, DNS, and outbounds",
			"host firewall rules",
		},
	}
	if len(inbounds) > 1 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("found %d VLESS TCP inbounds; selected the first one", len(inbounds)))
	}
	if strings.EqualFold(selected.Security, "tls") || strings.EqualFold(selected.Security, "reality") {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("selected inbound security is %s; backend compatibility must be verified before install", selected.Security))
	}

	return plan, nil
}

type rawConfig struct {
	Inbounds []rawInbound `json:"inbounds"`
}

type rawInbound struct {
	Tag            string            `json:"tag"`
	Listen         string            `json:"listen"`
	Port           json.RawMessage   `json:"port"`
	Protocol       string            `json:"protocol"`
	Settings       rawInboundSetting `json:"settings"`
	StreamSettings rawStreamSettings `json:"streamSettings"`
}

type rawInboundSetting struct {
	Clients []json.RawMessage `json:"clients"`
}

type rawStreamSettings struct {
	Network  string `json:"network"`
	Security string `json:"security"`
}

func parseXrayPort(data json.RawMessage) (int, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("port is required")
	}

	var number int
	if err := json.Unmarshal(data, &number); err == nil {
		return number, validatePort(number)
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return 0, fmt.Errorf("port must be a number or numeric string")
	}
	text = strings.TrimSpace(text)
	if strings.Contains(text, "-") {
		return 0, fmt.Errorf("port ranges are not supported for sidecar backend planning: %s", text)
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("parse port %q: %w", text, err)
	}
	return parsed, validatePort(parsed)
}

func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port out of range: %d", port)
	}
	return nil
}

func normalizeBackendHost(listen string) string {
	host := strings.TrimSpace(listen)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return "127.0.0.1"
	}
	return strings.Trim(host, "[]")
}

func inboundName(raw rawInbound) string {
	if strings.TrimSpace(raw.Tag) != "" {
		return raw.Tag
	}
	if strings.TrimSpace(raw.Protocol) != "" {
		return raw.Protocol
	}
	return "<unnamed>"
}

func isAddressInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "address already in use")
}
