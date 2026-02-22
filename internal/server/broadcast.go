package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/Arimodu/udp-broadcast-relay/internal/database"
	"github.com/Arimodu/udp-broadcast-relay/internal/protocol"
)

// BroadcastManager dynamically starts and stops per-rule capture goroutines.
// All methods are safe for concurrent use.
type BroadcastManager struct {
	mu        sync.Mutex
	active    map[int64]context.CancelFunc
	hub       *Hub
	db        *database.DB
	log       *slog.Logger
	parentCtx context.Context
}

func NewBroadcastManager(ctx context.Context, hub *Hub, db *database.DB, log *slog.Logger) *BroadcastManager {
	return &BroadcastManager{
		active:    make(map[int64]context.CancelFunc),
		hub:       hub,
		db:        db,
		log:       log,
		parentCtx: ctx,
	}
}

// Sync stops any existing capture for the rule and, if the rule is enabled
// and not client_to_server, starts a new one. Safe to call on create,
// update, and toggle.
func (m *BroadcastManager) Sync(rule database.ForwardRule) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Always stop any running capture for this rule first
	if cancel, ok := m.active[rule.ID]; ok {
		cancel()
		delete(m.active, rule.ID)
	}

	if !rule.IsEnabled || rule.Direction == "client_to_server" {
		return
	}

	ctx, cancel := context.WithCancel(m.parentCtx)
	m.active[rule.ID] = cancel
	bc := NewBroadcastCapture(rule, m.hub, m.db, m.log)
	go func() {
		if err := bc.Listen(ctx); err != nil && ctx.Err() == nil {
			m.log.Error("broadcast capture error", "rule", rule.Name, "error", err)
		}
	}()
}

// Stop terminates the capture goroutine for the given rule ID, if running.
func (m *BroadcastManager) Stop(ruleID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.active[ruleID]; ok {
		cancel()
		delete(m.active, ruleID)
	}
}

type BroadcastCapture struct {
	rule database.ForwardRule
	hub  *Hub
	db   *database.DB
	log  *slog.Logger
}

func NewBroadcastCapture(rule database.ForwardRule, hub *Hub, db *database.DB, log *slog.Logger) *BroadcastCapture {
	return &BroadcastCapture{
		rule: rule,
		hub:  hub,
		db:   db,
		log:  log,
	}
}

func (bc *BroadcastCapture) Listen(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", bc.rule.ListenIP, bc.rule.ListenPort)
	pc, err := net.ListenPacket("udp4", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	defer pc.Close()

	bc.log.Info("broadcast capture started", "rule", bc.rule.Name, "addr", addr)

	// Close when context is done
	go func() {
		<-ctx.Done()
		pc.Close()
	}()

	buf := make([]byte, 65535)
	for {
		n, srcAddr, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			bc.log.Error("reading broadcast", "rule", bc.rule.Name, "error", err)
			continue
		}

		// Parse source address
		udpAddr, ok := srcAddr.(*net.UDPAddr)
		if !ok {
			continue
		}

		srcIP := udpAddr.IP.To4()
		if srcIP == nil {
			continue
		}

		var srcIPArr, dstIPArr [4]byte
		copy(srcIPArr[:], srcIP)
		// Set destination to the broadcast address from the rule
		dstIP := net.ParseIP(bc.rule.DestBroadcast).To4()
		if dstIP == nil {
			dstIP = net.IPv4(255, 255, 255, 255).To4()
		}
		copy(dstIPArr[:], dstIP)

		payload := make([]byte, n)
		copy(payload, buf[:n])

		// Encode relay packet
		relayData := protocol.EncodeRelayPacket(
			uint32(bc.rule.ID),
			srcIPArr,
			dstIPArr,
			uint16(udpAddr.Port),
			uint16(bc.rule.ListenPort),
			payload,
		)

		// Log packet
		ruleID := bc.rule.ID
		bc.db.LogPacket(&ruleID, srcIP.String(), bc.rule.DestBroadcast, udpAddr.Port, bc.rule.ListenPort, n, "server_to_client", "")

		// Send to hub for fan-out
		bc.hub.Broadcast(&RelayMessage{
			SenderID: 0, // from server
			RuleID:   uint32(bc.rule.ID),
			Data:     relayData,
		})

		bc.log.Debug("broadcast captured",
			"rule", bc.rule.Name,
			"src", srcAddr.String(),
			"size", n,
		)
	}
}
