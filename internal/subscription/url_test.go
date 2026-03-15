package subscription

import "testing"

func TestNormalizeURLAddsHTTPS(t *testing.T) {
	normalized, err := NormalizeURL("example.com/api/v1/client/subscribe?token=abc")
	if err != nil {
		t.Fatalf("NormalizeURL() error = %v", err)
	}
	if normalized != "https://example.com/api/v1/client/subscribe?token=abc" {
		t.Fatalf("unexpected normalized URL %q", normalized)
	}
}

func TestNormalizeURLRejectsNonHTTP(t *testing.T) {
	_, err := NormalizeURL("ss://example")
	if err == nil {
		t.Fatal("expected scheme validation error")
	}
}
