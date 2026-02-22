//go:build !linux

package server

import "context"

func (m *Monitor) Run(ctx context.Context) error {
	m.log.Warn("broadcast monitor requires Linux (AF_PACKET), running in passive mode")
	<-ctx.Done()
	return nil
}
