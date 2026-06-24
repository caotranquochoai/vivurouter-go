package provider

import (
	"testing"
)

func TestNormalizeProxyURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Standard URLs
		{"http://user:pass@host:port", "http://user:pass@host:port"},
		{"socks5://user:pass@host:1080", "socks5://user:pass@host:1080"},
		{"", ""},
		{"   ", ""},

		// Compact host:port:user:pass
		{"171.236.172.228:28727:dBooxZ:rjKmRM", "http://dBooxZ:rjKmRM@171.236.172.228:28727"},
		{"example.com:80:myuser:mypass", "http://myuser:mypass@example.com:80"},

		// Compact with spaces
		{" 171.236.172.228 : 28727 : dBooxZ : rjKmRM ", "http://dBooxZ:rjKmRM@171.236.172.228:28727"},

		// Compact host:port (no credentials)
		{"171.236.172.228:28727", "http://171.236.172.228:28727"},
		{"example.com:8080", "http://example.com:8080"},
		{" example.com : 8080 ", "http://example.com:8080"},

		// Compact with scheme prefix
		{"socks5://171.236.172.228:28727:dBooxZ:rjKmRM", "socks5://dBooxZ:rjKmRM@171.236.172.228:28727"},
		{"http://171.236.172.228:28727:dBooxZ:rjKmRM", "http://dBooxZ:rjKmRM@171.236.172.228:28727"},

		// Invalid compact/standard combinations (should pass through without crashing)
		{"invalid-format", "invalid-format"},
		{"http://invalid-format", "http://invalid-format"},
		{"171.236.172.228:28727:dBooxZ", "171.236.172.228:28727:dBooxZ"}, // 3 parts, not supported
	}

	for _, tc := range tests {
		got := NormalizeProxyURL(tc.input)
		if got != tc.expected {
			t.Errorf("NormalizeProxyURL(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestParseProxyURL(t *testing.T) {
	// Verify that ParseProxyURL successfully parses normalized compact formats
	parsed, err := ParseProxyURL("171.236.172.228:28727:dBooxZ:rjKmRM")
	if err != nil {
		t.Fatalf("ParseProxyURL failed on compact format: %v", err)
	}
	if parsed == nil {
		t.Fatal("expected parsed URL, got nil")
	}
	if parsed.Scheme != "http" {
		t.Errorf("expected scheme http, got %q", parsed.Scheme)
	}
	if parsed.Host != "171.236.172.228:28727" {
		t.Errorf("expected host 171.236.172.228:28727, got %q", parsed.Host)
	}
	if parsed.User == nil {
		t.Fatal("expected user, got nil")
	}
	if parsed.User.Username() != "dBooxZ" {
		t.Errorf("expected username dBooxZ, got %q", parsed.User.Username())
	}
	pass, _ := parsed.User.Password()
	if pass != "rjKmRM" {
		t.Errorf("expected password rjKmRM, got %q", pass)
	}

	// Verify unsupported scheme rejection
	_, err = ParseProxyURL("ftp://171.236.172.228:28727")
	if err == nil {
		t.Fatal("expected error for unsupported scheme, got nil")
	}
}
