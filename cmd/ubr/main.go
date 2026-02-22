package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Arimodu/udp-broadcast-relay/internal/client"
	"github.com/Arimodu/udp-broadcast-relay/internal/config"
	"github.com/Arimodu/udp-broadcast-relay/internal/server"
	"github.com/Arimodu/udp-broadcast-relay/internal/setup"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "server":
		runServer(os.Args[2:])
	case "client":
		runClient(os.Args[2:])
	case "setup":
		runSetup(os.Args[2:])
	case "version":
		fmt.Printf("ubr %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `UDP Broadcast Relay (ubr) %s

Usage:
  ubr <command> [options]

Commands:
  server    Start the relay server
  client    Start the relay client
  setup     Run initial setup (interactive, unattended, or wizard)
  version   Print version information
  help      Show this help message

Run 'ubr <command> --help' for more information on a command.
`, version)
}

func runServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: ubr server [options]\n\nStart the UDP Broadcast Relay server.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	configPath := fs.String("config", "/etc/ubr/config.toml", "Path to configuration file")
	fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if err := server.Run(cfg, version); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func runClient(args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: ubr client [options]\n\nStart the UDP Broadcast Relay client.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	configPath := fs.String("config", "/etc/ubr/config.toml", "Path to configuration file")
	fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if err := client.Run(cfg, *configPath, version); err != nil {
		fmt.Fprintf(os.Stderr, "Client error: %v\n", err)
		os.Exit(1)
	}
}

func runSetup(args []string) {
	// Extract the mode positional arg first (it comes before flags)
	var mode string
	var flagArgs []string
	for _, a := range args {
		if mode == "" && (a == "server" || a == "client") {
			mode = a
		} else {
			flagArgs = append(flagArgs, a)
		}
	}

	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: ubr setup <server|client> [options]

Run initial setup to create configuration and optionally install as a systemd service.

Setup modes (choose one):
  (no flags)              Interactive CLI (prompts for each value)
  --wizard                Web-based wizard with broadcast discovery
  --admin-user/--admin-pass   Unattended server setup
  --server-address/--api-key  Unattended client setup

Server options:
  --admin-user NAME       Admin username
  --admin-pass PASS       Admin password
  --interface IFACE       Network interface for broadcast capture

Client options:
  --server-address ADDR   Server address (host:port, e.g. 10.0.0.1:14723)
  --api-key KEY           API key from the server WebUI
  --interfaces IFACES     Comma-separated interfaces to rebroadcast on (empty = all)
  Note: if no --api-key is given, 'ubr client' will prompt for credentials at startup.

Common options:
`)
		fs.PrintDefaults()
	}
	wizard := fs.Bool("wizard", false, "Run web-based setup wizard")
	// Server flags
	adminUser := fs.String("admin-user", "", "Admin username (server unattended)")
	adminPass := fs.String("admin-pass", "", "Admin password (server unattended)")
	iface := fs.String("interface", "", "Network interface for broadcast capture (server)")
	// Client flags
	serverAddr := fs.String("server-address", "", "Server address host:port (client unattended)")
	apiKey := fs.String("api-key", "", "API key (client unattended)")
	clientIfaces := fs.String("interfaces", "", "Rebroadcast interfaces, comma-separated (client)")
	// Common
	configPath := fs.String("config", "/etc/ubr/config.toml", "Config file path")
	fs.Parse(flagArgs)

	if mode != "server" && mode != "client" {
		fmt.Fprintf(os.Stderr, "Setup requires a mode: 'server' or 'client'\n\n")
		fs.Usage()
		os.Exit(1)
	}

	opts := setup.Options{
		Mode:         mode,
		Wizard:       *wizard,
		AdminUser:    *adminUser,
		AdminPass:    *adminPass,
		Interface:    *iface,
		ServerAddr:   *serverAddr,
		APIKey:       *apiKey,
		ClientIfaces: *clientIfaces,
		ConfigPath:   *configPath,
	}

	if err := setup.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Setup error: %v\n", err)
		os.Exit(1)
	}
}
