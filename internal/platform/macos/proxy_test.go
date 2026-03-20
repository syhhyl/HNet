package macos

import (
	"testing"

	"hnet/internal/config"
)

func TestParseKeyValueLines(t *testing.T) {
	values := parseKeyValueLines("Enabled: Yes\nServer: 127.0.0.1\nPort: 7890\n")

	if values["Enabled"] != "Yes" {
		t.Fatalf("expected Enabled to be Yes, got %q", values["Enabled"])
	}
	if values["Server"] != "127.0.0.1" {
		t.Fatalf("expected Server to be 127.0.0.1, got %q", values["Server"])
	}
	if values["Port"] != "7890" {
		t.Fatalf("expected Port to be 7890, got %q", values["Port"])
	}
}

func TestParseBool(t *testing.T) {
	tests := map[string]bool{
		"Yes":     true,
		"On":      true,
		"1":       true,
		"enabled": true,
		"No":      false,
		"Off":     false,
		"0":       false,
		"":        false,
	}

	for input, want := range tests {
		if got := parseBool(input); got != want {
			t.Fatalf("parseBool(%q) = %t, want %t", input, got, want)
		}
	}
}

func TestParseInt(t *testing.T) {
	if got := parseInt("7890"); got != 7890 {
		t.Fatalf("parseInt returned %d, want 7890", got)
	}
	if got := parseInt("bad"); got != 0 {
		t.Fatalf("parseInt returned %d, want 0 for invalid input", got)
	}
}

func TestNormalizeNetworksetupValue(t *testing.T) {
	if got := normalizeNetworksetupValue("(null)"); got != "" {
		t.Fatalf("expected (null) to normalize to empty string, got %q", got)
	}
	if got := normalizeNetworksetupValue(" 127.0.0.1 "); got != "127.0.0.1" {
		t.Fatalf("expected trimmed server value, got %q", got)
	}
}

func TestValidateSupportedServiceProxyRejectsAuthenticatedProxy(t *testing.T) {
	err := validateSupportedServiceProxy("Wi-Fi", config.SystemNetworkServiceProxy{
		Web: config.SystemManualProxy{Authenticated: true},
	})
	if err == nil {
		t.Fatal("expected authenticated proxy validation error")
	}
}

func TestValidateSupportedServiceProxyAllowsUnauthenticatedProxy(t *testing.T) {
	err := validateSupportedServiceProxy("Wi-Fi", config.SystemNetworkServiceProxy{
		Web:       config.SystemManualProxy{Enabled: true, Server: "127.0.0.1", Port: 7890},
		SecureWeb: config.SystemManualProxy{},
		Socks:     config.SystemManualProxy{},
	})
	if err != nil {
		t.Fatalf("validateSupportedServiceProxy() error = %v", err)
	}
}

func TestIsManagedProxyForPort(t *testing.T) {
	proxy := config.SystemNetworkServiceProxy{
		Web:       config.SystemManualProxy{Enabled: true, Server: "127.0.0.1", Port: 7890},
		SecureWeb: config.SystemManualProxy{Enabled: true, Server: "127.0.0.1", Port: 7890},
		Socks:     config.SystemManualProxy{Enabled: true, Server: "127.0.0.1", Port: 7890},
	}

	if !isManagedProxyForPort(proxy, 7890) {
		t.Fatal("expected managed proxy settings to match")
	}

	proxy.AutoProxyURL.Enabled = true
	if isManagedProxyForPort(proxy, 7890) {
		t.Fatal("expected auto proxy URL to disqualify managed proxy detection")
	}

	proxy.AutoProxyURL.Enabled = false
	proxy.Socks.Port = 7891
	if isManagedProxyForPort(proxy, 7890) {
		t.Fatal("expected mismatched port to disqualify managed proxy detection")
	}
}

func TestParseNetworkServiceOrder(t *testing.T) {
	output := `
An asterisk (*) denotes that a network service is disabled.
(1) Wi-Fi
(Hardware Port: Wi-Fi, Device: en0)
(2) USB 10/100/1000 LAN
(Hardware Port: USB 10/100/1000 LAN, Device: en5)
`

	bindings := parseNetworkServiceOrder(output)
	if len(bindings) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(bindings))
	}
	if bindings[0].Service != "Wi-Fi" || bindings[0].Device != "en0" {
		t.Fatalf("unexpected first binding: %#v", bindings[0])
	}
	if bindings[1].Service != "USB 10/100/1000 LAN" || bindings[1].Device != "en5" {
		t.Fatalf("unexpected second binding: %#v", bindings[1])
	}
}

func TestParseRouteInterface(t *testing.T) {
	output := `
   route to: default
destination: default
   interface: en0
`

	if got := parseRouteInterface(output); got != "en0" {
		t.Fatalf("expected interface en0, got %q", got)
	}
}
