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

	dnsConfig, ok := doc["dns"].(map[string]any)
	if !ok {
		t.Fatalf("expected dns config map, got %#v", doc["dns"])
	}
	if dnsConfig["enhanced-mode"] != "redir-host" {
		t.Fatalf("expected redir-host dns mode, got %#v", dnsConfig["enhanced-mode"])
	}
	if dnsConfig["respect-rules"] != true {
		t.Fatalf("expected respect-rules enabled, got %#v", dnsConfig["respect-rules"])
	}
	if !containsStringValue(dnsConfig["default-nameserver"], "223.5.5.5") {
		t.Fatalf("expected domestic bootstrap resolver, got %#v", dnsConfig["default-nameserver"])
	}
	if !containsStringValue(dnsConfig["proxy-server-nameserver"], "https://dns.alidns.com/dns-query") {
		t.Fatalf("expected proxy server DoH resolver, got %#v", dnsConfig["proxy-server-nameserver"])
	}
	if _, exists := dnsConfig["nameserver-policy"]; exists {
		t.Fatalf("expected lean dns config without nameserver-policy, got %#v", dnsConfig["nameserver-policy"])
	}

	rules, ok := doc["rules"].([]any)
	if !ok || len(rules) == 0 {
		t.Fatalf("expected runtime rules, got %#v", doc["rules"])
	}
	if !containsRule(rules, "DOMAIN-SUFFIX,openai.com,PROXY") {
		t.Fatalf("expected explicit OpenAI proxy rule, got %#v", rules)
	}
	if !containsRule(rules, "GEOIP,CN,DIRECT") {
		t.Fatalf("expected CN geoip direct rule, got %#v", rules)
	}
	if rules[len(rules)-1] != "MATCH,PROXY" {
		t.Fatalf("expected MATCH,PROXY as final rule, got %#v", rules[len(rules)-1])
	}

	geoxURL, ok := doc["geox-url"].(map[string]any)
	if !ok || geoxURL["mmdb"] == nil {
		t.Fatalf("expected geox-url mmdb config, got %#v", doc["geox-url"])
	}
	if _, ok := geoxURL["geosite"]; ok {
		t.Fatalf("expected lean geox-url without geosite config, got %#v", geoxURL["geosite"])
	}

	profile, ok := doc["profile"].(map[string]any)
	if !ok {
		t.Fatalf("expected profile config, got %#v", doc["profile"])
	}
	if profile["store-selected"] != true {
		t.Fatalf("expected store-selected enabled, got %#v", profile["store-selected"])
	}
	if _, ok := profile["store-fake-ip"]; ok {
		t.Fatalf("expected lean profile without store-fake-ip, got %#v", profile["store-fake-ip"])
	}
	if doc["log-level"] != "warning" {
		t.Fatalf("expected warning log level, got %#v", doc["log-level"])
	}
}

func containsRule(rules []any, expected string) bool {
	for _, rule := range rules {
		if value, ok := rule.(string); ok && value == expected {
			return true
		}
	}
	return false
}

func containsStringValue(value any, expected string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if s, ok := item.(string); ok && s == expected {
			return true
		}
	}
	return false
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
