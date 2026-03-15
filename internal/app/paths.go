package app

import (
	"os"
	"path/filepath"
)

type Paths struct {
	BaseDir          string
	RuntimeDir       string
	ProviderDir      string
	SocketPath       string
	PIDFile          string
	StatePath        string
	DaemonLogPath    string
	MihomoConfigPath string
	MihomoLogPath    string
	SubscriptionPath string
	ProviderPath     string
}

func ResolvePaths() (Paths, error) {
	baseDir := os.Getenv("HNET_HOME")
	if baseDir == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, err
		}
		baseDir = filepath.Join(configDir, "hnet")
	}

	runtimeDir := filepath.Join(baseDir, "runtime")
	providerDir := filepath.Join(runtimeDir, "providers")
	return Paths{
		BaseDir:          baseDir,
		RuntimeDir:       runtimeDir,
		ProviderDir:      providerDir,
		SocketPath:       filepath.Join(baseDir, "hnetd.sock"),
		PIDFile:          filepath.Join(baseDir, "hnetd.pid"),
		StatePath:        filepath.Join(baseDir, "state.json"),
		DaemonLogPath:    filepath.Join(baseDir, "hnetd.log"),
		MihomoConfigPath: filepath.Join(runtimeDir, "config.yaml"),
		MihomoLogPath:    filepath.Join(runtimeDir, "mihomo.log"),
		SubscriptionPath: filepath.Join(runtimeDir, "subscription.url"),
		ProviderPath:     filepath.Join(providerDir, "imported.yaml"),
	}, nil
}

func (p Paths) Ensure() error {
	if err := os.MkdirAll(p.BaseDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(p.RuntimeDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(p.ProviderDir, 0o755); err != nil {
		return err
	}
	return nil
}
