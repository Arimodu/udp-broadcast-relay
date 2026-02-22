package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"sync/atomic"
	"time"

	"github.com/Arimodu/udp-broadcast-relay/internal/database"
	"github.com/Arimodu/udp-broadcast-relay/internal/protocol"
)

// sendMsg carries a message type and payload for the write pump.
// Using a typed wrapper avoids the writePump having to hard-code MsgRelayPacket
// for all outbound messages (rule updates need MsgRuleUpdate instead).
type sendMsg struct {
	msgType uint8
	data    []byte
}

// ClientConn represents an authenticated client connection in the hub.
type ClientConn struct {
	ID        int64 // API key ID
	KeyName   string
	UserID    int64
	Addr      net.Addr
	SendCh    chan sendMsg // channel for outgoing messages
	Rules     []database.ForwardRule
	ConnectAt time.Time
	LastSeen  atomic.Value // time.Time
	BytesSent atomic.Int64
	BytesRecv atomic.Int64
}

type Hub struct {
	register   chan *ClientConn
	unregister chan *ClientConn
	broadcast  chan *RelayMessage
	snapshot   chan chan []ClientInfo // request/response for thread-safe snapshot
	ruleUpdate chan ruleUpdateReq
	clients    map[int64]*ClientConn // keyed by API key ID
	log        *slog.Logger
	db         *database.DB
}

type RelayMessage struct {
	SenderID int64 // API key ID of sender (0 if from server broadcast capture)
	RuleID   uint32
	Data     []byte // encoded relay packet
}

type ruleUpdateReq struct {
	KeyID int64
	Rules []database.ForwardRule
}

func NewHub(log *slog.Logger, db *database.DB) *Hub {
	return &Hub{
		register:   make(chan *ClientConn, 16),
		unregister: make(chan *ClientConn, 16),
		broadcast:  make(chan *RelayMessage, 256),
		snapshot:   make(chan chan []ClientInfo),
		ruleUpdate: make(chan ruleUpdateReq, 16),
		clients:    make(map[int64]*ClientConn),
		log:        log,
		db:         db,
	}
}

func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			for _, c := range h.clients {
				close(c.SendCh)
			}
			return

		case client := <-h.register:
			if old, ok := h.clients[client.ID]; ok {
				close(old.SendCh)
				h.log.Info("replaced existing connection", "key", old.KeyName)
			}
			h.clients[client.ID] = client
			h.log.Info("client connected", "key", client.KeyName, "addr", client.Addr)

		case client := <-h.unregister:
			if existing, ok := h.clients[client.ID]; ok && existing == client {
				delete(h.clients, client.ID)
				close(client.SendCh)
				h.log.Info("client disconnected", "key", client.KeyName)
			}

		case msg := <-h.broadcast:
			h.fanOut(msg)

		case replyCh := <-h.snapshot:
			replyCh <- h.buildSnapshot()

		case req := <-h.ruleUpdate:
			h.doRuleUpdate(req)
		}
	}
}

func (h *Hub) fanOut(msg *RelayMessage) {
	for id, client := range h.clients {
		if id == msg.SenderID {
			continue
		}

		if !h.clientHasRule(client, msg.RuleID) {
			continue
		}

		select {
		case client.SendCh <- sendMsg{msgType: protocol.MsgRelayPacket, data: msg.Data}:
			client.BytesSent.Add(int64(len(msg.Data)))
		default:
			h.log.Warn("client send buffer full, dropping packet", "key", client.KeyName)
		}
	}
}

func (h *Hub) clientHasRule(client *ClientConn, ruleID uint32) bool {
	for _, r := range client.Rules {
		if r.ID == int64(ruleID) {
			return true
		}
	}
	return false
}

func (h *Hub) buildSnapshot() []ClientInfo {
	infos := make([]ClientInfo, 0)
	for _, c := range h.clients {
		lastSeen, _ := c.LastSeen.Load().(time.Time)
		infos = append(infos, ClientInfo{
			KeyID:     c.ID,
			KeyName:   c.KeyName,
			Addr:      c.Addr.String(),
			ConnectAt: c.ConnectAt,
			LastSeen:  lastSeen,
			BytesSent: c.BytesSent.Load(),
			BytesRecv: c.BytesRecv.Load(),
			Online:    time.Since(lastSeen) < 45*time.Second,
		})
	}
	return infos
}

func (h *Hub) doRuleUpdate(req ruleUpdateReq) {
	client, ok := h.clients[req.KeyID]
	if !ok {
		return
	}

	client.Rules = req.Rules

	rulesJSON, err := json.Marshal(req.Rules)
	if err != nil {
		h.log.Error("marshaling rules for push", "error", err)
		return
	}

	select {
	case client.SendCh <- sendMsg{msgType: protocol.MsgRuleUpdate, data: rulesJSON}:
	default:
		h.log.Warn("could not push rule update, buffer full", "key", client.KeyName)
	}
}

func (h *Hub) Register(client *ClientConn) {
	h.register <- client
}

func (h *Hub) Unregister(client *ClientConn) {
	h.unregister <- client
}

func (h *Hub) Broadcast(msg *RelayMessage) {
	h.broadcast <- msg
}

// GetConnectedClients returns a thread-safe snapshot of connected clients.
func (h *Hub) GetConnectedClients() []ClientInfo {
	replyCh := make(chan []ClientInfo, 1)
	h.snapshot <- replyCh
	return <-replyCh
}

// PushRuleUpdate sends updated rules to a specific client (thread-safe).
func (h *Hub) PushRuleUpdate(keyID int64, rules []database.ForwardRule) {
	h.ruleUpdate <- ruleUpdateReq{KeyID: keyID, Rules: rules}
}

type ClientInfo struct {
	KeyID     int64     `json:"key_id"`
	KeyName   string    `json:"key_name"`
	Addr      string    `json:"addr"`
	ConnectAt time.Time `json:"connect_at"`
	LastSeen  time.Time `json:"last_seen"`
	BytesSent int64     `json:"bytes_sent"`
	BytesRecv int64     `json:"bytes_recv"`
	Online    bool      `json:"online"`
}
