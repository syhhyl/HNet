package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const (
	DefaultMixedPort      = 7890
	DefaultControllerPort = 61990
)

type PersistedState struct {
	SubscriptionURL    string    `json:"subscription_url,omitempty"`
	MihomoPath         string    `json:"mihomo_path,omitempty"`
	Secret             string    `json:"secret"`
	MixedPort          int       `json:"mixed_port"`
	ControllerPort     int       `json:"controller_port"`
	SystemProxyEnabled bool      `json:"system_proxy_enabled,omitempty"`
	LastSyncAt         time.Time `json:"last_sync_at,omitempty"`
	LastError          string    `json:"last_error,omitempty"`
}

func LoadState(path string) (PersistedState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultState()
		}
		return PersistedState{}, err
	}

	var state PersistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return PersistedState{}, err
	}
	if state.Secret == "" {
		state.Secret = randomSecret()
	}
	if state.MixedPort == 0 {
		state.MixedPort = DefaultMixedPort
	}
	if state.ControllerPort == 0 {
		state.ControllerPort = DefaultControllerPort
	}
	return state, nil
}

func SaveState(path string, state PersistedState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func defaultState() (PersistedState, error) {
	return PersistedState{
		Secret:         randomSecret(),
		MixedPort:      DefaultMixedPort,
		ControllerPort: DefaultControllerPort,
	}, nil
}

func randomSecret() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
