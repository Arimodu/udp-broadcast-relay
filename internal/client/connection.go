package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/Arimodu/udp-broadcast-relay/internal/config"
	"github.com/Arimodu/udp-broadcast-relay/internal/database"
	"github.com/Arimodu/udp-broadcast-relay/internal/protocol"
)

type Connection struct {
	cfg        *config.Config
	configPath string
	// username and password are only set when the user authenticated with
	// credentials instead of an API key. They are cleared (and never written to
	// disk) as soon as the server returns a generated API key.
	username  string
	password  string
	rebroadCh chan RebroadcastPacket // server -> client packets to rebroadcast locally
	sendCh    chan []byte            // client -> server: relay packets from local capture
	log       *slog.Logger
	rules     []database.ForwardRule
}

func NewConnection(cfg *config.Config, configPath, username, password string, rebroadCh chan RebroadcastPacket, log *slog.Logger) *Connection {
	return &Connection{
		cfg:        cfg,
		configPath: configPath,
		username:   username,
		password:   password,
		rebroadCh:  rebroadCh,
		sendCh:     make(chan []byte, 128),
		log:        log,
	}
}

// SendRelayPacket enqueues a captured packet to be sent to the server.
func (c *Connection) SendRelayPacket(data []byte) {
	select {
	case c.sendCh <- data:
	default:
		c.log.Warn("send buffer full, dropping captured packet")
	}
}

func (c *Connection) ConnectLoop(ctx context.Context) {
	backoff := 1 * time.Second
	maxBackoff := 60 * time.Second

	for {
		err := c.connectAndRun(ctx)
		if ctx.Err() != nil {
			return
		}

		c.log.Warn("disconnected from server", "error", err, "reconnect_in", backoff)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}

		backoff = min(backoff*2, maxBackoff)
	}
}

func (c *Connection) connectAndRun(ctx context.Context) error {
	c.log.Info("connecting to server", "addr", c.cfg.Client.ServerAddress)

	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", c.cfg.Client.ServerAddress)
	if err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	defer conn.Close()

	// Send auth frame: API key if we have one, credentials otherwise.
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if c.cfg.Client.APIKey != "" {
		if err := protocol.WriteFrame(conn, protocol.MsgAuth, []byte(c.cfg.Client.APIKey)); err != nil {
			return fmt.Errorf("sending auth: %w", err)
		}
	} else if c.username != "" {
		creds, _ := json.Marshal(map[string]string{
			"username": c.username,
			"password": c.password,
		})
		if err := protocol.WriteFrame(conn, protocol.MsgAuthCredentials, creds); err != nil {
			return fmt.Errorf("sending credentials: %w", err)
		}
	} else {
		return fmt.Errorf("no credentials available; set api_key in config or run interactively")
	}

	// Read auth response.
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	reader := bufio.NewReader(conn)
	msgType, payload, err := protocol.ReadFrame(reader)
	if err != nil {
		return fmt.Errorf("reading auth response: %w", err)
	}

	switch msgType {
	case protocol.MsgAuthOK:
		var authOK struct {
			APIKey string                 `json:"api_key"`
			Rules  []database.ForwardRule `json:"rules"`
		}
		if err := json.Unmarshal(payload, &authOK); err != nil {
			c.log.Warn("parsing AuthOK payload", "error", err)
		}
		c.rules = authOK.Rules
		c.log.Info("authenticated successfully", "rules", len(c.rules))

		// Server returned a generated API key (credential auth path).
		// Persist it to config and clear the in-memory credentials so all
		// subsequent reconnections use the key — credentials are never stored.
		if authOK.APIKey != "" {
			c.cfg.Client.APIKey = authOK.APIKey
			c.username = ""
			c.password = ""
			if err := config.Save(c.configPath, c.cfg); err != nil {
				c.log.Warn("failed to save generated API key to config", "error", err)
			} else {
				c.log.Info("API key saved to config; credentials discarded", "path", c.configPath)
			}
		}

	case protocol.MsgAuthFail:
		return fmt.Errorf("authentication failed: %s", string(payload))

	default:
		return fmt.Errorf("unexpected response type: 0x%02x", msgType)
	}

	conn.SetReadDeadline(time.Time{})
	conn.SetWriteDeadline(time.Time{})

	pumpCtx, pumpCancel := context.WithCancel(ctx)
	defer pumpCancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		c.readPump(pumpCtx, reader, conn)
		pumpCancel()
	}()

	go func() {
		defer wg.Done()
		c.writePump(pumpCtx, conn)
		pumpCancel()
	}()

	wg.Wait()
	return fmt.Errorf("connection closed")
}

func (c *Connection) readPump(ctx context.Context, reader *bufio.Reader, conn net.Conn) {
	for {
		conn.SetReadDeadline(time.Now().Add(45 * time.Second))

		msgType, payload, err := protocol.ReadFrame(reader)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.log.Debug("read error", "error", err)
			return
		}

		switch msgType {
		case protocol.MsgPing:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			protocol.WriteFrame(conn, protocol.MsgPong, nil)

		case protocol.MsgPong:
			// Keepalive acknowledged

		case protocol.MsgRelayPacket:
			header, udpPayload, err := protocol.DecodeRelayPacket(payload)
			if err != nil {
				c.log.Debug("invalid relay packet", "error", err)
				continue
			}

			select {
			case c.rebroadCh <- RebroadcastPacket{
				RuleID:  header.RuleID,
				SrcIP:   header.SrcIP,
				DstIP:   header.DstIP,
				SrcPort: header.SrcPort,
				DstPort: header.DstPort,
				Payload: udpPayload,
			}:
			default:
				c.log.Warn("rebroadcast buffer full, dropping packet")
			}

		case protocol.MsgRuleUpdate:
			var rules []database.ForwardRule
			if err := json.Unmarshal(payload, &rules); err != nil {
				c.log.Warn("parsing rule update", "error", err)
				continue
			}
			c.rules = rules
			c.log.Info("rules updated from server", "count", len(rules))

		default:
			c.log.Debug("unknown message type", "type", fmt.Sprintf("0x%02x", msgType))
		}
	}
}

func (c *Connection) writePump(ctx context.Context, conn net.Conn) {
	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case data, ok := <-c.sendCh:
			if !ok {
				return
			}
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := protocol.WriteFrame(conn, protocol.MsgRelayPacket, data); err != nil {
				c.log.Debug("write error (captured packet)", "error", err)
				return
			}

		case <-pingTicker.C:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := protocol.WriteFrame(conn, protocol.MsgPing, nil); err != nil {
				c.log.Debug("ping write error", "error", err)
				return
			}
		}
	}
}
