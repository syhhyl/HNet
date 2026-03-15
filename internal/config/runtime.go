package config

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultProviderPath = "./providers/imported.yaml"

type RuntimeSettings struct {
	MixedPort      int
	ControllerPort int
	Secret         string
}

func BuildProviderRuntimeConfig(subscriptionURL string, providerPath string, settings RuntimeSettings) ([]byte, error) {
	if strings.TrimSpace(subscriptionURL) == "" {
		return nil, errors.New("subscription URL cannot be empty")
	}
	if strings.TrimSpace(providerPath) == "" {
		providerPath = defaultProviderPath
	}

	doc := map[string]any{
		"mixed-port":          settings.MixedPort,
		"allow-lan":           false,
		"mode":                "rule",
		"external-controller": fmt.Sprintf("127.0.0.1:%d", settings.ControllerPort),
		"secret":              settings.Secret,
		"log-level":           "info",
		"profile": map[string]any{
			"store-selected": true,
			"store-fake-ip":  true,
		},
		"proxy-providers": map[string]any{
			"imported": map[string]any{
				"type":     "http",
				"url":      subscriptionURL,
				"path":     providerPath,
				"proxy":    "DIRECT",
				"interval": 3600,
				"header": map[string][]string{
					"User-Agent": {"clash.meta"},
					"Accept":     {"*/*"},
				},
			},
		},
		"proxy-groups": []map[string]any{
			{
				"name": "PROXY",
				"type": "select",
				"use":  []string{"imported"},
			},
		},
		"rules": []string{"MATCH,PROXY"},
	}

	encoded, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode runtime config: %w", err)
	}
	return encoded, nil
}

func SanitizeConfig(raw []byte) ([]byte, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode config yaml: %w", err)
	}
	if len(doc) == 0 {
		return nil, errors.New("config is empty or unsupported")
	}

	sanitizeConfigDoc(doc)

	encoded, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode config yaml: %w", err)
	}
	return encoded, nil
}

func sanitizeConfigDoc(doc map[string]any) {
	if !hasList(doc, "rules") {
		doc["rules"] = []string{"MATCH,PROXY"}
		return
	}

	if sanitized := sanitizeRules(doc["rules"]); len(sanitized) > 0 {
		doc["rules"] = sanitized
		return
	}
	doc["rules"] = []string{"MATCH,PROXY"}
}

func hasList(doc map[string]any, key string) bool {
	items, ok := doc[key].([]any)
	return ok && len(items) > 0
}

func sanitizeRules(value any) []string {
	rawRules, ok := value.([]any)
	if !ok {
		return nil
	}

	rules := make([]string, 0, len(rawRules))
	for _, rawRule := range rawRules {
		rule, ok := rawRule.(string)
		if !ok {
			continue
		}
		upperRule := strings.ToUpper(strings.TrimSpace(rule))
		if strings.HasPrefix(upperRule, "GEOIP,") || strings.HasPrefix(upperRule, "GEOSITE,") {
			continue
		}
		rules = append(rules, rule)
	}
	return rules
}
