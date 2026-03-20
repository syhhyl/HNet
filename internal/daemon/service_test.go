package daemon

import (
	"errors"
	"testing"

	"hnet/internal/config"
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
