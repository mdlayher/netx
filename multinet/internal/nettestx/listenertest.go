// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This code is copied and slightly modified from an open CL:
// https://go-review.googlesource.com/c/net/+/123056.

package nettestx

import (
	"net"
	"sync"
	"testing"
	"time"
)

var (
	aLongTimeAgo = time.Unix(233431200, 0)
	neverTimeout = time.Time{}
)

// MakeOpenerSet creates and returns a set of connection openers for
// both passive- and active-open sides.
// The stop function closes all resources including ln, cancels the
// dial operation, and should not be nil.
type MakeOpenerSet func() (ln net.Listener, dial func(addr net.Addr) (net.Conn, error), stop func(), err error)

// TestListener tests that a net.Listener implementation properly
// satisfies the interface.
// The tests should not produce any false positives, but may
// experience false negatives.
// Thus, some issues may only be detected when the test is run
// multiple times.
// For maximal effectiveness, run the tests under the race detector.
func TestListener(t *testing.T, mos MakeOpenerSet) {
	t.Run("Accept", func(t *testing.T) { openerSetTimeoutWrapper(t, mos, testOpenerSetAccept) })
	t.Run("RacyAccept", func(t *testing.T) { openerSetTimeoutWrapper(t, mos, testOpenerSetRacyAccept) })
	t.Run("PastTimeout", func(t *testing.T) { openerSetTimeoutWrapper(t, mos, testOpenerSetPastTimeout) })
	t.Run("PresentTimeout", func(t *testing.T) { openerSetTimeoutWrapper(t, mos, testOpenerSetPresentTimeout) })
	t.Run("FutureTimeout", func(t *testing.T) { openerSetTimeoutWrapper(t, mos, testOpenerSetFutureTimeout) })
	t.Run("CloseTimeout", func(t *testing.T) { openerSetTimeoutWrapper(t, mos, testOpenerSetCloseTimeout) })
	t.Run("ConcurrentMethods", func(t *testing.T) { openerSetTimeoutWrapper(t, mos, testOpenerSetConcurrentMethods) })
}

type listenerTester func(t *testing.T, ln net.Listener, dial func(addr net.Addr) (net.Conn, error))

func openerSetTimeoutWrapper(t *testing.T, mos MakeOpenerSet, f listenerTester) {
	t.Parallel()
	ln, dial, stop, err := mos()
	if err != nil {
		t.Fatalf("unable to make opener set: %v", err)
	}
	var once sync.Once
	defer once.Do(func() { stop() })
	timer := time.AfterFunc(time.Minute, func() {
		once.Do(func() {
			t.Error("test timed out; terminating opener set")
			stop()
		})
	})
	defer timer.Stop()
	f(t, ln, dial)
}

type deadlineListener interface {
	net.Listener

	SetDeadline(time.Time) error
}

// testOpenerSetAccept tests that the connection setup request invoked
// by dial is properly accepted on ln.
func testOpenerSetAccept(t *testing.T, ln net.Listener, dial func(net.Addr) (net.Conn, error)) {
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, err := dial(ln.Addr())
		if err != nil {
			t.Errorf("unexpected Dial error: %v", err)
			return
		}
		if err := c.Close(); err != nil {
			t.Errorf("unexpected Close error: %v", err)
			return
		}
	}()
	c, err := ln.Accept()
	if err != nil {
		t.Errorf("unexpected Accept error: %v", err)
		return
	}
	c.Close()
}

// testOpenerSetRacyAccept tests that it is safe to call Accept
// concurrently.
func testOpenerSetRacyAccept(t *testing.T, ln net.Listener, dial func(net.Addr) (net.Conn, error)) {
	dl, ok := ln.(deadlineListener)
	if !ok {
		t.Skip("deadline not implemented")
	}
	var wg sync.WaitGroup
	dl.SetDeadline(time.Now().Add(time.Millisecond))
	for i := 0; i < 5; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				c, err := dl.Accept()
				if err != nil {
					checkForTimeoutError(t, err)
					dl.SetDeadline(time.Now().Add(time.Millisecond))
					continue
				}
				c.Close()
			}
		}()
		go func() {
			defer wg.Done()
			c, err := dial(dl.Addr())
			if err != nil {
				return
			}
			c.Close()
		}()
	}
	wg.Wait()
}

// testOpenerSetPastTimeout tests that a deadline set in the past
// immediately times out Accept operations.
func testOpenerSetPastTimeout(t *testing.T, ln net.Listener, dial func(net.Addr) (net.Conn, error)) {
	dl, ok := ln.(deadlineListener)
	if !ok {
		t.Skip("deadline not implemented")
	}
	dl.SetDeadline(aLongTimeAgo)
	_, err := dl.Accept()
	checkForTimeoutError(t, err)
}

// testOpenerSetPresentTimeout tests that a deadline set while there
// are pending Accept operations immediately times out those
// operations.
func testOpenerSetPresentTimeout(t *testing.T, ln net.Listener, dial func(net.Addr) (net.Conn, error)) {
	dl, ok := ln.(deadlineListener)
	if !ok {
		t.Skip("deadline not implemented")
	}
	var wg sync.WaitGroup
	wg.Add(2)
	deadlineSet := make(chan bool, 1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		deadlineSet <- true
		dl.SetDeadline(aLongTimeAgo)
	}()
	go func() {
		defer wg.Done()
		_, err := dl.Accept()
		checkForTimeoutError(t, err)
		if len(deadlineSet) == 0 {
			t.Error("Accept timed out before deadline is set")
		}
	}()
	wg.Wait()
}

// testOpenerSetFutureTimeout tests that a future deadline will
// eventually time out Accept operations.
func testOpenerSetFutureTimeout(t *testing.T, ln net.Listener, dial func(net.Addr) (net.Conn, error)) {
	dl, ok := ln.(deadlineListener)
	if !ok {
		t.Skip("deadline not implemented")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	dl.SetDeadline(time.Now().Add(100 * time.Millisecond))
	go func() {
		defer wg.Done()
		_, err := dl.Accept()
		checkForTimeoutError(t, err)
	}()
	wg.Wait()
}

// testOpenerSetCloseTimeout tests that calling Close immediately
// times out pending Accept operations.
func testOpenerSetCloseTimeout(t *testing.T, ln net.Listener, dial func(net.Addr) (net.Conn, error)) {
	dl, ok := ln.(deadlineListener)
	if !ok {
		t.Skip("deadline not implemented")
	}
	var wg sync.WaitGroup
	wg.Add(2)
	// Test for cancelation upon connection closure.
	dl.SetDeadline(neverTimeout)
	go func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		dl.Close()
	}()
	go func() {
		defer wg.Done()
		var err error
		for err == nil {
			_, err = dl.Accept()
		}
	}()
	wg.Wait()
}

// testOpennerSetConcurrentMethods tests that the methods of
// net.Listener can safely be called concurrently.
func testOpenerSetConcurrentMethods(t *testing.T, ln net.Listener, dial func(net.Addr) (net.Conn, error)) {
	dl, ok := ln.(deadlineListener)
	if !ok {
		t.Skip("deadline not implemented")
	}
	// The results of the calls may be nonsensical, but this
	// should not trigger a race detector warning.
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			dl.Accept()
		}()
		go func() {
			defer wg.Done()
			dl.SetDeadline(time.Now().Add(10 * time.Millisecond))
		}()
		go func() {
			defer wg.Done()
			dl.Addr()
		}()
	}
	wg.Wait() // At worst, the deadline is set 10ms into the future
}

// checkForTimeoutError checks that the error satisfies the Error interface
// and that Timeout returns true.
func checkForTimeoutError(t *testing.T, err error) {
	t.Helper()
	if nerr, ok := err.(net.Error); ok {
		if !nerr.Timeout() {
			t.Errorf("err.Timeout() = false, want true")
		}
	} else {
		t.Errorf("got %T, want net.Error", err)
	}
}
