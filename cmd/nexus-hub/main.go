package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/hub"
	"github.com/AlexanderWagnerDev/agent-nexus/web"
)

func runHealthcheck(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func main() {
	var (
		addr        = flag.String("addr", envOr("NEXUS_ADDR", "127.0.0.1:8080"), "listen address")
		dataDir     = flag.String("data-dir", envOr("NEXUS_DATA_DIR", ""), "data directory")
		publicURL   = flag.String("public-url", envOr("NEXUS_PUBLIC_URL", ""), "public hub URL (https://...)")
		tlsCert     = flag.String("tls-cert", envOr("NEXUS_TLS_CERT", ""), "TLS certificate file")
		tlsKey      = flag.String("tls-key", envOr("NEXUS_TLS_KEY", ""), "TLS private key file")
		hubName     = flag.String("name", envOr("NEXUS_HUB_NAME", "AgentNexus"), "hub display name")
		metrics     = flag.Bool("metrics", envOr("NEXUS_METRICS", "") == "true", "expose /metrics")
		metricsTok  = flag.String("metrics-token", envOr("NEXUS_METRICS_TOKEN", ""), "bearer token required by /metrics")
		maxHost     = flag.Int("max-runs-per-host", envInt("NEXUS_MAX_RUNS_PER_HOST", 0), "concurrent run limit per host (0 = unlimited)")
		maxUser     = flag.Int("max-runs-per-user", envInt("NEXUS_MAX_RUNS_PER_USER", 0), "concurrent run limit per user (0 = unlimited)")
		maxProject  = flag.Int("max-runs-per-project", envInt("NEXUS_MAX_RUNS_PER_PROJECT", 0), "concurrent run limit per project (0 = unlimited)")
		signed      = flag.Bool("require-signed-commands", envOr("NEXUS_REQUIRE_SIGNED_COMMANDS", "") == "true", "reject prompt, approval and stop writes that are not signed by a registered device key")
		logLevel    = flag.String("log-level", envOr("NEXUS_LOG_LEVEL", "info"), "debug, info, warn or error")
		healthcheck = flag.Bool("healthcheck", false, "probe the local /healthz endpoint and exit")
		backup      = flag.String("backup", "", "write a consistent database snapshot to this file and exit")
	)
	flag.Parse()
	if *healthcheck {
		os.Exit(runHealthcheck(*addr))
	}

	logger := newLogger(*logLevel)
	if *dataDir == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			logger.Error("cannot determine config directory", "error", err)
			os.Exit(1)
		}
		*dataDir = filepath.Join(dir, "AgentNexus")
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		logger.Error("cannot create data directory", "error", err)
		os.Exit(1)
	}
	store, err := hub.OpenStore(filepath.Join(*dataDir, "hub.db"))
	if err != nil {
		logger.Error("cannot open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	box, err := hub.OpenSecretBox(*dataDir)
	if err != nil {
		logger.Error("cannot open secret key", "error", err)
		os.Exit(1)
	}
	store.SetSecretBox(box)
	signer, err := hub.OpenHubSigner(*dataDir)
	if err != nil {
		logger.Error("cannot open hub signing key", "error", err)
		os.Exit(1)
	}
	if err := store.MigrateSecrets(); err != nil {
		logger.Error("cannot encrypt stored secrets", "error", err)
		os.Exit(1)
	}
	if *backup != "" {
		if err := store.Backup(*backup); err != nil {
			logger.Error("backup failed", "error", err)
			os.Exit(1)
		}
		fmt.Printf("backup written to %s\n", *backup)
		fmt.Println("Copy secret.key from the data directory as well; encrypted secrets cannot be read without it.")
		return
	}

	srv := hub.NewServer(hub.Config{
		Addr:                  *addr,
		DataDir:               *dataDir,
		PublicURL:             *publicURL,
		TLSCert:               *tlsCert,
		TLSKey:                *tlsKey,
		HubName:               *hubName,
		MetricsEnabled:        *metrics,
		MetricsToken:          *metricsTok,
		MaxRunsPerHost:        *maxHost,
		MaxRunsPerUser:        *maxUser,
		MaxRunsPerProject:     *maxProject,
		RequireSignedCommands: *signed,
	}, store, logger)
	srv.SetSigner(signer)
	srv.SetAssets(web.Handler())

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // WebSocket and streaming responses
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	if *publicURL != "" {
		logger.Info("AgentNexus hub starting", "addr", *addr, "public_url", *publicURL, "data_dir", *dataDir)
	} else {
		logger.Info("AgentNexus hub starting", "addr", *addr, "data_dir", *dataDir)
	}
	if *tlsCert != "" && *tlsKey != "" {
		err = httpServer.ListenAndServeTLS(*tlsCert, *tlsKey)
	} else {
		err = httpServer.ListenAndServe()
	}
	if err != nil && err != http.ErrServerClosed {
		logger.Error("hub stopped", "error", err)
		os.Exit(1)
	}
	logger.Info("hub stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
