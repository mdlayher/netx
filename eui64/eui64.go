// Package eui64 enables creation and parsing of Modified EUI-64 format
// interface identifiers, as described in RFC 4291, Section 2.5.1.
package eui64

import (
	"errors"
	"net"
)

// Possible errors due to bad input.
var (
	errInvalidIP     = errors.New("eui64: IP must be an IPv6 address")
	errInvalidMAC    = errors.New("eui64: MAC address must be in EUI-48 or EUI-64 form")
	errInvalidPrefix = errors.New("eui64: prefix must be an IPv6 address prefix of /64 or less")
)

// ParseIP parses an input IPv6 address to retrieve its IPv6 address prefix and
// EUI-48 or EUI-64 MAC address. ip must be an IPv6 address or an error is
// returned.
func ParseIP(ip net.IP) (net.IP, net.HardwareAddr, error) {
	if !isIPv6Addr(ip) {
		return nil, nil, errInvalidIP
	}

	// Prefix is first 8 bytes of IPv6 address.
	prefix := make(net.IP, 16)
	copy(prefix[0:8], ip[0:8])

	// If IP address contains bytes 0xff and 0xfe adjacent in the middle
	// of the MAC address section, these bytes must be removed to parse
	// a EUI-48 hardware address.
	isEUI48 := ip[11] == 0xff && ip[12] == 0xfe

	// MAC address length is determined by whether address is EUI-48 or EUI-64.
	macLen := 8
	if isEUI48 {
		macLen = 6
	}

	mac := make(net.HardwareAddr, macLen)

	if isEUI48 {
		// Copy bytes preceeding and succeeding 0xff and 0xfe into MAC.
		copy(mac[0:3], ip[8:11])
		copy(mac[3:6], ip[13:16])
	} else {
		// Copy IP directly into MAC.
		copy(mac, ip[8:16])
	}

	// Flip 7th bit from left on the first byte of the MAC address, the
	// "universal/local (U/L)" bit.  See RFC 4291, Section 2.5.1 for more
	// information.
	mac[0] ^= 0x02

	return prefix, mac, nil
}

// ParseMAC parses an input IPv6 address prefix and EUI-48 or EUI-64 MAC
// address to retrieve an IPv6 address in EUI-64 modified form, with the
// designated prefix.
//
// An error is returned if prefix is not an IPv6 address with only the first 64
// bits or less set, or mac is not in EUI-48 or EUI-64 form.
func ParseMAC(prefix net.IP, mac net.HardwareAddr) (net.IP, error) {
	if !isIPv6Addr(prefix) {
		return nil, errInvalidIP
	}

	// Prefix must be 64 bits or less in length, meaning the last 8
	// bytes must be entirely zero.
	if !isAllZeroes(prefix[8:16]) {
		return nil, errInvalidPrefix
	}

	// MAC must be in EUI-48 or EUI64 form.
	if len(mac) != 6 && len(mac) != 8 {
		return nil, errInvalidMAC
	}

	// Copy prefix directly into first 8 bytes of IP address.
	ip := make(net.IP, 16)
	copy(ip[0:8], prefix[0:8])

	// Flip 7th bit from left on the first byte of the MAC address, the
	// "universal/local (U/L)" bit.  See RFC 4291, Section 2.5.1 for more
	// information.

	// If MAC is in EUI-64 form, directly copy it into output IP address.
	if len(mac) == 8 {
		copy(ip[8:16], mac)
		ip[8] ^= 0x02
		return ip, nil
	}

	// If MAC is in EUI-48 form, split first three bytes and last three bytes,
	// and inject 0xff and 0xfe between them.
	copy(ip[8:11], mac[0:3])
	ip[8] ^= 0x02
	ip[11] = 0xff
	ip[12] = 0xfe
	copy(ip[13:16], mac[3:6])

	return ip, nil
}

// isAllZeroes returns if a byte slice is entirely populated with byte 0.
func isAllZeroes(b []byte) bool {
	for i := 0; i < len(b); i++ {
		if b[i] != 0 {
			return false
		}
	}

	return true
}

// isIPv6Addr returns if an IP address is a valid IPv6 address.
func isIPv6Addr(ip net.IP) bool {
	if ip.To16() == nil {
		return false
	}

	return ip.To4() == nil
}
