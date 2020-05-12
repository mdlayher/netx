package multinet_test

import (
	"net"
	"testing"

	"github.com/mdlayher/netx/multinet"
	"github.com/mdlayher/netx/multinet/internal/nettestx"
)

func TestIntegrationNettestTestListener(t *testing.T) {
	mos := func() (ln net.Listener, dial func(net.Addr) (net.Conn, error), stop func(), err error) {
		l4, err := net.Listen("tcp", ":0")
		if err != nil {
			return nil, nil, nil, err
		}

		l := multinet.Listen(l4)

		stop = func() {
			_ = l.Close()
		}

		dial = func(addr net.Addr) (net.Conn, error) {
			return net.Dial(addr.Network(), addr.String())
		}

		return l, dial, stop, nil
	}

	nettestx.TestListener(t, mos)
}
