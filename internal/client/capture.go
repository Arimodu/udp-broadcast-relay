package client

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/Arimodu/udp-broadcast-relay/internal/database"
	"github.com/Arimodu/udp-broadcast-relay/internal/protocol"
)

// CaptureListener captures local broadcast packets for bidirectional forwarding.
type CaptureListener struct {
	rule   database.ForwardRule
	sendCh chan []byte // channel to write frames to server
	log    *slog.Logger
}

func NewCaptureListener(rule database.ForwardRule, sendCh chan []byte, log *slog.Logger) *CaptureListener {
	return &CaptureListener{
		rule:   rule,
		sendCh: sendCh,
		log:    log,
	}
}

func (cl *CaptureListener) Listen(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", cl.rule.ListenPort)
	pc, err := net.ListenPacket("udp4", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	defer pc.Close()

	cl.log.Info("capture listener started", "rule", cl.rule.Name, "port", cl.rule.ListenPort)

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
			cl.log.Debug("capture read error", "error", err)
			continue
		}

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

		dstIP := net.ParseIP(cl.rule.DestBroadcast).To4()
		if dstIP == nil {
			dstIP = net.IPv4(255, 255, 255, 255).To4()
		}
		copy(dstIPArr[:], dstIP)

		payload := make([]byte, n)
		copy(payload, buf[:n])

		relayData := protocol.EncodeRelayPacket(
			uint32(cl.rule.ID),
			srcIPArr,
			dstIPArr,
			uint16(udpAddr.Port),
			uint16(cl.rule.ListenPort),
			payload,
		)

		select {
		case cl.sendCh <- relayData:
		default:
			cl.log.Warn("send buffer full, dropping captured packet")
		}
	}
}
