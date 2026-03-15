package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBuildProviderRuntimeConfig(t *testing.T) {
	data, err := BuildProviderRuntimeConfig(
		"https://example.com/api/v1/client/subscribe?token=abc",
		"./providers/imported.yaml",
		RuntimeSettings{MixedPort: 7890, ControllerPort: 61990, Secret: "secret"},
	)
	if err != nil {
		t.Fatalf("BuildProviderRuntimeConfig() error = %v", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if doc["mixed-port"] != 7890 {
		t.Fatalf("expected mixed-port 7890, got %#v", doc["mixed-port"])
	}
	if doc["external-controller"] != "127.0.0.1:61990" {
		t.Fatalf("unexpected external-controller %#v", doc["external-controller"])
	}

	providers, ok := doc["proxy-providers"].(map[string]any)
	if !ok {
		t.Fatalf("expected proxy-providers map, got %#v", doc["proxy-providers"])
	}
	imported, ok := providers["imported"].(map[string]any)
	if !ok {
		t.Fatalf("expected imported provider, got %#v", providers["imported"])
	}
	if imported["url"] != "https://example.com/api/v1/client/subscribe?token=abc" {
		t.Fatalf("unexpected provider url %#v", imported["url"])
	}
	if imported["path"] != "./providers/imported.yaml" {
		t.Fatalf("unexpected provider path %#v", imported["path"])
	}

	groups, ok := doc["proxy-groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("expected single proxy group, got %#v", doc["proxy-groups"])
	}
}

func TestSanitizeConfigRemovesGeodataRules(t *testing.T) {
	raw := []byte(`mixed-port: 7890
rules:
  - DOMAIN-SUFFIX,example.com,DIRECT
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
`)

	data, err := SanitizeConfig(raw)
	if err != nil {
		t.Fatalf("SanitizeConfig() error = %v", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	rules, ok := doc["rules"].([]any)
	if !ok || len(rules) != 2 {
		t.Fatalf("unexpected sanitized rules %#v", doc["rules"])
	}
	if rules[0] != "DOMAIN-SUFFIX,example.com,DIRECT" || rules[1] != "MATCH,PROXY" {
		t.Fatalf("unexpected sanitized rules %#v", rules)
	}
}
