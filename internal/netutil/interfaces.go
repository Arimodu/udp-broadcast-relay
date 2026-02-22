package netutil

import (
	"fmt"
	"net"
)

// InterfaceInfo holds the network details of an interface.
type InterfaceInfo struct {
	Name          string
	IP            net.IP
	Mask          net.IPMask
	BroadcastAddr net.IP
}

// GetBroadcastInterfaces returns all non-loopback IPv4 interfaces with broadcast addresses.
func GetBroadcastInterfaces() ([]InterfaceInfo, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("listing interfaces: %w", err)
	}

	var result []InterfaceInfo
	for _, iface := range ifaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP.To4()
			if ip == nil {
				continue // skip IPv6
			}

			broadcast := calculateBroadcast(ip, ipNet.Mask)
			result = append(result, InterfaceInfo{
				Name:          iface.Name,
				IP:            ip,
				Mask:          ipNet.Mask,
				BroadcastAddr: broadcast,
			})
		}
	}

	return result, nil
}

// GetInterfaceByName returns the InterfaceInfo for a specific interface.
func GetInterfaceByName(name string) (*InterfaceInfo, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, fmt.Errorf("interface %s: %w", name, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("interface %s addrs: %w", name, err)
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}

		ip := ipNet.IP.To4()
		if ip == nil {
			continue
		}

		broadcast := calculateBroadcast(ip, ipNet.Mask)
		return &InterfaceInfo{
			Name:          iface.Name,
			IP:            ip,
			Mask:          ipNet.Mask,
			BroadcastAddr: broadcast,
		}, nil
	}

	return nil, fmt.Errorf("interface %s has no IPv4 address", name)
}

// GetInterfaceIndex returns the index of a named interface.
func GetInterfaceIndex(name string) (int, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return 0, err
	}
	return iface.Index, nil
}

// ListInterfaceNames returns the names of all up, non-loopback interfaces.
func ListInterfaceNames() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var names []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		names = append(names, iface.Name)
	}
	return names, nil
}

// GetLocalIPs returns all non-loopback IPv4 addresses on the machine.
func GetLocalIPs() ([]net.IP, error) {
	ifaces, err := GetBroadcastInterfaces()
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	for _, info := range ifaces {
		ips = append(ips, info.IP)
	}
	return ips, nil
}

func calculateBroadcast(ip net.IP, mask net.IPMask) net.IP {
	broadcast := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		broadcast[i] = ip[i] | ^mask[i]
	}
	return broadcast
}
