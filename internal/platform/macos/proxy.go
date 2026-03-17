package macos

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"hnet/internal/config"
)

type manualProxyCommand struct {
	getFlag      string
	setFlag      string
	setStateFlag string
	label        string
}

var manualProxyCommands = []manualProxyCommand{
	{getFlag: "-getwebproxy", setFlag: "-setwebproxy", setStateFlag: "-setwebproxystate", label: "web"},
	{getFlag: "-getsecurewebproxy", setFlag: "-setsecurewebproxy", setStateFlag: "-setsecurewebproxystate", label: "secure web"},
	{getFlag: "-getsocksfirewallproxy", setFlag: "-setsocksfirewallproxy", setStateFlag: "-setsocksfirewallproxystate", label: "socks"},
}

func CaptureSystemProxySnapshot() (*config.SystemProxySnapshot, error) {
	services, err := enabledServices()
	if err != nil {
		return nil, err
	}

	snapshot := &config.SystemProxySnapshot{
		Services: make(map[string]config.SystemNetworkServiceProxy, len(services)),
	}
	for _, service := range services {
		serviceProxy, err := captureNetworkServiceProxy(service)
		if err != nil {
			return nil, err
		}
		if err := validateSupportedServiceProxy(service, serviceProxy); err != nil {
			return nil, err
		}
		snapshot.Services[service] = serviceProxy
	}
	return snapshot, nil
}

func SetMixedProxy(port int) error {
	services, err := enabledServices()
	if err != nil {
		return err
	}

	var result error
	for _, service := range services {
		if err := setManagedProxyForService(service, port); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func RestoreSystemProxy(snapshot config.SystemProxySnapshot) error {
	if len(snapshot.Services) == 0 {
		return DisableSystemProxy()
	}

	services, err := enabledServices()
	if err != nil {
		return err
	}
	available := make(map[string]struct{}, len(services))
	for _, service := range services {
		available[service] = struct{}{}
	}

	var result error
	for service, proxy := range snapshot.Services {
		if _, ok := available[service]; !ok {
			continue
		}
		if err := restoreNetworkServiceProxy(service, proxy); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func DisableSystemProxy() error {
	services, err := enabledServices()
	if err != nil {
		return err
	}

	var result error
	for _, service := range services {
		if err := runNetworksetup("-setwebproxystate", service, "off"); err != nil {
			result = errors.Join(result, err)
		}
		if err := runNetworksetup("-setsecurewebproxystate", service, "off"); err != nil {
			result = errors.Join(result, err)
		}
		if err := runNetworksetup("-setsocksfirewallproxystate", service, "off"); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func captureNetworkServiceProxy(service string) (config.SystemNetworkServiceProxy, error) {
	web, err := readManualProxy(service, manualProxyCommands[0])
	if err != nil {
		return config.SystemNetworkServiceProxy{}, err
	}
	secureWeb, err := readManualProxy(service, manualProxyCommands[1])
	if err != nil {
		return config.SystemNetworkServiceProxy{}, err
	}
	socks, err := readManualProxy(service, manualProxyCommands[2])
	if err != nil {
		return config.SystemNetworkServiceProxy{}, err
	}
	autoProxyURL, err := readAutoProxyURL(service)
	if err != nil {
		return config.SystemNetworkServiceProxy{}, err
	}
	proxyAutoDiscovery, err := readProxyAutoDiscovery(service)
	if err != nil {
		return config.SystemNetworkServiceProxy{}, err
	}

	return config.SystemNetworkServiceProxy{
		Web:                web,
		SecureWeb:          secureWeb,
		Socks:              socks,
		AutoProxyURL:       autoProxyURL,
		ProxyAutoDiscovery: proxyAutoDiscovery,
	}, nil
}

func setManagedProxyForService(service string, port int) error {
	var result error
	if err := runNetworksetup("-setautoproxystate", service, "off"); err != nil {
		result = errors.Join(result, err)
	}
	if err := runNetworksetup("-setproxyautodiscovery", service, "off"); err != nil {
		result = errors.Join(result, err)
	}

	for _, command := range manualProxyCommands {
		if err := runNetworksetup(command.setFlag, service, "127.0.0.1", strconv.Itoa(port), "off"); err != nil {
			result = errors.Join(result, err)
			continue
		}
		if err := runNetworksetup(command.setStateFlag, service, "on"); err != nil {
			result = errors.Join(result, err)
		}
	}
	if result != nil {
		return fmt.Errorf("set system proxy for %q: %w", service, result)
	}
	return nil
}

func restoreNetworkServiceProxy(service string, proxy config.SystemNetworkServiceProxy) error {
	var result error
	if err := restoreManualProxy(service, manualProxyCommands[0], proxy.Web); err != nil {
		result = errors.Join(result, err)
	}
	if err := restoreManualProxy(service, manualProxyCommands[1], proxy.SecureWeb); err != nil {
		result = errors.Join(result, err)
	}
	if err := restoreManualProxy(service, manualProxyCommands[2], proxy.Socks); err != nil {
		result = errors.Join(result, err)
	}
	if err := restoreAutoProxyURL(service, proxy.AutoProxyURL); err != nil {
		result = errors.Join(result, err)
	}
	if err := runNetworksetup("-setproxyautodiscovery", service, onOff(proxy.ProxyAutoDiscovery)); err != nil {
		result = errors.Join(result, err)
	}
	if result != nil {
		return fmt.Errorf("restore system proxy for %q: %w", service, result)
	}
	return nil
}

func readManualProxy(service string, command manualProxyCommand) (config.SystemManualProxy, error) {
	output, err := runNetworksetupOutput(command.getFlag, service)
	if err != nil {
		return config.SystemManualProxy{}, err
	}
	values := parseKeyValueLines(output)
	return config.SystemManualProxy{
		Enabled:       parseBool(values["Enabled"]),
		Server:        normalizeNetworksetupValue(values["Server"]),
		Port:          parseInt(values["Port"]),
		Authenticated: parseBool(firstNonEmpty(values["Authenticated Proxy Enabled"], values["Authenticated Proxy"])),
	}, nil
}

func restoreManualProxy(service string, command manualProxyCommand, proxy config.SystemManualProxy) error {
	if proxy.Authenticated {
		return fmt.Errorf("restore %s proxy: authenticated macOS proxies are not supported", command.label)
	}

	var result error
	if proxy.Server != "" && proxy.Port > 0 {
		if err := runNetworksetup(command.setFlag, service, proxy.Server, strconv.Itoa(proxy.Port), "off"); err != nil {
			result = errors.Join(result, err)
		}
	}
	if err := runNetworksetup(command.setStateFlag, service, onOff(proxy.Enabled)); err != nil {
		result = errors.Join(result, err)
	}
	if result != nil {
		return fmt.Errorf("restore %s proxy: %w", command.label, result)
	}
	return nil
}

func readAutoProxyURL(service string) (config.SystemAutoProxy, error) {
	output, err := runNetworksetupOutput("-getautoproxyurl", service)
	if err != nil {
		return config.SystemAutoProxy{}, err
	}
	values := parseKeyValueLines(output)
	return config.SystemAutoProxy{
		Enabled: parseBool(values["Enabled"]),
		URL:     normalizeNetworksetupValue(firstNonEmpty(values["URL"], values["URLString"])),
	}, nil
}

func restoreAutoProxyURL(service string, autoProxy config.SystemAutoProxy) error {
	var result error
	if autoProxy.URL != "" {
		if err := runNetworksetup("-setautoproxyurl", service, autoProxy.URL); err != nil {
			result = errors.Join(result, err)
		}
	}
	if err := runNetworksetup("-setautoproxystate", service, onOff(autoProxy.Enabled)); err != nil {
		result = errors.Join(result, err)
	}
	if result != nil {
		return fmt.Errorf("restore auto proxy URL: %w", result)
	}
	return nil
}

func readProxyAutoDiscovery(service string) (bool, error) {
	output, err := runNetworksetupOutput("-getproxyautodiscovery", service)
	if err != nil {
		return false, err
	}
	values := parseKeyValueLines(output)
	if value := firstNonEmpty(values["Auto Proxy Discovery"], values["Enabled"]); value != "" {
		return parseBool(value), nil
	}
	return parseBool(strings.TrimSpace(output)), nil
}

func enabledServices() ([]string, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("system proxy is only supported on macOS")
	}

	output, err := runNetworksetupOutput("-listallnetworkservices")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(output, "\n")
	services := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "An asterisk") || strings.HasPrefix(line, "*") {
			continue
		}
		services = append(services, line)
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no enabled network services found")
	}
	return services, nil
}

func parseKeyValueLines(output string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "on", "true", "1", "enabled":
		return true
	default:
		return false
	}
}

func parseInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeNetworksetupValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.EqualFold(trimmed, "(null)") {
		return ""
	}
	return trimmed
}

func validateSupportedServiceProxy(service string, proxy config.SystemNetworkServiceProxy) error {
	checks := []struct {
		label string
		proxy config.SystemManualProxy
	}{
		{label: manualProxyCommands[0].label, proxy: proxy.Web},
		{label: manualProxyCommands[1].label, proxy: proxy.SecureWeb},
		{label: manualProxyCommands[2].label, proxy: proxy.Socks},
	}

	for _, check := range checks {
		if check.proxy.Authenticated {
			return fmt.Errorf("network service %q uses an authenticated %s proxy; hnet cannot safely manage authenticated macOS proxies", service, check.label)
		}
	}
	return nil
}

func runNetworksetup(args ...string) error {
	_, err := runNetworksetupOutput(args...)
	return err
}

func runNetworksetupOutput(args ...string) (string, error) {
	cmd := exec.Command("networksetup", args...)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		return "", fmt.Errorf("networksetup %s: %w: %s", strings.Join(args, " "), err, bytes.TrimSpace(output))
	}
	return trimmed, nil
}
