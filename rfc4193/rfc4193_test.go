package rfc4193

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

var (
	p48 = net.CIDRMask(48, 128)
	p64 = net.CIDRMask(64, 128)
)

func TestGenerateRandom(t *testing.T) {
	tests := []struct {
		name string
		mac  net.HardwareAddr
		ok   bool
	}{
		{
			name: "bad MAC",
			mac:  net.HardwareAddr{0xff},
		},
		{
			name: "OK MAC",
			mac:  net.HardwareAddr{0xde, 0xad, 0xbe, 0xef, 0xde, 0xad},
			ok:   true,
		},
		{
			name: "nil MAC",
			ok:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Generate(tt.mac)
			if tt.ok && err != nil {
				t.Fatalf("failed to generate prefix: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected an error, but none occurred")
			}
			if err != nil {
				return
			}

			// GlobalID portion is randomized in this test, but the rest of the
			// address is deterministic and can be tested for comparison after
			// zeroing it.
			//
			// It is possible (but very unlikely) that GlobalID would be randomly
			// set to all zero, causing this check to fail.
			if p.GlobalID == [5]byte{} {
				t.Fatalf("global ID for prefix was not set: %v", p.GlobalID)
			}
			p.GlobalID = [5]byte{}

			// Generate always produces a /48 with the local flag set.
			want := &Prefix{
				Local: true,
				mask:  net.CIDRMask(48, 128),
			}

			testPrefixes(t, want, p, want.IPNet())
		})
	}
}

func TestGenerateDeterministic(t *testing.T) {
	tests := []struct {
		name string
		seed net.HardwareAddr
		ok   bool
		p    *Prefix
		ipn  *net.IPNet
	}{
		{
			name: "bad seed",
			seed: net.HardwareAddr{0xff},
		},
		{
			name: "OK seed",
			seed: net.HardwareAddr{0xde, 0xad, 0xbe, 0xef, 0xde, 0xad},
			ok:   true,
			p: &Prefix{
				Local:    true,
				GlobalID: [5]byte{0x5a, 0x5c, 0x39, 0x0f, 0xc1},
				mask:     p48,
			},
			ipn: &net.IPNet{
				IP:   net.ParseIP("fd5a:5c39:fc1::"),
				Mask: p48,
			},
		},
		{
			name: "nil seed",
			ok:   true,
			p: &Prefix{
				Local:    true,
				GlobalID: [5]byte{0xd0, 0x9c, 0x74, 0xd0, 0x17},
				mask:     p48,
			},
			ipn: &net.IPNet{
				IP:   net.ParseIP("fdd0:9c74:d017::"),
				Mask: p48,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up g for deterministic output with a fixed timestamp and
			// reader bytes.
			g := &generator{
				now: func() time.Time { return time.Unix(1, 0) },
				cr:  bytes.NewReader(make([]byte, 8)),
			}

			p, err := g.generate(tt.seed)
			if tt.ok && err != nil {
				t.Fatalf("failed to generate prefix: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected an error, but none occurred")
			}
			if err != nil {
				return
			}

			testPrefixes(t, tt.p, p, tt.ipn)
		})
	}
}

func TestPrefixManual(t *testing.T) {
	tests := []struct {
		name string
		p    *Prefix
		ipn  *net.IPNet
	}{
		{
			name: "local false /48",
			p: &Prefix{
				GlobalID: [5]byte{0: 0x01},
			},
			ipn: &net.IPNet{
				IP:   net.ParseIP("fc01::"),
				Mask: p48,
			},
		},
		{
			name: "local true /48",
			p: &Prefix{
				Local:    true,
				GlobalID: [5]byte{0: 0x02},
			},
			ipn: &net.IPNet{
				IP:   net.ParseIP("fd02::"),
				Mask: p48,
			},
		},
		{
			name: "local false /64",
			p: &Prefix{
				GlobalID: [5]byte{0: 0x03},
				SubnetID: 0x1010,
			},
			ipn: &net.IPNet{
				IP:   net.ParseIP("fc03:0:0:1010::"),
				Mask: p64,
			},
		},
		{
			name: "local true /64",
			p: &Prefix{
				Local:    true,
				GlobalID: [5]byte{0: 0x04},
				SubnetID: 0x2020,
			},
			ipn: &net.IPNet{
				IP:   net.ParseIP("fd04:0:0:2020::"),
				Mask: p64,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.ipn, tt.p.IPNet()); diff != "" {
				t.Fatalf("unexpected Prefix.IPNet (-want +got):\n%s", diff)
			}

			// Child subnet with a matching subnet ID should always reside
			// within (or be equal to for /64) their parent.
			child := tt.p.Subnet(tt.p.SubnetID).IPNet()
			if !tt.ipn.Contains(child.IP) {
				t.Fatalf("parent prefix %q does not contain child prefix %q", tt.ipn, child)
			}

			// For /64s exclusively, a different subnet ID produces a
			// non-overlapping sibling /64 prefix.
			if ones, _ := tt.ipn.Mask.Size(); ones != 48 {
				sibling := tt.p.Subnet(tt.p.SubnetID + 1).IPNet()
				if child.Contains(sibling.IP) {
					t.Fatalf("child prefix %q contains sibling prefix %q", child, sibling)
				}
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		s    string
		ok   bool
	}{
		{
			name: "bad",
			s:    "foo",
		},
		{
			name: "IPv4",
			s:    "192.0.2.0/24",
		},
		{
			name: "individual IP",
			s:    "fd00::1/64",
		},
		{
			name: "global unicast prefix",
			s:    "2001:db8::/32",
		},
		{
			name: "wrong subnet size",
			s:    "2001:db8::/56",
		},
		{
			name: "local false /48",
			s:    "fc01::/48",
			ok:   true,
		},
		{
			name: "local true /48",
			s:    "fd02::/48",
			ok:   true,
		},
		{
			name: "local false /64",
			s:    "fc03:0:0:1010::/64",
			ok:   true,
		},
		{
			name: "local true /64",
			s:    "fd04:0:0:2020::/64",
			ok:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.s)
			if tt.ok && err != nil {
				t.Fatalf("failed to parse: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected an error, but none occurred")
			}
			if err != nil {
				t.Logf("err: %v", err)
				return
			}

			if diff := cmp.Diff(tt.s, p.String()); diff != "" {
				t.Fatalf("unexpected Prefix string (-want +got):\n%s", diff)
			}
		})
	}
}

func testPrefixes(t *testing.T, want, got *Prefix, parent *net.IPNet) {
	t.Helper()

	// Expect want, got, and parent to all represent the same values in
	// different forms.
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(Prefix{})); diff != "" {
		t.Fatalf("unexpected Prefix (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(want.IPNet(), got.IPNet()); diff != "" {
		t.Fatalf("unexpected Prefix.IPNet (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(parent, got.IPNet()); diff != "" {
		t.Fatalf("unexpected parent Prefix (-want +got):\n%s", diff)
	}

	if ones, bits := parent.Mask.Size(); ones != 48 || bits != 128 {
		t.Fatalf("parent prefix must be IPv6 /48: %q", parent)
	}

	// Iterate through subnets of the Prefix and verify each is a valid /64
	// with its own subnet ID.
	for i := uint16(0); i < 257; i++ {
		sub := got.Subnet(i).IPNet()
		if !parent.Contains(sub.IP) {
			t.Fatalf("parent prefix %q does not contain child prefix %q", parent, sub)
		}

		if ones, bits := sub.Mask.Size(); ones != 64 || bits != 128 {
			t.Fatalf("child prefix must be IPv6 /64: %q", sub)
		}

		// Verify the subnet ID is incremented as appropriate for each subnet.
		id := make(net.IP, 2)
		binary.BigEndian.PutUint16(id, i)

		if diff := cmp.Diff(id, sub.IP[6:8]); diff != "" {
			t.Fatalf("unexpected child prefix subnet ID (-want +got):\n%s", diff)
		}
	}
}
