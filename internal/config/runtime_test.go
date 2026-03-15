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
	policy, ok := dnsConfig["nameserver-policy"].(map[string]any)
	if !ok {
		t.Fatalf("expected nameserver-policy map, got %#v", dnsConfig["nameserver-policy"])
	}
	if !containsStringValue(policy["geosite:cn"], "223.5.5.5") {
		t.Fatalf("expected CN policy resolver, got %#v", policy["geosite:cn"])
	}
	if !containsStringValue(policy["geosite:geolocation-!cn"], "https://1.1.1.1/dns-query") {
		t.Fatalf("expected global policy DoH resolver, got %#v", policy["geosite:geolocation-!cn"])
	}

	rules, ok := doc["rules"].([]any)
	if !ok || len(rules) == 0 {
		t.Fatalf("expected runtime rules, got %#v", doc["rules"])
	}
	if !containsRule(rules, "DOMAIN-SUFFIX,openai.com,PROXY") {
		t.Fatalf("expected explicit OpenAI proxy rule, got %#v", rules)
	}
	if !containsRule(rules, "GEOSITE,CN,DIRECT") {
		t.Fatalf("expected CN geosite direct rule, got %#v", rules)
	}
	if !containsRule(rules, "GEOIP,CN,DIRECT") {
		t.Fatalf("expected CN geoip direct rule, got %#v", rules)
	}
	if !containsRule(rules, "GEOSITE,geolocation-!cn,PROXY") {
		t.Fatalf("expected geolocation catch-all proxy rule, got %#v", rules)
	}
	if rules[len(rules)-1] != "MATCH,PROXY" {
		t.Fatalf("expected MATCH,PROXY as final rule, got %#v", rules[len(rules)-1])
	}

	geoxURL, ok := doc["geox-url"].(map[string]any)
	if !ok || geoxURL["mmdb"] == nil {
		t.Fatalf("expected geox-url mmdb config, got %#v", doc["geox-url"])
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
