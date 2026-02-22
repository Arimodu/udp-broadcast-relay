package protocol

import (
	"encoding/binary"
	"fmt"
)

// BuildIPUDPPacket constructs a raw IP+UDP packet ready for injection via raw socket.
func BuildIPUDPPacket(srcIP, dstIP [4]byte, srcPort, dstPort uint16, payload []byte) []byte {
	udpLen := 8 + len(payload)
	totalLen := 20 + udpLen
	packet := make([]byte, totalLen)

	// IP Header (20 bytes)
	packet[0] = 0x45           // Version 4, IHL 5
	packet[1] = 0x00           // DSCP/ECN
	binary.BigEndian.PutUint16(packet[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(packet[4:6], 0)     // ID
	binary.BigEndian.PutUint16(packet[6:8], 0)     // Flags/Fragment
	packet[8] = 64             // TTL
	packet[9] = 17             // Protocol: UDP
	packet[10] = 0             // Header checksum (will be filled)
	packet[11] = 0
	copy(packet[12:16], srcIP[:])
	copy(packet[16:20], dstIP[:])

	// IP header checksum
	ipChecksum := Checksum(packet[:20])
	binary.BigEndian.PutUint16(packet[10:12], ipChecksum)

	// UDP Header (8 bytes)
	binary.BigEndian.PutUint16(packet[20:22], srcPort)
	binary.BigEndian.PutUint16(packet[22:24], dstPort)
	binary.BigEndian.PutUint16(packet[24:26], uint16(udpLen))
	// UDP checksum = 0 (optional for IPv4)
	packet[26] = 0
	packet[27] = 0

	// UDP Payload
	copy(packet[28:], payload)

	return packet
}

// ParseIPUDPPacket extracts header fields and payload from a raw IP+UDP packet.
func ParseIPUDPPacket(packet []byte) (srcIP, dstIP [4]byte, srcPort, dstPort uint16, payload []byte, err error) {
	if len(packet) < 28 { // minimum IP (20) + UDP (8)
		return srcIP, dstIP, 0, 0, nil, fmt.Errorf("packet too short: %d bytes", len(packet))
	}

	// Check IP version
	version := packet[0] >> 4
	if version != 4 {
		return srcIP, dstIP, 0, 0, nil, fmt.Errorf("not IPv4: version %d", version)
	}

	ihl := int(packet[0]&0x0F) * 4
	if ihl < 20 || len(packet) < ihl+8 {
		return srcIP, dstIP, 0, 0, nil, fmt.Errorf("invalid IHL: %d", ihl)
	}

	// Check protocol is UDP
	if packet[9] != 17 {
		return srcIP, dstIP, 0, 0, nil, fmt.Errorf("not UDP: protocol %d", packet[9])
	}

	copy(srcIP[:], packet[12:16])
	copy(dstIP[:], packet[16:20])

	srcPort = binary.BigEndian.Uint16(packet[ihl : ihl+2])
	dstPort = binary.BigEndian.Uint16(packet[ihl+2 : ihl+4])
	udpLen := binary.BigEndian.Uint16(packet[ihl+4 : ihl+6])

	if int(udpLen) < 8 || ihl+int(udpLen) > len(packet) {
		return srcIP, dstIP, srcPort, dstPort, nil, fmt.Errorf("invalid UDP length: %d", udpLen)
	}

	payload = packet[ihl+8 : ihl+int(udpLen)]
	return srcIP, dstIP, srcPort, dstPort, payload, nil
}
