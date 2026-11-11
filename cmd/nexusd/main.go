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
		statusCmd()
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
  nexusd service <install|uninstall|status>
  nexusd update --check [--channel stable|prerelease]
  nexusd update --stage [--channel ...]
  nexusd update --install [--force] [--allow-checksum-only]
  nexusd update --rollback [--force]
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

func statusCmd() {
	cfg, err := host.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("host:   %s\nid:     %s\nhub:    %s\npolicy: roots=%v agents=%v new=%v fork=%v\n",
		cfg.HostName, cfg.HostID, cfg.HubURL, cfg.Policy.AllowedRoots, cfg.Policy.AllowedAgents, cfg.Policy.AllowNew, cfg.Policy.AllowFork)
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
	channel := fs.String("channel", "auto", "stable, prerelease or auto")
	force := fs.Bool("force", false, "restart even if active agent runs are reported")
	allowChecksumOnly := fs.Bool("allow-checksum-only", false, "accept a release when cosign is unavailable (checksum only)")
	_ = fs.Parse(args)
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
	staged, err := host.StageUpdate(ctx, info, stagedDir, !*allowChecksumOnly)
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		os.Exit(1)
	}
	fmt.Println("staged: ", staged)
	if *check && !*install {
		fmt.Println("checksum verified")
		return
	}
	if *install {
		if err := host.InstallStagedUpdate(staged, *force); err != nil {
			fmt.Fprintln(os.Stderr, "update:", err)
			os.Exit(1)
		}
		fmt.Println("update applied; check nexusd doctor and the service status")
	}
}
