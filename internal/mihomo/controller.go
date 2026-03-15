package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"hnet/internal/api"
)

type controllerProxy struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	ProviderName string   `json:"provider-name"`
	Alive        bool     `json:"alive"`
	All          []string `json:"all"`
	Now          string   `json:"now"`
}

type controllerProxiesResponse struct {
	Proxies map[string]controllerProxy `json:"proxies"`
}

type controllerDelayResponse struct {
	Delay int `json:"delay"`
}

func GetProxyGroup(controllerPort int, secret string, group string) (string, []api.ProxyOption, error) {
	var response controllerProxiesResponse
	if err := doControllerRequest(controllerPort, secret, http.MethodGet, "/proxies", nil, &response); err != nil {
		return "", nil, err
	}

	groupProxy, ok := response.Proxies[group]
	if !ok {
		return "", nil, fmt.Errorf("proxy group %q not found", group)
	}

	options := make([]api.ProxyOption, 0, len(groupProxy.All))
	for _, name := range groupProxy.All {
		proxy := response.Proxies[name]
		options = append(options, api.ProxyOption{
			Name:         name,
			Type:         proxy.Type,
			ProviderName: proxy.ProviderName,
			Alive:        proxy.Alive,
		})
	}

	return groupProxy.Now, options, nil
}

func SelectProxy(controllerPort int, secret string, group string, name string) error {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return err
	}
	path := "/proxies/" + url.PathEscape(group)
	return doControllerRequest(controllerPort, secret, http.MethodPut, path, bytes.NewReader(body), nil)
}

func TestProxyDelay(controllerPort int, secret string, name string, targetURL string, timeout time.Duration) (int, error) {
	path := fmt.Sprintf(
		"/proxies/%s/delay?url=%s&timeout=%s",
		url.PathEscape(name),
		url.QueryEscape(targetURL),
		strconv.Itoa(int(timeout/time.Millisecond)),
	)

	var response controllerDelayResponse
	if err := doControllerRequest(controllerPort, secret, http.MethodGet, path, nil, &response); err != nil {
		return 0, err
	}
	return response.Delay, nil
}

func doControllerRequest(controllerPort int, secret string, method string, path string, body io.Reader, out any) error {
	transport := &http.Transport{Proxy: nil}
	client := &http.Client{Timeout: 5 * time.Second, Transport: transport}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d%s", controllerPort, path)

	req, err := http.NewRequestWithContext(context.Background(), method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		if len(data) == 0 {
			return fmt.Errorf("controller request failed: %s", resp.Status)
		}
		return fmt.Errorf("controller request failed: %s: %s", resp.Status, bytes.TrimSpace(data))
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}
