package netutil

import "fmt"

// IdentifyBroadcast identifies the protocol type of a broadcast packet
// based on destination port and other characteristics.
func IdentifyBroadcast(dstPort uint16) string {
	switch dstPort {
	case 67, 68:
		return "DHCP"
	case 137:
		return "NetBIOS-NS"
	case 138:
		return "NetBIOS-DGM"
	case 139:
		return "NetBIOS-SSN"
	case 520:
		return "RIP"
	case 631:
		return "IPP/CUPS"
	case 1900:
		return "SSDP/UPnP"
	case 3702:
		return "WS-Discovery"
	case 5353:
		return "mDNS"
	case 5355:
		return "LLMNR"
	case 9:
		return "WOL"
	case 111:
		return "SunRPC"
	case 161, 162:
		return "SNMP"
	case 443:
		return "QUIC/HTTPS"
	case 514:
		return "Syslog"
	case 1194:
		return "OpenVPN"
	case 4500:
		return "IPSec-NAT"
	case 10001:
		return "Ubiquiti"
	default:
		return fmt.Sprintf("Unknown:%d", dstPort)
	}
}
