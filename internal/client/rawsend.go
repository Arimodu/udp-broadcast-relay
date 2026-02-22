package client

import (
	"log/slog"

	"github.com/Arimodu/udp-broadcast-relay/internal/netutil"
)

type RawSender struct {
	fd         int
	interfaces []string
	log        *slog.Logger
}

func (rs *RawSender) getInterfaces() ([]netutil.InterfaceInfo, error) {
	if len(rs.interfaces) > 0 {
		var result []netutil.InterfaceInfo
		for _, name := range rs.interfaces {
			info, err := netutil.GetInterfaceByName(name)
			if err != nil {
				rs.log.Warn("skipping interface", "name", name, "error", err)
				continue
			}
			result = append(result, *info)
		}
		return result, nil
	}

	return netutil.GetBroadcastInterfaces()
}
