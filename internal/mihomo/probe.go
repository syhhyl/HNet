package mihomo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultDelayTestURL = "https://www.gstatic.com/generate_204"
	defaultDelayTimeout = 5 * time.Second
	defaultSpeedTestURL = "https://speed.cloudflare.com/__down?bytes=262144"
	defaultSpeedTimeout = 12 * time.Second
	defaultSpeedBytes   = 256 * 1024
)

func DefaultDelayTestURL() string {
	return defaultDelayTestURL
}

func DefaultDelayTimeout() time.Duration {
	return defaultDelayTimeout
}

func MeasureDownloadSpeedViaMixedPort(mixedPort int) (int64, error) {
	proxyURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", mixedPort))
	if err != nil {
		return 0, err
	}

	transport := &http.Transport{
		Proxy:              http.ProxyURL(proxyURL),
		DisableCompression: true,
	}
	client := &http.Client{Transport: transport}

	ctx, cancel := context.WithTimeout(context.Background(), defaultSpeedTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, defaultSpeedTestURL, nil)
	if err != nil {
		return 0, err
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return 0, fmt.Errorf("speed test failed: %s", resp.Status)
	}

	bytesRead, copyErr := io.CopyN(io.Discard, resp.Body, defaultSpeedBytes)
	if copyErr != nil && copyErr != io.EOF {
		return 0, copyErr
	}

	elapsed := time.Since(start)
	if elapsed <= 0 || bytesRead <= 0 {
		return 0, fmt.Errorf("speed test produced no data")
	}

	return int64(float64(bytesRead) / elapsed.Seconds()), nil
}
