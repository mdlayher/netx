package eui64

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"testing"
)

// TestParseIP verifies that ParseIP generates appropriate output IPv6 prefixes
// and MAC addresses for input IP addresses.
func TestParseIP(t *testing.T) {
	tests := []struct {
		desc   string
		ip     net.IP
		prefix net.IP
		mac    net.HardwareAddr
		err    error
	}{
		{
			desc: "nil IP address",
			err:  errInvalidIP,
		},
		{
			desc: "short IP address",
			ip:   bytes.Repeat([]byte{0}, 15),
			err:  errInvalidIP,
		},
		{
			desc: "long IP address",
			ip:   bytes.Repeat([]byte{0}, 17),
			err:  errInvalidIP,
		},
		{
			desc: "IPv4 address",
			ip:   net.IPv4(192, 168, 1, 1),
			err:  errInvalidIP,
		},
		{
			desc:   "IPv6 EUI-64 MAC",
			ip:     net.ParseIP("2001:db8::1"),
			prefix: net.ParseIP("2001:db8::"),
			mac:    net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
		},
		{
			desc:   "IPv6 EUI-48 MAC",
			ip:     net.ParseIP("fe80::212:7fff:feeb:6b40"),
			prefix: net.ParseIP("fe80::"),
			mac:    net.HardwareAddr{0x00, 0x12, 0x7f, 0xeb, 0x6b, 0x40},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			// Copy input value to ensure it is not modified later
			origIP := make(net.IP, len(tt.ip))
			copy(origIP, tt.ip)

			prefix, mac, err := ParseIP(tt.ip)
			if err != nil {
				if want, got := tt.err, err; want != got {
					t.Fatalf("unexpected error:\n- want: %v\n-  got: %v",
						want, got)
				}

				return
			}

			// Verify input value was not modified
			if want, got := origIP, tt.ip; !want.Equal(got) {
				t.Fatalf("IP was modified:\n- want: %v\n-  got: %v",
					want, got)
			}

			if want, got := tt.prefix, prefix; !want.Equal(got) {
				t.Fatalf("unexpected IPv6 prefix:\n- want: %v\n-  got: %v",
					want, got)
			}
			if want, got := tt.mac, mac; !bytes.Equal(want, got) {
				t.Fatalf("unexpected MAC address:\n- want: %v\n-  got: %v",
					want, got)
			}
		})
	}
}

// TestParseMAC verifies that ParseMAC generates appropriate output IPv6
// addresses for input IPv6 prefixes and EUI-48 or EUI-64 MAC addresses.
func TestParseMAC(t *testing.T) {
	tests := []struct {
		desc   string
		prefix net.IP
		mac    net.HardwareAddr
		ip     net.IP
		err    error
	}{
		{
			desc: "nil IPv6 prefix",
			err:  errInvalidIP,
		},
		{
			desc:   "short IPv6 prefix",
			prefix: bytes.Repeat([]byte{0}, 15),
			err:    errInvalidIP,
		},
		{
			desc:   "long IPv6 prefix",
			prefix: bytes.Repeat([]byte{0}, 17),
			err:    errInvalidIP,
		},
		{
			desc:   "IPv4 prefix",
			prefix: net.IPv4(192, 168, 1, 1),
			err:    errInvalidIP,
		},
		{
			desc:   "IPv6 /128 prefix",
			prefix: net.ParseIP("fe80::1"),
			err:    errInvalidPrefix,
		},
		{
			desc:   "nil MAC address",
			prefix: net.ParseIP("fe80::"),
			err:    errInvalidMAC,
		},
		{
			desc:   "length 5 MAC address",
			prefix: net.ParseIP("fe80::"),
			mac:    net.HardwareAddr{0xde, 0xad, 0xbe, 0xef, 0xde},
			err:    errInvalidMAC,
		},
		{
			desc:   "length 9 MAC address",
			prefix: net.ParseIP("fe80::"),
			mac:    net.HardwareAddr{0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde},
			err:    errInvalidMAC,
		},
		{
			desc:   "EUI-48 MAC address 02:00:00:00:00:01",
			prefix: net.ParseIP("2002:db8::"),
			mac:    net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
			ip:     net.ParseIP("2002:db8::ff:fe00:1"),
		},
		{
			desc:   "EUI-48 MAC address 00:12:7f:eb:6b:40",
			prefix: net.ParseIP("fe80::"),
			mac:    net.HardwareAddr{0x00, 0x12, 0x7f, 0xeb, 0x6b, 0x40},
			ip:     net.ParseIP("fe80::212:7fff:feeb:6b40"),
		},
		{
			desc:   "EUI-48 MAC address 22:ac:9e:18:be:80",
			prefix: net.ParseIP("fe80::"),
			mac:    net.HardwareAddr{0x22, 0xac, 0x9e, 0x18, 0xbe, 0x80},
			ip:     net.ParseIP("fe80::20ac:9eff:fe18:be80"),
		},
		{
			desc:   "EUI-64 MAC address 00:00:00:ff:fe:00:00:01",
			prefix: net.ParseIP("2002:db8::"),
			mac:    net.HardwareAddr{0x00, 0x00, 0x00, 0xff, 0xfe, 0x00, 0x00, 0x01},
			ip:     net.ParseIP("2002:db8::200:ff:fe00:1"),
		},
		{
			desc:   "EUI-64 MAC address 00:12:7f:ff:fe:eb:6b:40",
			prefix: net.ParseIP("fe80::"),
			mac:    net.HardwareAddr{0x00, 0x12, 0x7f, 0xff, 0xfe, 0xeb, 0x6b, 0x40},
			ip:     net.ParseIP("fe80::212:7fff:feeb:6b40"),
		},
		{
			desc:   "EUI-64 MAC address 22:ac:9e:ff:fe:18:be:80",
			prefix: net.ParseIP("fe80::"),
			mac:    net.HardwareAddr{0x22, 0xac, 0x9e, 0xff, 0xfe, 0x18, 0xbe, 0x80},
			ip:     net.ParseIP("fe80::20ac:9eff:fe18:be80"),
		},
	}

	for i, tt := range tests {
		// Copy input values to ensure they are not modified later
		origPrefix := make(net.IP, len(tt.prefix))
		copy(origPrefix, tt.prefix)
		origMAC := make(net.HardwareAddr, len(tt.mac))
		copy(origMAC, tt.mac)

		ip, err := ParseMAC(tt.prefix, tt.mac)
		if err != nil {
			if want, got := tt.err, err; want != got {
				t.Fatalf("[%02d] test %q, unexpected error:\n- want: %v\n-  got: %v",
					i, tt.desc, want, got)
			}

			continue
		}

		// Verify input values were not modified
		if want, got := origPrefix, tt.prefix; !want.Equal(got) {
			t.Fatalf("[%02d] test %q, prefix was modified:\n- want: %v\n-  got: %v",
				i, tt.desc, want, got)
		}
		if want, got := origMAC, tt.mac; !bytes.Equal(want, got) {
			t.Fatalf("[%02d] test %q, MAC was modified:\n- want: %v\n-  got: %v",
				i, tt.desc, want, got)
		}

		if want, got := tt.ip, ip; !want.Equal(got) {
			t.Fatalf("[%02d] test %q, unexpected IPv6 address:\n- want: %v\n-  got: %v",
				i, tt.desc, want, got)
		}
	}
}

// ExampleParseIP demonstrates usage of ParseIP.  This example parses an
// input IPv6 address into a IPv6 prefix and a MAC address.
func ExampleParseIP() {
	// Example data taken from:
	// http://packetlife.net/blog/2008/aug/4/eui-64-ipv6/
	ip := net.ParseIP("fe80::212:7fff:feeb:6b40")

	// Retrieve IPv6 prefix and MAC address from IPv6 address
	prefix, mac, err := ParseIP(ip)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("    ip: %s\nprefix: %s\n   mac: %s", ip, prefix, mac)

	// Output:
	//     ip: fe80::212:7fff:feeb:6b40
	// prefix: fe80::
	//    mac: 00:12:7f:eb:6b:40
}

// ExampleParseMAC demonstrates usage of ParseMAC.  This example parses an
// input IPv6 address into a IPv6 prefix and a MAC address.
func ExampleParseMAC() {
	// Example data taken from:
	// http://packetlife.net/blog/2008/aug/4/eui-64-ipv6/
	prefix := net.ParseIP("fe80::")
	mac, err := net.ParseMAC("00:12:7f:eb:6b:40")
	if err != nil {
		log.Fatal(err)
	}

	// Retrieve IPv6 address from IPv6 prefix and MAC address
	ip, err := ParseMAC(prefix, mac)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("prefix: %s\n   mac: %s\n    ip: %s", prefix, mac, ip)

	// Output:
	// prefix: fe80::
	//    mac: 00:12:7f:eb:6b:40
	//     ip: fe80::212:7fff:feeb:6b40
}
