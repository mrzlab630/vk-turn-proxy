package sidecarlifecycle

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

const (
	ActionStatus  = "status"
	ActionStart   = "start"
	ActionStop    = "stop"
	ActionRestart = "restart"
	ActionLogs    = "logs"

	StateRunning = "running"
	StateStopped = "stopped"
	StateMissing = "missing"
)

var ErrMissingUnit = errors.New("sidecar unit is not installed under root")

type Options struct {
	Root      string
	DryRun    bool
	Now       time.Time
	LogLines  int
	LogStream string
}

type Result struct {
	Action       string    `json:"action"`
	DryRun       bool      `json:"dry_run"`
	Root         string    `json:"root"`
	ServiceName  string    `json:"service_name"`
	State        string    `json:"state"`
	UnitPath     string    `json:"unit_path"`
	ManifestPath string    `json:"manifest_path"`
	JournalPath  string    `json:"journal_path"`
	LogPath      string    `json:"log_path,omitempty"`
	Logs         []string  `json:"logs,omitempty"`
	Message      string    `json:"message"`
	Changed      bool      `json:"changed"`
	Timestamp    time.Time `json:"timestamp"`
}

type stateFile struct {
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
}

func Status(opts Options) (Result, error) {
	return run(ActionStatus, opts)
}

func Start(opts Options) (Result, error) {
	return run(ActionStart, opts)
}

func Stop(opts Options) (Result, error) {
	return run(ActionStop, opts)
}

func Restart(opts Options) (Result, error) {
	return run(ActionRestart, opts)
}

func Logs(opts Options) (Result, error) {
	return run(ActionLogs, opts)
}

func run(action string, opts Options) (Result, error) {
	if !opts.DryRun {
		return Result{}, fmt.Errorf("%s currently requires --dry-run on the development workflow", action)
	}
	if strings.TrimSpace(opts.Root) == "" || opts.Root == "/" {
		return Result{}, fmt.Errorf("--root must point to a fixture or temporary directory for dry-run lifecycle")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if opts.LogLines <= 0 {
		opts.LogLines = 50
	}
	if opts.LogStream == "" {
		opts.LogStream = "server.log"
	}

	paths := buildPaths(opts.Root, opts.LogStream)
	result := Result{
		Action:       action,
		DryRun:       true,
		Root:         opts.Root,
		ServiceName:  sidecarinstall.DefaultServiceName,
		UnitPath:     paths.unitPath,
		ManifestPath: paths.manifestPath,
		JournalPath:  paths.journalPath,
		LogPath:      paths.logPath,
		Timestamp:    opts.Now,
	}

	if _, err := os.Stat(paths.unitPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.State = StateMissing
			result.Message = "sidecar unit is missing under dry-run root"
			return result, ErrMissingUnit
		}
		return result, fmt.Errorf("stat unit: %w", err)
	}
	if _, err := os.Stat(paths.manifestPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.State = StateMissing
			result.Message = "sidecar install manifest is missing under dry-run root"
			return result, ErrMissingUnit
		}
		return result, fmt.Errorf("stat manifest: %w", err)
	}

	currentState, err := readState(paths.statePath)
	if err != nil {
		return result, err
	}

	switch action {
	case ActionStatus:
		result.State = currentState
		result.Message = fmt.Sprintf("%s is %s under dry-run root", result.ServiceName, currentState)
	case ActionStart:
		result.State = StateRunning
		result.Changed = currentState != StateRunning
		result.Message = fmt.Sprintf("planned start for %s under dry-run root", result.ServiceName)
		if err := writeState(paths.statePath, stateFile{State: StateRunning, UpdatedAt: opts.Now}); err != nil {
			return result, err
		}
		if err := appendJournal(paths.journalPath, opts.Now, "start", result.Message); err != nil {
			return result, err
		}
	case ActionStop:
		result.State = StateStopped
		result.Changed = currentState != StateStopped
		result.Message = fmt.Sprintf("planned stop for %s under dry-run root", result.ServiceName)
		if err := writeState(paths.statePath, stateFile{State: StateStopped, UpdatedAt: opts.Now}); err != nil {
			return result, err
		}
		if err := appendJournal(paths.journalPath, opts.Now, "stop", result.Message); err != nil {
			return result, err
		}
	case ActionRestart:
		result.State = StateRunning
		result.Changed = true
		result.Message = fmt.Sprintf("planned restart for %s under dry-run root", result.ServiceName)
		if err := writeState(paths.statePath, stateFile{State: StateRunning, UpdatedAt: opts.Now}); err != nil {
			return result, err
		}
		if err := appendJournal(paths.journalPath, opts.Now, "restart", result.Message); err != nil {
			return result, err
		}
	case ActionLogs:
		logs, err := readTail(paths.logPath, opts.LogLines)
		if err != nil {
			return result, err
		}
		result.State = currentState
		result.Logs = logs
		result.Message = fmt.Sprintf("read %d log line(s) from %s", len(logs), paths.logPath)
	default:
		return result, fmt.Errorf("unsupported lifecycle action: %s", action)
	}

	return result, nil
}

type lifecyclePaths struct {
	unitPath     string
	manifestPath string
	statePath    string
	journalPath  string
	logPath      string
}

func buildPaths(root, logStream string) lifecyclePaths {
	return lifecyclePaths{
		unitPath:     rootedPath(root, sidecarinstall.DefaultUnitPath),
		manifestPath: rootedPath(root, sidecarinstall.DefaultManifest),
		statePath:    rootedPath(root, "/run/vkturn/server-state.json"),
		journalPath:  rootedPath(root, "/run/vkturn/lifecycle.log"),
		logPath:      rootedPath(root, filepath.Join(sidecarinstall.DefaultLogDir, logStream)),
	}
}

func readState(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StateStopped, nil
		}
		return "", fmt.Errorf("read state: %w", err)
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return "", fmt.Errorf("parse state: %w", err)
	}
	if state.State == "" {
		return StateStopped, nil
	}
	return state.State, nil
}

func writeState(path string, state stateFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("render state: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

func appendJournal(path string, timestamp time.Time, action, message string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create journal dir: %w", err)
	}
	line := fmt.Sprintf("%s action=%s %s\n", timestamp.Format(time.RFC3339), action, message)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open journal: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(line); err != nil {
		return fmt.Errorf("write journal: %w", err)
	}
	return nil
}

func readTail(path string, maxLines int) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read logs: %w", err)
	}
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return nil, nil
	}
	lines := strings.Split(trimmed, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines, nil
}

func rootedPath(root, path string) string {
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) {
		clean = strings.TrimPrefix(clean, string(os.PathSeparator))
	}
	return filepath.Join(root, clean)
}
