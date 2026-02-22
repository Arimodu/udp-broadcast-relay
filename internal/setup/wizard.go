package setup

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Arimodu/udp-broadcast-relay/internal/config"
	"github.com/Arimodu/udp-broadcast-relay/internal/database"
	"github.com/Arimodu/udp-broadcast-relay/internal/netutil"
	"github.com/Arimodu/udp-broadcast-relay/internal/protocol"
	"github.com/Arimodu/udp-broadcast-relay/internal/server"
	"github.com/Arimodu/udp-broadcast-relay/web"

	"golang.org/x/crypto/bcrypt"
)

type wizardState struct {
	mu          sync.Mutex
	monitor     *server.Monitor
	monitorDB   *database.DB
	monitorDir  string
	monitorStop context.CancelFunc
}

func (ws *wizardState) startMonitor(ifaceName string, log *slog.Logger) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	ws.stopMonitorLocked()

	tmpDir, err := os.MkdirTemp("", "ubr-wizard-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	ws.monitorDir = tmpDir

	db, err := database.Open(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("opening temp db: %w", err)
	}
	ws.monitorDB = db

	ctx, cancel := context.WithCancel(context.Background())
	ws.monitorStop = cancel

	ws.monitor = server.NewMonitor(ifaceName, db, log)
	go ws.monitor.Run(ctx)
	return nil
}

func (ws *wizardState) stopMonitorLocked() {
	if ws.monitorStop != nil {
		ws.monitorStop()
		ws.monitorStop = nil
	}
	if ws.monitorDB != nil {
		ws.monitorDB.Close()
		ws.monitorDB = nil
	}
	if ws.monitorDir != "" {
		os.RemoveAll(ws.monitorDir)
		ws.monitorDir = ""
	}
	ws.monitor = nil
}

func (ws *wizardState) stopMonitor() {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.stopMonitorLocked()
}

func (ws *wizardState) getDiscoveries() ([]database.BroadcastObservation, error) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.monitorDB == nil {
		return nil, nil
	}
	return ws.monitorDB.GetBroadcastObservations()
}

func runWizard(opts Options) error {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Find a random available port via OS
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return fmt.Errorf("finding available port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	// Print access URLs
	fmt.Printf("\n=== UDP Broadcast Relay - Setup Wizard ===\n\n")
	fmt.Printf("Open one of these URLs in your browser:\n")
	fmt.Printf("  http://localhost:%d\n", port)

	if ips, err := netutil.GetLocalIPs(); err == nil {
		for _, ip := range ips {
			fmt.Printf("  http://%s:%d\n", ip, port)
		}
	}

	if hn, err := os.Hostname(); err == nil {
		fmt.Printf("  http://%s:%d\n", hn, port)
	}

	fmt.Printf("\nWaiting for setup to complete (Ctrl+C to abort)...\n\n")

	cfg := config.Defaults()
	done := make(chan struct{})
	ws := &wizardState{}
	defer ws.stopMonitor()

	mux := http.NewServeMux()

	// Static assets
	staticFS, err := fs.Sub(web.Assets, "static")
	if err != nil {
		return fmt.Errorf("static fs: %w", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Wizard page
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFS(web.Assets, "templates/wizard.html")
		if err != nil {
			http.Error(w, "template error: "+err.Error(), 500)
			return
		}
		tmpl.Execute(w, map[string]string{"Mode": opts.Mode})
	})

	// API: list interfaces
	mux.HandleFunc("GET /api/wizard/interfaces", func(w http.ResponseWriter, r *http.Request) {
		ifaces, err := netutil.GetBroadcastInterfaces()
		if err != nil {
			jsonError(w, err.Error(), 500)
			return
		}

		type ifaceInfo struct {
			Name      string `json:"name"`
			IP        string `json:"ip"`
			Broadcast string `json:"broadcast"`
		}

		var result []ifaceInfo
		for _, iface := range ifaces {
			result = append(result, ifaceInfo{
				Name:      iface.Name,
				IP:        iface.IP.String(),
				Broadcast: iface.BroadcastAddr.String(),
			})
		}

		jsonResponse(w, result)
	})

	// API: start broadcast monitor
	mux.HandleFunc("POST /api/wizard/start-monitor", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Interface string `json:"interface"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if req.Interface == "" {
			jsonError(w, "interface required", 400)
			return
		}

		if err := ws.startMonitor(req.Interface, log); err != nil {
			log.Warn("monitor start failed (may need CAP_NET_RAW)", "error", err)
			// Non-fatal - wizard still works without monitor
		}

		jsonResponse(w, map[string]bool{"success": true})
	})

	// API: get discoveries (polls from the temp monitor DB)
	mux.HandleFunc("GET /api/wizard/discoveries", func(w http.ResponseWriter, r *http.Request) {
		observations, err := ws.getDiscoveries()
		if err != nil {
			jsonError(w, "reading discoveries: "+err.Error(), 500)
			return
		}
		if observations == nil {
			observations = []database.BroadcastObservation{}
		}
		jsonResponse(w, observations)
	})

	// API: stop monitor
	mux.HandleFunc("POST /api/wizard/stop-monitor", func(w http.ResponseWriter, r *http.Request) {
		ws.stopMonitor()
		jsonResponse(w, map[string]bool{"success": true})
	})

	// API: save configuration
	mux.HandleFunc("POST /api/wizard/save", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Mode           string `json:"mode"`
			// Server fields
			AdminUser      string `json:"admin_user"`
			AdminPass      string `json:"admin_pass"`
			Interface      string `json:"interface"`
			InstallSystemd bool   `json:"install_systemd"`
			Rules          []struct {
				Name       string `json:"name"`
				ListenPort int    `json:"listen_port"`
				Direction  string `json:"direction"`
			} `json:"rules"`
			// Client fields
			ServerAddr   string `json:"server_addr"`
			APIKey       string `json:"api_key"`
			Username     string `json:"username"`
			Password     string `json:"password"`
			ClientIfaces string `json:"client_ifaces"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request", 400)
			return
		}

		ws.stopMonitor()

		configDir := filepath.Dir(opts.ConfigPath)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			jsonError(w, "creating config directory: "+err.Error(), 500)
			return
		}

		var note string

		if req.Mode == "client" {
			// Client setup
			hasAPIKey := req.APIKey != ""
			hasCreds := req.Username != "" && req.Password != ""
			if req.ServerAddr == "" || (!hasAPIKey && !hasCreds) {
				jsonError(w, "server address and either an API key or username+password are required", 400)
				return
			}

			cfg.Client.ServerAddress = req.ServerAddr
			cfg.Client.Interfaces = req.ClientIfaces

			if hasAPIKey {
				cfg.Client.APIKey = req.APIKey
			} else {
				// Exchange credentials with the server right now so that only
				// the generated API key is written to disk — never the password.
				apiKey, err := exchangeCredentialsForAPIKey(req.ServerAddr, req.Username, req.Password)
				if err != nil {
					jsonError(w, "credential exchange with server failed: "+err.Error(), 500)
					return
				}
				cfg.Client.APIKey = apiKey
				log.Info("credential exchange succeeded, API key saved", "server", req.ServerAddr)
			}

			if err := config.Save(opts.ConfigPath, cfg); err != nil {
				jsonError(w, "saving config: "+err.Error(), 500)
				return
			}

			if req.InstallSystemd {
				if !HasSystemd() {
					note = " (note: systemd not detected on this machine)"
				} else if err := installSystemd("client", opts.ConfigPath); err != nil {
					log.Error("installing systemd service", "error", err)
					note = fmt.Sprintf(" (warning: systemd install failed: %v)", err)
				}
			}

			note = "Client setup complete!" + note

		} else {
			// Server setup (default)
			if req.AdminUser == "" || req.AdminPass == "" {
				jsonError(w, "admin username and password required", 400)
				return
			}

			if req.Interface != "" {
				cfg.Server.Interface = req.Interface
			}

			if err := os.MkdirAll(cfg.Server.DataDir, 0755); err != nil {
				jsonError(w, "creating data directory: "+err.Error(), 500)
				return
			}

			if err := config.Save(opts.ConfigPath, cfg); err != nil {
				jsonError(w, "saving config: "+err.Error(), 500)
				return
			}

			db, err := database.Open(cfg.Server.DataDir)
			if err != nil {
				jsonError(w, "creating database: "+err.Error(), 500)
				return
			}
			defer db.Close()

			hash, err := bcrypt.GenerateFromPassword([]byte(req.AdminPass), bcrypt.DefaultCost)
			if err != nil {
				jsonError(w, "hashing password: "+err.Error(), 500)
				return
			}

			if _, err := db.CreateUser(req.AdminUser, string(hash), true); err != nil {
				jsonError(w, "creating user: "+err.Error(), 500)
				return
			}

			for _, rule := range req.Rules {
				dir := rule.Direction
				if dir == "" {
					dir = "server_to_client"
				}
				if _, err := db.CreateRule(rule.Name, rule.ListenPort, "0.0.0.0", "255.255.255.255", dir); err != nil {
					log.Error("creating rule", "name", rule.Name, "error", err)
				}
			}

			if req.InstallSystemd {
				if !HasSystemd() {
					note = " (note: systemd not detected on this machine)"
				} else if err := installSystemd("server", opts.ConfigPath); err != nil {
					log.Error("installing systemd service", "error", err)
					note = fmt.Sprintf(" (warning: systemd install failed: %v)", err)
				}
			}

			note = "Setup complete!" + note
		}

		jsonResponse(w, map[string]interface{}{
			"success": true,
			"note":    note,
		})

		// Shut down wizard after brief delay
		go func() {
			time.Sleep(2 * time.Second)
			select {
			case <-done:
			default:
				close(done)
			}
		}()
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		<-done
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("wizard server: %w", err)
	}

	if opts.Mode == "client" {
		fmt.Println("\nSetup complete! You can now start the client with:")
		fmt.Printf("  ubr client --config %s\n\n", opts.ConfigPath)
	} else {
		fmt.Println("\nSetup complete! You can now start the server with:")
		fmt.Printf("  ubr server --config %s\n\n", opts.ConfigPath)
	}
	return nil
}

// exchangeCredentialsForAPIKey connects to the UBR relay port, authenticates
// with username+password, and returns the API key the server generates.
// The caller saves only the key; credentials are never written to disk.
func exchangeCredentialsForAPIKey(serverAddr, username, password string) (string, error) {
	conn, err := net.DialTimeout("tcp", serverAddr, 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("connecting: %w", err)
	}
	defer conn.Close()

	creds, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := protocol.WriteFrame(conn, protocol.MsgAuthCredentials, creds); err != nil {
		return "", fmt.Errorf("sending credentials: %w", err)
	}

	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	msgType, payload, err := protocol.ReadFrame(bufio.NewReader(conn))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if msgType == protocol.MsgAuthFail {
		return "", fmt.Errorf("%s", string(payload))
	}
	if msgType != protocol.MsgAuthOK {
		return "", fmt.Errorf("unexpected response type: 0x%02x", msgType)
	}

	var authOK struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(payload, &authOK); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}
	if authOK.APIKey == "" {
		return "", fmt.Errorf("server did not return an API key")
	}
	return authOK.APIKey, nil
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
