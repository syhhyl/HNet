package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hnet/internal/api"
	"hnet/internal/app"
	"hnet/internal/config"
	"hnet/internal/mihomo"
)

func TestDeleteSubscriptionSyncInactivePersistsWithoutRuntimeRebuild(t *testing.T) {
	paths := testPaths(t)
	svc := &Service{
		paths: paths,
		state: config.PersistedState{
			SubscriptionURL: "https://one.example.com/sub",
			Subscriptions: []config.SubscriptionEntry{
				{Name: "one.example.com", URL: "https://one.example.com/sub"},
				{Name: "two.example.com", URL: "https://two.example.com/sub"},
			},
			Secret: "secret",
		},
		proxyMetrics: make(map[string]proxyMetric),
		supervisor:   mihomo.NewSupervisor(paths, nil),
	}
	writeProviderCache(t, svc, "https://two.example.com/sub", []byte("stale"))

	status, changed, err := svc.deleteSubscriptionSync("https://two.example.com/sub")
	if err != nil {
		t.Fatalf("deleteSubscriptionSync() error = %v", err)
	}
	if changed {
		t.Fatal("expected inactive delete to avoid runtime rebuild")
	}
	if status.SubscriptionURL != "https://one.example.com/sub" {
		t.Fatalf("expected active subscription to stay unchanged, got %q", status.SubscriptionURL)
	}
	if len(status.Subscriptions) != 1 || status.Subscriptions[0].URL != "https://one.example.com/sub" {
		t.Fatalf("unexpected subscriptions after delete: %#v", status.Subscriptions)
	}
	if _, err := os.Stat(svc.providerPathForSubscription("https://two.example.com/sub")); !os.IsNotExist(err) {
		t.Fatalf("expected deleted provider cache to be removed, stat err = %v", err)
	}
	if _, err := os.Stat(paths.MihomoConfigPath); !os.IsNotExist(err) {
		t.Fatalf("expected no runtime config rewrite for inactive delete, stat err = %v", err)
	}

	saved, err := config.LoadState(paths.StatePath)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if saved.SubscriptionURL != "https://one.example.com/sub" {
		t.Fatalf("expected saved active subscription to stay unchanged, got %q", saved.SubscriptionURL)
	}
	if len(saved.Subscriptions) != 1 || saved.Subscriptions[0].URL != "https://one.example.com/sub" {
		t.Fatalf("unexpected saved subscriptions after delete: %#v", saved.Subscriptions)
	}
}

func TestDeleteSubscriptionSyncLastActiveRemovesManagedRuntime(t *testing.T) {
	paths := testPaths(t)
	lastSyncAt := time.Now().UTC()
	svc := &Service{
		paths: paths,
		state: config.PersistedState{
			SubscriptionURL: "https://one.example.com/sub",
			Subscriptions: []config.SubscriptionEntry{
				{Name: "one.example.com", URL: "https://one.example.com/sub"},
			},
			Secret:     "secret",
			LastError:  "old error",
			LastSyncAt: lastSyncAt,
		},
		proxyMetrics: make(map[string]proxyMetric),
		supervisor:   mihomo.NewSupervisor(paths, nil),
	}
	if err := os.WriteFile(paths.MihomoConfigPath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("seed runtime config: %v", err)
	}

	status, changed, err := svc.deleteSubscriptionSync("https://one.example.com/sub")
	if err != nil {
		t.Fatalf("deleteSubscriptionSync() error = %v", err)
	}
	if !changed {
		t.Fatal("expected deleting the last active subscription to change runtime")
	}
	if status.SubscriptionURL != "" {
		t.Fatalf("expected no active subscription after delete, got %q", status.SubscriptionURL)
	}
	if len(status.Subscriptions) != 0 {
		t.Fatalf("expected no subscriptions after delete, got %#v", status.Subscriptions)
	}
	if svc.state.SubscriptionURL != "" {
		t.Fatalf("expected in-memory active subscription to be cleared, got %q", svc.state.SubscriptionURL)
	}
	if !svc.state.LastSyncAt.IsZero() {
		t.Fatalf("expected last sync time to be cleared, got %v", svc.state.LastSyncAt)
	}
	if svc.state.LastError != "" {
		t.Fatalf("expected last error to be cleared, got %q", svc.state.LastError)
	}
	if _, err := os.Stat(paths.MihomoConfigPath); !os.IsNotExist(err) {
		t.Fatalf("expected runtime config to be removed, stat err = %v", err)
	}
	legacyPath := filepath.Join(paths.BaseDir, "shell_proxy.sh")
	legacyData, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("read legacy shell proxy cleanup script: %v", err)
	}
	if !strings.Contains(string(legacyData), "unset http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY") {
		t.Fatalf("expected legacy shell cleanup script, got %q", string(legacyData))
	}

	saved, err := config.LoadState(paths.StatePath)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if saved.SubscriptionURL != "" || len(saved.Subscriptions) != 0 {
		t.Fatalf("expected cleared saved state, got %#v", saved)
	}
	if !saved.LastSyncAt.IsZero() {
		t.Fatalf("expected saved last sync time to be cleared, got %v", saved.LastSyncAt)
	}
}

func TestDeleteSubscriptionSyncActiveSelectsNeighborAndRewritesRuntimeBeforeApplyFailure(t *testing.T) {
	paths := testPaths(t)
	t.Setenv("HNET_MIHOMO_BIN", paths.BaseDir)
	svc := &Service{
		paths: paths,
		state: config.PersistedState{
			SubscriptionURL: "https://two.example.com/sub",
			Subscriptions: []config.SubscriptionEntry{
				{Name: "one.example.com", URL: "https://one.example.com/sub"},
				{Name: "two.example.com", URL: "https://two.example.com/sub"},
				{Name: "three.example.com", URL: "https://three.example.com/sub"},
			},
			Secret: "secret",
		},
		proxyMetrics: make(map[string]proxyMetric),
		supervisor:   mihomo.NewSupervisor(paths, nil),
	}
	writeProviderCache(t, svc, "https://two.example.com/sub", []byte("stale deleted"))
	writeProviderCache(t, svc, "https://three.example.com/sub", []byte("stale next"))

	_, changed, err := svc.deleteSubscriptionSync("https://two.example.com/sub")
	if err == nil {
		t.Fatal("expected apply failure when fake mihomo binary is a directory")
	}
	if !changed {
		t.Fatal("expected active delete with remaining subscriptions to enter runtime rebuild path")
	}
	if svc.state.SubscriptionURL != "https://three.example.com/sub" {
		t.Fatalf("expected next subscription to become active, got %q", svc.state.SubscriptionURL)
	}
	if _, err := os.Stat(svc.providerPathForSubscription("https://two.example.com/sub")); !os.IsNotExist(err) {
		t.Fatalf("expected deleted subscription provider cache to be removed, stat err = %v", err)
	}
	if _, err := os.Stat(svc.providerPathForSubscription("https://three.example.com/sub")); !os.IsNotExist(err) {
		t.Fatalf("expected next subscription provider cache to be cleared before rebuild, stat err = %v", err)
	}
	rawConfig, err := os.ReadFile(paths.MihomoConfigPath)
	if err != nil {
		t.Fatalf("read rewritten runtime config: %v", err)
	}
	content := string(rawConfig)
	if !strings.Contains(content, "https://three.example.com/sub") {
		t.Fatalf("expected runtime config to target next subscription, got %q", content)
	}
	if strings.Contains(content, "https://two.example.com/sub") {
		t.Fatalf("expected runtime config to stop targeting deleted subscription, got %q", content)
	}

	saved, loadErr := config.LoadState(paths.StatePath)
	if loadErr != nil {
		t.Fatalf("LoadState() error = %v", loadErr)
	}
	if saved.SubscriptionURL != "https://three.example.com/sub" {
		t.Fatalf("expected saved state to keep next subscription active after apply failure, got %q", saved.SubscriptionURL)
	}
	if saved.LastError == "" {
		t.Fatal("expected apply failure to be recorded in saved state")
	}
}

func TestDeleteSubscriptionActiveStartsAsyncOperation(t *testing.T) {
	paths := testPaths(t)
	t.Setenv("HNET_MIHOMO_BIN", paths.BaseDir)
	svc := &Service{
		paths: paths,
		state: config.PersistedState{
			SubscriptionURL: "https://one.example.com/sub",
			Subscriptions: []config.SubscriptionEntry{
				{Name: "one.example.com", URL: "https://one.example.com/sub"},
				{Name: "two.example.com", URL: "https://two.example.com/sub"},
			},
			Secret: "secret",
		},
		proxyMetrics: make(map[string]proxyMetric),
		supervisor:   mihomo.NewSupervisor(paths, nil),
	}

	status, async, err := svc.deleteSubscription("https://one.example.com/sub")
	if err != nil {
		t.Fatalf("deleteSubscription() error = %v", err)
	}
	if !async {
		t.Fatal("expected active delete to start an async operation")
	}
	if status.SubscriptionOp == nil {
		t.Fatal("expected delete operation status to be present")
	}
	if status.SubscriptionOp.Kind != "delete" {
		t.Fatalf("expected delete operation kind, got %#v", status.SubscriptionOp)
	}
	if status.SubscriptionOp.TargetURL != "https://one.example.com/sub" {
		t.Fatalf("expected delete target URL to be preserved, got %#v", status.SubscriptionOp)
	}
	if svc.subscriptionOp == nil || svc.subscriptionOp.Kind != "delete" {
		t.Fatalf("expected service to retain delete operation metadata, got %#v", svc.subscriptionOp)
	}
}

func testPaths(t *testing.T) app.Paths {
	t.Helper()

	baseDir := t.TempDir()
	paths := app.Paths{
		BaseDir:          baseDir,
		RuntimeDir:       filepath.Join(baseDir, "runtime"),
		ProviderDir:      filepath.Join(baseDir, "runtime", "providers"),
		SocketPath:       filepath.Join(baseDir, "hnetd.sock"),
		PIDFile:          filepath.Join(baseDir, "hnetd.pid"),
		StatePath:        filepath.Join(baseDir, "state.json"),
		DaemonLogPath:    filepath.Join(baseDir, "hnetd.log"),
		MihomoConfigPath: filepath.Join(baseDir, "runtime", "config.yaml"),
		MihomoLogPath:    filepath.Join(baseDir, "runtime", "mihomo.log"),
		ProviderPath:     filepath.Join(baseDir, "runtime", "providers", "imported.yaml"),
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("paths.Ensure() error = %v", err)
	}
	return paths
}

func writeProviderCache(t *testing.T, svc *Service, subscriptionURL string, data []byte) {
	t.Helper()

	providerPath := svc.providerPathForSubscription(subscriptionURL)
	if err := os.MkdirAll(filepath.Dir(providerPath), 0o700); err != nil {
		t.Fatalf("create provider cache dir: %v", err)
	}
	if err := os.WriteFile(providerPath, data, 0o600); err != nil {
		t.Fatalf("write provider cache: %v", err)
	}
}

func TestMergeSystemProxySnapshots(t *testing.T) {
	current := &config.SystemProxySnapshot{
		Services: map[string]config.SystemNetworkServiceProxy{
			"Wi-Fi": {
				Web: config.SystemManualProxy{Enabled: true, Server: "old", Port: 1},
			},
		},
	}
	captured := &config.SystemProxySnapshot{
		Services: map[string]config.SystemNetworkServiceProxy{
			"Wi-Fi": {
				Web: config.SystemManualProxy{Enabled: true, Server: "new", Port: 2},
			},
			"USB LAN": {
				Web: config.SystemManualProxy{Enabled: true, Server: "usb", Port: 3},
			},
		},
	}

	merged := mergeSystemProxySnapshots(current, captured)
	if merged == nil {
		t.Fatal("expected merged snapshot")
	}
	if len(merged.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(merged.Services))
	}
	if got := merged.Services["Wi-Fi"].Web.Server; got != "old" {
		t.Fatalf("expected existing service snapshot to be preserved, got %q", got)
	}
	if got := merged.Services["USB LAN"].Web.Server; got != "usb" {
		t.Fatalf("expected new service snapshot to be added, got %q", got)
	}
}

func TestRetryRuntimePlanAfterApplyFailureReassignsBusyPorts(t *testing.T) {
	mixedListener, mixedPort := busyPort(t)
	defer mixedListener.Close()

	controllerListener, controllerPort := busyPort(t)
	defer controllerListener.Close()

	svc := &Service{
		state: config.PersistedState{
			SubscriptionURL: "https://example.com/sub",
			Secret:          "secret",
			MixedPort:       mixedPort,
			ControllerPort:  controllerPort,
		},
	}
	plan := runtimePlan{
		state: svc.state,
	}

	retryPlan, retried, err := svc.retryRuntimePlanAfterApplyFailure(plan, errors.New("controller did not become ready"))
	if err != nil {
		t.Fatalf("retryRuntimePlanAfterApplyFailure() error = %v", err)
	}
	if !retried {
		t.Fatal("expected runtime plan retry when both ports are busy")
	}
	if retryPlan.state.MixedPort == mixedPort {
		t.Fatalf("expected mixed port to change from %d", mixedPort)
	}
	if retryPlan.state.ControllerPort == controllerPort {
		t.Fatalf("expected controller port to change from %d", controllerPort)
	}
}

func TestDisableLegacyShellProxyEnvWritesUnsetScript(t *testing.T) {
	baseDir := t.TempDir()
	legacyPath := filepath.Join(baseDir, "shell_proxy.sh")
	if err := os.WriteFile(legacyPath, []byte("export http_proxy=http://127.0.0.1:7890\n"), 0o600); err != nil {
		t.Fatalf("seed legacy shell proxy file: %v", err)
	}

	svc := &Service{
		paths: app.Paths{BaseDir: baseDir},
	}
	if err := svc.disableLegacyShellProxyEnv(); err != nil {
		t.Fatalf("disableLegacyShellProxyEnv() error = %v", err)
	}

	data, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("read legacy shell proxy file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "unset http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY") {
		t.Fatalf("expected unset script, got %q", content)
	}
	if strings.Contains(content, "export http_proxy") {
		t.Fatalf("expected legacy exports to be removed, got %q", content)
	}
}

func TestSelectSubscriptionNoOpWhenAlreadyActive(t *testing.T) {
	svc := &Service{
		state: config.PersistedState{
			SubscriptionURL: "https://one.example.com/sub",
			Subscriptions: []config.SubscriptionEntry{
				{Name: "one.example.com", URL: "https://one.example.com/sub"},
			},
		},
		supervisor: mihomo.NewSupervisor(app.Paths{}, nil),
	}

	status, async, err := svc.selectSubscription("https://one.example.com/sub")
	if err != nil {
		t.Fatalf("selectSubscription() error = %v", err)
	}
	if async {
		t.Fatal("expected active subscription select to be synchronous")
	}
	if status.SubscriptionURL != "https://one.example.com/sub" {
		t.Fatalf("expected active subscription to stay unchanged, got %q", status.SubscriptionURL)
	}
}

func TestBuildStatusUsesCachedProxiesWhileSubscriptionOperationRunning(t *testing.T) {
	startedAt := nowUTC()
	svc := &Service{}

	status := svc.buildStatus(serviceSnapshot{
		state: config.PersistedState{
			SubscriptionURL: "https://one.example.com/sub",
			ControllerPort:  61990,
			Secret:          "secret",
		},
		running:            true,
		cachedCurrentProxy: "node-a",
		cachedAvailableProxy: []api.ProxyOption{
			{Name: "node-a", Alive: true},
		},
		subscriptionOp: &api.SubscriptionOperation{
			Kind:      "select",
			State:     "running",
			TargetURL: "https://two.example.com/sub",
			StartedAt: &startedAt,
		},
	})

	if status.CurrentProxy != "node-a" {
		t.Fatalf("expected cached current proxy, got %q", status.CurrentProxy)
	}
	if len(status.AvailableProxies) != 1 || status.AvailableProxies[0].Name != "node-a" {
		t.Fatalf("expected cached proxies, got %#v", status.AvailableProxies)
	}
	if status.SubscriptionOp == nil || status.SubscriptionOp.State != "running" {
		t.Fatalf("expected running subscription operation, got %#v", status.SubscriptionOp)
	}
}

func TestRefreshSubscriptionRejectsInactiveURLImmediately(t *testing.T) {
	svc := &Service{
		state: config.PersistedState{
			Subscriptions: []config.SubscriptionEntry{
				{Name: "one.example.com", URL: "https://one.example.com/sub"},
				{Name: "two.example.com", URL: "https://two.example.com/sub"},
			},
			SubscriptionURL: "https://one.example.com/sub",
		},
	}

	_, err := svc.refreshSubscription("https://two.example.com/sub")
	if err == nil {
		t.Fatal("expected refreshSubscription() to reject inactive subscription")
	}
	if !strings.Contains(err.Error(), "only the active subscription can be refreshed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
