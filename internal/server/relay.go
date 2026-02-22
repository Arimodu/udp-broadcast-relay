package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	authpkg "github.com/Arimodu/udp-broadcast-relay/internal/auth"
	"github.com/Arimodu/udp-broadcast-relay/internal/database"
	"github.com/Arimodu/udp-broadcast-relay/internal/protocol"
	"golang.org/x/crypto/bcrypt"
)

type RelayListener struct {
	port int
	hub  *Hub
	db   *database.DB
	log  *slog.Logger
}

func NewRelayListener(port int, hub *Hub, db *database.DB, log *slog.Logger) *RelayListener {
	return &RelayListener{
		port: port,
		hub:  hub,
		db:   db,
		log:  log,
	}
}

func (rl *RelayListener) Listen(ctx context.Context) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", rl.port))
	if err != nil {
		return fmt.Errorf("listening on port %d: %w", rl.port, err)
	}

	rl.log.Info("relay listener started", "port", rl.port)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			rl.log.Error("accepting connection", "error", err)
			continue
		}

		go rl.handleConnection(ctx, conn)
	}
}

// authOKPayload is the JSON body of MsgAuthOK.
// APIKey is only populated when the client authenticated with credentials;
// the client should persist it and use it for all future connections.
type authOKPayload struct {
	APIKey string                  `json:"api_key,omitempty"`
	Rules  []database.ForwardRule  `json:"rules"`
}

func (rl *RelayListener) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	log := rl.log.With("remote", conn.RemoteAddr())
	log.Debug("new TCP connection")

	// First message must be MsgAuth or MsgAuthCredentials (within 30s)
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	reader := bufio.NewReader(conn)
	firstType, payload, err := protocol.ReadFrame(reader)
	if err != nil {
		log.Debug("reading auth frame failed", "error", err)
		return
	}

	var ak *database.APIKey
	var user *database.User
	var generatedKey string // non-empty when we created a new key for the client

	switch firstType {
	case protocol.MsgAuth:
		// API key authentication
		apiKey := string(payload)
		ak, user, err = rl.db.ValidateAPIKey(apiKey)
		if err != nil {
			log.Error("validating API key", "error", err)
			protocol.WriteFrame(conn, protocol.MsgAuthFail, []byte("internal error"))
			return
		}
		if ak == nil || user == nil {
			log.Info("authentication failed (invalid key)")
			protocol.WriteFrame(conn, protocol.MsgAuthFail, []byte("invalid or revoked API key"))
			return
		}

	case protocol.MsgAuthCredentials:
		// Username + password authentication: verify credentials, generate a new API key
		var creds struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.Unmarshal(payload, &creds); err != nil || creds.Username == "" || creds.Password == "" {
			protocol.WriteFrame(conn, protocol.MsgAuthFail, []byte("invalid credentials payload"))
			return
		}

		user, err = rl.db.GetUserByUsername(creds.Username)
		if err != nil {
			log.Error("looking up user for credential auth", "error", err)
			protocol.WriteFrame(conn, protocol.MsgAuthFail, []byte("internal error"))
			return
		}
		if user == nil || !user.IsActive {
			log.Info("credential auth failed (unknown or inactive user)", "username", creds.Username)
			protocol.WriteFrame(conn, protocol.MsgAuthFail, []byte("invalid username or password"))
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(creds.Password)); err != nil {
			log.Info("credential auth failed (wrong password)", "username", creds.Username)
			protocol.WriteFrame(conn, protocol.MsgAuthFail, []byte("invalid username or password"))
			return
		}

		// Generate a new API key and persist it so the client can use it next time
		generatedKey = authpkg.GenerateAPIKey()
		keyName := fmt.Sprintf("Auto (%s)", conn.RemoteAddr())
		ak, err = rl.db.CreateAPIKey(user.ID, generatedKey, keyName)
		if err != nil {
			log.Error("creating API key for credential auth", "error", err)
			protocol.WriteFrame(conn, protocol.MsgAuthFail, []byte("internal error"))
			return
		}
		log.Info("credential auth succeeded, API key generated",
			"username", user.Username, "key_id", ak.ID)

	default:
		log.Debug("expected auth message, got unexpected type",
			"type", fmt.Sprintf("0x%02x", firstType))
		protocol.WriteFrame(conn, protocol.MsgAuthFail, []byte("authentication required"))
		return
	}

	// Get rules assigned to this API key
	rules, err := rl.db.GetRulesForClient(ak.ID)
	if err != nil {
		log.Error("getting client rules", "error", err)
		protocol.WriteFrame(conn, protocol.MsgAuthFail, []byte("internal error"))
		return
	}

	// Send AuthOK; include the generated API key only for credential auth so the
	// client can persist it and skip password auth on future connections.
	okJSON, _ := json.Marshal(authOKPayload{APIKey: generatedKey, Rules: rules})
	if err := protocol.WriteFrame(conn, protocol.MsgAuthOK, okJSON); err != nil {
		log.Error("sending AuthOK", "error", err)
		return
	}

	log = log.With("key", ak.Name, "user", user.Username)
	log.Info("client authenticated", "rules", len(rules))

	// Clear read deadline
	conn.SetReadDeadline(time.Time{})

	now := time.Now()
	client := &ClientConn{
		ID:        ak.ID,
		KeyName:   ak.Name,
		UserID:    user.ID,
		Addr:      conn.RemoteAddr(),
		SendCh:    make(chan []byte, 256),
		Rules:     rules,
		ConnectAt: now,
	}
	client.LastSeen.Store(now)

	rl.hub.Register(client)
	defer rl.hub.Unregister(client)

	// Bidirectional read and write pumps
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	defer pumpCancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		rl.writePump(pumpCtx, conn, client)
		pumpCancel()
	}()
	go func() {
		defer wg.Done()
		rl.readPump(pumpCtx, reader, conn, client)
		pumpCancel()
	}()

	wg.Wait()
	log.Info("client connection closed", "key", client.KeyName)
}

func (rl *RelayListener) writePump(ctx context.Context, conn net.Conn, client *ClientConn) {
	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case data, ok := <-client.SendCh:
			if !ok {
				return
			}
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := protocol.WriteFrame(conn, protocol.MsgRelayPacket, data); err != nil {
				rl.log.Debug("write error", "key", client.KeyName, "error", err)
				return
			}

		case <-pingTicker.C:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := protocol.WriteFrame(conn, protocol.MsgPing, nil); err != nil {
				rl.log.Debug("ping write error", "key", client.KeyName, "error", err)
				return
			}
		}
	}
}

func (rl *RelayListener) readPump(ctx context.Context, reader *bufio.Reader, conn net.Conn, client *ClientConn) {
	for {
		// Keepalive: 45s receive deadline
		conn.SetReadDeadline(time.Now().Add(45 * time.Second))

		msgType, payload, err := protocol.ReadFrame(reader)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			rl.log.Debug("read error", "key", client.KeyName, "error", err)
			return
		}

		client.LastSeen.Store(time.Now())

		switch msgType {
		case protocol.MsgPing:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			protocol.WriteFrame(conn, protocol.MsgPong, nil)

		case protocol.MsgPong:
			// Already updated LastSeen above

		case protocol.MsgRelayPacket:
			// Client is sending a broadcast packet (bidirectional / client_to_server rule)
			client.BytesRecv.Add(int64(len(payload)))
			rl.handleClientRelayPacket(client, payload)

		default:
			rl.log.Debug("unknown message type from client",
				"type", fmt.Sprintf("0x%02x", msgType), "key", client.KeyName)
		}
	}
}

func (rl *RelayListener) handleClientRelayPacket(client *ClientConn, payload []byte) {
	header, udpPayload, err := protocol.DecodeRelayPacket(payload)
	if err != nil {
		rl.log.Debug("invalid relay packet from client", "key", client.KeyName, "error", err)
		return
	}

	// Verify this client is authorized for the rule and the rule direction allows C->S
	var rule *database.ForwardRule
	for i := range client.Rules {
		if client.Rules[i].ID == int64(header.RuleID) {
			r := client.Rules[i]
			rule = &r
			break
		}
	}
	if rule == nil {
		rl.log.Warn("client sent packet for unauthorized rule",
			"key", client.KeyName, "rule_id", header.RuleID)
		return
	}
	if rule.Direction == "server_to_client" {
		rl.log.Warn("client sent packet for server_to_client rule (not allowed)",
			"key", client.KeyName, "rule", rule.Name)
		return
	}

	// Log the packet
	srcIP := net.IP(header.SrcIP[:]).String()
	dstIP := net.IP(header.DstIP[:]).String()
	ruleID := int64(header.RuleID)
	if err := rl.db.LogPacket(&ruleID, srcIP, dstIP, int(header.SrcPort), int(header.DstPort),
		len(udpPayload), "client_to_server", client.KeyName); err != nil {
		rl.log.Debug("logging packet", "error", err)
	}

	// Fan out to other clients via hub
	rl.hub.Broadcast(&RelayMessage{
		SenderID: client.ID,
		RuleID:   header.RuleID,
		Data:     payload,
	})
}

func (rl *RelayListener) clientHasRule(client *ClientConn, ruleID uint32) bool {
	for _, r := range client.Rules {
		if r.ID == int64(ruleID) {
			return true
		}
	}
	return false
}
