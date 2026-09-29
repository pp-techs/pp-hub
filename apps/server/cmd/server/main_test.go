package main

import (
	"errors"
	"net"
	"testing"
)

// freeAddr is a loopback address nothing listens on.
func freeAddr(t *testing.T) string {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()
	return addr
}

// The hub releases every room lock when it starts, which is only safe once
// this process owns the address: a bind that fails — another copy still
// serving — must not start it, a start runs with the listener already
// accepting, and a start that fails gives the address back.
func TestListenThenStartBindsBeforeStarting(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	started := false
	if _, err := listenThenStart(busy.Addr().String(), func() error { started = true; return nil }); err == nil {
		t.Fatal("bound an address already in use")
	}
	if started {
		t.Fatal("started while another process held the address")
	}

	addr := freeAddr(t)
	ln, err := listenThenStart(addr, func() error {
		started = true
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return err
		}
		return c.Close()
	})
	if err != nil {
		t.Fatalf("start with the listener bound: %v", err)
	}
	defer ln.Close()
	if !started {
		t.Fatal("start did not run")
	}

	failed := errors.New("start failed")
	addr = freeAddr(t)
	if _, err := listenThenStart(addr, func() error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("start error: %v, want %v", err, failed)
	}
	again, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listener left open after a failed start: %v", err)
	}
	_ = again.Close()
}
