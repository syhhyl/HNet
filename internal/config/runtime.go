package config

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultProviderPath = "./providers/imported.yaml"

var domesticResolvers = []string{
	"223.5.5.5",
	"119.29.29.29",
}

var domesticDoHResolvers = []string{
	"https://dns.alidns.com/dns-query",
	"https://doh.pub/dns-query",
}

var localDirectRules = []string{
	"DOMAIN-SUFFIX,lan,DIRECT",
	"DOMAIN-SUFFIX,local,DIRECT",
	"DOMAIN-SUFFIX,localhost,DIRECT",
	"DOMAIN-SUFFIX,home.arpa,DIRECT",
	"IP-CIDR,0.0.0.0/8,DIRECT,no-resolve",
	"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve",
	"IP-CIDR,100.64.0.0/10,DIRECT,no-resolve",
	"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve",
	"IP-CIDR,172.16.0.0/12,DIRECT,no-resolve",
	"IP-CIDR,192.168.0.0/16,DIRECT,no-resolve",
	"IP-CIDR,169.254.0.0/16,DIRECT,no-resolve",
	"IP-CIDR,198.18.0.0/15,DIRECT,no-resolve",
	"IP-CIDR,224.0.0.0/4,DIRECT,no-resolve",
	"IP-CIDR,240.0.0.0/4,DIRECT,no-resolve",
	"IP-CIDR,255.255.255.255/32,DIRECT,no-resolve",
	"IP-CIDR6,::1/128,DIRECT,no-resolve",
	"IP-CIDR6,fc00::/7,DIRECT,no-resolve",
	"IP-CIDR6,fe80::/10,DIRECT,no-resolve",
	"IP-CIDR6,ff00::/8,DIRECT,no-resolve",
}

var explicitProxyRules = []string{
	"DOMAIN-SUFFIX,openai.com,PROXY",
	"DOMAIN-SUFFIX,chatgpt.com,PROXY",
	"DOMAIN-SUFFIX,oaistatic.com,PROXY",
	"DOMAIN-SUFFIX,oaiusercontent.com,PROXY",
	"DOMAIN-SUFFIX,anthropic.com,PROXY",
	"DOMAIN-SUFFIX,claude.ai,PROXY",
	"DOMAIN-SUFFIX,models.dev,PROXY",
	"DOMAIN-SUFFIX,openrouter.ai,PROXY",
	"DOMAIN-SUFFIX,google.com,PROXY",
	"DOMAIN-SUFFIX,googleapis.com,PROXY",
	"DOMAIN-SUFFIX,gstatic.com,PROXY",
	"DOMAIN-SUFFIX,gvt1.com,PROXY",
	"DOMAIN-SUFFIX,youtube.com,PROXY",
	"DOMAIN-SUFFIX,youtu.be,PROXY",
	"DOMAIN-SUFFIX,ytimg.com,PROXY",
	"DOMAIN-SUFFIX,googlevideo.com,PROXY",
	"DOMAIN-SUFFIX,github.com,PROXY",
	"DOMAIN-SUFFIX,githubusercontent.com,PROXY",
	"DOMAIN-SUFFIX,githubassets.com,PROXY",
	"DOMAIN-SUFFIX,telegram.org,PROXY",
	"DOMAIN-SUFFIX,t.me,PROXY",
	"DOMAIN-SUFFIX,telegra.ph,PROXY",
	"DOMAIN-SUFFIX,tdesktop.com,PROXY",
	"DOMAIN-SUFFIX,twitter.com,PROXY",
	"DOMAIN-SUFFIX,x.com,PROXY",
	"DOMAIN-SUFFIX,twimg.com,PROXY",
	"DOMAIN-SUFFIX,facebook.com,PROXY",
	"DOMAIN-SUFFIX,fbcdn.net,PROXY",
	"DOMAIN-SUFFIX,instagram.com,PROXY",
	"DOMAIN-SUFFIX,threads.net,PROXY",
	"DOMAIN-SUFFIX,whatsapp.com,PROXY",
	"DOMAIN-SUFFIX,whatsapp.net,PROXY",
	"DOMAIN-SUFFIX,discord.com,PROXY",
	"DOMAIN-SUFFIX,discord.gg,PROXY",
	"DOMAIN-SUFFIX,discordapp.com,PROXY",
	"DOMAIN-SUFFIX,netflix.com,PROXY",
	"DOMAIN-SUFFIX,nflxvideo.net,PROXY",
	"DOMAIN-SUFFIX,nflximg.net,PROXY",
	"DOMAIN-SUFFIX,nflxext.com,PROXY",
	"DOMAIN-SUFFIX,disneyplus.com,PROXY",
	"DOMAIN-SUFFIX,reddit.com,PROXY",
	"DOMAIN-SUFFIX,redd.it,PROXY",
}

var chinaDirectRules = []string{
	"DOMAIN-SUFFIX,cn,DIRECT",
	"GEOIP,CN,DIRECT",
}

var fallbackProxyRules = []string{
	"MATCH,PROXY",
}

func defaultRules() []string {
	rules := make([]string, 0, len(localDirectRules)+len(explicitProxyRules)+len(chinaDirectRules)+len(fallbackProxyRules))
	rules = append(rules, localDirectRules...)
	rules = append(rules, explicitProxyRules...)
	rules = append(rules, chinaDirectRules...)
	rules = append(rules, fallbackProxyRules...)
	return rules
}

func defaultDNSConfig() map[string]any {
	return map[string]any{
		"enable":                  true,
		"ipv6":                    false,
		"respect-rules":           true,
		"enhanced-mode":           "redir-host",
		"use-hosts":               true,
		"default-nameserver":      append([]string(nil), domesticResolvers...),
		"nameserver":              append([]string(nil), domesticResolvers...),
		"proxy-server-nameserver": append([]string(nil), domesticDoHResolvers...),
	}
}

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
		"mixed-port": settings.MixedPort,
		"allow-lan":  false,
		"mode":       "rule",
		"geox-url": map[string]any{
			"mmdb": "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/country.mmdb",
		},
		"external-controller": fmt.Sprintf("127.0.0.1:%d", settings.ControllerPort),
		"secret":              settings.Secret,
		"log-level":           "warning",
		"profile": map[string]any{
			"store-selected": true,
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
		"dns":   defaultDNSConfig(),
		"rules": defaultRules(),
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
