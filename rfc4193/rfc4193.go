package rfc4193

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// ula is the IPv6 Unique Local Address prefix.
var ula = &net.IPNet{
	IP:   net.ParseIP("fc00::"),
	Mask: net.CIDRMask(7, 128),
}

// A Prefix represents a Local IPv6 Unicast Address prefix, as described in
// RFC 4193, section 3.1.
type Prefix struct {
	// Local indicates if the prefix is locally assigned.
	Local bool

	// GlobalID stores an identifier for a globally unique prefix.
	GlobalID [5]byte

	// SubnetID identifies an individual /64 subnet within a Prefix.
	SubnetID uint16

	// mask is the Prefix's length. It is set to either /48 or /64 when IPNet or
	// Subnet is called, depending on whether the Prefix was created by Generate
	// or manually by the caller.
	mask net.IPMask
}

// IPNet produces a *net.IPNet prefix value from a Prefix.
func (p *Prefix) IPNet() *net.IPNet {
	// Finalize the computation started by Generate:
	//
	// "6) Concatenate FC00::/7, the L bit set to 1, and the 40-bit Global
	// ID to create a Local IPv6 address prefix."
	ip := net.IP{0: 0xfc, 15: 0x00}
	if p.Local {
		ip[0] |= 0x01
	}

	copy(ip[1:6], p.GlobalID[:])

	// Also set the subnet ID portion. If this Prefix was produced by Generate
	// and no subnet ID and mask were previously assigned, we will produce a /48.
	//
	// However, if Prefix.Subnet was called, all subsequent calls will produce
	// a /64 subnet within the parent /48 prefix.
	binary.BigEndian.PutUint16(ip[6:8], p.SubnetID)
	if p.mask == nil {
		if p.SubnetID == 0 {
			p.mask = net.CIDRMask(48, 128)
		} else {
			p.mask = net.CIDRMask(64, 128)
		}
	}

	return &net.IPNet{
		IP:   ip,
		Mask: p.mask,
	}
}

// Subnet produces a /64 Prefix with the specified subnet ID.
//
// If p is a /48 Prefix, the new /64 Prefix will be a child of that parent
// /48 Prefix.
//
// If p is a /64 Prefix (typically produced through a previous call to Subnet),
// the new /64 Prefix will be a sibling /64 Prefix of the source /64 Prefix.
func (p *Prefix) Subnet(id uint16) *Prefix {
	// Make a copy of p's parameters and produce a /64 prefix.
	pp := *p
	pp.SubnetID = id
	pp.mask = net.CIDRMask(64, 128)
	return &pp
}

// String returns the CIDR notation string for a Prefix.
func (p *Prefix) String() string { return p.IPNet().String() }

// Parse parses a /48 or /64 Prefix from a CIDR notation string. If s is not a
// /48 or /64 IPv6 Unique Local Address prefix, it returns an error.
func Parse(s string) (*Prefix, error) {
	ip, cidr, err := net.ParseCIDR(s)
	if err != nil {
		return nil, err
	}

	// Only accept IPv6 ULA /48 or /64 prefixes.
	if ip.To16() == nil || ip.To4() != nil {
		return nil, fmt.Errorf("rfc4193: invalid IPv6 address: %s", s)
	}

	ones, _ := cidr.Mask.Size()
	if !cidr.IP.Equal(ip) || !ula.Contains(ip) || (ones != 48 && ones != 64) {
		return nil, fmt.Errorf("rfc4193: must specify a Unique Local Address /48 or /64 IPv6 prefix: %s", s)
	}

	p := Prefix{
		Local:    ip[0]&0x01 == 1,
		SubnetID: binary.BigEndian.Uint16(ip[6:8]),
		mask:     net.CIDRMask(ones, 128),
	}
	copy(p.GlobalID[:], ip[1:6])

	return &p, nil
}

// Generate produces a /48 Prefix by using mac (typically the MAC address of a
// network interface) as a seed. It uses the algorithm specified in RFC 4193,
// section 3.2.2.
//
// mac must either be nil or a 6-byte EUI-48 format MAC address. If mac is nil,
// cryptographically-secure random bytes will be used as a seed.
func Generate(mac net.HardwareAddr) (*Prefix, error) {
	// Generate a Prefix using real timestamps and crypto/rand.Reader.
	return (&generator{
		now: time.Now,
		cr:  rand.Reader,
	}).generate(mac)
}

// A generator backs the logic for Generate. Its fields can be modified to
// generate deterministic output for tests.
type generator struct {
	now func() time.Time
	cr  io.Reader
}

// generate generates a Prefix using the configured generator and seed.
func (g *generator) generate(seed net.HardwareAddr) (*Prefix, error) {
	// Store a timestamp and 8-byte value for hash input.
	in := make([]byte, 16)

	// "1) Obtain the current time of day in 64-bit NTP format [NTP]."
	binary.BigEndian.PutUint64(in[:8], uint64(g.now().UnixNano()))

	// Produce an 8-byte value:
	//
	// "2) Obtain an EUI-64 identifier from the system running this
	// algorithm.  If an EUI-64 does not exist, one can be created from
	// a 48-bit MAC address as specified in [ADDARCH].  If an EUI-64
	// cannot be obtained or created, a suitably unique identifier,
	// local to the node, should be used (e.g., system serial number)."
	//
	// And combine it with the timestamp:
	//
	// "3) Concatenate the time of day with the system-specific identifier
	// in order to create a key."
	switch {
	case len(seed) == 6:
		// EUI-48 input; produce an EUI-64 value as input.
		// Reference: https://packetlife.net/blog/2008/aug/4/eui-64-ipv6/.
		in[11] = 0xff
		in[12] = 0xfe

		copy(in[8:], seed[:3])
		copy(in[13:], seed[3:])
		in[8] ^= 0x02
	case seed == nil:
		// No seed; so we will use an io.Reader (usually crypto/rand.Reader) to
		// produce the "suitably unique identifier".
		if _, err := io.ReadFull(g.cr, in[8:]); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("rfc4193: expected an EUI-48 format MAC address or nil MAC address")
	}

	// Always produce a /48 with the local flag set, per the
	p := &Prefix{
		// Always set to true, per RFC 4193, section 3.2.2.
		Local: true,
		mask:  net.CIDRMask(48, 128),
	}

	// "4) Compute an SHA-1 digest on the key as specified in [FIPS, SHA1];
	// the resulting value is 160 bits.""
	//
	// "5) Use the least significant 40 bits as the Global ID."
	out := sha1.Sum(in)
	copy(p.GlobalID[:], out[15:])

	return p, nil
}
