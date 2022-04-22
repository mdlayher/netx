package multinet_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/mdlayher/netx/multinet"
	"golang.org/x/net/nettest"
	"golang.org/x/sync/errgroup"
)

// TODO: copy over nettest.TestListener from vsock.

func TestListenerAddr(t *testing.T) {
	l := multinet.Listen(
		localListener("tcp4"),
		localListener("tcp6"),
		localListener("unix"),
	)
	defer l.Close()

	if diff := cmp.Diff("tcp,tcp,unix", l.Addr().Network()); diff != "" {
		t.Fatalf("unexpected networks (-want +got):\n%s", diff)
	}

	// Unpack each individual address from the slice to verify the fields
	// of the individual addresses.
	var (
		tcp4 *net.TCPAddr
		tcp6 *net.TCPAddr
		unix *net.UnixAddr
	)

	for i, a := range l.Addr().(multinet.Addr) {
		switch i {
		case 0:
			tcp4 = a.(*net.TCPAddr)
		case 1:
			tcp6 = a.(*net.TCPAddr)
		case 2:
			unix = a.(*net.UnixAddr)
		default:
			panic("l.Addr() returned too many addresses")
		}
	}

	// Port will be randomized, so just verify the correct localhost IP for
	// TCP addresses.
	if !tcp4.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("unexpected IPv4 address: %s", tcp4.IP)
	}

	if !tcp6.IP.Equal(net.IPv6loopback) {
		t.Fatalf("unexpected IPv6 address: %s", tcp6.IP)
	}

	// The filename is randomized, so just look for a temporary directory prefix.
	// TODO: make work on non-UNIX.
	if filepath.Dir(unix.Name) != "/tmp" {
		t.Fatalf("unexpected UNIX address: %s", unix.Name)
	}

	// Finally, verify the String output in a deterministic way.
	tcp4.Port = 80
	tcp6.Port = 80
	unix.Name = "/tmp/foo"

	got := multinet.Addr{tcp4, tcp6, unix}.String()
	if diff := cmp.Diff("127.0.0.1:80,[::1]:80,/tmp/foo", got); diff != "" {
		t.Fatalf("unexpected string output (-want +got):\n%s", diff)
	}
}

func TestListenerHTTP(t *testing.T) {
	// Open several local listeners using different socket types so that we can
	// verify each works as expected for HTTP requests.
	var (
		tcp4 = localListener("tcp4")
		tcp6 = localListener("tcp6")
		unix = localListener("unix")
	)

	l := multinet.Listen(tcp4, tcp6, unix)

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Echo the client's remote address back to them.
			_, _ = io.WriteString(w, r.RemoteAddr)
		}),
	}

	// Serve HTTP on Listener until server is closed at the end of the test.
	var eg errgroup.Group
	eg.Go(func() error {
		if err := srv.Serve(l); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("failed to serve: %v", err)
		}

		return nil
	})

	defer func() {
		if err := srv.Close(); err != nil {
			t.Fatalf("failed to close server: %v", err)
		}

		if err := eg.Wait(); err != nil {
			t.Fatalf("failed to wait for server: %v", err)
		}
	}()

	// Given a certain listener address, perform an HTTP GET to the
	// multi-listener server and verify that the server handled our request
	// using the appropriate listener family.
	tests := []struct {
		name string
		l    net.Listener
		fn   func(t *testing.T, addr string)
	}{
		{
			name: "TCPv4",
			l:    tcp4,
			fn: func(t *testing.T, addr string) {
				ip := parseAddrIP(addr)
				if ip.To16() == nil || ip.To4() == nil {
					t.Fatalf("IP %q is not an IPv4 address", ip)
				}
			},
		},
		{
			name: "TCPv6",
			l:    tcp6,
			fn: func(t *testing.T, addr string) {
				ip := parseAddrIP(addr)
				if ip.To16() == nil || ip.To4() != nil {
					t.Fatalf("IP %q is not an IPv6 address", ip)
				}
			},
		},
		{
			name: "UNIX",
			l:    unix,
			fn: func(t *testing.T, addr string) {
				// UNIX socket addresses seem to be represented this way.
				if diff := cmp.Diff("@", addr); diff != "" {
					t.Fatalf("unexpected UNIX address (-want +got):\n%s", diff)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fn(t, httpGet(t, tt.l.Addr()))
		})
	}
}

func TestListenerCloseError(t *testing.T) {
	// Verify that an error from a single listener is propagated back to the
	// caller on Close, and that further calls return no error.
	var (
		errFoo = errors.New("some error")

		// The first listener returns the expected error and the second's value
		// should be ignored. Close should be called on both.
		el1 = &errListener{err: errFoo}
		el2 = &errListener{err: errors.New("another error")}
	)

	l := multinet.Listen(
		localListener("tcp"),
		el1,
		el2,
	)

	var errs []error
	for i := 0; i < 3; i++ {
		errs = append(errs, l.Close())
	}

	if diff := cmp.Diff([]error{errFoo, nil, nil}, errs, cmp.Comparer(compareErrors)); diff != "" {
		t.Fatalf("unexpected Close errors (-want +got):\n%s", diff)
	}

	if !el1.closed {
		t.Fatal("first errListener was not closed")
	}
	if !el2.closed {
		t.Fatal("second errListener was not closed")
	}
}

func TestListenerNoSetDeadline(t *testing.T) {
	// TCP listener supports deadlines, but errListener does not.
	l := multinet.Listen(localListener("tcp"), &errListener{})
	defer l.Close()

	if err := l.SetDeadline(time.Now()); err == nil {
		t.Fatal("expected an error, but none occurred")
	}
}

func TestListenNoListeners(t *testing.T) {
	// While a Listener constructed with no net.Listeners wouldn't be useful,
	// we should verify it doesn't panic or similar.
	l := multinet.Listen()

	if diff := cmp.Diff(multinet.Addr{}, l.Addr()); diff != "" {
		t.Fatalf("unexpected Addr (-want +got):\n%s", diff)
	}

	// Close before and after accept to verify sanity.
	doClose := func() {
		for i := 0; i < 3; i++ {
			if err := l.Close(); err != nil {
				t.Fatalf("failed to close listener: %v", err)
			}
		}
	}

	doClose()

	if c, err := l.Accept(); err == nil || c != nil {
		t.Fatalf("expected nil net.Conn (got: %#v) and non-nil error", c)
	}

	doClose()
}

func compareErrors(x, y error) bool {
	switch {
	case x == nil && y == nil:
		// Both nil.
		return true
	case x == nil || y == nil:
		// One or the other nil.
		return false
	default:
		// Verify by string contents.
		return x.Error() == y.Error()
	}
}

func localListener(network string) net.Listener {
	l, err := nettest.NewLocalListener(network)
	if err != nil {
		panicf("failed to create local listener: %v", err)
	}

	return l
}

func httpGet(t *testing.T, addr net.Addr) string {
	t.Helper()

	var (
		transport = &http.Transport{}
		reqAddr   string
	)

	switch addr.(type) {
	case *net.TCPAddr:
		// Send requests to the TCP address of the server.
		reqAddr = (&url.URL{
			Scheme: "http",
			Host:   addr.String(),
		}).String()
	case *net.UnixAddr:
		// Send requests over UNIX socket instead of TCP.
		transport.DialContext = func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", addr.String())
		}

		// It seems this just has to be set to something nonsensical since we
		// are overriding Dial with a known address anyway.
		reqAddr = "http://foo"
	default:
		panicf("unhandled type: %T", addr)
	}

	c := &http.Client{
		Timeout:   1 * time.Second,
		Transport: transport,
	}

	req, err := http.NewRequest(http.MethodGet, reqAddr, nil)
	if err != nil {
		t.Fatalf("failed to create HTTP request: %v", err)
	}

	res, err := c.Do(req)
	if err != nil {
		t.Fatalf("failed to perform HTTP request: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, but got HTTP %d", res.StatusCode)
	}

	b, err := ioutil.ReadAll(io.LimitReader(res.Body, 1024))
	if err != nil {
		t.Fatalf("failed to read HTTP body: %v", err)
	}

	return string(b)
}

func parseAddrIP(s string) net.IP {
	// Assume s is a host:port string where host is always an IP.
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		panicf("failed to split host/port: %v", err)
	}

	ip := net.ParseIP(host)
	if ip == nil {
		panicf("failed to parse IP address: %v", err)
	}

	return ip
}

func panicf(format string, a ...interface{}) {
	panic(fmt.Sprintf(format, a...))
}

type errListener struct {
	err    error
	closed bool
}

var _ net.Listener = &errListener{}

func (*errListener) Addr() net.Addr            { panic("unimplemented") }
func (*errListener) Accept() (net.Conn, error) { panic("unimplemented") }
func (l *errListener) Close() error {
	l.closed = true
	return l.err
}
