package macos

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func SetMixedProxy(port int) error {
	services, err := enabledServices()
	if err != nil {
		return err
	}
	for _, service := range services {
		if err := runNetworksetup("-setwebproxy", service, "127.0.0.1", strconv.Itoa(port), "off"); err != nil {
			return err
		}
		if err := runNetworksetup("-setsecurewebproxy", service, "127.0.0.1", strconv.Itoa(port), "off"); err != nil {
			return err
		}
		if err := runNetworksetup("-setsocksfirewallproxy", service, "127.0.0.1", strconv.Itoa(port), "off"); err != nil {
			return err
		}
		if err := runNetworksetup("-setwebproxystate", service, "on"); err != nil {
			return err
		}
		if err := runNetworksetup("-setsecurewebproxystate", service, "on"); err != nil {
			return err
		}
		if err := runNetworksetup("-setsocksfirewallproxystate", service, "on"); err != nil {
			return err
		}
	}
	return nil
}

func DisableSystemProxy() error {
	services, err := enabledServices()
	if err != nil {
		return err
	}
	for _, service := range services {
		if err := runNetworksetup("-setwebproxystate", service, "off"); err != nil {
			return err
		}
		if err := runNetworksetup("-setsecurewebproxystate", service, "off"); err != nil {
			return err
		}
		if err := runNetworksetup("-setsocksfirewallproxystate", service, "off"); err != nil {
			return err
		}
	}
	return nil
}

func enabledServices() ([]string, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("system proxy is only supported on macOS")
	}

	cmd := exec.Command("networksetup", "-listallnetworkservices")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list network services: %w: %s", err, bytes.TrimSpace(output))
	}

	lines := strings.Split(string(output), "\n")
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

func runNetworksetup(args ...string) error {
	cmd := exec.Command("networksetup", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("networksetup %s: %w: %s", strings.Join(args, " "), err, bytes.TrimSpace(output))
	}
	return nil
}
