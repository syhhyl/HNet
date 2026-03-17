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
