// Command eui64 provides a simple utility to convert an IPv6 address to an
// IPv6 prefix and MAC address, or to convert an IPv6 prefix and MAC address
// to an IPv6 address.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"

	"github.com/mdlayher/netx/eui64"
)

var (
	ipFlag  = flag.String("ip", "fe80::", "IPv6 address or IPv6 prefix to parse")
	macFlag = flag.String("mac", "", "EUI-48 or EUI-64 MAC address to parse")
)

func main() {
	flag.Parse()

	// IP flag required for both operations.
	ip := net.ParseIP(*ipFlag)
	if ip == nil {
		log.Fatalf("invalid IP address: %s", *ipFlag)
	}

	// Attempt to parse prefix and MAC address from an IPv6 address.
	if *ipFlag != "" && *macFlag == "" {
		prefix, mac, err := eui64.ParseIP(ip)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("Prefix: %s\n   MAC: %s\n", prefix, mac)
		return
	}

	// Attempt to parse IPv6 address from IPv6 prefix and MAC address.

	mac, err := net.ParseMAC(*macFlag)
	if err != nil {
		log.Fatal(err)
	}

	outIP, err := eui64.ParseMAC(ip, mac)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("IP: %s\n", outIP)
}
