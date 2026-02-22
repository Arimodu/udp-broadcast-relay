# UDP Broadcast Relay (UBR)

UBR forwards UDP broadcast traffic between isolated network segments over TCP.
It solves the problem of LAN-discovery protocols (DHCP, mDNS, NetBIOS, SSDP,
WS-Discovery, etc.) not crossing router or VLAN boundaries.

A **server** captures broadcast packets from its local network and relays them
to connected **clients**, which rebroadcast them locally with the original
source IP preserved. Forwarding is bidirectional: clients can also capture
local broadcasts and send them back to the server.

All traffic flows over a single authenticated TCP connection per client.
Clients reconnect automatically with exponential backoff after any disconnect.

---

## Features

- **Broadcast capture** via raw sockets; preserves source IP on rebroadcast
- **Bidirectional forwarding** — server→client, client→server, or both
- **Automatic broadcast discovery** — passively detects all broadcast traffic
  on the server network and identifies known protocols by port
- **WebUI** — dashboard, forwarding rules CRUD, packet log, live monitor,
  API key management
- **Three setup modes** — interactive CLI, unattended flags, web wizard
- **API key authentication** — keys generated server-side, managed in the WebUI
- **Credential login** — clients may authenticate with username + password once;
  the server generates an API key that is saved to the client config
  automatically. Passwords are never stored on disk.
- **Systemd integration** — optional self-install as a system service
- **Single static binary** — all web assets embedded; no runtime dependencies

---

## Architecture

```
┌─────────────────────────────┐          ┌─────────────────────────────┐
│         UBR Server          │          │         UBR Client          │
│                             │          │                             │
│  ┌────────────────────────┐ │  TCP     │  ┌────────────────────────┐ │
│  │  Broadcast capture     │ │◄────────►│  │  Raw socket rebroadcast│ │
│  │  (raw socket / bind)   │ │ :14723   │  │  (SOCK_RAW IP_HDRINCL) │ │
│  └────────────────────────┘ │          │  └────────────────────────┘ │
│  ┌────────────────────────┐ │          │  ┌────────────────────────┐ │
│  │  Hub (channel fan-out) │ │          │  │  Capture (bidirectional│ │
│  └────────────────────────┘ │          │  │  rules, client→server) │ │
│  ┌────────────────────────┐ │          │  └────────────────────────┘ │
│  │  AF_PACKET monitor     │ │          │                             │
│  │  (broadcast discovery) │ │          │  Reconnects: 1s → 60s      │
│  └────────────────────────┘ │          │  exponential backoff        │
│  ┌────────────────────────┐ │          └─────────────────────────────┘
│  │  WebUI  :21480         │ │
│  │  SQLite database       │ │
│  └────────────────────────┘ │
└─────────────────────────────┘
```

### Default ports

| Port  | Purpose                    |
|-------|----------------------------|
| 14723 | Client relay TCP           |
| 21480 | Server WebUI (HTTP)        |

Both are configurable in `config.toml`.

---

## Building

**Native (for testing on the build host):**
```sh
go build -o ubr ./cmd/ubr
```

**Cross-compile for Linux x86-64 (production target):**
```sh
GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=0.1.0" \
    -o ubr-linux-amd64 ./cmd/ubr
```

The binary embeds all web assets. No external files are needed at runtime.

**Requirements:** Go 1.22+ (uses `net/http` route patterns with `{id}` syntax).

---

## Quick Start

### Server

```sh
# Interactive setup (creates admin account, optionally installs systemd service)
sudo ./ubr setup server

# Or unattended (for scripting/provisioning)
sudo ./ubr setup server \
    --admin-user admin --admin-pass 'changeme' \
    --interface eth0 \
    --config /etc/ubr/config.toml

# Then start the server
sudo ./ubr server --config /etc/ubr/config.toml
```

Open `http://<server-ip>:21480` and log in with the admin credentials.
Create forwarding rules under **Rules**, then create an API key under **API Keys**
and assign rules to it.

### Client

```sh
# Interactive setup
sudo ./ubr setup client

# Unattended (requires an API key from the server WebUI)
sudo ./ubr setup client \
    --server-address 10.0.0.1:14723 \
    --api-key ubr_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx \
    --config /etc/ubr/config.toml

# Start the client
sudo ./ubr client --config /etc/ubr/config.toml
```

If no `api_key` is set in the client config, `ubr client` will prompt for
username and password at startup. The server generates an API key and the
client saves it automatically — the password is never written to disk.

### Web Wizard

```sh
# Server wizard (includes live broadcast discovery)
sudo ./ubr setup server --wizard

# Client wizard
sudo ./ubr setup client --wizard
```

The wizard starts a temporary web server and prints the access URLs:

```
Open one of these URLs in your browser:
  http://localhost:52341
  http://10.0.0.1:52341
  http://myhost:52341
```

Walk through the steps, click **Save & Finish**, and the wizard exits.

---

## Setup Modes

All three modes produce identical config files and database state.

| Mode | Command | Use case |
|------|---------|----------|
| Interactive CLI | `ubr setup server` | First-time manual setup |
| Unattended | `ubr setup server --admin-user X --admin-pass Y` | Scripting, CI/CD |
| Web wizard | `ubr setup server --wizard` | Remote/headless setup |

---

## Configuration

Default location: `/etc/ubr/config.toml`

```toml
[server]
webui_port = 21480
relay_port = 14723
interface  = ""          # empty = listen on all interfaces
data_dir   = "/var/lib/ubr"
log_level  = "info"      # debug | info | warn | error

[client]
server_address = "10.0.0.1:14723"
api_key        = "ubr_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
interfaces     = ""      # empty = rebroadcast on all interfaces (comma-separated)
log_level      = "info"
```

The server and client sections coexist in the same file; only the relevant
section is read by each subcommand.

---

## Authentication

### WebUI (server)

Browser → `POST /api/auth/login` with `{username, password}` → bcrypt verify →
session cookie (`ubr_session`, 24 h, HttpOnly, SameSite=Strict).

### Clients (TCP relay)

Two methods, selected automatically based on config:

| Config state | Wire message | Server action |
|---|---|---|
| `api_key` present | `MsgAuth` (0x10) — raw key bytes | Validate key in DB |
| No `api_key` | `MsgAuthCredentials` (0x13) — JSON `{username, password}` | bcrypt verify → generate & persist new API key → return in AuthOK |

When credential auth succeeds, the server returns the new key in `MsgAuthOK`
and the client writes it to `api_key` in `config.toml`. All subsequent
reconnections use the key; credentials are discarded from memory immediately.

### API key format

`ubr_` + 32 characters from `base64.RawURLEncoding` of 32 cryptographically
random bytes (256 bits of entropy).

---

## Wire Protocol

All TCP framing:

```
[4 bytes: uint32 big-endian length] [1 byte: message type] [payload...]
```

Length includes the type byte. Maximum frame size: 65 536 bytes.

| Type | Code | Direction | Payload |
|------|------|-----------|---------|
| Ping | 0x01 | Both | — |
| Pong | 0x02 | Both | — |
| Auth | 0x10 | C→S | API key (raw UTF-8) |
| AuthOK | 0x11 | S→C | JSON `{"api_key":"…","rules":[…]}` |
| AuthFail | 0x12 | S→C | Error message (UTF-8) |
| AuthCredentials | 0x13 | C→S | JSON `{"username":"…","password":"…"}` |
| RelayPacket | 0x20 | Both | 18-byte header + UDP payload |
| RuleUpdate | 0x30 | S→C | JSON `[…]` forwarding rules |

`api_key` in `AuthOK` is only populated for `AuthCredentials` auth. `Auth`
(API key) responses return an empty string.

### RelayPacket header (18 bytes)

```
RuleID(4) SrcIP(4) DstIP(4) SrcPort(2) DstPort(2) PayloadLen(2)
```

### Keepalive

Both sides send `Ping` every 15 s. Connection is considered dead if no data
is received within 45 s.

---

## Forwarding Rules

Created and managed in the WebUI (**Rules** page).

| Field | Values |
|-------|--------|
| `listen_port` | UDP port to capture |
| `listen_ip` | Source IP filter (`0.0.0.0` = all) |
| `dest_broadcast` | Broadcast address to use on rebroadcast |
| `direction` | `server_to_client` · `client_to_server` · `bidirectional` |
| `is_enabled` | Toggle without deleting |

Rules are assigned to specific API keys (clients) via the **API Keys** page.
Each client only receives/forwards traffic for its assigned rules.
Rule assignments are pushed live to connected clients via `MsgRuleUpdate`.

---

## Broadcast Monitor

The server runs an `AF_PACKET` raw socket (requires `CAP_NET_RAW`) that
passively observes all broadcast IP traffic on the configured interface.
Detected streams are classified by destination port:

| Protocol | Port(s) |
|----------|---------|
| DHCP | 67, 68 |
| mDNS | 5353 |
| NetBIOS | 137, 138 |
| SSDP | 1900 |
| WS-Discovery | 3702 |
| LLMNR | 5355 |
| IPP | 631 |
| Wake-on-LAN | 9 |
| RIP | 520 |

The **Monitor** page in the WebUI shows detected broadcasts with counts and a
**Create Rule** button. The setup wizard's discovery step uses the same data.

---

## Systemd Service

When requested during setup (or manually with `--wizard`), UBR:

1. Copies itself to `/usr/local/bin/ubr`
2. Writes a unit file to `/etc/systemd/system/ubr-{server|client}.service`
3. Runs `systemctl daemon-reload && systemctl enable && systemctl start`

The unit uses `AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN` so the process
does not need to run as root after startup.

Example unit:

```ini
[Unit]
Description=UDP Broadcast Relay server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/ubr server --config /etc/ubr/config.toml
Restart=on-failure
RestartSec=5
AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

---

## Database

SQLite, stored in `data_dir` (default `/var/lib/ubr`). WAL journal mode,
foreign keys enabled.

| Table | Purpose |
|-------|---------|
| `users` | Admin accounts (bcrypt passwords) |
| `api_keys` | Client API keys with optional expiry and revocation |
| `sessions` | WebUI session tokens (24 h TTL) |
| `forward_rules` | Forwarding rule definitions |
| `client_rules` | Junction: which rules are assigned to which key |
| `packet_log` | Rolling packet log (capped at 10 000 rows) |
| `broadcast_observations` | AF_PACKET monitor results |

---

## WebUI Pages

| Page | Path | Description |
|------|------|-------------|
| Dashboard | `/` | Connected clients: IP, key name, connect time, bytes, status |
| Rules | `/rules` | Forwarding rules CRUD + enable/disable toggle |
| Packet Log | `/packets` | Filterable, paginated; auto-refreshes every 2 s |
| Monitor | `/monitor` | Detected broadcast streams with protocol identification |
| API Keys | `/keys` | Create, list, revoke, delete; assign rules per key |
| Settings | `/settings` | Change admin password |

---

## Security Notes

- Passwords are hashed with bcrypt (default cost).
- API keys carry 256 bits of entropy and are stored in plaintext in the DB
  (they are long-lived bearer tokens, similar to a session token).
- Client passwords are **never** stored on disk. They are held in process
  memory only until the server returns the generated API key, then zeroed.
- The WebUI has no HTTPS support by default. Put a reverse proxy (nginx,
  Caddy) in front if the WebUI is exposed beyond localhost.
- The relay TCP port has no TLS. For WAN use, consider a VPN or SSH tunnel.

---

## Dependencies

| Package | Reason |
|---------|--------|
| `modernc.org/sqlite` | Pure-Go SQLite driver (no CGO required) |
| `golang.org/x/crypto/bcrypt` | Password hashing — never DIY crypto |
| `github.com/BurntSushi/toml` | TOML config parser |

All other functionality uses the Go standard library.

---

## Project Layout

```
cmd/ubr/main.go              Entry point, subcommand routing
internal/
  auth/                      API key generation, session token helpers
  client/                    Client orchestrator, TCP connection, raw socket rebroadcast
  config/                    TOML config load/save
  database/                  SQLite open, migrations, all CRUD
  netutil/                   Interface enumeration, broadcast type identification
  protocol/                  Wire protocol framing, RelayPacket encode/decode
  server/                    Hub, TCP relay listener, broadcast capture, WebUI, monitor
  setup/                     Interactive/unattended/wizard setup
web/
  embed.go                   go:embed directive
  static/css/style.css       Simple.css + custom styles
  static/js/app.js           Vanilla JS SPA (main WebUI)
  static/js/wizard.js        Setup wizard JS
  templates/                 HTML templates (login, index, wizard)
```
