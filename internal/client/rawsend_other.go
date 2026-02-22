//go:build !linux

package client

import (
	"fmt"
	"log/slog"
)

func NewRawSender(interfaces []string, log *slog.Logger) (*RawSender, error) {
	return nil, fmt.Errorf("raw socket rebroadcast is only supported on Linux")
}

func (rs *RawSender) Send(pkt RebroadcastPacket) error {
	return fmt.Errorf("raw socket not supported on this platform")
}

func (rs *RawSender) Close() error {
	return nil
}
