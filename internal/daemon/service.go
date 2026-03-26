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
	version                  = "v0.0.1"
	proxyGroupName           = "PROXY"
	proxyLoadTimeout         = 15 * time.Second
	systemProxyGuardInterval = 3 * time.Second
	statusControllerTimeout  = 500 * time.Millisecond
)

type Service struct {
	paths                app.Paths
	state                config.PersistedState
	supervisor           *mihomo.Supervisor
	mu                   sync.Mutex
	runtimeMu            sync.Mutex
	controllerMu         sync.Mutex
	proxyMetrics         map[string]proxyMetric
	cachedCurrentProxy   string
	cachedAvailableProxy []api.ProxyOption
	subscriptionOp       *api.SubscriptionOperation
	restartPending       bool
	shuttingDown         bool
}

type serviceSnapshot struct {
	state                config.PersistedState
	running              bool
	binaryPath           string
	cachedCurrentProxy   string
	cachedAvailableProxy []api.ProxyOption
	subscriptionOp       *api.SubscriptionOperation
}

type proxyMetric struct {
	latencyMS int
	speedBPS  int64
}

type runtimePlan struct {
	state         config.PersistedState
	runtimeConfig []byte
	updateSync    bool
}

func NewService(paths app.Paths) (*Service, error) {
	if err := paths.Ensure(); err != nil {
		return nil, err
	}

	state, err := config.LoadState(paths.StatePath)
	if err != nil {
		return nil, err
	}

	svc := &Service{
		paths:        paths,
		state:        state,
		proxyMetrics: make(map[string]proxyMetric),
	}
	svc.supervisor = mihomo.NewSupervisor(paths, svc.handleUnexpectedMihomoExit)
	return svc, nil
}

func (s *Service) Serve(ctx context.Context) error {
	if err := s.writePID(); err != nil {
		return err
	}
	defer os.Remove(s.paths.PIDFile)

	if err := os.Remove(s.paths.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.disableLegacyShellProxyEnv(); err != nil {
		return err
	}

	s.restoreRuntime()

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
	go s.watchSystemProxy(ctx)

	select {
	case <-ctx.Done():
		s.mu.Lock()
		s.shuttingDown = true
		s.mu.Unlock()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		stopErr := s.supervisor.Stop()
		proxyErr := s.shutdownSystemProxy()
		shellErr := s.disableLegacyShellProxyEnv()
		return errors.Join(stopErr, proxyErr, shellErr)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Service) restoreRuntime() {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()

	if err := s.repairStaleManagedSystemProxy(); err != nil {
		s.recordError(fmt.Sprintf("repair stale system proxy: %v", err))
	}

	s.mu.Lock()
	if strings.TrimSpace(s.state.SubscriptionURL) != "" {
		normalizedURL, err := subscription.NormalizeURL(s.state.SubscriptionURL)
		if err != nil {
			s.mu.Unlock()
			s.recordError(err.Error())
			return
		}
		s.state.SubscriptionURL = normalizedURL

		plan, err := s.buildRuntimePlanLocked(false, false)
		if err != nil {
			s.mu.Unlock()
			s.recordError(err.Error())
			return
		}
		stateToSave := s.state
		s.mu.Unlock()

		if err := config.SaveState(s.paths.StatePath, stateToSave); err != nil {
			s.recordError(fmt.Sprintf("save state: %v", err))
			return
		}
		if err := s.applyRuntimePlan(plan); err != nil {
			return
		}
		if err := s.waitForManagedProxies(); err != nil {
			s.recordError(err.Error())
		}
		return
	}

	systemProxyEnabled := s.state.SystemProxyEnabled
	s.mu.Unlock()

	if _, err := os.Stat(s.paths.MihomoConfigPath); err == nil {
		if _, err := s.sanitizeExistingConfig(); err != nil {
			s.recordError(fmt.Sprintf("sanitize config: %v", err))
			return
		}

		s.mu.Lock()
		controllerPort := s.state.ControllerPort
		secret := s.state.Secret
		mixedPort := s.state.MixedPort
		s.mu.Unlock()

		binaryPath, err := s.supervisor.Apply(controllerPort, secret)
		if err != nil {
			if systemProxyEnabled {
				err = s.recoverRuntimeFailure(err)
			}
			s.recordError(err.Error())
			return
		}
		s.mu.Lock()
		s.state.MihomoPath = binaryPath
		s.state.LastError = ""
		_ = config.SaveState(s.paths.StatePath, s.state)
		s.mu.Unlock()
		if systemProxyEnabled {
			if err := s.ensureSystemProxyApplied(mixedPort); err != nil {
				err = s.recoverRuntimeFailure(err)
				s.recordError(err.Error())
			}
		}
		return
	}

	if systemProxyEnabled {
		if err := s.disableSystemProxy(); err != nil {
			s.recordError(fmt.Sprintf("restore system proxy: %v", err))
			return
		}
		if err := s.saveState(); err != nil {
			s.recordError(fmt.Sprintf("save state: %v", err))
		}
	}
}

func (s *Service) repairStaleManagedSystemProxy() error {
	s.mu.Lock()
	systemProxyEnabled := s.state.SystemProxyEnabled
	mixedPort := s.state.MixedPort
	snapshot := s.state.SystemProxySnapshot
	s.mu.Unlock()

	if !systemProxyEnabled || mixedPort <= 0 {
		return nil
	}
	if localTCPPortReachable(mixedPort, 250*time.Millisecond) {
		return nil
	}

	managed, err := macos.IsManagedMixedProxyActive(mixedPort)
	if err != nil {
		return err
	}
	if !managed {
		return nil
	}
	return restoreSystemProxySnapshot(snapshot)
}

func localTCPPortReachable(port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func restoreSystemProxySnapshot(snapshot *config.SystemProxySnapshot) error {
	if snapshot != nil && !snapshot.Empty() {
		return macos.RestoreSystemProxy(*snapshot)
	}
	return macos.DisableSystemProxy()
}

func buildLegacyShellProxyCleanupEnv() []byte {
	return []byte(strings.Join([]string{
		"# generated by hnetd for legacy shell compatibility",
		"unset HNET_HTTP_PROXY HNET_HTTPS_PROXY HNET_ALL_PROXY",
		"unset http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY",
		"",
	}, "\n"))
}

func (s *Service) disableLegacyShellProxyEnv() error {
	legacyPath := filepath.Join(s.paths.BaseDir, "shell_proxy.sh")
	return fileutil.WriteFileAtomic(legacyPath, buildLegacyShellProxyCleanupEnv(), 0o600)
}

func mergeSystemProxySnapshots(current *config.SystemProxySnapshot, captured *config.SystemProxySnapshot) *config.SystemProxySnapshot {
	if captured == nil || captured.Empty() {
		return current
	}
	if current == nil || current.Empty() {
		merged := &config.SystemProxySnapshot{
			Services: make(map[string]config.SystemNetworkServiceProxy, len(captured.Services)),
		}
		for service, proxy := range captured.Services {
			merged.Services[service] = proxy
		}
		return merged
	}

	merged := &config.SystemProxySnapshot{
		Services: make(map[string]config.SystemNetworkServiceProxy, len(current.Services)+len(captured.Services)),
	}
	for service, proxy := range current.Services {
		merged.Services[service] = proxy
	}
	for service, proxy := range captured.Services {
		if _, exists := merged.Services[service]; exists {
			continue
		}
		merged.Services[service] = proxy
	}
	return merged
}

func (s *Service) buildRuntimePlanLocked(keepExisting bool, updateSync bool) (runtimePlan, error) {
	updatedState, _, err := ensureRuntimePorts(s.state, keepExisting)
	if err != nil {
		return runtimePlan{}, err
	}
	s.state = updatedState

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
		return runtimePlan{}, err
	}

	return runtimePlan{
		state:         s.state,
		runtimeConfig: runtimeConfig,
		updateSync:    updateSync,
	}, nil
}

func (s *Service) writeManagedConfig(plan runtimePlan) error {
	if err := fileutil.WriteFileAtomic(s.paths.MihomoConfigPath, plan.runtimeConfig, 0o600); err != nil {
		return err
	}
	return nil
}

func (s *Service) applyRuntimePlan(plan runtimePlan) error {
	for attempt := 0; attempt < 2; attempt++ {
		if err := s.writeManagedConfig(plan); err != nil {
			s.recordError(err.Error())
			return err
		}

		binaryPath, err := s.supervisor.Apply(plan.state.ControllerPort, plan.state.Secret)
		if err != nil {
			nextPlan, retried, retryErr := s.retryRuntimePlanAfterApplyFailure(plan, err)
			if retryErr != nil {
				err = retryErr
			}
			if retried {
				plan = nextPlan
				continue
			}

			err = s.recoverRuntimeFailure(err)
			s.recordError(err.Error())
			return err
		}

		if plan.state.SystemProxyEnabled {
			if err := s.ensureSystemProxyApplied(plan.state.MixedPort); err != nil {
				err = s.recoverRuntimeFailure(err)
				s.recordError(err.Error())
				return err
			}
		}

		s.mu.Lock()
		s.state.MihomoPath = binaryPath
		if plan.updateSync {
			s.state.LastSyncAt = nowUTC()
		}
		s.state.LastError = ""
		saveErr := config.SaveState(s.paths.StatePath, s.state)
		s.mu.Unlock()
		if saveErr != nil {
			err := fmt.Errorf("save state: %w", saveErr)
			s.recordError(err.Error())
			return err
		}
		return nil
	}

	return errors.New("apply runtime plan exhausted retries")
}

func (s *Service) retryRuntimePlanAfterApplyFailure(plan runtimePlan, applyErr error) (runtimePlan, bool, error) {
	if portAvailable(plan.state.MixedPort) && portAvailable(plan.state.ControllerPort) {
		return runtimePlan{}, false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	retryPlan, err := s.buildRuntimePlanLocked(false, plan.updateSync)
	if err != nil {
		return runtimePlan{}, false, err
	}
	if retryPlan.state.MixedPort == plan.state.MixedPort && retryPlan.state.ControllerPort == plan.state.ControllerPort {
		return runtimePlan{}, false, nil
	}

	s.state.LastError = fmt.Sprintf(
		"retrying runtime with new ports after startup failure on mixed/controller %d/%d: %v",
		plan.state.MixedPort,
		plan.state.ControllerPort,
		applyErr,
	)
	_ = config.SaveState(s.paths.StatePath, s.state)
	return retryPlan, true, nil
}

func (s *Service) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", s.handleStatus)
	mux.HandleFunc("/v1/subscription", s.handleSubscription)
	mux.HandleFunc("/v1/subscription/delete", s.handleSubscriptionDelete)
	mux.HandleFunc("/v1/subscription/refresh", s.handleSubscriptionRefresh)
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

	status, err := s.queueImportSubscription(request.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusAccepted, status)
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

	status, async, err := s.selectSubscription(request.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	statusCode := http.StatusOK
	if async {
		statusCode = http.StatusAccepted
	}
	s.writeJSON(w, statusCode, status)
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

	status, async, err := s.deleteSubscription(request.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	statusCode := http.StatusOK
	if async {
		statusCode = http.StatusAccepted
	}
	s.writeJSON(w, statusCode, status)
}

func (s *Service) handleSubscriptionRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request api.RefreshSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	status, err := s.refreshSubscription(request.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusAccepted, status)
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

func (s *Service) queueImportSubscription(rawURL string) (api.StatusResponse, error) {
	normalizedURL, err := subscription.NormalizeURL(rawURL)
	if err != nil {
		return api.StatusResponse{}, err
	}
	return s.startSubscriptionOperation("import", normalizedURL, func() error {
		_, runErr := s.importSubscriptionSync(normalizedURL)
		return runErr
	})
}

func (s *Service) importSubscriptionSync(rawURL string) (api.StatusResponse, error) {
	normalizedURL, err := subscription.NormalizeURL(rawURL)
	if err != nil {
		return api.StatusResponse{}, err
	}

	return s.applySubscription(normalizedURL, true)
}

func (s *Service) selectSubscription(rawURL string) (api.StatusResponse, bool, error) {
	normalizedURL, err := subscription.NormalizeURL(rawURL)
	if err != nil {
		return api.StatusResponse{}, false, err
	}

	s.mu.Lock()
	if !containsSelectedSubscription(s.state.Subscriptions, normalizedURL) {
		s.mu.Unlock()
		return api.StatusResponse{}, false, errors.New("subscription not found")
	}
	if s.state.SubscriptionURL == normalizedURL {
		s.mu.Unlock()
		return s.status(), false, nil
	}
	s.mu.Unlock()

	status, err := s.startSubscriptionOperation("select", normalizedURL, func() error {
		_, runErr := s.selectSubscriptionSync(normalizedURL)
		return runErr
	})
	return status, true, err
}

func (s *Service) selectSubscriptionSync(rawURL string) (api.StatusResponse, error) {
	normalizedURL, err := subscription.NormalizeURL(rawURL)
	if err != nil {
		return api.StatusResponse{}, err
	}
	return s.applySubscription(normalizedURL, false)
}

func (s *Service) deleteSubscription(rawURL string) (api.StatusResponse, bool, error) {
	normalizedURL, err := subscription.NormalizeURL(rawURL)
	if err != nil {
		return api.StatusResponse{}, false, err
	}

	s.mu.Lock()
	active := s.state.SubscriptionURL == normalizedURL
	s.mu.Unlock()
	if active {
		status, startErr := s.startSubscriptionOperation("delete", normalizedURL, func() error {
			_, _, runErr := s.deleteSubscriptionSync(normalizedURL)
			return runErr
		})
		return status, true, startErr
	}
	status, _, err := s.deleteSubscriptionSync(normalizedURL)
	return status, false, err
}

func (s *Service) deleteSubscriptionSync(rawURL string) (api.StatusResponse, bool, error) {
	normalizedURL, err := subscription.NormalizeURL(rawURL)
	if err != nil {
		return api.StatusResponse{}, false, err
	}

	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()

	s.mu.Lock()
	wasActive := s.state.SubscriptionURL == normalizedURL
	removed := s.state.DeleteSubscription(normalizedURL)
	if !removed {
		s.mu.Unlock()
		return api.StatusResponse{}, false, errors.New("subscription not found")
	}
	if err := s.clearProviderCacheLocked(normalizedURL); err != nil {
		s.mu.Unlock()
		return api.StatusResponse{}, false, err
	}
	s.proxyMetrics = make(map[string]proxyMetric)

	if !wasActive {
		if err := config.SaveState(s.paths.StatePath, s.state); err != nil {
			s.mu.Unlock()
			return api.StatusResponse{}, false, fmt.Errorf("save state: %w", err)
		}
		s.mu.Unlock()
		return s.status(), false, nil
	}

	if strings.TrimSpace(s.state.SubscriptionURL) == "" {
		s.mu.Unlock()
		if err := s.removeManagedRuntime(); err != nil {
			return api.StatusResponse{}, true, err
		}
		if err := s.saveState(); err != nil {
			return api.StatusResponse{}, true, fmt.Errorf("save state: %w", err)
		}
		return s.status(), true, nil
	}

	if err := s.clearProviderCacheLocked(s.state.SubscriptionURL); err != nil {
		s.mu.Unlock()
		return api.StatusResponse{}, true, err
	}
	plan, err := s.buildRuntimePlanLocked(s.supervisor.Running(), true)
	s.mu.Unlock()
	if err != nil {
		s.recordError(err.Error())
		return api.StatusResponse{}, true, err
	}

	if err := s.applyRuntimePlan(plan); err != nil {
		return api.StatusResponse{}, true, err
	}

	if err := s.waitForManagedProxies(); err != nil {
		return api.StatusResponse{}, true, err
	}
	return s.status(), true, nil
}

func (s *Service) refreshSubscription(rawURL string) (api.StatusResponse, error) {
	normalizedURL, err := subscription.NormalizeURL(rawURL)
	if err != nil {
		return api.StatusResponse{}, err
	}
	s.mu.Lock()
	activeURL := s.state.SubscriptionURL
	saved := containsSelectedSubscription(s.state.Subscriptions, normalizedURL)
	s.mu.Unlock()
	if !saved {
		return api.StatusResponse{}, errors.New("subscription not found")
	}
	if activeURL != normalizedURL {
		return api.StatusResponse{}, errors.New("only the active subscription can be refreshed")
	}
	return s.startSubscriptionOperation("refresh", normalizedURL, func() error {
		_, runErr := s.refreshSubscriptionSync(normalizedURL)
		return runErr
	})
}

func (s *Service) refreshSubscriptionSync(rawURL string) (api.StatusResponse, error) {
	normalizedURL, err := subscription.NormalizeURL(rawURL)
	if err != nil {
		return api.StatusResponse{}, err
	}

	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()

	s.mu.Lock()
	if s.state.SubscriptionURL != normalizedURL {
		s.mu.Unlock()
		return api.StatusResponse{}, errors.New("only the active subscription can be refreshed")
	}
	if err := s.clearProviderCacheLocked(normalizedURL); err != nil {
		s.mu.Unlock()
		return api.StatusResponse{}, err
	}
	s.proxyMetrics = make(map[string]proxyMetric)
	plan, err := s.buildRuntimePlanLocked(s.supervisor.Running(), true)
	s.mu.Unlock()
	if err != nil {
		s.recordError(err.Error())
		return api.StatusResponse{}, err
	}
	if err := s.applyRuntimePlan(plan); err != nil {
		return api.StatusResponse{}, err
	}
	if err := s.waitForManagedProxies(); err != nil {
		return api.StatusResponse{}, err
	}
	return s.status(), nil
}

func (s *Service) applySubscription(normalizedURL string, addToList bool) (api.StatusResponse, error) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()

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
	plan, err := s.buildRuntimePlanLocked(s.supervisor.Running(), true)
	s.mu.Unlock()
	if err != nil {
		s.recordError(err.Error())
		return api.StatusResponse{}, err
	}

	if err := s.applyRuntimePlan(plan); err != nil {
		return api.StatusResponse{}, err
	}

	if err := s.waitForManagedProxies(); err != nil {
		return api.StatusResponse{}, err
	}
	return s.status(), nil
}

func (s *Service) removeManagedRuntime() error {
	s.mu.Lock()
	systemProxyEnabled := s.state.SystemProxyEnabled
	s.mu.Unlock()

	if err := s.supervisor.Stop(); err != nil {
		return err
	}
	if err := s.disableLegacyShellProxyEnv(); err != nil {
		return err
	}
	if systemProxyEnabled {
		if err := s.disableSystemProxy(); err != nil {
			return err
		}
	}

	s.mu.Lock()
	s.state.SubscriptionURL = ""
	s.state.LastError = ""
	s.state.LastSyncAt = time.Time{}
	s.mu.Unlock()

	paths := []string{s.paths.MihomoConfigPath}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
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

	current, proxies, err := mihomo.GetProxyGroup(snapshot.state.ControllerPort, snapshot.state.Secret, proxyGroupName)
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

	s.storeProxyMetric(proxyMetricKeyForName(proxies, selected), latencyMS, speedBPS)
	return s.status(), nil
}

func (s *Service) setSystemProxy(enabled bool) (api.StatusResponse, error) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()

	if enabled {
		if err := s.enableSystemProxy(); err != nil {
			return api.StatusResponse{}, err
		}
	} else {
		if err := s.disableSystemProxy(); err != nil {
			return api.StatusResponse{}, err
		}
	}

	s.mu.Lock()
	s.state.LastError = ""
	err := config.SaveState(s.paths.StatePath, s.state)
	s.mu.Unlock()
	if err != nil {
		return api.StatusResponse{}, fmt.Errorf("save state: %w", err)
	}
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
		state:                s.state,
		running:              s.supervisor.Running(),
		binaryPath:           currentBinaryPath(s.state),
		cachedCurrentProxy:   s.cachedCurrentProxy,
		cachedAvailableProxy: cloneProxyOptions(s.cachedAvailableProxy),
		subscriptionOp:       cloneSubscriptionOperation(s.subscriptionOp),
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
	if snapshot.subscriptionOp != nil {
		status.SubscriptionOp = snapshot.subscriptionOp
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

	if snapshot.subscriptionOp != nil && snapshot.subscriptionOp.State == "running" {
		status.CurrentProxy = snapshot.cachedCurrentProxy
		status.AvailableProxies = s.withProxyMetrics(snapshot.cachedAvailableProxy)
		return status
	}

	current, proxies, err := mihomo.GetProxyGroupWithTimeout(snapshot.state.ControllerPort, snapshot.state.Secret, proxyGroupName, statusControllerTimeout)
	if err != nil {
		if snapshot.cachedCurrentProxy != "" || len(snapshot.cachedAvailableProxy) > 0 {
			status.CurrentProxy = snapshot.cachedCurrentProxy
			status.AvailableProxies = s.withProxyMetrics(snapshot.cachedAvailableProxy)
		}
		if status.LastError == "" {
			status.LastError = err.Error()
		}
		return status
	}
	s.storeProxySnapshot(current, proxies)
	status.CurrentProxy = current
	status.AvailableProxies = s.withProxyMetrics(proxies)
	return status
}

func (s *Service) writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Service) startSubscriptionOperation(kind string, targetURL string, run func() error) (api.StatusResponse, error) {
	startedAt := nowUTC()

	s.mu.Lock()
	if s.subscriptionOp != nil && s.subscriptionOp.State == "running" {
		current := cloneSubscriptionOperation(s.subscriptionOp)
		s.mu.Unlock()
		if current != nil && current.Kind != "" {
			return api.StatusResponse{}, fmt.Errorf("subscription operation already running: %s", current.Kind)
		}
		return api.StatusResponse{}, errors.New("subscription operation already running")
	}
	s.subscriptionOp = &api.SubscriptionOperation{
		Kind:      kind,
		State:     "running",
		TargetURL: targetURL,
		StartedAt: &startedAt,
	}
	s.mu.Unlock()

	go s.runSubscriptionOperation(kind, targetURL, run)
	return s.status(), nil
}

func (s *Service) runSubscriptionOperation(kind string, targetURL string, run func() error) {
	err := run()
	finishedAt := nowUTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subscriptionOp == nil || s.subscriptionOp.Kind != kind || s.subscriptionOp.TargetURL != targetURL {
		return
	}
	s.subscriptionOp.FinishedAt = &finishedAt
	if err != nil {
		s.subscriptionOp.State = "failed"
		s.subscriptionOp.Message = err.Error()
		return
	}
	s.subscriptionOp.State = "succeeded"
	s.subscriptionOp.Message = "done"
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
		current, proxies, err := mihomo.GetProxyGroup(snapshot.state.ControllerPort, snapshot.state.Secret, proxyGroupName)
		if err == nil && hasManagedProxies(proxies) {
			s.storeProxySnapshot(current, proxies)
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

func (s *Service) recordError(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordErrorLocked(message)
}

func (s *Service) recordErrorLocked(message string) {
	s.state.LastError = message
	_ = config.SaveState(s.paths.StatePath, s.state)
}

func (s *Service) saveState() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return config.SaveState(s.paths.StatePath, s.state)
}

func (s *Service) enableSystemProxy() error {
	s.mu.Lock()
	running := s.supervisor.Running()
	mixedPort := s.state.MixedPort
	s.mu.Unlock()

	if !running {
		return errors.New("mihomo is not running")
	}

	return s.ensureSystemProxyApplied(mixedPort)
}

func (s *Service) disableSystemProxy() error {
	s.mu.Lock()
	snapshot := s.state.SystemProxySnapshot
	s.mu.Unlock()

	if snapshot != nil && !snapshot.Empty() {
		if err := macos.RestoreSystemProxy(*snapshot); err != nil {
			return err
		}
	} else {
		if err := macos.DisableSystemProxy(); err != nil {
			return err
		}
	}

	s.mu.Lock()
	s.state.SystemProxyEnabled = false
	s.state.SystemProxySnapshot = nil
	s.mu.Unlock()
	return nil
}

func (s *Service) ensureSystemProxyApplied(mixedPort int) error {
	capturedSnapshot, err := macos.CaptureSystemProxySnapshot()
	if err != nil {
		return err
	}

	s.mu.Lock()
	mergedSnapshot := mergeSystemProxySnapshots(s.state.SystemProxySnapshot, capturedSnapshot)
	s.mu.Unlock()

	if err := macos.SetMixedProxy(mixedPort); err != nil {
		if capturedSnapshot != nil && !capturedSnapshot.Empty() {
			if restoreErr := restoreSystemProxySnapshot(capturedSnapshot); restoreErr != nil {
				return fmt.Errorf("%w; additionally failed to restore previous system proxy settings: %v", err, restoreErr)
			}
			return fmt.Errorf("%w; restored current system proxy settings", err)
		}
		return err
	}

	s.mu.Lock()
	s.state.SystemProxySnapshot = mergedSnapshot
	s.state.SystemProxyEnabled = true
	s.mu.Unlock()
	return nil
}

func (s *Service) watchSystemProxy(ctx context.Context) {
	ticker := time.NewTicker(systemProxyGuardInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		s.runtimeMu.Lock()

		s.mu.Lock()
		enabled := s.state.SystemProxyEnabled
		mixedPort := s.state.MixedPort
		s.mu.Unlock()
		running := s.supervisor.Running()

		if !enabled || !running || mixedPort <= 0 {
			s.runtimeMu.Unlock()
			continue
		}

		managed, err := macos.IsManagedMixedProxyActive(mixedPort)
		if err != nil {
			s.runtimeMu.Unlock()
			s.recordError(fmt.Sprintf("check system proxy guard: %v", err))
			continue
		}
		if managed {
			s.runtimeMu.Unlock()
			continue
		}

		err = s.ensureSystemProxyApplied(mixedPort)
		s.runtimeMu.Unlock()
		if err != nil {
			s.recordError(fmt.Sprintf("reapply system proxy: %v", err))
		}
	}
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

func (s *Service) recoverRuntimeFailure(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recoverRuntimeFailureLocked(err)
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

func (s *Service) handleUnexpectedMihomoExit(err error) {
	message := "mihomo exited unexpectedly"
	if err != nil {
		message = fmt.Sprintf("mihomo exited unexpectedly: %v", err)
	}

	s.mu.Lock()
	if s.shuttingDown {
		s.mu.Unlock()
		return
	}
	s.state.LastError = message
	_ = config.SaveState(s.paths.StatePath, s.state)

	shouldRestart := strings.TrimSpace(s.state.SubscriptionURL) != "" && !s.restartPending
	if shouldRestart {
		s.restartPending = true
	}
	s.mu.Unlock()

	if shellErr := s.disableLegacyShellProxyEnv(); shellErr != nil {
		s.recordError(fmt.Sprintf("write legacy shell proxy cleanup: %v", shellErr))
	}

	if shouldRestart {
		go s.restartManagedRuntime()
	}
}

func (s *Service) restartManagedRuntime() {
	defer func() {
		s.mu.Lock()
		s.restartPending = false
		s.mu.Unlock()
	}()

	backoff := time.Second
	for attempt := 1; attempt <= 3; attempt++ {
		s.runtimeMu.Lock()

		s.mu.Lock()
		if s.shuttingDown || strings.TrimSpace(s.state.SubscriptionURL) == "" {
			s.mu.Unlock()
			s.runtimeMu.Unlock()
			return
		}
		plan, err := s.buildRuntimePlanLocked(false, false)
		s.mu.Unlock()
		if err != nil {
			s.runtimeMu.Unlock()
			s.recordError(err.Error())
			return
		}

		err = s.applyRuntimePlan(plan)
		s.runtimeMu.Unlock()
		if err == nil {
			if waitErr := s.waitForManagedProxies(); waitErr == nil {
				return
			} else {
				err = waitErr
				s.recordError(err.Error())
			}
		}

		if attempt == 3 {
			return
		}
		time.Sleep(backoff)
		backoff *= 2
	}
}

func (s *Service) storeProxyMetric(key string, latencyMS int, speedBPS int64) {
	if strings.TrimSpace(key) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proxyMetrics[key] = proxyMetric{
		latencyMS: latencyMS,
		speedBPS:  speedBPS,
	}
}

func (s *Service) storeProxySnapshot(current string, proxies []api.ProxyOption) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cachedCurrentProxy = current
	s.cachedAvailableProxy = cloneProxyOptions(proxies)
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
		metric, ok := s.proxyMetrics[proxyMetricKey(merged[i])]
		if !ok {
			continue
		}
		merged[i].LatencyMS = metric.latencyMS
		merged[i].SpeedBPS = metric.speedBPS
	}
	return merged
}

func cloneProxyOptions(proxies []api.ProxyOption) []api.ProxyOption {
	if len(proxies) == 0 {
		return nil
	}
	cloned := make([]api.ProxyOption, len(proxies))
	copy(cloned, proxies)
	return cloned
}

func cloneSubscriptionOperation(op *api.SubscriptionOperation) *api.SubscriptionOperation {
	if op == nil {
		return nil
	}
	cloned := *op
	if op.StartedAt != nil {
		started := *op.StartedAt
		cloned.StartedAt = &started
	}
	if op.FinishedAt != nil {
		finished := *op.FinishedAt
		cloned.FinishedAt = &finished
	}
	return &cloned
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
		options = append(options, api.SubscriptionOption{ID: subscription.ID, Name: subscription.Name, URL: subscription.URL})
	}
	return options
}

func proxyMetricKey(option api.ProxyOption) string {
	if strings.TrimSpace(option.ID) != "" {
		return option.ID
	}
	return strings.TrimSpace(option.Name)
}

func proxyMetricKeyForName(options []api.ProxyOption, name string) string {
	target := strings.TrimSpace(name)
	if target == "" {
		return ""
	}
	for _, option := range options {
		if option.Name == target {
			return proxyMetricKey(option)
		}
	}
	return target
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
