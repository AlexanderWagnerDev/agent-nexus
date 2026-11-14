package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/host"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "pair":
		pairCmd(os.Args[2:])
	case "run":
		runCmd(os.Args[2:])
	case "doctor":
		doctorCmd()
	case "discover":
		discoverCmd(os.Args[2:])
	case "acp":
		acpCmd(os.Args[2:])
	case "status":
		statusCmd(os.Args[2:])
	case "index":
		indexCmd(os.Args[2:])
	case "update":
		updateCmd(os.Args[2:])
	case "service":
		serviceCmd(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("nexusd " + host.Version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `AgentNexus host daemon

Usage:
  nexusd pair --hub <url> --code <code> [--name <name>]
  nexusd run
  nexusd doctor
  nexusd discover [--json]
  nexusd acp [--agent <name> [--command <adapter>]]
  nexusd status
  nexusd index [status|rebuild]
  nexusd service <install|uninstall|status>
  nexusd update --check [--channel stable|prerelease|development]
  nexusd update --stage [--channel ...] [--allow-checksum-only]
  nexusd update --install [--force] [--allow-checksum-only]
  nexusd update --rollback [--force]
  nexusd update --status [--json]
  nexusd update --configure [--channel ...] [--auto on|off]
             [--install-when-idle on|off] [--checksum-only|--require-signature]
             [--allow-remote-install on|off] [--unblock VERSION]
  nexusd version

The daemon runs as the logged-in user; it never needs root/SYSTEM.
`)
}

func logger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}

func pairCmd(args []string) {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	hubURL := fs.String("hub", "", "hub URL, e.g. https://nexus.example.com")
	code := fs.String("code", "", "one-time pairing code from the web app")
	name := fs.String("name", "", "display name for this host")
	force := fs.Bool("force", false, "overwrite an existing pairing")
	fs.Parse(args)
	if *hubURL == "" || *code == "" {
		fmt.Fprintln(os.Stderr, "pair requires --hub and --code")
		os.Exit(2)
	}
	if !*force {
		if _, err := host.LoadConfig(); err == nil {
			fmt.Fprintln(os.Stderr, "this host is already paired; use --force to replace the identity")
			os.Exit(1)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, sas, err := host.Pair(ctx, *hubURL, *code, *name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pairing failed:", err)
		os.Exit(1)
	}
	fmt.Printf("Paired with %s as host %s (%s).\n", cfg.HubURL, cfg.HostID, cfg.HostName)
	if sas != "" {
		fmt.Printf("Pairing SAS: %s\nConfirm this code in the hub web UI (Hosts page) before the host can connect.\n", sas)
	}
	fmt.Println("Next: nexusd service install && nexusd status")
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	level := fs.String("log-level", "info", "debug, info, warn or error")
	fs.Parse(args)
	cfg, err := host.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	d := host.NewDaemon(cfg, logger(*level))
	if err := d.Run(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "daemon stopped:", err)
		os.Exit(1)
	}
}

func doctorCmd() {
	cfg, err := host.LoadConfig()
	issues := host.Doctor(cfg, err, os.Stdout)
	if issues > 0 {
		os.Exit(1)
	}
}

func discoverCmd(args []string) {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	fs.Parse(args)
	cfg, err := host.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	d := host.NewDaemon(cfg, logger("warn"))
	if *asJSON {
		var all []any
		for _, a := range d.Adapters() {
			sessions, err := a.ListSessions(ctx)
			if err != nil {
				continue
			}
			for _, s := range sessions {
				all = append(all, s)
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(all)
		return
	}
	total := 0
	for _, a := range d.Adapters() {
		sessions, err := a.ListSessions(ctx)
		if err != nil {
			fmt.Printf("%-9s error: %v\n", a.Name(), err)
			continue
		}
		fmt.Printf("%-9s %d session(s)\n", a.Name(), len(sessions))
		total += len(sessions)
	}
	fmt.Printf("total     %d\n", total)
}

func statusCmd(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print machine-readable status")
	_ = fs.Parse(args)
	cfg, err := host.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	runtime, fresh := host.ReadRuntimeStatus()
	active := 0
	if fresh {
		active = runtime.ActiveRuns
	}
	if *jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"host": cfg.HostName, "id": cfg.HostID, "hub": cfg.HubURL,
			"active_runs": active, "runtime_fresh": fresh,
			"version": host.Version,
		})
		return
	}
	fmt.Printf("host:   %s\nid:     %s\nhub:    %s\npolicy: roots=%v agents=%v new=%v fork=%v\nactive runs: %d (runtime status fresh=%v)\n",
		cfg.HostName, cfg.HostID, cfg.HubURL, cfg.Policy.AllowedRoots, cfg.Policy.AllowedAgents, cfg.Policy.AllowNew, cfg.Policy.AllowFork, active, fresh)
}

func indexCmd(args []string) {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	fs.Parse(args)
	action := "status"
	if fs.NArg() > 0 {
		action = fs.Arg(0)
	}
	cfg, _ := host.LoadConfig()
	dir, err := host.ConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !cfg.SearchIndexEnabled() {
		fmt.Println("search index: disabled by search_index=false")
		return
	}
	index, err := host.OpenSearchIndex(filepath.Join(dir, "search-index.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open search index:", err)
		os.Exit(1)
	}
	defer index.Close()
	switch action {
	case "status":
		count, _ := index.Count()
		fmt.Printf("search index: %s (%d sessions, %s)\n", index.Status().State, count, index.MarshalStatus())
	case "rebuild":
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		adapters := host.DetectAdapters(logger("warn"))
		fmt.Printf("rebuilding with %d adapter(s) ...\n", len(adapters))
		if err := index.Rebuild(ctx, adapters, cfg.Policy, logger("warn")); err != nil {
			fmt.Fprintln(os.Stderr, "rebuild failed:", err)
			os.Exit(1)
		}
		count, _ := index.Count()
		fmt.Printf("search index: ready (%d sessions)\n", count)
	default:
		fmt.Fprintf(os.Stderr, "unknown index action %q; use status or rebuild\n", action)
		os.Exit(2)
	}
}

func acpCmd(args []string) {
	fs := flag.NewFlagSet("acp", flag.ExitOnError)
	agent := fs.String("agent", "", "agent name, e.g. claude")
	command := fs.String("command", "", "ACP adapter command line; empty removes the override")
	fs.Parse(args)
	cfg, err := host.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *agent == "" {
		if len(cfg.ACPCommands) == 0 {
			fmt.Println("no ACP adapter overrides configured")
		}
		for name, cmd := range cfg.ACPCommands {
			fmt.Printf("%-9s %s\n", name, cmd)
		}
		if detected := cfg.ACPCommand("claude"); detected != "" && cfg.ACPCommands["claude"] == "" {
			fmt.Printf("%-9s %s (auto-detected)\n", "claude", detected)
		}
		return
	}
	if *command == "" {
		effective := cfg.ACPCommand(*agent)
		if effective == "" {
			fmt.Printf("%s: no ACP adapter configured\n", *agent)
			return
		}
		fmt.Println(effective)
		return
	}
	if cfg.ACPCommands == nil {
		cfg.ACPCommands = map[string]string{}
	}
	cfg.ACPCommands[*agent] = *command
	if err := host.SaveConfig(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "save config:", err)
		os.Exit(1)
	}
	fmt.Printf("%s: ACP adapter set to %s\nRestart nexusd to apply.\n", *agent, *command)
}

func serviceCmd(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: nexusd service <install|uninstall|status>")
		os.Exit(2)
	}
	if err := host.ServiceAction(args[0]); err != nil {
		fmt.Fprintln(os.Stderr, "service:", err)
		os.Exit(1)
	}
}

func updateCmd(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.Bool("check", false, "check the release feed")
	stage := fs.Bool("stage", false, "download and verify the latest release")
	install := fs.Bool("install", false, "install the staged release and restart the service")
	rollback := fs.Bool("rollback", false, "restore nexusd.previous and restart the service")
	status := fs.Bool("status", false, "show the persisted updater state")
	configure := fs.Bool("configure", false, "change host-local update settings")
	asJSON := fs.Bool("json", false, "print machine-readable output")
	channel := fs.String("channel", "", "stable, prerelease or development (default: configured channel)")
	auto := fs.String("auto", "", "on or off")
	installIdle := fs.String("install-when-idle", "", "on or off")
	remoteInstall := fs.String("allow-remote-install", "", "on or off")
	checksumOnly := fs.Bool("checksum-only", false, "accept checksum-only updates (weaker trust mode)")
	requireSignature := fs.Bool("require-signature", false, "require a verified cosign bundle")
	unblock := fs.String("unblock", "", "remove a version from updates.blocked_versions")
	force := fs.Bool("force", false, "restart even if active agent runs are reported")
	allowChecksumOnly := fs.Bool("allow-checksum-only", false, "accept a release when cosign is unavailable (checksum only)")
	verifyRollback := fs.Bool("verify-rollback", false, "internal: verify a started update and roll back on failure")
	target := fs.String("target", "", "internal: installed binary path")
	previous := fs.String("previous", "", "internal: rollback binary path")
	expected := fs.String("expected", "", "internal: expected version")
	_ = fs.Parse(args)

	if *verifyRollback {
		if err := host.VerifyUpdateAndRollback(*target, *previous, *expected); err != nil {
			fmt.Fprintln(os.Stderr, "update watchdog:", err)
			os.Exit(1)
		}
		return
	}

	cfg, cfgErr := host.LoadConfig()
	prefs := host.UpdateSettings{}.Resolve()
	if cfgErr == nil {
		prefs = cfg.UpdatePrefs()
	}

	if *status || *auto != "" || *installIdle != "" || *remoteInstall != "" ||
		*checksumOnly || *requireSignature || *unblock != "" || *configure {
		configuring := *configure || *auto != "" || *installIdle != "" || *remoteInstall != "" ||
			*checksumOnly || *requireSignature || *unblock != ""
		if configuring && cfgErr != nil {
			fmt.Fprintln(os.Stderr, "update:", cfgErr)
			os.Exit(1)
		}
		if *auto != "" {
			enabled, err := parseOnOff(*auto)
			if err != nil {
				fmt.Fprintln(os.Stderr, "update:", err)
				os.Exit(2)
			}
			prefs.Disabled = !enabled
		}
		if *channel != "" {
			prefs.Channel = host.NormalizeUpdateChannel(*channel, host.Version)
		}
		if *installIdle != "" {
			value, err := parseOnOff(*installIdle)
			if err != nil {
				fmt.Fprintln(os.Stderr, "update:", err)
				os.Exit(2)
			}
			prefs.InstallWhenIdle = &value
		}
		if *remoteInstall != "" {
			value, err := parseOnOff(*remoteInstall)
			if err != nil {
				fmt.Fprintln(os.Stderr, "update:", err)
				os.Exit(2)
			}
			prefs.AllowRemoteInstall = value
		}
		if *checksumOnly {
			value := false
			prefs.RequireSignature = &value
		}
		if *requireSignature {
			value := true
			prefs.RequireSignature = &value
		}
		if *unblock != "" {
			want := strings.TrimPrefix(strings.TrimSpace(*unblock), "v")
			kept := prefs.BlockedVersions[:0]
			for _, version := range prefs.BlockedVersions {
				if strings.TrimPrefix(strings.TrimSpace(version), "v") != want {
					kept = append(kept, version)
				}
			}
			prefs.BlockedVersions = kept
		}
		if *configure || *auto != "" || *installIdle != "" || *remoteInstall != "" ||
			*checksumOnly || *requireSignature || *unblock != "" {
			if err := host.SaveUpdatePrefs(prefs); err != nil {
				fmt.Fprintln(os.Stderr, "update:", err)
				os.Exit(1)
			}
			if *unblock != "" {
				host.ClearUpdateBlock(*unblock)
			}
			if !*status {
				fmt.Println("update settings saved")
				return
			}
		}
		prefs = host.UpdateSettings{
			Disabled: prefs.Disabled, Channel: prefs.Channel,
			CheckIntervalMinutes: prefs.CheckIntervalMinutes,
			InstallWhenIdle:      prefs.InstallWhenIdle, RequireSignature: prefs.RequireSignature,
			AllowRemoteInstall: prefs.AllowRemoteInstall, BlockedVersions: prefs.BlockedVersions,
		}.Resolve()
	}
	if *status {
		state, ok := host.ReadUpdateState()
		if *asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"version": host.Version, "settings": prefs, "state": state, "persisted": ok,
			})
			return
		}
		fmt.Printf("current:       %s\nchannel:       %s\nauto update:   %v\ninstall idle:  %v\nsignature:     %s\n",
			host.Version, prefs.Channel, !prefs.Disabled, prefs.InstallIdle(), signatureMode(prefs))
		if !ok {
			fmt.Println("state:         no update check recorded yet")
			return
		}
		fmt.Printf("phase:         %s\nlatest:        %s\nchecked:       %s\nlast success:  %s\n",
			state.Phase, state.Info.Latest, formatTime(state.LastCheck), formatTime(state.LastSuccess))
		if state.Message != "" {
			fmt.Printf("message:       %s\n", state.Message)
		}
		if state.LastError != "" {
			fmt.Printf("last error:    %s\n", state.LastError)
		}
		return
	}
	if *rollback {
		if err := host.RollbackUpdate(*force); err != nil {
			fmt.Fprintln(os.Stderr, "update:", err)
			os.Exit(1)
		}
		fmt.Println("rollback scheduled; the service restarts with nexusd.previous")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if *channel == "" {
		*channel = prefs.Channel
	}
	info, err := host.CheckForUpdate(ctx, host.Version, *channel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		os.Exit(1)
	}
	fmt.Printf("current: %s\nlatest:  %s\nasset:   %s\n", info.Current, info.Latest, info.AssetName)
	if !info.Available {
		fmt.Println("status:  up to date")
		return
	}
	if *check && !*stage && !*install {
		fmt.Println("status:  update available")
		return
	}
	dir, err := host.ConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		os.Exit(1)
	}
	stagedDir := filepath.Join(dir, "updates", info.Latest)
	staged, err := host.StageUpdate(ctx, info, stagedDir, !*allowChecksumOnly && prefs.SignatureRequired())
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		os.Exit(1)
	}
	if err := host.VerifyStagedBinary(ctx, staged, info.Latest); err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		os.Exit(1)
	}
	fmt.Println("staged: ", staged)
	if *check && !*install {
		fmt.Println("checksum and version verified")
		return
	}
	if *install {
		if err := host.StartUpdateWatchdog(info.Latest); err != nil {
			fmt.Fprintln(os.Stderr, "update: cannot start the restart watchdog:", err)
			os.Exit(1)
		}
		if err := host.InstallStagedUpdate(staged, *force); err != nil {
			fmt.Fprintln(os.Stderr, "update:", err)
			os.Exit(1)
		}
		fmt.Println("replacement scheduled; the service restarts into", info.Latest,
			"and the watchdog restores the previous binary if it does not start")
	}
}

func parseOnOff(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "1", "yes", "enabled":
		return true, nil
	case "off", "false", "0", "no", "disabled":
		return false, nil
	}
	return false, fmt.Errorf("expected on or off, got %q", value)
}

func signatureMode(prefs host.UpdateSettings) string {
	if prefs.SignatureRequired() {
		return "cosign required"
	}
	return "checksum-only (weaker)"
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "never"
	}
	return value.Local().Format(time.RFC3339)
}
