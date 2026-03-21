package api

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"time"
)

type ImportSubscriptionRequest struct {
	URL string `json:"url"`
}

type SelectSubscriptionRequest struct {
	URL string `json:"url"`
}

type DeleteSubscriptionRequest struct {
	URL string `json:"url"`
}

type RefreshSubscriptionRequest struct {
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
	ID           string `json:"id,omitempty"`
	Fingerprint  string `json:"fingerprint,omitempty"`
	Name         string `json:"name"`
	Type         string `json:"type,omitempty"`
	ProviderName string `json:"provider_name,omitempty"`
	Alive        bool   `json:"alive"`
	LatencyMS    int    `json:"latency_ms,omitempty"`
	SpeedBPS     int64  `json:"speed_bps,omitempty"`
}

type SubscriptionOption struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url"`
}

type SubscriptionOperation struct {
	Kind       string     `json:"kind"`
	State      string     `json:"state"`
	TargetURL  string     `json:"target_url,omitempty"`
	Message    string     `json:"message,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type StatusResponse struct {
	DaemonVersion      string                 `json:"daemon_version"`
	SocketPath         string                 `json:"socket_path"`
	SubscriptionURL    string                 `json:"subscription_url,omitempty"`
	Subscriptions      []SubscriptionOption   `json:"subscriptions,omitempty"`
	ConfigPath         string                 `json:"config_path"`
	LogPath            string                 `json:"log_path"`
	MihomoPath         string                 `json:"mihomo_path,omitempty"`
	MixedPort          int                    `json:"mixed_port"`
	ControllerPort     int                    `json:"controller_port"`
	CurrentProxy       string                 `json:"current_proxy,omitempty"`
	AvailableProxies   []ProxyOption          `json:"available_proxies,omitempty"`
	SubscriptionOp     *SubscriptionOperation `json:"subscription_op,omitempty"`
	SystemProxyEnabled bool                   `json:"system_proxy_enabled"`
	Running            bool                   `json:"running"`
	LastSyncAt         *time.Time             `json:"last_sync_at,omitempty"`
	LastError          string                 `json:"last_error,omitempty"`
	Hint               string                 `json:"hint,omitempty"`
}

func ProxyFingerprint(name string, proxyType string, providerName string) string {
	parts := []string{
		strings.TrimSpace(providerName),
		strings.TrimSpace(proxyType),
		strings.TrimSpace(name),
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return "proxy_" + hex.EncodeToString(sum[:8])
}
