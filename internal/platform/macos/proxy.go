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

type networkServiceBinding struct {
	Service string
	Device  string
}

func CaptureSystemProxySnapshot() (*config.SystemProxySnapshot, error) {
	services, err := managedServices()
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
	services, err := managedServices()
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

func IsManagedMixedProxyActive(port int) (bool, error) {
	services, err := managedServices()
	if err != nil {
		return false, err
	}

	for _, service := range services {
		proxy, err := captureNetworkServiceProxy(service)
		if err != nil {
			return false, err
		}
		if !isManagedProxyForPort(proxy, port) {
			return false, nil
		}
	}
	return true, nil
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

func managedServices() ([]string, error) {
	active, err := activeServices()
	if err == nil && len(active) > 0 {
		return active, nil
	}
	return enabledServices()
}

func activeServices() ([]string, error) {
	bindings, err := networkServiceBindings()
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return nil, nil
	}

	devices := activeDefaultDevices()
	if len(devices) == 0 {
		return nil, nil
	}

	services := make([]string, 0, len(bindings))
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		if _, ok := devices[binding.Device]; !ok {
			continue
		}
		if _, ok := seen[binding.Service]; ok {
			continue
		}
		seen[binding.Service] = struct{}{}
		services = append(services, binding.Service)
	}
	return services, nil
}

func networkServiceBindings() ([]networkServiceBinding, error) {
	services, err := enabledServices()
	if err != nil {
		return nil, err
	}

	output, err := runNetworksetupOutput("-listnetworkserviceorder")
	if err != nil {
		return nil, err
	}

	bindings := parseNetworkServiceOrder(output)
	if len(bindings) == 0 {
		return nil, nil
	}

	enabled := make(map[string]struct{}, len(services))
	for _, service := range services {
		enabled[service] = struct{}{}
	}

	filtered := make([]networkServiceBinding, 0, len(bindings))
	for _, binding := range bindings {
		if _, ok := enabled[binding.Service]; !ok {
			continue
		}
		filtered = append(filtered, binding)
	}
	return filtered, nil
}

func parseNetworkServiceOrder(output string) []networkServiceBinding {
	lines := strings.Split(output, "\n")
	bindings := make([]networkServiceBinding, 0, len(lines)/2)
	var currentService string

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "An asterisk") {
			continue
		}
		if strings.Contains(line, "Device:") {
			if currentService == "" {
				continue
			}
			if device := parseOrderedServiceDevice(line); device != "" {
				bindings = append(bindings, networkServiceBinding{
					Service: currentService,
					Device:  device,
				})
				currentService = ""
			}
			continue
		}
		if strings.HasPrefix(line, "(") {
			if service := parseOrderedServiceName(line); service != "" {
				currentService = service
			}
			continue
		}
	}
	return bindings
}

func parseOrderedServiceName(line string) string {
	_, rest, ok := strings.Cut(line, ")")
	if !ok {
		return ""
	}
	return strings.TrimSpace(rest)
}

func parseOrderedServiceDevice(line string) string {
	marker := "Device:"
	index := strings.Index(line, marker)
	if index == -1 {
		return ""
	}
	device := strings.TrimSpace(line[index+len(marker):])
	if comma := strings.Index(device, ","); comma != -1 {
		device = strings.TrimSpace(device[:comma])
	}
	device = strings.TrimSuffix(device, ")")
	return strings.TrimSpace(device)
}

func activeDefaultDevices() map[string]struct{} {
	devices := make(map[string]struct{}, 2)
	for _, args := range [][]string{{"-n", "get", "default"}, {"-n", "get", "-inet6", "default"}} {
		output, err := runCommandOutput("route", args...)
		if err != nil {
			continue
		}
		if device := parseRouteInterface(output); device != "" {
			devices[device] = struct{}{}
		}
	}
	return devices
}

func parseRouteInterface(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "interface:") {
			continue
		}
		_, value, ok := strings.Cut(line, ":")
		if !ok {
			return ""
		}
		return strings.TrimSpace(value)
	}
	return ""
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

func isManagedProxyForPort(proxy config.SystemNetworkServiceProxy, port int) bool {
	if port <= 0 {
		return false
	}
	if proxy.AutoProxyURL.Enabled || strings.TrimSpace(proxy.AutoProxyURL.URL) != "" {
		return false
	}
	if proxy.ProxyAutoDiscovery {
		return false
	}

	checks := []config.SystemManualProxy{proxy.Web, proxy.SecureWeb, proxy.Socks}
	for _, candidate := range checks {
		if !candidate.Enabled {
			return false
		}
		if candidate.Server != "127.0.0.1" {
			return false
		}
		if candidate.Port != port {
			return false
		}
	}
	return true
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

func runCommandOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, bytes.TrimSpace(output))
	}
	return trimmed, nil
}
