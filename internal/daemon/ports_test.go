package daemon

import (
	"net"
	"testing"

	"hnet/internal/config"
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

func TestEnsureRuntimePortsRecoversDefaultPortsOnColdStart(t *testing.T) {
	ports := freePorts(t, 4)
	defaultMixed := ports[0]
	defaultController := ports[1]
	restoreDefaults := swapRuntimePortDefaults(defaultMixed, defaultController)
	defer restoreDefaults()

	state := config.PersistedState{
		MixedPort:      ports[2],
		ControllerPort: ports[3],
	}

	updated, changed, err := ensureRuntimePorts(state, false)
	if err != nil {
		t.Fatalf("ensureRuntimePorts() error = %v", err)
	}
	if !changed {
		t.Fatal("expected startup probing to detect a port change")
	}
	if updated.MixedPort != defaultMixed {
		t.Fatalf("expected mixed port to recover to default %d, got %d", defaultMixed, updated.MixedPort)
	}
	if updated.ControllerPort != defaultController {
		t.Fatalf("expected controller port to recover to default %d, got %d", defaultController, updated.ControllerPort)
	}
}

func TestEnsureRuntimePortsFallsBackWhenDefaultMixedPortBusy(t *testing.T) {
	defaultMixedListener, defaultMixed := busyPort(t)
	defer defaultMixedListener.Close()

	ports := freePorts(t, 3)
	defaultController := ports[0]
	restoreDefaults := swapRuntimePortDefaults(defaultMixed, defaultController)
	defer restoreDefaults()

	fallbackMixed := ports[1]
	fallbackController := ports[2]
	state := config.PersistedState{
		MixedPort:      fallbackMixed,
		ControllerPort: fallbackController,
	}

	updated, changed, err := ensureRuntimePorts(state, false)
	if err != nil {
		t.Fatalf("ensureRuntimePorts() error = %v", err)
	}
	if !changed {
		t.Fatal("expected startup probing to keep a fallback port when the default is busy")
	}
	if updated.MixedPort != fallbackMixed {
		t.Fatalf("expected mixed port fallback %d, got %d", fallbackMixed, updated.MixedPort)
	}
	if updated.ControllerPort != defaultController {
		t.Fatalf("expected controller port default %d, got %d", defaultController, updated.ControllerPort)
	}

	defaultMixedListener.Close()

	recovered, changed, err := ensureRuntimePorts(updated, false)
	if err != nil {
		t.Fatalf("ensureRuntimePorts() recovery error = %v", err)
	}
	if !changed {
		t.Fatal("expected startup probing to recover the default mixed port once it is free")
	}
	if recovered.MixedPort != defaultMixed {
		t.Fatalf("expected recovered mixed port %d, got %d", defaultMixed, recovered.MixedPort)
	}
}

func TestEnsureRuntimePortsKeepsExistingPortsWhileRunning(t *testing.T) {
	ports := freePorts(t, 4)
	defaultMixed := ports[0]
	defaultController := ports[1]
	restoreDefaults := swapRuntimePortDefaults(defaultMixed, defaultController)
	defer restoreDefaults()

	state := config.PersistedState{
		MixedPort:      ports[2],
		ControllerPort: ports[3],
	}

	updated, changed, err := ensureRuntimePorts(state, true)
	if err != nil {
		t.Fatalf("ensureRuntimePorts() error = %v", err)
	}
	if changed {
		t.Fatal("expected running runtime ports to stay unchanged")
	}
	if updated.MixedPort != state.MixedPort || updated.ControllerPort != state.ControllerPort {
		t.Fatalf("expected runtime ports to stay at %d/%d, got %d/%d", state.MixedPort, state.ControllerPort, updated.MixedPort, updated.ControllerPort)
	}
}

func swapRuntimePortDefaults(mixedPort int, controllerPort int) func() {
	previousMixed := defaultMixedPort
	previousController := defaultControllerPort
	defaultMixedPort = mixedPort
	defaultControllerPort = controllerPort
	return func() {
		defaultMixedPort = previousMixed
		defaultControllerPort = previousController
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

func freePorts(t *testing.T, count int) []int {
	t.Helper()

	listeners := make([]net.Listener, 0, count)
	ports := make([]int, 0, count)
	for range count {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			for _, held := range listeners {
				_ = held.Close()
			}
			t.Fatalf("net.Listen() error = %v", err)
		}
		listeners = append(listeners, listener)

		addr, ok := listener.Addr().(*net.TCPAddr)
		if !ok {
			for _, held := range listeners {
				_ = held.Close()
			}
			t.Fatal("expected TCP listener address")
		}
		ports = append(ports, addr.Port)
	}

	for _, listener := range listeners {
		_ = listener.Close()
	}
	return ports
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
