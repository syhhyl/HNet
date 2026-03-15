package daemon

import (
	"net"
	"testing"
)

func TestChoosePortPrefersPreferredPort(t *testing.T) {
	preferred := freePort(t)
	fallback := freePort(t)

	port, err := choosePort(preferred, fallback, nil)
	if err != nil {
		t.Fatalf("choosePort() error = %v", err)
	}
	if port != preferred {
		t.Fatalf("expected preferred port %d, got %d", preferred, port)
	}
}

func TestChoosePortFallsBackWhenPreferredBusy(t *testing.T) {
	listener, preferred := busyPort(t)
	defer listener.Close()

	fallback := freePort(t)
	port, err := choosePort(preferred, fallback, nil)
	if err != nil {
		t.Fatalf("choosePort() error = %v", err)
	}
	if port != fallback {
		t.Fatalf("expected fallback port %d, got %d", fallback, port)
	}
}

func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("expected TCP listener address")
	}
	return addr.Port
}

func busyPort(t *testing.T) (net.Listener, int) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		listener.Close()
		t.Fatal("expected TCP listener address")
	}
	return listener, addr.Port
}
