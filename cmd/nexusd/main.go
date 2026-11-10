package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
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
	case "status":
		statusCmd()
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
  nexusd status
  nexusd service <install|uninstall|status>
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
