package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hnet/internal/api"
	"hnet/internal/app"
	"hnet/internal/config"
	"hnet/internal/mihomo"
)

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
