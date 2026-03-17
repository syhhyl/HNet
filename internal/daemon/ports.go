package daemon

import (
	"fmt"
	"net"
	"strconv"

	"hnet/internal/config"
)

var (
	defaultMixedPort      = config.DefaultMixedPort
	defaultControllerPort = config.DefaultControllerPort
)

func ensureRuntimePorts(state config.PersistedState, keepExisting bool) (config.PersistedState, bool, error) {
	if keepExisting {
		return state, false, nil
	}

	mixedPort, err := choosePort(defaultMixedPort, state.MixedPort, nil)
	if err != nil {
		return state, false, err
	}
	controllerPort, err := choosePort(defaultControllerPort, state.ControllerPort, map[int]struct{}{mixedPort: {}})
	if err != nil {
		return state, false, err
	}

	changed := state.MixedPort != mixedPort || state.ControllerPort != controllerPort
	state.MixedPort = mixedPort
	state.ControllerPort = controllerPort
	return state, changed, nil
}

func choosePort(preferred int, fallback int, excluded map[int]struct{}) (int, error) {
	if portUsable(preferred, excluded) {
		return preferred, nil
	}
	if fallback > 0 && fallback != preferred && portUsable(fallback, excluded) {
		return fallback, nil
	}
	return randomPort(excluded)
}

func portUsable(port int, excluded map[int]struct{}) bool {
	if port <= 0 {
		return false
	}
	if _, found := excluded[port]; found {
		return false
	}
	return portAvailable(port)
}

func portAvailable(port int) bool {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func randomPort(excluded map[int]struct{}) (int, error) {
	for range 32 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		addr, ok := listener.Addr().(*net.TCPAddr)
		port := 0
		if ok {
			port = addr.Port
		}
		_ = listener.Close()
		if port == 0 {
			continue
		}
		if _, found := excluded[port]; found {
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("failed to allocate a free local port")
}
