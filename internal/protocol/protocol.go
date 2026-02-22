package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Message types
const (
	MsgPing            uint8 = 0x01
	MsgPong            uint8 = 0x02
	MsgAuth            uint8 = 0x10 // C→S: API key (raw bytes)
	MsgAuthOK          uint8 = 0x11 // S→C: JSON {"api_key":"...","rules":[...]}
	MsgAuthFail        uint8 = 0x12 // S→C: error string
	MsgAuthCredentials uint8 = 0x13 // C→S: JSON {"username":"...","password":"..."}
	MsgRelayPacket     uint8 = 0x20
	MsgRuleUpdate      uint8 = 0x30
)

// Frame format: [4 bytes length][1 byte type][payload...]
// Length = len(type) + len(payload) = 1 + len(payload)

const (
	FrameHeaderSize = 4 // length prefix
	MaxFrameSize    = 65536
)

// WriteFrame writes a framed message to the writer.
func WriteFrame(w io.Writer, msgType uint8, payload []byte) error {
	length := uint32(1 + len(payload))
	if length > MaxFrameSize {
		return fmt.Errorf("frame too large: %d", length)
	}

	header := make([]byte, FrameHeaderSize+1)
	binary.BigEndian.PutUint32(header[:4], length)
	header[4] = msgType

	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("writing frame header: %w", err)
	}

	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return fmt.Errorf("writing frame payload: %w", err)
		}
	}

	return nil
}

// ReadFrame reads a framed message from the reader.
// Returns the message type and payload.
func ReadFrame(r io.Reader) (uint8, []byte, error) {
	header := make([]byte, FrameHeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, fmt.Errorf("reading frame header: %w", err)
	}

	length := binary.BigEndian.Uint32(header)
	if length == 0 || length > MaxFrameSize {
		return 0, nil, fmt.Errorf("invalid frame length: %d", length)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return 0, nil, fmt.Errorf("reading frame data: %w", err)
	}

	msgType := data[0]
	var payload []byte
	if length > 1 {
		payload = data[1:]
	}

	return msgType, payload, nil
}

// RelayPacket header format:
// RuleID(4) + SrcIP(4) + DstIP(4) + SrcPort(2) + DstPort(2) + PayloadLen(2) = 18 bytes
const RelayHeaderSize = 18

type RelayPacketHeader struct {
	RuleID     uint32
	SrcIP      [4]byte
	DstIP      [4]byte
	SrcPort    uint16
	DstPort    uint16
	PayloadLen uint16
}

func EncodeRelayPacket(ruleID uint32, srcIP, dstIP [4]byte, srcPort, dstPort uint16, payload []byte) []byte {
	buf := make([]byte, RelayHeaderSize+len(payload))
	binary.BigEndian.PutUint32(buf[0:4], ruleID)
	copy(buf[4:8], srcIP[:])
	copy(buf[8:12], dstIP[:])
	binary.BigEndian.PutUint16(buf[12:14], srcPort)
	binary.BigEndian.PutUint16(buf[14:16], dstPort)
	binary.BigEndian.PutUint16(buf[16:18], uint16(len(payload)))
	copy(buf[18:], payload)
	return buf
}

func DecodeRelayPacket(data []byte) (*RelayPacketHeader, []byte, error) {
	if len(data) < RelayHeaderSize {
		return nil, nil, fmt.Errorf("relay packet too short: %d bytes", len(data))
	}

	h := &RelayPacketHeader{
		RuleID:     binary.BigEndian.Uint32(data[0:4]),
		SrcPort:    binary.BigEndian.Uint16(data[12:14]),
		DstPort:    binary.BigEndian.Uint16(data[14:16]),
		PayloadLen: binary.BigEndian.Uint16(data[16:18]),
	}
	copy(h.SrcIP[:], data[4:8])
	copy(h.DstIP[:], data[8:12])

	payload := data[RelayHeaderSize:]
	if len(payload) < int(h.PayloadLen) {
		return nil, nil, fmt.Errorf("relay payload truncated: expected %d, got %d", h.PayloadLen, len(payload))
	}
	payload = payload[:h.PayloadLen]

	return h, payload, nil
}
