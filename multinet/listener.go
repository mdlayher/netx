package multinet

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// An Addr is net.Addr which stores network address information for all
// net.Listeners being used by a Listener.
type Addr []net.Addr

var _ net.Addr = Addr{}

// Network implements net.Addr, returning a comma-separated list of Network
// values for each net.Addr in a.
func (a Addr) Network() string {
	return a.join(func(addr net.Addr) string { return addr.Network() })
}

// String implements net.Addr, returning a comma-separated list of String
// values for each net.Addr in a.
func (a Addr) String() string {
	return a.join(func(addr net.Addr) string { return addr.String() })
}

// join invokes fn for each net.Addr stored in Addr and collects the results
// into a comma-separated string.
func (a Addr) join(fn func(addr net.Addr) string) string {
	ss := make([]string, 0, len(a))
	for _, addr := range a {
		ss = append(ss, fn(addr))
	}

	return strings.Join(ss, ",")
}

// A Listener is a net.Listener which aggregates multiple net.Listeners. The
// net.Listeners do not have to be of the same underlying type. Any connection
// or error from an individual net.Listener will be forwarded to the Listener.
type Listener struct {
	ls                    []net.Listener
	acceptOnce, closeOnce sync.Once
	wg                    sync.WaitGroup
	doneC                 chan struct{}
	acceptC               chan accept
}

var _ net.Listener = &Listener{}

// Listen creates a Listener which aggregates multiple net.Listeners. Although
// it is possible to construct a Listener with no net.Listeners, it will always
// return an error on Accept.
func Listen(ls ...net.Listener) *Listener {
	return &Listener{
		ls:      ls,
		doneC:   make(chan struct{}),
		acceptC: make(chan accept, len(ls)),
	}
}

// Accept accepts a net.Conn from one of the owned net.Listeners.
func (l *Listener) Accept() (net.Conn, error) {
	if len(l.ls) == 0 {
		// No listeners, nothing to do.
		return nil, errors.New("multinet: no net.Listeners added to Listener")
	}

	l.acceptOnce.Do(func() {
		// On first Accept, create accept multiplexing goroutines which will
		// feed accepted connections and errors over l.acceptC.
		l.wg.Add(len(l.ls))

		for _, ln := range l.ls {
			go func(ln net.Listener) {
				defer l.wg.Done()
				l.accept(ln)
			}(ln)
		}
	})

	select {
	case a := <-l.acceptC:
		return a.c, a.err
	case <-l.doneC:
		// TODO: good enough?
		return nil, errors.New("multinet: use of closed network connection")
	}
}

// Addr creates a net.Addr of type Addr with all the aggregated addresses of
// the owned net.Listeners.
func (l *Listener) Addr() net.Addr {
	addrs := make(Addr, 0, len(l.ls))
	for _, ln := range l.ls {
		addrs = append(addrs, ln.Addr())
	}

	return addrs
}

// A deadlineListener is a net.Listener with deadline support.
type deadlineListener interface {
	net.Listener
	SetDeadline(t time.Time) error
}

// SetDeadline sets a deadline t on all net.Listeners owned by this Listener.
// All net.Listeners must support the method "SetDeadline(t time.Time) error"
// or an error will be returned. If more than one net.Listener returns an error,
// only the first error is returned.
func (l *Listener) SetDeadline(t time.Time) error {
	dls := make([]deadlineListener, 0, len(l.ls))
	for _, ln := range l.ls {
		dl, ok := ln.(deadlineListener)
		if !ok {
			return fmt.Errorf("multinet: net.Listener %T does not have a SetDeadline method", ln)
		}

		dls = append(dls, dl)
	}

	var err error
	for _, dl := range dls {
		// Only propagate the first returned error to the caller.
		if lerr := dl.SetDeadline(t); lerr != nil && err == nil {
			err = lerr
		}
	}

	return err
}

// Close closes all net.Listeners owned by this Listener. If more than one
// net.Listener returns an error, only the first error is returned.
func (l *Listener) Close() error {
	var err error

	l.closeOnce.Do(func() {
		// On first invocation of Close, halt all accept multiplexing
		// goroutines and Close the individual listeners.
		defer l.wg.Wait()
		close(l.doneC)

		for _, ln := range l.ls {
			// Close all listeners to avoid any file descriptor leaks, but only
			// propagate the first returned error to the caller.
			if lerr := ln.Close(); lerr != nil && err == nil {
				err = lerr
			}
		}
	})

	return err
}

// An accept is the result of the Accept method.
type accept struct {
	c   net.Conn
	err error
}

// accept begins accepting connections on ln, sending the results to l.acceptC.
func (l *Listener) accept(ln net.Listener) {
	for {
		c, err := ln.Accept()

		// Prioritize the done signal over accepting a connection, but allow
		// either to occur later to satisfy nettest.
		select {
		case <-l.doneC:
			return
		default:
		}

		select {
		case <-l.doneC:
			return
		case l.acceptC <- accept{c: c, err: err}:
		}
	}
}
