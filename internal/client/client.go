package client

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Arimodu/udp-broadcast-relay/internal/config"
)

type Client struct {
	cfg       *config.Config
	log       *slog.Logger
	conn      *Connection
	rawSender *RawSender
	rebroadCh chan RebroadcastPacket
	ifaces    []string
}

type RebroadcastPacket struct {
	RuleID  uint32
	SrcIP   [4]byte
	DstIP   [4]byte
	SrcPort uint16
	DstPort uint16
	Payload []byte
}

func Run(cfg *config.Config, configPath, version string) error {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Client.LogLevel),
	}))

	if cfg.Client.ServerAddress == "" {
		return fmt.Errorf("server_address not configured; run 'ubr setup client' or edit config")
	}

	// Prompt for credentials interactively if no API key is stored.
	// Credentials are never written to disk; the server returns a generated
	// API key on first successful login, which is saved in their place.
	var username, password string
	if cfg.Client.APIKey == "" {
		var err error
		username, password, err = promptCredentials(log)
		if err != nil {
			return fmt.Errorf("reading credentials: %w", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ifaces []string
	if cfg.Client.Interfaces != "" {
		for _, s := range strings.Split(cfg.Client.Interfaces, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				ifaces = append(ifaces, s)
			}
		}
	}

	c := &Client{
		cfg:       cfg,
		log:       log,
		rebroadCh: make(chan RebroadcastPacket, 256),
		ifaces:    ifaces,
	}

	rawSender, err := NewRawSender(ifaces, log)
	if err != nil {
		return fmt.Errorf("creating raw sender: %w", err)
	}
	c.rawSender = rawSender

	go c.rebroadcastLoop(ctx)

	conn := NewConnection(cfg, configPath, username, password, version, c.rebroadCh, log)
	c.conn = conn

	log.Info("client starting", "server", cfg.Client.ServerAddress)
	go conn.ConnectLoop(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Info("received signal, shutting down", "signal", sig)

	cancel()
	if c.rawSender != nil {
		c.rawSender.Close()
	}
	return nil
}

// promptCredentials reads username and password from stdin.
// The password is visible while typing; credentials are held in memory only
// and never written to any file.
func promptCredentials(log *slog.Logger) (username, password string, err error) {
	log.Info("no API key configured; enter credentials to generate one")
	fmt.Println("The server will issue an API key that is saved to config automatically.")
	fmt.Println("Your password is never stored.")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Username: ")
	username, err = reader.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("reading username: %w", err)
	}
	username = strings.TrimSpace(username)

	fmt.Print("Password: ")
	password, err = reader.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("reading password: %w", err)
	}
	password = strings.TrimSpace(password)

	if username == "" || password == "" {
		return "", "", fmt.Errorf("username and password cannot be empty")
	}
	return username, password, nil
}

func (c *Client) rebroadcastLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case pkt := <-c.rebroadCh:
			if err := c.rawSender.Send(pkt); err != nil {
				c.log.Debug("rebroadcast error", "error", err)
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
