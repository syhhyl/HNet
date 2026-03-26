package config

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"hnet/internal/fileutil"
)

const (
	DefaultMixedPort      = 7890
	DefaultControllerPort = 61990
)

type PersistedState struct {
	SubscriptionURL     string               `json:"subscription_url,omitempty"`
	Subscriptions       []SubscriptionEntry  `json:"subscriptions,omitempty"`
	MihomoPath          string               `json:"mihomo_path,omitempty"`
	Secret              string               `json:"secret"`
	MixedPort           int                  `json:"mixed_port"`
	ControllerPort      int                  `json:"controller_port"`
	SystemProxyEnabled  bool                 `json:"system_proxy_enabled,omitempty"`
	SystemProxySnapshot *SystemProxySnapshot `json:"system_proxy_snapshot,omitempty"`
	LastSyncAt          time.Time            `json:"last_sync_at,omitempty"`
	LastError           string               `json:"last_error,omitempty"`
}

type SubscriptionEntry struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url"`
}

type SystemProxySnapshot struct {
	Services map[string]SystemNetworkServiceProxy `json:"services,omitempty"`
}

type SystemNetworkServiceProxy struct {
	Web                SystemManualProxy `json:"web,omitempty"`
	SecureWeb          SystemManualProxy `json:"secure_web,omitempty"`
	Socks              SystemManualProxy `json:"socks,omitempty"`
	AutoProxyURL       SystemAutoProxy   `json:"auto_proxy_url,omitempty"`
	ProxyAutoDiscovery bool              `json:"proxy_auto_discovery,omitempty"`
}

type SystemManualProxy struct {
	Enabled       bool   `json:"enabled,omitempty"`
	Server        string `json:"server,omitempty"`
	Port          int    `json:"port,omitempty"`
	Authenticated bool   `json:"authenticated,omitempty"`
}

type SystemAutoProxy struct {
	Enabled bool   `json:"enabled,omitempty"`
	URL     string `json:"url,omitempty"`
}

func (s *SystemProxySnapshot) Empty() bool {
	return s == nil || len(s.Services) == 0
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
		return PersistedState{}, fmt.Errorf("decode state file %q: %w", path, err)
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
	state.normalizeSubscriptions()
	return state, nil
}

func SaveState(path string, state PersistedState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(path, data, 0o600)
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

func (s *PersistedState) normalizeSubscriptions() {
	if s == nil {
		return
	}

	if s.SubscriptionURL != "" && !containsSubscription(s.Subscriptions, s.SubscriptionURL) {
		s.Subscriptions = append([]SubscriptionEntry{{ID: SubscriptionIDForURL(s.SubscriptionURL), URL: s.SubscriptionURL}}, s.Subscriptions...)
	}

	if s.SubscriptionURL == "" && len(s.Subscriptions) > 0 {
		s.SubscriptionURL = s.Subscriptions[0].URL
	}

	seen := make(map[string]struct{}, len(s.Subscriptions))
	filtered := make([]SubscriptionEntry, 0, len(s.Subscriptions))
	for _, subscription := range s.Subscriptions {
		if subscription.URL == "" {
			continue
		}
		if subscription.ID == "" {
			subscription.ID = SubscriptionIDForURL(subscription.URL)
		}
		if _, ok := seen[subscription.URL]; ok {
			continue
		}
		seen[subscription.URL] = struct{}{}
		filtered = append(filtered, subscription)
	}
	assignSubscriptionNames(filtered)
	s.Subscriptions = filtered

	if s.SubscriptionURL != "" && !containsSubscription(s.Subscriptions, s.SubscriptionURL) {
		s.SubscriptionURL = ""
	}
	if s.SubscriptionURL == "" && len(s.Subscriptions) > 0 {
		s.SubscriptionURL = s.Subscriptions[0].URL
	}
}

func (s *PersistedState) UpsertSubscription(url string) {
	if s == nil || url == "" {
		return
	}
	if !containsSubscription(s.Subscriptions, url) {
		s.Subscriptions = append(s.Subscriptions, SubscriptionEntry{ID: SubscriptionIDForURL(url), URL: url})
	}
	s.SubscriptionURL = url
	s.normalizeSubscriptions()
}

func (s *PersistedState) SelectSubscription(url string) bool {
	if s == nil || url == "" || !containsSubscription(s.Subscriptions, url) {
		return false
	}
	s.SubscriptionURL = url
	return true
}

func (s *PersistedState) DeleteSubscription(url string) bool {
	if s == nil || url == "" {
		return false
	}

	index := -1
	filtered := make([]SubscriptionEntry, 0, len(s.Subscriptions))
	for i, subscription := range s.Subscriptions {
		if subscription.URL == url {
			if index == -1 {
				index = i
			}
			continue
		}
		filtered = append(filtered, subscription)
	}
	if index == -1 {
		return false
	}

	deletedActive := s.SubscriptionURL == url
	s.Subscriptions = filtered
	if len(filtered) == 0 {
		s.SubscriptionURL = ""
		s.normalizeSubscriptions()
		return true
	}

	if deletedActive {
		nextIndex := index
		if nextIndex >= len(filtered) {
			nextIndex = len(filtered) - 1
		}
		s.SubscriptionURL = filtered[nextIndex].URL
	}
	s.normalizeSubscriptions()
	return true
}

func containsSubscription(subscriptions []SubscriptionEntry, url string) bool {
	for _, subscription := range subscriptions {
		if subscription.URL == url {
			return true
		}
	}
	return false
}

func SubscriptionIDForURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	sum := sha1.Sum([]byte(trimmed))
	return "sub_" + hex.EncodeToString(sum[:8])
}

func assignSubscriptionNames(subscriptions []SubscriptionEntry) {
	if len(subscriptions) == 0 {
		return
	}
	used := make(map[string]int, len(subscriptions))
	for i := range subscriptions {
		base := strings.TrimSpace(subscriptions[i].Name)
		if base == "" {
			base = defaultSubscriptionName(subscriptions[i].URL)
		}
		count := used[base]
		used[base] = count + 1
		if count == 0 {
			subscriptions[i].Name = base
			continue
		}
		subscriptions[i].Name = base + " " + strconv.Itoa(count+1)
	}
}

func defaultSubscriptionName(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "subscription"
	}
	parsed, err := url.Parse(trimmed)
	if err == nil {
		if host := strings.TrimSpace(parsed.Host); host != "" {
			return host
		}
	}
	return "subscription"
}
