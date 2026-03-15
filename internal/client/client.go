package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"hnet/internal/api"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
}

func New(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, addr string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}

	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second, Transport: transport},
		baseURL:    "http://hnetd",
	}
}

func (c *Client) Status() (*api.StatusResponse, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/v1/status", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) ImportSubscription(url string) (*api.StatusResponse, error) {
	body, err := json.Marshal(api.ImportSubscriptionRequest{URL: url})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v1/subscription", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) SelectProxy(name string) (*api.StatusResponse, error) {
	body, err := json.Marshal(api.SelectProxyRequest{Name: name})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v1/proxy", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) TestProxy(name string) (*api.StatusResponse, error) {
	body, err := json.Marshal(api.TestProxyRequest{Name: name})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v1/proxy/test", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) SetSystemProxy(enabled bool) (*api.StatusResponse, error) {
	body, err := json.Marshal(api.SystemProxyRequest{Enabled: enabled})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v1/system-proxy", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) do(req *http.Request) (*api.StatusResponse, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("%s", bytes.TrimSpace(data))
	}

	var status api.StatusResponse
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	return &status, nil
}
