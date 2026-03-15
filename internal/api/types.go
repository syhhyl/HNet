package api

import "time"

type ImportSubscriptionRequest struct {
	URL string `json:"url"`
}

type SelectProxyRequest struct {
	Name string `json:"name"`
}

type TestProxyRequest struct {
	Name string `json:"name"`
}

type SystemProxyRequest struct {
	Enabled bool `json:"enabled"`
}

type ProxyOption struct {
	Name         string `json:"name"`
	Type         string `json:"type,omitempty"`
	ProviderName string `json:"provider_name,omitempty"`
	Alive        bool   `json:"alive"`
	LatencyMS    int    `json:"latency_ms,omitempty"`
	SpeedBPS     int64  `json:"speed_bps,omitempty"`
}

type StatusResponse struct {
	DaemonVersion      string        `json:"daemon_version"`
	SocketPath         string        `json:"socket_path"`
	SubscriptionURL    string        `json:"subscription_url,omitempty"`
	ConfigPath         string        `json:"config_path"`
	LogPath            string        `json:"log_path"`
	MihomoPath         string        `json:"mihomo_path,omitempty"`
	MixedPort          int           `json:"mixed_port"`
	ControllerPort     int           `json:"controller_port"`
	CurrentProxy       string        `json:"current_proxy,omitempty"`
	AvailableProxies   []ProxyOption `json:"available_proxies,omitempty"`
	SystemProxyEnabled bool          `json:"system_proxy_enabled"`
	Running            bool          `json:"running"`
	LastSyncAt         *time.Time    `json:"last_sync_at,omitempty"`
	LastError          string        `json:"last_error,omitempty"`
	Hint               string        `json:"hint,omitempty"`
}
