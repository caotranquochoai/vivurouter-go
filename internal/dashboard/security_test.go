package dashboard

import (
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func TestSanitizeSettingsMasksSecrets(t *testing.T) {
	in := store.Settings{
		AdminPasscode: "supersecretpass",
		LocalAPIKey:   "sk-local-abcdef1234",
		APIKeys:       []store.APIKeyPolicy{{ID: "k1", Key: "sk-team-zzzz9999"}},
	}
	out := sanitizeSettings(in)
	if out.AdminPasscode == in.AdminPasscode || !isMaskedSecret(out.AdminPasscode) {
		t.Fatalf("admin passcode not masked: %q", out.AdminPasscode)
	}
	if out.LocalAPIKey == in.LocalAPIKey || !isMaskedSecret(out.LocalAPIKey) {
		t.Fatalf("local api key not masked: %q", out.LocalAPIKey)
	}
	if out.APIKeys[0].Key == in.APIKeys[0].Key || !isMaskedSecret(out.APIKeys[0].Key) {
		t.Fatalf("api key not masked: %q", out.APIKeys[0].Key)
	}
	// The original must not be mutated.
	if in.AdminPasscode != "supersecretpass" {
		t.Fatalf("input mutated: %q", in.AdminPasscode)
	}
}

func TestMergeSettingsSecretsPreservesOnMask(t *testing.T) {
	current := store.Settings{
		AdminPasscode: "realpass",
		LocalAPIKey:   "sk-local-real",
		APIKeys:       []store.APIKeyPolicy{{ID: "k1", Key: "sk-real-key"}},
	}
	incoming := sanitizeSettings(current) // client echoes back masked values
	incoming.DashboardMessage = "changed"
	mergeSettingsSecrets(&incoming, current)
	if incoming.AdminPasscode != "realpass" {
		t.Fatalf("passcode not preserved: %q", incoming.AdminPasscode)
	}
	if incoming.LocalAPIKey != "sk-local-real" {
		t.Fatalf("local key not preserved: %q", incoming.LocalAPIKey)
	}
	if incoming.APIKeys[0].Key != "sk-real-key" {
		t.Fatalf("api key not preserved: %q", incoming.APIKeys[0].Key)
	}
}

func TestMergeSettingsSecretsAcceptsNewValue(t *testing.T) {
	current := store.Settings{AdminPasscode: "oldpass", LocalAPIKey: "old"}
	incoming := store.Settings{AdminPasscode: "brandnewpass", LocalAPIKey: "newkey"}
	mergeSettingsSecrets(&incoming, current)
	if incoming.AdminPasscode != "brandnewpass" {
		t.Fatalf("new passcode lost: %q", incoming.AdminPasscode)
	}
	if incoming.LocalAPIKey != "newkey" {
		t.Fatalf("new local key lost: %q", incoming.LocalAPIKey)
	}
}

func TestMergeProviderSecretsPreservesOnMask(t *testing.T) {
	current := store.Provider{ID: "p1", APIKey: "real-api", AccessToken: "real-access", RefreshToken: "real-refresh"}
	incoming := sanitizeProvider(current)
	incoming.Name = "renamed"
	mergeProviderSecrets(&incoming, current)
	if incoming.APIKey != "real-api" || incoming.AccessToken != "real-access" || incoming.RefreshToken != "real-refresh" {
		t.Fatalf("provider secrets not preserved: %+v", incoming)
	}
}

func TestValidateOutboundURLBlocksInternal(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1/admin",
		"http://localhost:8080/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.5/",
		"http://192.168.1.1/",
		"http://[::1]/",
		"ftp://example.com/",
		"file:///etc/passwd",
	}
	for _, raw := range blocked {
		if err := validateOutboundURL(raw); err == nil {
			t.Errorf("expected %q to be blocked", raw)
		}
	}
}

func TestValidateOutboundURLAllowsPublic(t *testing.T) {
	// Uses a literal public IP to avoid DNS in tests.
	if err := validateOutboundURL("https://8.8.8.8/v1/models"); err != nil {
		t.Fatalf("expected public IP allowed, got %v", err)
	}
}
