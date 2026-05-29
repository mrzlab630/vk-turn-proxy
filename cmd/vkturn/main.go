package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cacggghp/vk-turn-proxy/internal/sidecarinstall"
	"github.com/cacggghp/vk-turn-proxy/internal/sidecarlifecycle"
	"github.com/cacggghp/vk-turn-proxy/internal/sidecarrollback"
	"github.com/cacggghp/vk-turn-proxy/internal/xraydoctor"
	"github.com/cacggghp/vk-turn-proxy/internal/xrayplan"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return fmt.Errorf("command is required")
	}

	switch args[0] {
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "server":
		return runServer(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func runDoctor(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("vkturn doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "/", "filesystem root to inspect read-only")
	processSnapshot := fs.String("process-snapshot", "", "optional process snapshot file for fixture-based discovery")
	sidecarListen := fs.String("sidecar-listen", xrayplan.DefaultSidecarListen, "sidecar UDP listen host")
	portStart := fs.Int("sidecar-port-start", xrayplan.DefaultSidecarPortStart, "first UDP sidecar port to try")
	portEnd := fs.Int("sidecar-port-end", xrayplan.DefaultSidecarPortEnd, "last UDP sidecar port to try")
	noPortProbe := fs.Bool("no-port-probe", false, "skip UDP bind/release availability probe")
	checkBackend := fs.Bool("check-backend", false, "try TCP dials to discovered VLESS backend targets")
	skipHostCommands := fs.Bool("skip-host-commands", false, "skip PATH and docker command checks for deterministic fixtures")
	jsonOutput := fs.Bool("json", false, "print JSON report")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected extra arguments: %s", strings.Join(fs.Args(), " "))
	}

	report := xraydoctor.Run(context.Background(), xraydoctor.Options{
		Root:                  *root,
		ProcessSnapshotPath:   *processSnapshot,
		SidecarListen:         *sidecarListen,
		SidecarPortStart:      *portStart,
		SidecarPortEnd:        *portEnd,
		NoPortProbe:           *noPortProbe,
		CheckBackendReachable: *checkBackend,
		SkipHostCommands:      *skipHostCommands,
	})
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	printHumanDoctor(stdout, report)
	return nil
}

func runServer(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printServerUsage(stderr)
		return fmt.Errorf("server subcommand is required")
	}

	switch args[0] {
	case "plan":
		return runServerPlan(args[1:], stdout, stderr)
	case "install":
		return runServerInstall(args[1:], stdout, stderr)
	case "uninstall":
		return runServerUninstall(args[1:], stdout, stderr)
	case "status", "start", "stop", "restart", "logs":
		return runServerLifecycle(args[0], args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printServerUsage(stdout)
		return nil
	default:
		printServerUsage(stderr)
		return fmt.Errorf("unknown server subcommand: %s", args[0])
	}
}

func runServerUninstall(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("vkturn server uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "fixture or temporary root for dry-run uninstall")
	dryRun := fs.Bool("dry-run", false, "plan sidecar artifact removal under --root without applying to the host")
	write := fs.Bool("write", false, "remove dry-run sidecar artifacts under --root")
	jsonOutput := fs.Bool("json", false, "print JSON rollback result")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected extra arguments: %s", strings.Join(fs.Args(), " "))
	}
	if !*dryRun {
		return fmt.Errorf("server uninstall currently requires --dry-run on the development workflow")
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("--root is required for --dry-run uninstall")
	}

	result, err := sidecarrollback.BuildDryRun(sidecarrollback.Options{Root: *root, DryRun: true})
	if err != nil {
		if errors.Is(err, sidecarrollback.ErrMissingManifest) {
			if *jsonOutput {
				encoder := json.NewEncoder(stdout)
				encoder.SetIndent("", "  ")
				_ = encoder.Encode(result)
			}
			return fmt.Errorf("%s: %w", result.Message, err)
		}
		return err
	}
	if *write {
		var applyErr error
		result, applyErr = sidecarrollback.ApplyDryRun(result)
		if applyErr != nil {
			return applyErr
		}
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	printHumanUninstall(stdout, result, *write)
	return nil
}

func runServerLifecycle(action string, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("vkturn server "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "fixture or temporary root for dry-run lifecycle")
	dryRun := fs.Bool("dry-run", false, "run lifecycle command against --root without host systemctl")
	jsonOutput := fs.Bool("json", false, "print JSON lifecycle result")
	logLines := fs.Int("lines", 50, "number of log lines for server logs")
	logStream := fs.String("stream", "server.log", "log stream filename under /var/log/vk-turn-proxy")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected extra arguments: %s", strings.Join(fs.Args(), " "))
	}
	if !*dryRun {
		return fmt.Errorf("server %s currently requires --dry-run on the development workflow", action)
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("--root is required for --dry-run lifecycle")
	}

	opts := sidecarlifecycle.Options{
		Root:      *root,
		DryRun:    true,
		LogLines:  *logLines,
		LogStream: *logStream,
	}
	result, err := runLifecycleAction(action, opts)
	if err != nil {
		if errors.Is(err, sidecarlifecycle.ErrMissingUnit) {
			if *jsonOutput {
				encoder := json.NewEncoder(stdout)
				encoder.SetIndent("", "  ")
				_ = encoder.Encode(result)
			}
			return fmt.Errorf("%s: %w", result.Message, err)
		}
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	printHumanLifecycle(stdout, result)
	return nil
}

func runLifecycleAction(action string, opts sidecarlifecycle.Options) (sidecarlifecycle.Result, error) {
	switch action {
	case sidecarlifecycle.ActionStatus:
		return sidecarlifecycle.Status(opts)
	case sidecarlifecycle.ActionStart:
		return sidecarlifecycle.Start(opts)
	case sidecarlifecycle.ActionStop:
		return sidecarlifecycle.Stop(opts)
	case sidecarlifecycle.ActionRestart:
		return sidecarlifecycle.Restart(opts)
	case sidecarlifecycle.ActionLogs:
		return sidecarlifecycle.Logs(opts)
	default:
		return sidecarlifecycle.Result{}, fmt.Errorf("unsupported lifecycle action: %s", action)
	}
}

func runServerInstall(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("vkturn server install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	xrayConfig := fs.String("xray-config", "", "path to existing Xray JSON config")
	xrayService := fs.String("xray-service", xrayplan.DefaultXrayServiceName, "existing Xray service name metadata")
	root := fs.String("root", "", "fixture or temporary root for dry-run artifacts")
	dryRun := fs.Bool("dry-run", false, "generate artifacts under --root without applying to the host")
	sidecarListen := fs.String("sidecar-listen", xrayplan.DefaultSidecarListen, "sidecar UDP listen host")
	portStart := fs.Int("sidecar-port-start", xrayplan.DefaultSidecarPortStart, "first UDP sidecar port to try")
	portEnd := fs.Int("sidecar-port-end", xrayplan.DefaultSidecarPortEnd, "last UDP sidecar port to try")
	noPortProbe := fs.Bool("no-port-probe", false, "skip UDP bind/release availability probe and pick the first port in range")
	writeArtifacts := fs.Bool("write", false, "write dry-run artifacts under --root")
	jsonOutput := fs.Bool("json", false, "print JSON install result")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected extra arguments: %s", strings.Join(fs.Args(), " "))
	}
	if !*dryRun {
		return fmt.Errorf("server install currently requires --dry-run on the development workflow")
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("--root is required for --dry-run install")
	}
	if strings.TrimSpace(*xrayConfig) == "" {
		return fmt.Errorf("--xray-config is required")
	}

	planOpts := xrayplan.PlanOptions{
		XrayConfigPath:  *xrayConfig,
		XrayServiceName: *xrayService,
		SidecarListen:   *sidecarListen,
		PortStart:       *portStart,
		PortEnd:         *portEnd,
	}
	if *noPortProbe {
		planOpts.PortBusy = func(port int) (bool, error) { return false, nil }
	}
	plan, err := xrayplan.BuildPlan(planOpts)
	if err != nil {
		if errors.Is(err, xrayplan.ErrNoVLESSTCPInbound) {
			return fmt.Errorf("no supported VLESS TCP inbound found in %s", *xrayConfig)
		}
		return err
	}

	result, err := sidecarinstall.BuildDryRun(sidecarinstall.Options{
		Root:   *root,
		DryRun: true,
		Plan:   plan,
	})
	if err != nil {
		return err
	}
	if *writeArtifacts {
		if err := sidecarinstall.WriteDryRunArtifacts(result); err != nil {
			return err
		}
	}

	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	printHumanInstall(stdout, result, *writeArtifacts)
	return nil
}

func runServerPlan(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("vkturn server plan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	xrayConfig := fs.String("xray-config", "", "path to existing Xray JSON config")
	xrayService := fs.String("xray-service", xrayplan.DefaultXrayServiceName, "existing Xray service name metadata")
	sidecarListen := fs.String("sidecar-listen", xrayplan.DefaultSidecarListen, "sidecar UDP listen host")
	portStart := fs.Int("sidecar-port-start", xrayplan.DefaultSidecarPortStart, "first UDP sidecar port to try")
	portEnd := fs.Int("sidecar-port-end", xrayplan.DefaultSidecarPortEnd, "last UDP sidecar port to try")
	noPortProbe := fs.Bool("no-port-probe", false, "skip UDP bind/release availability probe and pick the first port in range")
	jsonOutput := fs.Bool("json", false, "print JSON plan")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected extra arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*xrayConfig) == "" {
		return fmt.Errorf("--xray-config is required")
	}

	planOpts := xrayplan.PlanOptions{
		XrayConfigPath:  *xrayConfig,
		XrayServiceName: *xrayService,
		SidecarListen:   *sidecarListen,
		PortStart:       *portStart,
		PortEnd:         *portEnd,
	}
	if *noPortProbe {
		planOpts.PortBusy = func(port int) (bool, error) { return false, nil }
	}
	plan, err := xrayplan.BuildPlan(planOpts)
	if err != nil {
		if errors.Is(err, xrayplan.ErrNoVLESSTCPInbound) {
			return fmt.Errorf("no supported VLESS TCP inbound found in %s", *xrayConfig)
		}
		return err
	}

	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(plan)
	}
	printHumanPlan(stdout, plan)
	return nil
}

func printHumanPlan(w io.Writer, plan xrayplan.Plan) {
	fmt.Fprintln(w, "vkturn server plan")
	fmt.Fprintln(w, "mode: read-only, no files will be written and Xray will not be restarted")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Xray config: %s\n", plan.XrayConfigPath)
	fmt.Fprintf(w, "Xray service: %s\n", plan.XrayServiceName)
	fmt.Fprintf(w, "Selected inbound: tag=%s protocol=%s network=%s security=%s listen=%s port=%d clients=%d\n",
		valueOrDash(plan.SelectedInbound.Tag),
		plan.SelectedInbound.Protocol,
		plan.SelectedInbound.Network,
		plan.SelectedInbound.Security,
		valueOrDash(plan.SelectedInbound.Listen),
		plan.SelectedInbound.Port,
		plan.SelectedInbound.ClientCount,
	)
	fmt.Fprintf(w, "Backend target for sidecar: %s\n", plan.BackendAddress)
	fmt.Fprintf(w, "Selected sidecar UDP listen: %s\n", plan.SidecarAddress)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Server command: %s\n", strings.Join(plan.ServerCommand, " "))
	printList(w, "Will create", plan.WillCreate)
	printList(w, "Will not touch", plan.WillNotTouch)
	if len(plan.Warnings) > 0 {
		printList(w, "Warnings", plan.Warnings)
	}
}

func printHumanDoctor(w io.Writer, report xraydoctor.Report) {
	fmt.Fprintln(w, "vkturn doctor")
	fmt.Fprintln(w, "mode: read-only, no files will be written and services will not be restarted")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Root: %s\n", report.Root)
	fmt.Fprintf(w, "OS: %s/%s\n", report.OS.GOOS, report.OS.GOARCH)
	fmt.Fprintf(w, "Privileges: uid=%d root=%t\n", report.Privileges.UID, report.Privileges.IsRoot)
	printCommandChecks(w, report.Commands)
	printServiceCandidates(w, report.XrayServices)
	printProcessCandidates(w, report.XrayProcesses)
	printConfigCandidates(w, report.XrayConfigs)
	if report.SelectedConfig != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Selected Xray config: %s\n", report.SelectedConfig)
		for _, inbound := range report.VLESSInbounds {
			fmt.Fprintf(w, "- VLESS TCP inbound: tag=%s listen=%s port=%d security=%s backend=%s clients=%d\n",
				valueOrDash(inbound.Tag), valueOrDash(inbound.Listen), inbound.Port, inbound.Security, inbound.BackendAddress(), inbound.ClientCount)
		}
	}
	printEndpointChecks(w, "Backend checks", report.BackendChecks)
	printPortChecks(w, report.PortChecks)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Docker: status=%s available=%t", report.Docker.Status, report.Docker.Available)
	if report.Docker.Version != "" {
		fmt.Fprintf(w, " version=%s", report.Docker.Version)
	}
	if report.Docker.Error != "" {
		fmt.Fprintf(w, " error=%s", report.Docker.Error)
	}
	fmt.Fprintln(w)
	printList(w, "Summary", report.Summary)
}

func printHumanInstall(w io.Writer, result sidecarinstall.Result, wrote bool) {
	fmt.Fprintln(w, "vkturn server install")
	fmt.Fprintln(w, "mode: dry-run, no host files were changed and Xray was not restarted")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Root: %s\n", result.Root)
	fmt.Fprintf(w, "Artifacts %s:\n", installArtifactVerb(wrote))
	for _, artifact := range result.Artifacts {
		fmt.Fprintf(w, "- %s %s -> %s mode=%s", artifact.Kind, artifact.Path, artifact.RootedPath, artifact.Mode)
		if artifact.Source != "" {
			fmt.Fprintf(w, " source=%s", artifact.Source)
		}
		fmt.Fprintln(w)
	}
	printList(w, "Will not touch", result.WillNotTouch)
	printList(w, "Skipped apply actions", result.SkippedApply)
	printList(w, "Future systemd commands", result.SystemdCommands)
}

func printHumanLifecycle(w io.Writer, result sidecarlifecycle.Result) {
	fmt.Fprintf(w, "vkturn server %s\n", result.Action)
	fmt.Fprintln(w, "mode: dry-run, no host systemctl command was executed")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Root: %s\n", result.Root)
	fmt.Fprintf(w, "Service: %s\n", result.ServiceName)
	fmt.Fprintf(w, "State: %s\n", result.State)
	fmt.Fprintf(w, "Changed: %t\n", result.Changed)
	fmt.Fprintf(w, "Unit: %s\n", result.UnitPath)
	fmt.Fprintf(w, "Manifest: %s\n", result.ManifestPath)
	fmt.Fprintf(w, "Journal: %s\n", result.JournalPath)
	if result.LogPath != "" {
		fmt.Fprintf(w, "Log: %s\n", result.LogPath)
	}
	fmt.Fprintf(w, "Message: %s\n", result.Message)
	if len(result.Logs) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Logs:")
		for _, line := range result.Logs {
			fmt.Fprintf(w, "- %s\n", line)
		}
	}
}

func printHumanUninstall(w io.Writer, result sidecarrollback.Result, wrote bool) {
	fmt.Fprintln(w, "vkturn server uninstall")
	fmt.Fprintln(w, "mode: dry-run, no host files were changed and Xray was not restarted")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Root: %s\n", result.Root)
	fmt.Fprintf(w, "Manifest: %s\n", result.ManifestPath)
	fmt.Fprintf(w, "Applied: %t\n", result.Applied)
	fmt.Fprintf(w, "Message: %s\n", result.Message)
	fmt.Fprintf(w, "Removals %s:\n", uninstallRemovalVerb(wrote))
	for _, removal := range result.Removals {
		fmt.Fprintf(w, "- %s %s -> %s exists=%t\n", removal.Kind, removal.Path, removal.RootedPath, removal.Exists)
	}
	printList(w, "Will not touch", result.WillNotTouch)
	printList(w, "Skipped apply actions", result.SkippedApply)
}

func uninstallRemovalVerb(wrote bool) string {
	if wrote {
		return "applied"
	}
	return "planned"
}

func installArtifactVerb(wrote bool) string {
	if wrote {
		return "written"
	}
	return "planned"
}

func printCommandChecks(w io.Writer, checks []xraydoctor.CommandCheck) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	if len(checks) == 0 {
		fmt.Fprintln(w, "- none")
		return
	}
	for _, check := range checks {
		line := fmt.Sprintf("- %s: %s", check.Name, check.Status)
		if check.Path != "" {
			line += fmt.Sprintf(" (%s)", check.Path)
		}
		fmt.Fprintln(w, line)
	}
}

func printServiceCandidates(w io.Writer, services []xraydoctor.ServiceCandidate) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Xray service candidates:")
	if len(services) == 0 {
		fmt.Fprintln(w, "- none")
		return
	}
	for _, service := range services {
		fmt.Fprintf(w, "- %s status=%s confidence=%s path=%s config=%s\n",
			service.Name, service.Status, service.Confidence, service.Path, valueOrDash(service.ConfigPath))
	}
}

func printProcessCandidates(w io.Writer, processes []xraydoctor.ProcessCandidate) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Xray process candidates:")
	if len(processes) == 0 {
		fmt.Fprintln(w, "- none")
		return
	}
	for _, process := range processes {
		fmt.Fprintf(w, "- pid=%s status=%s config=%s command=%s\n",
			valueOrDash(process.PID), process.Status, valueOrDash(process.ConfigPath), process.Command)
	}
}

func printConfigCandidates(w io.Writer, configs []xraydoctor.ConfigCandidate) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Xray config candidates:")
	if len(configs) == 0 {
		fmt.Fprintln(w, "- none")
		return
	}
	for _, cfg := range configs {
		line := fmt.Sprintf("- %s status=%s confidence=%s vless_tcp=%d", cfg.Path, cfg.Status, cfg.Confidence, cfg.VLESSCount)
		if cfg.FromService != "" {
			line += fmt.Sprintf(" service=%s", cfg.FromService)
		}
		if cfg.Error != "" {
			line += fmt.Sprintf(" error=%s", cfg.Error)
		}
		fmt.Fprintln(w, line)
	}
}

func printEndpointChecks(w io.Writer, title string, checks []xraydoctor.EndpointCheck) {
	if len(checks) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s:\n", title)
	for _, check := range checks {
		line := fmt.Sprintf("- %s %s %s status=%s", valueOrDash(check.Name), check.Network, check.Address, check.Status)
		if check.Error != "" {
			line += fmt.Sprintf(" error=%s", check.Error)
		}
		fmt.Fprintln(w, line)
	}
}

func printPortChecks(w io.Writer, checks []xraydoctor.PortCheck) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Sidecar port checks:")
	if len(checks) == 0 {
		fmt.Fprintln(w, "- none")
		return
	}
	for _, check := range checks {
		line := fmt.Sprintf("- %s %s:%d status=%s", check.Network, check.Address, check.Port, check.Status)
		if check.Error != "" {
			line += fmt.Sprintf(" error=%s", check.Error)
		}
		fmt.Fprintln(w, line)
	}
}

func printList(w io.Writer, title string, values []string) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s:\n", title)
	for _, value := range values {
		fmt.Fprintf(w, "- %s\n", value)
	}
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  vkturn doctor [--root <path>] [--json]")
	fmt.Fprintln(w, "  vkturn server plan --xray-config <path> [--json]")
	fmt.Fprintln(w, "  vkturn server install --dry-run --root <path> --xray-config <path> [--write] [--json]")
	fmt.Fprintln(w, "  vkturn server uninstall --dry-run --root <path> [--write] [--json]")
	fmt.Fprintln(w, "  vkturn server status|start|stop|restart|logs --dry-run --root <path> [--json]")
}

func printServerUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  vkturn server plan --xray-config <path> [--json]")
	fmt.Fprintln(w, "  vkturn server install --dry-run --root <path> --xray-config <path> [--write] [--json]")
	fmt.Fprintln(w, "  vkturn server uninstall --dry-run --root <path> [--write] [--json]")
	fmt.Fprintln(w, "  vkturn server status|start|stop|restart|logs --dry-run --root <path> [--json]")
}
