package codexoauth

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var proxyClientCache sync.Map

func clientForProxy(defaultClient *http.Client, proxyURL string) (*http.Client, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return defaultClient, nil
	}
	if cached, ok := proxyClientCache.Load(proxyURL); ok {
		if client, ok := cached.(*http.Client); ok {
			return client, nil
		}
	}
	parsed, err := ParseProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyURL(parsed),
			MaxIdleConns:          64,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
	cached, _ := proxyClientCache.LoadOrStore(proxyURL, client)
	if client, ok := cached.(*http.Client); ok {
		return client, nil
	}
	return nil, fmt.Errorf("proxy client cache returned unexpected value")
}

func normalizeCompactProxyURL(scheme, raw string) (string, bool) {
	parts := strings.Split(raw, ":")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	switch len(parts) {
	case 2:
		if parts[0] == "" || parts[1] == "" {
			return "", false
		}
		return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(parts[0], parts[1])}).String(), true
	case 4:
		if parts[0] == "" || parts[1] == "" || parts[2] == "" || parts[3] == "" {
			return "", false
		}
		return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(parts[0], parts[1]), User: url.UserPassword(parts[2], parts[3])}).String(), true
	default:
		return "", false
	}
}

func NormalizeProxyURL(proxyURL string) string {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return ""
	}
	lower := strings.ToLower(proxyURL)
	for _, scheme := range []string{"http", "https", "socks5", "socks5h"} {
		prefix := scheme + "://"
		if strings.HasPrefix(lower, prefix) {
			if normalized, ok := normalizeCompactProxyURL(scheme, proxyURL[len(prefix):]); ok {
				return normalized
			}
			return proxyURL
		}
	}
	if normalized, ok := normalizeCompactProxyURL("http", proxyURL); ok {
		return normalized
	}
	return proxyURL
}

func ParseProxyURL(proxyURL string) (*url.URL, error) {
	proxyURL = NormalizeProxyURL(proxyURL)
	if proxyURL == "" {
		return nil, nil
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("proxy URL không hợp lệ: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("proxy URL phải có scheme và host, ví dụ http://user:pass@host:port")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return parsed, nil
	default:
		return nil, fmt.Errorf("proxy scheme %q chưa được hỗ trợ", parsed.Scheme)
	}
}
