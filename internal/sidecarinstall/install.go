package sidecarinstall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cacggghp/vk-turn-proxy/internal/xrayplan"
)

const (
	DefaultServiceName = "vkturn-server.service"
	DefaultBinaryPath  = "/opt/vk-turn-proxy/vk-turn-proxy-server"
	DefaultConfigPath  = "/etc/vkturn/server.json"
	DefaultEnvPath     = "/etc/default/vkturn-server"
	DefaultUnitPath    = "/etc/systemd/system/vkturn-server.service"
	DefaultLogDir      = "/var/log/vk-turn-proxy"
	DefaultManifest    = "/etc/vkturn/install-manifest.json"
	parentDirMode      = 0o750
)

type Options struct {
	Root        string
	DryRun      bool
	Plan        xrayplan.Plan
	GeneratedAt time.Time
}

type Result struct {
	DryRun          bool       `json:"dry_run"`
	Root            string     `json:"root"`
	GeneratedAt     time.Time  `json:"generated_at"`
	Artifacts       []Artifact `json:"artifacts"`
	WillNotTouch    []string   `json:"will_not_touch"`
	SkippedApply    []string   `json:"skipped_apply,omitempty"`
	SystemdCommands []string   `json:"systemd_commands"`
}

type Artifact struct {
	Kind       string `json:"kind"`
	Path       string `json:"path"`
	RootedPath string `json:"rooted_path"`
	Mode       string `json:"mode"`
	Content    string `json:"content,omitempty"`
	Source     string `json:"source,omitempty"`
}

type Manifest struct {
	ServiceName    string               `json:"service_name"`
	BinaryPath     string               `json:"binary_path"`
	ConfigPath     string               `json:"config_path"`
	EnvPath        string               `json:"env_path"`
	UnitPath       string               `json:"unit_path"`
	LogDir         string               `json:"log_dir"`
	XrayConfigPath string               `json:"xray_config_path"`
	XrayService    string               `json:"xray_service"`
	BackendAddress string               `json:"backend_address"`
	SidecarAddress string               `json:"sidecar_address"`
	ServerConfig   ServerConfigArtifact `json:"server_config"`
	GeneratedAt    time.Time            `json:"generated_at"`
}

type ServerConfigArtifact struct {
	ListenAddr     string `json:"listen_addr"`
	ConnectAddr    string `json:"connect_addr"`
	VLESSMode      bool   `json:"vless_mode"`
	CheckBackend   bool   `json:"check_backend"`
	BackendNetwork string `json:"backend_network"`
	LogLevel       string `json:"log_level,omitempty"`
	ServiceName    string `json:"service_name,omitempty"`
}

func BuildDryRun(opts Options) (Result, error) {
	if !opts.DryRun {
		return Result{}, fmt.Errorf("only dry-run install is supported on the development workflow")
	}
	if strings.TrimSpace(opts.Root) == "" || opts.Root == "/" {
		return Result{}, fmt.Errorf("--root must point to a fixture or temporary directory for dry-run install")
	}
	if strings.TrimSpace(opts.Plan.BackendAddress) == "" || strings.TrimSpace(opts.Plan.SidecarAddress) == "" {
		return Result{}, fmt.Errorf("install plan requires backend and sidecar addresses")
	}
	if opts.GeneratedAt.IsZero() {
		opts.GeneratedAt = time.Now().UTC()
	}

	manifest := BuildManifest(opts.Plan, opts.GeneratedAt)
	serverConfig, err := RenderServerConfig(manifest.ServerConfig)
	if err != nil {
		return Result{}, err
	}
	manifestContent, err := RenderManifest(manifest)
	if err != nil {
		return Result{}, err
	}
	unit := RenderSystemdUnit(manifest)
	env := RenderEnvironment(manifest)

	artifacts := []Artifact{
		{
			Kind:       "directory",
			Path:       filepath.Dir(DefaultConfigPath),
			RootedPath: rootedPath(opts.Root, filepath.Dir(DefaultConfigPath)),
			Mode:       "0750",
		},
		{
			Kind:       "directory",
			Path:       filepath.Dir(DefaultBinaryPath),
			RootedPath: rootedPath(opts.Root, filepath.Dir(DefaultBinaryPath)),
			Mode:       "0755",
		},
		{
			Kind:       "directory",
			Path:       DefaultLogDir,
			RootedPath: rootedPath(opts.Root, DefaultLogDir),
			Mode:       "0750",
		},
		{
			Kind:       "file",
			Path:       DefaultConfigPath,
			RootedPath: rootedPath(opts.Root, DefaultConfigPath),
			Mode:       "0600",
			Content:    serverConfig,
		},
		{
			Kind:       "file",
			Path:       DefaultEnvPath,
			RootedPath: rootedPath(opts.Root, DefaultEnvPath),
			Mode:       "0600",
			Content:    env,
		},
		{
			Kind:       "file",
			Path:       DefaultUnitPath,
			RootedPath: rootedPath(opts.Root, DefaultUnitPath),
			Mode:       "0644",
			Content:    unit,
		},
		{
			Kind:       "file",
			Path:       DefaultManifest,
			RootedPath: rootedPath(opts.Root, DefaultManifest),
			Mode:       "0600",
			Content:    manifestContent,
		},
		{
			Kind:       "binary",
			Path:       DefaultBinaryPath,
			RootedPath: rootedPath(opts.Root, DefaultBinaryPath),
			Mode:       "0755",
			Source:     "current vk-turn-proxy-server build artifact",
		},
	}

	return Result{
		DryRun:      true,
		Root:        opts.Root,
		GeneratedAt: opts.GeneratedAt,
		Artifacts:   artifacts,
		WillNotTouch: []string{
			opts.Plan.XrayConfigPath,
			opts.Plan.XrayServiceName,
			"Xray VLESS users, inbounds, routing, DNS, and outbounds",
			"host firewall rules",
		},
		SkippedApply: []string{
			"write files to real /etc, /opt, or /var",
			"systemctl daemon-reload",
			"systemctl enable --now " + DefaultServiceName,
			"restart or reload Xray",
		},
		SystemdCommands: []string{
			"systemctl daemon-reload",
			"systemctl enable --now " + DefaultServiceName,
			"systemctl status " + DefaultServiceName,
		},
	}, nil
}

func BuildManifest(plan xrayplan.Plan, generatedAt time.Time) Manifest {
	return Manifest{
		ServiceName:    DefaultServiceName,
		BinaryPath:     DefaultBinaryPath,
		ConfigPath:     DefaultConfigPath,
		EnvPath:        DefaultEnvPath,
		UnitPath:       DefaultUnitPath,
		LogDir:         DefaultLogDir,
		XrayConfigPath: plan.XrayConfigPath,
		XrayService:    plan.XrayServiceName,
		BackendAddress: plan.BackendAddress,
		SidecarAddress: plan.SidecarAddress,
		ServerConfig: ServerConfigArtifact{
			ListenAddr:     plan.SidecarAddress,
			ConnectAddr:    plan.BackendAddress,
			VLESSMode:      true,
			CheckBackend:   true,
			BackendNetwork: "tcp",
			LogLevel:       "info",
			ServiceName:    strings.TrimSuffix(DefaultServiceName, ".service"),
		},
		GeneratedAt: generatedAt,
	}
}

func RenderServerConfig(cfg ServerConfigArtifact) (string, error) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("render server config: %w", err)
	}
	return string(data) + "\n", nil
}

func RenderManifest(manifest Manifest) (string, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("render install manifest: %w", err)
	}
	return string(data) + "\n", nil
}

func RenderEnvironment(manifest Manifest) string {
	vars := map[string]string{
		"VKTURN_CONFIG":  manifest.ConfigPath,
		"VKTURN_LOG_DIR": manifest.LogDir,
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&builder, "%s=%s\n", key, shellQuote(vars[key]))
	}
	return builder.String()
}

func RenderSystemdUnit(manifest Manifest) string {
	return fmt.Sprintf(`[Unit]
Description=VK TURN VLESS Sidecar
After=network-online.target %s
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=-%s
ExecStart=%s -config %s
Restart=on-failure
RestartSec=5s
User=nobody
Group=nogroup
RuntimeDirectory=vkturn
StateDirectory=vkturn
LogsDirectory=vk-turn-proxy
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=%s
StandardOutput=append:%s/server.log
StandardError=append:%s/server.err

[Install]
WantedBy=multi-user.target
`, manifest.XrayService, manifest.EnvPath, manifest.BinaryPath, manifest.ConfigPath, manifest.LogDir, manifest.LogDir, manifest.LogDir)
}

func WriteDryRunArtifacts(result Result) error {
	if !result.DryRun {
		return fmt.Errorf("refusing to write non-dry-run artifacts")
	}
	if strings.TrimSpace(result.Root) == "" || result.Root == "/" {
		return fmt.Errorf("refusing to write dry-run artifacts without a fixture root")
	}
	for _, artifact := range result.Artifacts {
		if !isWithinRoot(result.Root, artifact.RootedPath) {
			return fmt.Errorf("refusing to write artifact outside root: %s", artifact.RootedPath)
		}
		mode, err := parseFileMode(artifact.Mode)
		if err != nil {
			return fmt.Errorf("parse mode for %s: %w", artifact.Path, err)
		}
		switch artifact.Kind {
		case "directory":
			if err := os.MkdirAll(artifact.RootedPath, mode); err != nil {
				return fmt.Errorf("create directory %s: %w", artifact.RootedPath, err)
			}
		case "file":
			// #nosec G301 -- dry-run artifacts mirror system directories; final artifact
			// permissions are enforced per artifact mode below.
			if err := os.MkdirAll(filepath.Dir(artifact.RootedPath), parentDirMode); err != nil {
				return fmt.Errorf("create parent %s: %w", filepath.Dir(artifact.RootedPath), err)
			}
			if err := os.WriteFile(artifact.RootedPath, []byte(artifact.Content), mode); err != nil {
				return fmt.Errorf("write file %s: %w", artifact.RootedPath, err)
			}
		case "binary":
			// #nosec G301 -- executable install parent directories need traverse access;
			// this dry-run never writes outside the supplied root.
			if err := os.MkdirAll(filepath.Dir(artifact.RootedPath), parentDirMode); err != nil {
				return fmt.Errorf("create parent %s: %w", filepath.Dir(artifact.RootedPath), err)
			}
			content := []byte("dry-run placeholder for vk-turn-proxy-server\n")
			if err := os.WriteFile(artifact.RootedPath, content, mode); err != nil {
				return fmt.Errorf("write binary placeholder %s: %w", artifact.RootedPath, err)
			}
		default:
			return fmt.Errorf("unsupported artifact kind %q", artifact.Kind)
		}
	}
	return nil
}

func parseFileMode(value string) (os.FileMode, error) {
	parsed, err := strconv.ParseUint(value, 8, 32)
	if err != nil {
		return 0, err
	}
	return os.FileMode(parsed), nil
}

func rootedPath(root, path string) string {
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) {
		clean = strings.TrimPrefix(clean, string(os.PathSeparator))
	}
	return filepath.Join(root, clean)
}

func isWithinRoot(root, path string) bool {
	cleanRoot := filepath.Clean(root)
	cleanPath := filepath.Clean(path)
	return cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
