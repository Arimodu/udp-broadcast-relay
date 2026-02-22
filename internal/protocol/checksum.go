package protocol

// Checksum calculates the Internet checksum (RFC 1071).
func Checksum(data []byte) uint16 {
	var sum uint32
	length := len(data)

	for i := 0; i+1 < length; i += 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
	}

	// Handle odd byte
	if length%2 == 1 {
		sum += uint32(data[length-1]) << 8
	}

	// Fold 32-bit sum to 16 bits
	for sum>>16 != 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}

	return ^uint16(sum)
}
