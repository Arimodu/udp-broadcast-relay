package server

import (
	"context"
	"encoding/binary"
	"net"
	"syscall"
	"unsafe"

	"github.com/Arimodu/udp-broadcast-relay/internal/netutil"
)

func htons(v uint16) uint16 {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return *(*uint16)(unsafe.Pointer(&b[0]))
}

func (m *Monitor) Run(ctx context.Context) error {
	ifIndex, err := netutil.GetInterfaceIndex(m.ifaceName)
	if err != nil {
		m.log.Warn("broadcast monitor unavailable", "error", err)
		return nil
	}

	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_DGRAM, int(htons(syscall.ETH_P_IP)))
	if err != nil {
		m.log.Warn("AF_PACKET socket failed (need CAP_NET_RAW), monitor disabled", "error", err)
		return nil
	}
	defer syscall.Close(fd)

	addr := &syscall.SockaddrLinklayer{
		Protocol: htons(syscall.ETH_P_IP),
		Ifindex:  ifIndex,
	}
	if err := syscall.Bind(fd, addr); err != nil {
		m.log.Warn("binding AF_PACKET socket", "error", err)
		return nil
	}

	tv := syscall.Timeval{Sec: 1, Usec: 0}
	syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)

	m.log.Info("broadcast monitor started", "interface", m.ifaceName)

	buf := make([]byte, 65536)
	for {
		if ctx.Err() != nil {
			return nil
		}

		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			continue
		}

		if n < 20 {
			continue
		}

		m.processPacket(buf[:n])
	}
}

func (m *Monitor) processPacket(data []byte) {
	if len(data) < 20 {
		return
	}

	version := data[0] >> 4
	if version != 4 {
		return
	}

	ihl := int(data[0]&0x0F) * 4
	proto := data[9]

	if proto != 17 {
		return
	}

	if len(data) < ihl+8 {
		return
	}

	var srcIP, dstIP [4]byte
	copy(srcIP[:], data[12:16])
	copy(dstIP[:], data[16:20])

	dst := net.IP(dstIP[:])
	if !isBroadcast(dst) {
		return
	}

	srcPort := binary.BigEndian.Uint16(data[ihl : ihl+2])
	dstPort := binary.BigEndian.Uint16(data[ihl+2 : ihl+4])

	protocolType := netutil.IdentifyBroadcast(dstPort)

	srcIPStr := net.IP(srcIP[:]).String()
	dstIPStr := dst.String()

	m.db.UpsertBroadcastObservation(srcIPStr, dstIPStr, int(srcPort), int(dstPort), protocolType)
}

func isBroadcast(ip net.IP) bool {
	ip = ip.To4()
	if ip == nil {
		return false
	}

	if ip.Equal(net.IPv4(255, 255, 255, 255)) {
		return true
	}

	if ip[3] == 255 {
		return true
	}

	if ip[0] >= 224 && ip[0] <= 239 {
		return true
	}

	return false
}
