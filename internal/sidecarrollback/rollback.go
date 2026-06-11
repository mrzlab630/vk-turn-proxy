package sidecarrollback

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cacggghp/vk-turn-proxy/internal/sidecarinstall"
)

var ErrMissingManifest = errors.New("sidecar install manifest is missing under root")

type Options struct {
	Root      string
	DryRun    bool
	Generated time.Time
}

type Result struct {
	DryRun       bool      `json:"dry_run"`
	Root         string    `json:"root"`
	GeneratedAt  time.Time `json:"generated_at"`
	ManifestPath string    `json:"manifest_path"`
	Removals     []Removal `json:"removals"`
	WillNotTouch []string  `json:"will_not_touch"`
	SkippedApply []string  `json:"skipped_apply,omitempty"`
	Applied      bool      `json:"applied"`
	Message      string    `json:"message"`
}

type Removal struct {
	Kind       string `json:"kind"`
	Path       string `json:"path"`
	RootedPath string `json:"rooted_path"`
	Exists     bool   `json:"exists"`
}

func BuildDryRun(opts Options) (Result, error) {
	if !opts.DryRun {
		return Result{}, fmt.Errorf("only dry-run uninstall is supported on the development workflow")
	}
	if strings.TrimSpace(opts.Root) == "" || opts.Root == "/" {
		return Result{}, fmt.Errorf("--root must point to a fixture or temporary directory for dry-run uninstall")
	}
	if opts.Generated.IsZero() {
		opts.Generated = time.Now().UTC()
	}

	manifestPath := rootedPath(opts.Root, sidecarinstall.DefaultManifest)
	manifest, err := readManifest(manifestPath)
	if err != nil {
		result := Result{
			DryRun:       true,
			Root:         opts.Root,
			GeneratedAt:  opts.Generated,
			ManifestPath: manifestPath,
			Message:      "sidecar install manifest is missing; nothing can be safely removed",
		}
		if errors.Is(err, os.ErrNotExist) {
			return result, ErrMissingManifest
		}
		return result, err
	}

	paths := plannedRemovalPaths(manifest)
	removals := make([]Removal, 0, len(paths))
	for _, item := range paths {
		rooted := rootedPath(opts.Root, item.path)
		removals = append(removals, Removal{
			Kind:       item.kind,
			Path:       item.path,
			RootedPath: rooted,
			Exists:     pathExists(rooted),
		})
	}

	return Result{
		DryRun:       true,
		Root:         opts.Root,
		GeneratedAt:  opts.Generated,
		ManifestPath: manifestPath,
		Removals:     removals,
		WillNotTouch: []string{
			manifest.XrayConfigPath,
			manifest.XrayService,
			"Xray VLESS users, inbounds, routing, DNS, and outbounds",
			"host firewall rules",
		},
		SkippedApply: []string{
			"systemctl disable --now " + manifest.ServiceName,
			"systemctl daemon-reload",
			"restart or reload Xray",
		},
		Message: fmt.Sprintf("planned removal of %d sidecar-owned path(s)", len(removals)),
	}, nil
}

func ApplyDryRun(result Result) (Result, error) {
	if !result.DryRun {
		return result, fmt.Errorf("refusing to apply non-dry-run rollback")
	}
	if strings.TrimSpace(result.Root) == "" || result.Root == "/" {
		return result, fmt.Errorf("refusing to apply rollback outside a dry-run root")
	}
	for _, removal := range result.Removals {
		if !strings.HasPrefix(filepath.Clean(removal.RootedPath), filepath.Clean(result.Root)+string(os.PathSeparator)) && filepath.Clean(removal.RootedPath) != filepath.Clean(result.Root) {
			return result, fmt.Errorf("refusing to remove path outside root: %s", removal.RootedPath)
		}
		if err := os.RemoveAll(removal.RootedPath); err != nil {
			return result, fmt.Errorf("remove %s: %w", removal.RootedPath, err)
		}
	}
	result.Applied = true
	result.Message = fmt.Sprintf("removed %d sidecar-owned path(s) under dry-run root", len(result.Removals))
	return result, nil
}

type removalPath struct {
	kind string
	path string
}

func plannedRemovalPaths(manifest sidecarinstall.Manifest) []removalPath {
	paths := []removalPath{
		{kind: "file", path: manifest.UnitPath},
		{kind: "file", path: manifest.EnvPath},
		{kind: "file", path: manifest.ConfigPath},
		{kind: "file", path: manifest.BinaryPath},
		{kind: "file", path: sidecarinstall.DefaultManifest},
		{kind: "file", path: "/run/vkturn/server-state.json"},
		{kind: "file", path: "/run/vkturn/lifecycle.log"},
		{kind: "directory", path: manifest.LogDir},
		{kind: "directory", path: filepath.Dir(manifest.BinaryPath)},
		{kind: "directory", path: filepath.Dir(manifest.ConfigPath)},
	}

	seen := make(map[string]bool, len(paths))
	unique := make([]removalPath, 0, len(paths))
	for _, item := range paths {
		if strings.TrimSpace(item.path) == "" || seen[item.path] {
			continue
		}
		seen[item.path] = true
		unique = append(unique, item)
	}
	return unique
}

func readManifest(path string) (sidecarinstall.Manifest, error) {
	// #nosec G304 -- manifest path is derived from the required dry-run root.
	data, err := os.ReadFile(path)
	if err != nil {
		return sidecarinstall.Manifest{}, err
	}
	var manifest sidecarinstall.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return sidecarinstall.Manifest{}, fmt.Errorf("parse install manifest: %w", err)
	}
	return manifest, nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func rootedPath(root, path string) string {
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) {
		clean = strings.TrimPrefix(clean, string(os.PathSeparator))
	}
	return filepath.Join(root, clean)
}
