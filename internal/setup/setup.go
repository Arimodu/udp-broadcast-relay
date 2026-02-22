package setup

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Arimodu/udp-broadcast-relay/internal/config"
	"github.com/Arimodu/udp-broadcast-relay/internal/database"

	"golang.org/x/crypto/bcrypt"
)

type Options struct {
	Mode      string // "server" or "client"
	Wizard    bool
	// Server unattended
	AdminUser string
	AdminPass string
	Interface string
	// Client unattended
	ServerAddr   string
	APIKey       string
	ClientIfaces string
	// Common
	ConfigPath string
}

func Run(opts Options) error {
	if opts.Wizard {
		return runWizard(opts)
	}

	// Unattended: server needs admin creds, client needs server address + API key.
	if opts.Mode == "server" && opts.AdminUser != "" && opts.AdminPass != "" {
		return runUnattended(opts)
	}
	if opts.Mode == "client" && opts.ServerAddr != "" && opts.APIKey != "" {
		return runUnattended(opts)
	}

	return runInteractive(opts)
}

func runInteractive(opts Options) error {
	// Single buffered reader for all stdin input.
	reader := bufio.NewReader(os.Stdin)
	cfg := config.Defaults()

	prompt := func(msg string) string {
		fmt.Print(msg)
		s, _ := reader.ReadString('\n')
		return strings.TrimSpace(s)
	}

	promptDefault := func(msg, def string) string {
		if def != "" {
			fmt.Printf("%s [%s]: ", msg, def)
		} else {
			fmt.Print(msg + ": ")
		}
		s, _ := reader.ReadString('\n')
		s = strings.TrimSpace(s)
		if s == "" {
			return def
		}
		return s
	}

	if opts.Mode == "server" {
		fmt.Println("=== UDP Broadcast Relay - Server Setup ===")
		fmt.Println()

		username := prompt("Admin username: ")
		if username == "" {
			return fmt.Errorf("username cannot be empty")
		}

		password := prompt("Admin password: ")
		if password == "" {
			return fmt.Errorf("password cannot be empty")
		}

		iface := promptDefault("Network interface for broadcast capture (empty = all)", "")
		if iface != "" {
			cfg.Server.Interface = iface
		}

		if err := saveConfig(opts.ConfigPath, cfg); err != nil {
			return err
		}

		if err := createAdminUser(cfg.Server.DataDir, username, password); err != nil {
			return err
		}
		fmt.Printf("Admin user '%s' created\n", username)

		answer := promptDefault("\nInstall as systemd service", "N")
		if isYes(answer) {
			if err := installSystemd(opts.Mode, opts.ConfigPath); err != nil {
				return fmt.Errorf("installing systemd service: %w", err)
			}
			fmt.Println("Systemd service installed and enabled")
		}

	} else { // client
		fmt.Println("=== UDP Broadcast Relay - Client Setup ===")
		fmt.Println()

		addr := prompt("Server address (host:port, e.g. 10.0.0.1:14723): ")
		if addr == "" {
			return fmt.Errorf("server address cannot be empty")
		}
		cfg.Client.ServerAddress = addr

		fmt.Println()
		fmt.Println("Authentication — choose one:")
		fmt.Println("  [1] API key  (obtain from server WebUI → API Keys)")
		fmt.Println("  [2] Username + password  (enter at startup; API key generated and saved on first connect)")
		fmt.Println()
		method := promptDefault("Choice", "1")

		if method == "2" {
			fmt.Println()
			fmt.Println("OK. When you run 'ubr client', you will be prompted for your credentials.")
			fmt.Println("The server will issue an API key that is saved to config automatically.")
			fmt.Println("Your password is never stored on disk.")
		} else {
			key := prompt("\nAPI key (ubr_...): ")
			if key == "" {
				return fmt.Errorf("API key cannot be empty")
			}
			cfg.Client.APIKey = key
		}

		ifaces := promptDefault("Rebroadcast interfaces, comma-separated (empty = all)", "")
		cfg.Client.Interfaces = ifaces

		if err := saveConfig(opts.ConfigPath, cfg); err != nil {
			return err
		}

		answer := promptDefault("\nInstall as systemd service", "N")
		if isYes(answer) {
			if err := installSystemd(opts.Mode, opts.ConfigPath); err != nil {
				return fmt.Errorf("installing systemd service: %w", err)
			}
			fmt.Println("Systemd service installed and enabled")
		}
	}

	fmt.Println("\nSetup complete!")
	return nil
}

func runUnattended(opts Options) error {
	cfg := config.Defaults()

	if opts.Mode == "server" {
		if opts.Interface != "" {
			cfg.Server.Interface = opts.Interface
		}

		if err := saveConfig(opts.ConfigPath, cfg); err != nil {
			return err
		}

		if err := createAdminUser(cfg.Server.DataDir, opts.AdminUser, opts.AdminPass); err != nil {
			return err
		}
		fmt.Printf("Admin user '%s' created\n", opts.AdminUser)

	} else { // client
		cfg.Client.ServerAddress = opts.ServerAddr
		cfg.Client.APIKey = opts.APIKey
		if opts.ClientIfaces != "" {
			cfg.Client.Interfaces = opts.ClientIfaces
		}

		if err := saveConfig(opts.ConfigPath, cfg); err != nil {
			return err
		}
	}

	fmt.Printf("Config saved to %s\n", opts.ConfigPath)
	return nil
}

// saveConfig ensures the directory exists and writes the config file.
func saveConfig(configPath string, cfg *config.Config) error {
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	fmt.Printf("Config saved to %s\n", configPath)
	return nil
}

// createAdminUser opens (or creates) the DB and inserts the admin user.
func createAdminUser(dataDir, username, password string) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}
	db, err := database.Open(dataDir)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	if _, err := db.CreateUser(username, string(hash), true); err != nil {
		return fmt.Errorf("creating admin user: %w", err)
	}
	return nil
}

func isYes(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes"
}

func HasSystemd() bool {
	_, err := os.Stat("/run/systemd/system")
	return err == nil
}

func installSystemd(mode, configPath string) error {
	if !HasSystemd() {
		return fmt.Errorf("systemd not detected (/run/systemd/system not found)")
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	destPath := "/usr/local/bin/ubr"
	if err := copyFile(exePath, destPath); err != nil {
		return fmt.Errorf("copying binary to %s: %w", destPath, err)
	}
	os.Chmod(destPath, 0755)

	unitName := fmt.Sprintf("ubr-%s.service", mode)
	unitPath := filepath.Join("/etc/systemd/system", unitName)
	if err := os.WriteFile(unitPath, []byte(generateUnit(mode, configPath)), 0644); err != nil {
		return fmt.Errorf("writing unit file: %w", err)
	}

	cmds := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", unitName},
		{"systemctl", "start", unitName},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %s: %w", args, strings.TrimSpace(string(out)), err)
		}
	}

	return nil
}

func generateUnit(mode, configPath string) string {
	return fmt.Sprintf(`[Unit]
Description=UDP Broadcast Relay %s
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/ubr %s --config %s
Restart=on-failure
RestartSec=5
AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`, mode, mode, configPath)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}
