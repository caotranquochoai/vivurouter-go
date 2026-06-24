package dashboard

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/local/vivurouter-go/internal/store"
)

// secretMask is the placeholder returned in API responses in place of stored
// secrets. When the same value is sent back on save we keep the stored secret.
const secretMask = "********"

// maskSecret returns a masked representation that reveals only a short suffix so
// the operator can recognize which credential is stored without exposing it.
func maskSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return secretMask
	}
	return secretMask + value[len(value)-4:]
}

// isMaskedSecret reports whether an incoming value is a placeholder we emitted,
// meaning the client did not intend to change the stored secret.
func isMaskedSecret(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), secretMask)
}

func redactProxyURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return maskSecret(value)
	}
	if parsed.User != nil {
		parsed.User = url.User(secretMask)
	}
	res := parsed.String()
	res = strings.ReplaceAll(res, "%2A%2A%2A%2A%2A%2A%2A%2A", secretMask)
	return res
}

// sanitizeSettings returns a copy of settings safe to expose over the API:
// passcode and all API keys are masked.
func sanitizeSettings(s store.Settings) store.Settings {
	out := s
	out.AdminPasscode = maskSecret(s.AdminPasscode)
	out.LocalAPIKey = maskSecret(s.LocalAPIKey)
	keys := make([]store.APIKeyPolicy, len(s.APIKeys))
	for i, k := range s.APIKeys {
		k.Key = maskSecret(k.Key)
		keys[i] = k
	}
	out.APIKeys = keys
	return out
}

// mergeSettingsSecrets restores stored secrets when the incoming payload carries
// masked placeholders, so a save from the dashboard never blanks credentials.
func mergeSettingsSecrets(incoming *store.Settings, current store.Settings) {
	if isMaskedSecret(incoming.AdminPasscode) || strings.TrimSpace(incoming.AdminPasscode) == "" {
		incoming.AdminPasscode = current.AdminPasscode
	}
	if isMaskedSecret(incoming.LocalAPIKey) {
		incoming.LocalAPIKey = current.LocalAPIKey
	}
	currentByID := map[string]string{}
	for _, k := range current.APIKeys {
		currentByID[k.ID] = k.Key
	}
	for i := range incoming.APIKeys {
		if isMaskedSecret(incoming.APIKeys[i].Key) {
			if prev, ok := currentByID[incoming.APIKeys[i].ID]; ok {
				incoming.APIKeys[i].Key = prev
			}
		}
	}
}

// sanitizeProvider masks provider credentials for API responses.
func sanitizeProvider(p store.Provider) store.Provider {
	p.APIKey = maskSecret(p.APIKey)
	p.AccessToken = maskSecret(p.AccessToken)
	p.RefreshToken = maskSecret(p.RefreshToken)
	for i := range p.Keys {
		p.Keys[i].Key = maskSecret(p.Keys[i].Key)
	}
	return p
}

func sanitizeProviders(in []store.Provider) []store.Provider {
	out := make([]store.Provider, len(in))
	for i, p := range in {
		out[i] = sanitizeProvider(p)
	}
	return out
}

// mergeProviderSecrets restores stored provider credentials when the incoming
// payload carries masked placeholders.
func mergeProviderSecrets(incoming *store.Provider, current store.Provider) {
	if isMaskedSecret(incoming.APIKey) {
		incoming.APIKey = current.APIKey
	}
	if isMaskedSecret(incoming.AccessToken) {
		incoming.AccessToken = current.AccessToken
	}
	if isMaskedSecret(incoming.RefreshToken) {
		incoming.RefreshToken = current.RefreshToken
	}
	currentKeys := map[string]string{}
	for _, k := range current.Keys {
		currentKeys[k.ID] = k.Key
	}
	for i := range incoming.Keys {
		if isMaskedSecret(incoming.Keys[i].Key) {
			if prev, ok := currentKeys[incoming.Keys[i].ID]; ok {
				incoming.Keys[i].Key = prev
			}
		}
	}
}

func sanitizeProxy(p store.Proxy) store.Proxy {
	p.URL = redactProxyURL(p.URL)
	return p
}

func sanitizeProxies(in []store.Proxy) []store.Proxy {
	out := make([]store.Proxy, len(in))
	for i, p := range in {
		out[i] = sanitizeProxy(p)
	}
	return out
}

func isMaskedProxyURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if isMaskedSecret(value) {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.User == nil {
		return false
	}
	username := parsed.User.Username()
	return username == secretMask || strings.Contains(username, secretMask)
}

func mergeProxySecrets(incoming *store.Proxy, current store.Proxy) {
	if isMaskedProxyURL(incoming.URL) {
		incoming.URL = current.URL
	}
}

// validateOutboundURL blocks SSRF by rejecting non-http(s) schemes and any host
// that resolves to a loopback, private, link-local, or unspecified address.
func validateOutboundURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid target URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("target scheme %q is not allowed", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("target URL has no host")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("cannot resolve target host")
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("target host resolves to a blocked internal address")
		}
	}
	return nil
}

// isBlockedIP reports whether an IP is in a range we must not let the server
// reach on behalf of a request (cloud metadata, localhost, internal networks).
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsPrivate() || ip.IsMulticast() {
		return true
	}
	// Cloud metadata endpoint (also covered by link-local, kept explicit).
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return true
	}
	// Carrier-grade NAT 100.64.0.0/10.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return true
	}
	return false
}
