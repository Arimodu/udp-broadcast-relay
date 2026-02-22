package client

import (
	"fmt"
	"log/slog"
	"net"
	"syscall"

	"github.com/Arimodu/udp-broadcast-relay/internal/protocol"
)

func NewRawSender(interfaces []string, log *slog.Logger) (*RawSender, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_RAW)
	if err != nil {
		return nil, fmt.Errorf("creating raw socket (need CAP_NET_RAW): %w", err)
	}

	if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_HDRINCL, 1); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("setting IP_HDRINCL: %w", err)
	}

	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("setting SO_BROADCAST: %w", err)
	}

	return &RawSender{
		fd:         fd,
		interfaces: interfaces,
		log:        log,
	}, nil
}

func (rs *RawSender) Send(pkt RebroadcastPacket) error {
	ifaces, err := rs.getInterfaces()
	if err != nil {
		return fmt.Errorf("getting interfaces: %w", err)
	}

	for _, iface := range ifaces {
		broadcastIP := iface.BroadcastAddr.To4()
		if broadcastIP == nil {
			continue
		}

		var dstAddr [4]byte
		copy(dstAddr[:], broadcastIP)

		rawPacket := protocol.BuildIPUDPPacket(
			pkt.SrcIP,
			dstAddr,
			pkt.SrcPort,
			pkt.DstPort,
			pkt.Payload,
		)

		addr := &syscall.SockaddrInet4{
			Port: int(pkt.DstPort),
		}
		copy(addr.Addr[:], broadcastIP)

		if err := syscall.Sendto(rs.fd, rawPacket, 0, addr); err != nil {
			rs.log.Debug("sendto failed", "interface", iface.Name, "broadcast", broadcastIP, "error", err)
			continue
		}

		rs.log.Debug("rebroadcast sent",
			"interface", iface.Name,
			"broadcast", broadcastIP,
			"src", net.IP(pkt.SrcIP[:]),
			"dst_port", pkt.DstPort,
			"size", len(pkt.Payload),
		)
	}

	return nil
}

func (rs *RawSender) Close() error {
	return syscall.Close(rs.fd)
}
