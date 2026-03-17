package daemon

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hnet/internal/api"
	"hnet/internal/app"
	"hnet/internal/config"
	"hnet/internal/fileutil"
	"hnet/internal/mihomo"
	"hnet/internal/platform/macos"
	"hnet/internal/subscription"
)

const (
	version          = "v0.0.1"
	proxyGroupName   = "PROXY"
	proxyLoadTimeout = 15 * time.Second
)

type Service struct {
	paths        app.Paths
	state        config.PersistedState
	supervisor   *mihomo.Supervisor
	mu           sync.Mutex
	controllerMu sync.Mutex
	proxyMetrics map[string]proxyMetric
}

type serviceSnapshot struct {
	state      config.PersistedState
	running    bool
	binaryPath string
}

type proxyMetric struct {
	latencyMS int
	speedBPS  int64
}

func NewService(paths app.Paths) (*Service, error) {
	if err := paths.Ensure(); err != nil {
		return nil, err
	}

	state, err := config.LoadState(paths.StatePath)
	if err != nil {
		return nil, err
	}

	return &Service{
		paths:        paths,
		state:        state,
		supervisor:   mihomo.NewSupervisor(paths),
		proxyMetrics: make(map[string]proxyMetric),
	}, nil
}

func (s *Service) Serve(ctx context.Context) error {
	if err := s.writePID(); err != nil {
		return err
	}
	defer os.Remove(s.paths.PIDFile)

	if err := os.Remove(s.paths.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	listener, err := net.Listen("unix", s.paths.SocketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(s.paths.SocketPath, 0o600); err != nil {
		return err
	}

	server := &http.Server{Handler: s.routes()}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	s.restoreRuntime()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		stopErr := s.supervisor.Stop()
		proxyErr := s.shutdownSystemProxy()
		return errors.Join(stopErr, proxyErr)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Service) restoreRuntime() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(s.state.SubscriptionURL) != "" {
		normalizedURL, err := subscription.NormalizeURL(s.state.SubscriptionURL)
		if err != nil {
			s.recordErrorLocked(err.Error())
			return
		}
		s.state.SubscriptionURL = normalizedURL

		updatedState, _, err := ensureRuntimePorts(s.state, false)
		if err != nil {
			s.recordErrorLocked(err.Error())
			return
		}
		s.state = updatedState

		if err := s.writeManagedConfigLocked(); err != nil {
			s.recordErrorLocked(err.Error())
			return
		}
		if err := config.SaveState(s.paths.StatePath, s.state); err != nil {
			s.recordErrorLocked(fmt.Sprintf("save state: %v", err))
			return
		}
	} else if _, err := os.Stat(s.paths.MihomoConfigPath); err == nil {
		if _, err := s.sanitizeExistingConfig(); err != nil {
			s.recordErrorLocked(fmt.Sprintf("sanitize config: %v", err))
			return
		}
	} else {
		if s.state.SystemProxyEnabled {
			if err := s.disableSystemProxyLocked(); err != nil {
				s.recordErrorLocked(fmt.Sprintf("restore system proxy: %v", err))
				return
			}
			_ = config.SaveState(s.paths.StatePath, s.state)
		}
		return
	}

	binaryPath, err := s.supervisor.Apply(s.state.ControllerPort, s.state.Secret)
	if err != nil {
		s.recordErrorLocked(s.recoverRuntimeFailureLocked(err).Error())
		return
	}

	s.state.MihomoPath = binaryPath
	s.state.LastError = ""
	if s.state.SystemProxyEnabled {
		if err := macos.SetMixedProxy(s.state.MixedPort); err != nil {
			s.recordErrorLocked(s.recoverRuntimeFailureLocked(err).Error())
			return
		}
	}
	_ = config.SaveState(s.paths.StatePath, s.state)
}

func (s *Service) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", s.handleStatus)
	mux.HandleFunc("/v1/subscription", s.handleSubscription)
	mux.HandleFunc("/v1/subscription/delete", s.handleSubscriptionDelete)
	mux.HandleFunc("/v1/subscription/select", s.handleSubscriptionSelect)
	mux.HandleFunc("/v1/proxy/test", s.handleProxyTest)
	mux.HandleFunc("/v1/proxy", s.handleProxy)
	mux.HandleFunc("/v1/system-proxy", s.handleSystemProxy)
	return mux
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.writeJSON(w, http.StatusOK, s.status())
}

func (s *Service) handleSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request api.ImportSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	status, err := s.importSubscription(request.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, status)
}

func (s *Service) handleSubscriptionSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request api.SelectSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	status, err := s.selectSubscription(request.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, status)
}

func (s *Service) handleSubscriptionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request api.DeleteSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	status, err := s.deleteSubscription(request.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, status)
}

func (s *Service) handleProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request api.SelectProxyRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	status, err := s.selectProxy(request.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, status)
}

func (s *Service) handleProxyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request api.TestProxyRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	status, err := s.testProxy(request.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, status)
}

func (s *Service) handleSystemProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request api.SystemProxyRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	status, err := s.setSystemProxy(request.Enabled)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, status)
}

func (s *Service) importSubscription(rawURL string) (api.StatusResponse, error) {
	normalizedURL, err := subscription.NormalizeURL(rawURL)
	if err != nil {
		return api.StatusResponse{}, err
	}

	return s.applySubscription(normalizedURL, true)
}

func (s *Service) selectSubscription(rawURL string) (api.StatusResponse, error) {
	normalizedURL, err := subscription.NormalizeURL(rawURL)
	if err != nil {
		return api.StatusResponse{}, err
	}

	s.mu.Lock()
	if !containsSelectedSubscription(s.state.Subscriptions, normalizedURL) {
		s.mu.Unlock()
		return api.StatusResponse{}, errors.New("subscription not found")
	}
	s.mu.Unlock()

	return s.applySubscription(normalizedURL, false)
}

func (s *Service) deleteSubscription(rawURL string) (api.StatusResponse, error) {
	normalizedURL, err := subscription.NormalizeURL(rawURL)
	if err != nil {
		return api.StatusResponse{}, err
	}

	s.mu.Lock()
	removed, deletedActive, nextURL := s.state.DeleteSubscription(normalizedURL)
	if !removed {
		s.mu.Unlock()
		return api.StatusResponse{}, errors.New("subscription not found")
	}
	if err := s.clearProviderCacheLocked(normalizedURL); err != nil {
		s.mu.Unlock()
		return api.StatusResponse{}, err
	}
	s.proxyMetrics = make(map[string]proxyMetric)

	if !deletedActive {
		if err := config.SaveState(s.paths.StatePath, s.state); err != nil {
			s.mu.Unlock()
			return api.StatusResponse{}, fmt.Errorf("save state: %w", err)
		}
		s.mu.Unlock()
		return s.status(), nil
	}

	if nextURL == "" {
		if err := s.removeManagedRuntimeLocked(); err != nil {
			s.mu.Unlock()
			return api.StatusResponse{}, err
		}
		if err := config.SaveState(s.paths.StatePath, s.state); err != nil {
			s.mu.Unlock()
			return api.StatusResponse{}, fmt.Errorf("save state: %w", err)
		}
		s.mu.Unlock()
		return s.status(), nil
	}

	if err := s.clearProviderCacheLocked(nextURL); err != nil {
		s.mu.Unlock()
		return api.StatusResponse{}, err
	}

	if err := s.applyCurrentSubscriptionLocked(); err != nil {
		s.mu.Unlock()
		return api.StatusResponse{}, err
	}
	s.mu.Unlock()

	if err := s.waitForManagedProxies(); err != nil {
		return api.StatusResponse{}, err
	}
	return s.status(), nil
}

func (s *Service) applySubscription(normalizedURL string, addToList bool) (api.StatusResponse, error) {
	s.mu.Lock()
	if addToList {
		s.state.UpsertSubscription(normalizedURL)
	} else if !s.state.SelectSubscription(normalizedURL) {
		s.mu.Unlock()
		return api.StatusResponse{}, errors.New("subscription not found")
	}
	if err := s.clearProviderCacheLocked(normalizedURL); err != nil {
		s.mu.Unlock()
		return api.StatusResponse{}, err
	}
	s.proxyMetrics = make(map[string]proxyMetric)
	err := s.applyCurrentSubscriptionLocked()
	s.mu.Unlock()
	if err != nil {
		return api.StatusResponse{}, err
	}

	if err := s.waitForManagedProxies(); err != nil {
		return api.StatusResponse{}, err
	}
	return s.status(), nil
}

func (s *Service) applyCurrentSubscriptionLocked() error {
	updatedState, _, err := ensureRuntimePorts(s.state, s.supervisor.Running())
	if err != nil {
		s.recordErrorLocked(err.Error())
		return err
	}
	s.state = updatedState

	if err := s.writeManagedConfigLocked(); err != nil {
		s.recordErrorLocked(err.Error())
		return err
	}

	binaryPath, err := s.supervisor.Apply(s.state.ControllerPort, s.state.Secret)
	if err != nil {
		err = s.recoverRuntimeFailureLocked(err)
		s.recordErrorLocked(err.Error())
		return err
	}

	s.state.MihomoPath = binaryPath
	s.state.LastSyncAt = nowUTC()
	s.state.LastError = ""
	if s.state.SystemProxyEnabled {
		if err := macos.SetMixedProxy(s.state.MixedPort); err != nil {
			err = s.recoverRuntimeFailureLocked(err)
			s.recordErrorLocked(err.Error())
			return err
		}
	}
	if err := config.SaveState(s.paths.StatePath, s.state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}

func (s *Service) selectProxy(name string) (api.StatusResponse, error) {
	selected := strings.TrimSpace(name)
	if selected == "" {
		return api.StatusResponse{}, errors.New("proxy name cannot be empty")
	}

	snapshot := s.snapshot()
	if !snapshot.running {
		return api.StatusResponse{}, errors.New("mihomo is not running")
	}

	s.controllerMu.Lock()
	defer s.controllerMu.Unlock()

	if err := mihomo.SelectProxy(snapshot.state.ControllerPort, snapshot.state.Secret, proxyGroupName, selected); err != nil {
		return api.StatusResponse{}, err
	}
	return s.status(), nil
}

func (s *Service) testProxy(name string) (api.StatusResponse, error) {
	selected := strings.TrimSpace(name)
	if selected == "" {
		return api.StatusResponse{}, errors.New("proxy name cannot be empty")
	}

	snapshot := s.snapshot()
	if !snapshot.running {
		return api.StatusResponse{}, errors.New("mihomo is not running")
	}

	s.controllerMu.Lock()
	defer s.controllerMu.Unlock()

	current, _, err := mihomo.GetProxyGroup(snapshot.state.ControllerPort, snapshot.state.Secret, proxyGroupName)
	if err != nil {
		return api.StatusResponse{}, err
	}

	latencyMS, err := mihomo.TestProxyDelay(
		snapshot.state.ControllerPort,
		snapshot.state.Secret,
		selected,
		mihomo.DefaultDelayTestURL(),
		mihomo.DefaultDelayTimeout(),
	)
	if err != nil {
		return api.StatusResponse{}, err
	}

	speedBPS := int64(0)
	if current != "" && current != selected {
		if err := mihomo.SelectProxy(snapshot.state.ControllerPort, snapshot.state.Secret, proxyGroupName, selected); err != nil {
			return api.StatusResponse{}, err
		}
	}

	speedBPS, err = mihomo.MeasureDownloadSpeedViaMixedPort(snapshot.state.MixedPort)
	if current != "" && current != selected {
		if restoreErr := mihomo.SelectProxy(snapshot.state.ControllerPort, snapshot.state.Secret, proxyGroupName, current); restoreErr != nil {
			return api.StatusResponse{}, restoreErr
		}
	}
	if err != nil {
		speedBPS = 0
	}

	s.storeProxyMetric(selected, latencyMS, speedBPS)
	return s.status(), nil
}

func (s *Service) setSystemProxy(enabled bool) (api.StatusResponse, error) {
	s.mu.Lock()

	if enabled {
		if err := s.enableSystemProxyLocked(); err != nil {
			s.mu.Unlock()
			return api.StatusResponse{}, err
		}
	} else {
		if err := s.disableSystemProxyLocked(); err != nil {
			s.mu.Unlock()
			return api.StatusResponse{}, err
		}
	}

	s.state.LastError = ""
	if err := config.SaveState(s.paths.StatePath, s.state); err != nil {
		s.mu.Unlock()
		return api.StatusResponse{}, fmt.Errorf("save state: %w", err)
	}
	s.mu.Unlock()
	return s.status(), nil
}

func (s *Service) status() api.StatusResponse {
	snapshot := s.snapshot()
	return s.buildStatus(snapshot)
}

func (s *Service) snapshot() serviceSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return serviceSnapshot{
		state:      s.state,
		running:    s.supervisor.Running(),
		binaryPath: currentBinaryPath(s.state),
	}
}

func (s *Service) buildStatus(snapshot serviceSnapshot) api.StatusResponse {
	status := api.StatusResponse{
		DaemonVersion:      version,
		SocketPath:         s.paths.SocketPath,
		SubscriptionURL:    snapshot.state.SubscriptionURL,
		Subscriptions:      toSubscriptionOptions(snapshot.state.Subscriptions),
		ConfigPath:         s.paths.MihomoConfigPath,
		LogPath:            s.paths.MihomoLogPath,
		MihomoPath:         snapshot.binaryPath,
		MixedPort:          snapshot.state.MixedPort,
		ControllerPort:     snapshot.state.ControllerPort,
		SystemProxyEnabled: snapshot.state.SystemProxyEnabled,
		Running:            snapshot.running,
		LastError:          snapshot.state.LastError,
	}
	if !snapshot.state.LastSyncAt.IsZero() {
		lastSyncAt := snapshot.state.LastSyncAt
		status.LastSyncAt = &lastSyncAt
	}
	if status.MihomoPath == "" {
		status.Hint = "install mihomo first, for example: brew install mihomo"
	}
	if !snapshot.running {
		return status
	}

	current, proxies, err := mihomo.GetProxyGroup(snapshot.state.ControllerPort, snapshot.state.Secret, proxyGroupName)
	if err != nil {
		if status.LastError == "" {
			status.LastError = err.Error()
		}
		return status
	}
	status.CurrentProxy = current
	status.AvailableProxies = s.withProxyMetrics(proxies)
	return status
}

func (s *Service) writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Service) sanitizeExistingConfig() (bool, error) {
	rawConfig, err := os.ReadFile(s.paths.MihomoConfigPath)
	if err != nil {
		return false, err
	}

	sanitizedConfig, err := config.SanitizeConfig(rawConfig)
	if err != nil {
		return false, err
	}

	if string(sanitizedConfig) == string(rawConfig) {
		return false, nil
	}

	if err := fileutil.WriteFileAtomic(s.paths.MihomoConfigPath, sanitizedConfig, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) writeManagedConfigLocked() error {
	runtimeConfig, err := config.BuildProviderRuntimeConfig(
		s.state.SubscriptionURL,
		s.providerPathForSubscription(s.state.SubscriptionURL),
		config.RuntimeSettings{
			MixedPort:      s.state.MixedPort,
			ControllerPort: s.state.ControllerPort,
			Secret:         s.state.Secret,
		},
	)
	if err != nil {
		return err
	}

	if err := fileutil.WriteFileAtomic(s.paths.SubscriptionPath, []byte(s.state.SubscriptionURL+"\n"), 0o600); err != nil {
		return err
	}
	if err := fileutil.WriteFileAtomic(s.paths.MihomoConfigPath, runtimeConfig, 0o600); err != nil {
		return err
	}
	return nil
}

func (s *Service) clearProviderCacheLocked(subscriptionURL string) error {
	providerPath := s.providerPathForSubscription(subscriptionURL)
	if providerPath == "" {
		return nil
	}
	if err := os.Remove(providerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove provider cache: %w", err)
	}
	return nil
}

func (s *Service) removeManagedRuntimeLocked() error {
	if err := s.supervisor.Stop(); err != nil {
		return err
	}
	if s.state.SystemProxyEnabled {
		if err := s.disableSystemProxyLocked(); err != nil {
			return err
		}
	}
	s.state.SubscriptionURL = ""
	s.state.LastError = ""
	s.state.LastSyncAt = time.Time{}
	paths := []string{s.paths.SubscriptionPath, s.paths.MihomoConfigPath}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Service) providerPathForSubscription(subscriptionURL string) string {
	trimmed := strings.TrimSpace(subscriptionURL)
	if trimmed == "" {
		return s.paths.ProviderPath
	}
	sum := sha1.Sum([]byte(trimmed))
	name := "subscription-" + hex.EncodeToString(sum[:8]) + ".yaml"
	return filepath.Join(s.paths.ProviderDir, name)
}

func (s *Service) waitForManagedProxies() error {
	snapshot := s.snapshot()
	if !snapshot.running {
		return errors.New("mihomo is not running")
	}

	deadline := time.Now().Add(proxyLoadTimeout)
	for time.Now().Before(deadline) {
		_, proxies, err := mihomo.GetProxyGroup(snapshot.state.ControllerPort, snapshot.state.Secret, proxyGroupName)
		if err == nil && hasManagedProxies(proxies) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return errors.New("subscription nodes did not load in time")
}

func hasManagedProxies(proxies []api.ProxyOption) bool {
	if len(proxies) == 0 {
		return false
	}
	if len(proxies) == 1 && strings.EqualFold(strings.TrimSpace(proxies[0].Name), "COMPATIBLE") {
		return false
	}
	return true
}

func (s *Service) recordErrorLocked(message string) {
	s.state.LastError = message
	_ = config.SaveState(s.paths.StatePath, s.state)
}

func (s *Service) enableSystemProxyLocked() error {
	if !s.supervisor.Running() {
		return errors.New("mihomo is not running")
	}

	var capturedSnapshot *config.SystemProxySnapshot
	if !s.state.SystemProxyEnabled {
		snapshot, err := macos.CaptureSystemProxySnapshot()
		if err != nil {
			return err
		}
		capturedSnapshot = snapshot
		s.state.SystemProxySnapshot = snapshot
	}
	if err := macos.SetMixedProxy(s.state.MixedPort); err != nil {
		if capturedSnapshot != nil {
			if restoreErr := macos.RestoreSystemProxy(*capturedSnapshot); restoreErr != nil {
				s.state.SystemProxySnapshot = nil
				return fmt.Errorf("%w; additionally failed to restore previous system proxy settings: %v", err, restoreErr)
			}
			s.state.SystemProxySnapshot = nil
			return fmt.Errorf("%w; restored previous system proxy settings", err)
		}
		return err
	}
	s.state.SystemProxyEnabled = true
	return nil
}

func (s *Service) disableSystemProxyLocked() error {
	if s.state.SystemProxySnapshot != nil && !s.state.SystemProxySnapshot.Empty() {
		if err := macos.RestoreSystemProxy(*s.state.SystemProxySnapshot); err != nil {
			return err
		}
	} else {
		if err := macos.DisableSystemProxy(); err != nil {
			return err
		}
	}
	s.state.SystemProxyEnabled = false
	s.state.SystemProxySnapshot = nil
	return nil
}

func (s *Service) recoverRuntimeFailureLocked(err error) error {
	if !s.state.SystemProxyEnabled {
		return err
	}
	if restoreErr := s.disableSystemProxyLocked(); restoreErr != nil {
		return fmt.Errorf("%w; additionally failed to restore system proxy: %v", err, restoreErr)
	}
	return fmt.Errorf("%w; system proxy restored to previous settings", err)
}

func (s *Service) storeProxyMetric(name string, latencyMS int, speedBPS int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proxyMetrics[name] = proxyMetric{
		latencyMS: latencyMS,
		speedBPS:  speedBPS,
	}
}

func (s *Service) withProxyMetrics(proxies []api.ProxyOption) []api.ProxyOption {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(proxies) == 0 {
		return proxies
	}

	merged := make([]api.ProxyOption, len(proxies))
	copy(merged, proxies)
	for i := range merged {
		metric, ok := s.proxyMetrics[merged[i].Name]
		if !ok {
			continue
		}
		merged[i].LatencyMS = metric.latencyMS
		merged[i].SpeedBPS = metric.speedBPS
	}
	return merged
}

func toSubscriptionOptions(subscriptions []config.SubscriptionEntry) []api.SubscriptionOption {
	if len(subscriptions) == 0 {
		return nil
	}
	options := make([]api.SubscriptionOption, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		if strings.TrimSpace(subscription.URL) == "" {
			continue
		}
		options = append(options, api.SubscriptionOption{Name: subscription.Name, URL: subscription.URL})
	}
	return options
}

func containsSelectedSubscription(subscriptions []config.SubscriptionEntry, url string) bool {
	for _, subscription := range subscriptions {
		if subscription.URL == url {
			return true
		}
	}
	return false
}

func (s *Service) writePID() error {
	pid := os.Getpid()
	return fileutil.WriteFileAtomic(s.paths.PIDFile, []byte(fmt.Sprintf("%d", pid)), 0o600)
}

func (s *Service) shutdownSystemProxy() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.state.SystemProxyEnabled {
		return nil
	}
	if err := s.disableSystemProxyLocked(); err != nil {
		return err
	}
	if err := config.SaveState(s.paths.StatePath, s.state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func currentBinaryPath(state config.PersistedState) string {
	if state.MihomoPath != "" {
		return state.MihomoPath
	}
	if detectedPath, err := mihomo.FindBinary(); err == nil {
		return detectedPath
	}
	return ""
}

const httpShutdownTimeout = 3 * time.Second
