package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Arimodu/udp-broadcast-relay/internal/config"
	"github.com/Arimodu/udp-broadcast-relay/internal/database"
	"github.com/Arimodu/udp-broadcast-relay/internal/updater"
)

type Server struct {
	cfg     *config.Config
	db      *database.DB
	hub     *Hub
	relay   *RelayListener
	webui   *WebUI
	monitor *Monitor
	log     *slog.Logger
}

func Run(cfg *config.Config, version string) error {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Server.LogLevel),
	}))

	// Open database
	db, err := database.Open(cfg.Server.DataDir)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	// Check if setup has been run
	count, err := db.UserCount()
	if err != nil {
		return fmt.Errorf("checking users: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("no users found. Run 'ubr setup server' first to create an admin account")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &Server{
		cfg: cfg,
		db:  db,
		log: log,
	}

	// Create hub
	s.hub = NewHub(log, db)
	go s.hub.Run(ctx)

	// Start relay listener
	s.relay = NewRelayListener(cfg.Server.RelayPort, s.hub, db, log)
	go func() {
		if err := s.relay.Listen(ctx); err != nil && ctx.Err() == nil {
			log.Error("relay listener error", "error", err)
		}
	}()

	// Start broadcast listeners for enabled rules
	rules, err := db.ListEnabledRules()
	if err != nil {
		return fmt.Errorf("loading rules: %w", err)
	}
	for _, rule := range rules {
		if rule.Direction == "client_to_server" {
			continue // server doesn't capture for client-to-server rules
		}
		bc := NewBroadcastCapture(rule, s.hub, db, log)
		go func() {
			if err := bc.Listen(ctx); err != nil && ctx.Err() == nil {
				log.Error("broadcast capture error", "rule", rule.Name, "error", err)
			}
		}()
	}

	// Start broadcast monitor
	if cfg.Server.Interface != "" {
		s.monitor = NewMonitor(cfg.Server.Interface, db, log)
		go func() {
			if err := s.monitor.Run(ctx); err != nil && ctx.Err() == nil {
				log.Error("broadcast monitor error", "error", err)
			}
		}()
	}

	// Start update checker
	var checker *updater.Checker
	if cfg.Server.CheckUpdates {
		checker = updater.New(version)
		go s.updateCheckLoop(ctx, checker)
	}

	// Start WebUI
	s.webui = NewWebUI(cfg.Server.WebUIPort, db, s.hub, log, checker)
	go func() {
		if err := s.webui.Serve(ctx); err != nil && ctx.Err() == nil {
			log.Error("webui error", "error", err)
		}
	}()

	// Start packet log cleanup goroutine
	go s.cleanupLoop(ctx)

	log.Info("server started",
		"relay_port", cfg.Server.RelayPort,
		"webui_port", cfg.Server.WebUIPort,
		"interface", cfg.Server.Interface,
		"rules", len(rules),
	)

	// Wait for signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Info("received signal, shutting down", "signal", sig)

	cancel()
	time.Sleep(2 * time.Second) // give goroutines time to drain

	return nil
}

func (s *Server) updateCheckLoop(ctx context.Context, checker *updater.Checker) {
	// Initial check after a short delay so startup isn't slowed down
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	if err := checker.Check(); err != nil {
		s.log.Warn("update check failed", "error", err)
	} else {
		available, latest, _ := checker.Status()
		if available {
			s.log.Info("update available", "latest", latest.Tag, "current", checker.CurrentVersion())
		}
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := checker.Check(); err != nil {
				s.log.Warn("update check failed", "error", err)
			}
		}
	}
}

func (s *Server) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.db.PrunePacketLog(10000); err != nil {
				s.log.Error("pruning packet log", "error", err)
			}
		}
	}
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
