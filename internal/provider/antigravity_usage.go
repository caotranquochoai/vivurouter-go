package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

const (
	defaultAntigravityQuotaURL       = "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	defaultAntigravityLoadProjectURL = "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
)

// AntigravityQuotaReport is the normalized quota/model availability view used by the dashboard.
type AntigravityQuotaReport struct {
	ProviderID string       `json:"provider_id"`
	Plan       string       `json:"plan"`
	Quotas     []CodexQuota `json:"quotas"`
	Models     []string     `json:"models,omitempty"`
	Message    string       `json:"message,omitempty"`
	FetchedAt  time.Time    `json:"fetched_at"`
}

// FetchQuota reads Antigravity / Cloud Code Assist model availability and quota metadata.
func (e *AntigravityExecutor) FetchQuota(ctx context.Context, provider store.Provider) (AntigravityQuotaReport, error) {
	token := providerBearerToken(provider)
	if token == "" {
		return AntigravityQuotaReport{}, fmt.Errorf("provider %s has no Antigravity access token", provider.ID)
	}

	quotaURL := strings.TrimSpace(os.Getenv("ANTIGRAVITY_QUOTA_URL"))
	if quotaURL == "" {
		quotaURL = defaultAntigravityQuotaURL
	}
	client, err := clientForProvider(e.Client, provider)
	if err != nil {
		return AntigravityQuotaReport{}, err
	}
	subscription := e.fetchAntigravitySubscriptionInfo(ctx, client, provider, token)
	projectID := strings.TrimSpace(firstString(subscription, "cloudaicompanionProject", "project", "projectId"))

	body := map[string]any{}
	if projectID != "" {
		body["project"] = projectID
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return AntigravityQuotaReport{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, quotaURL, bytes.NewReader(raw))
	if err != nil {
		return AntigravityQuotaReport{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", antigravityUserAgent())
	req.Header.Set("X-Client-Name", "antigravity")
	req.Header.Set("X-Client-Version", "1.107.0")
	req.Header.Set(antigravityRequestSourceHeader, antigravityRequestSource)

	resp, err := client.Do(req)
	if err != nil {
		return AntigravityQuotaReport{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AntigravityQuotaReport{
			ProviderID: provider.ID,
			Message:    fmt.Sprintf("Antigravity connected. Quota API temporarily unavailable (%d).", resp.StatusCode),
			FetchedAt:  time.Now().UTC(),
		}, nil
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return AntigravityQuotaReport{}, err
	}
	report := ParseAntigravityQuotaPayload(payload)
	if plan := antigravitySubscriptionPlan(subscription); plan != "" && report.Plan == "unknown" {
		report.Plan = plan
	}
	report.ProviderID = provider.ID
	report.FetchedAt = time.Now().UTC()
	if report.Message == "" && len(report.Quotas) == 0 && len(report.Models) > 0 {
		report.Message = fmt.Sprintf("%d Antigravity models available. Quota windows were not returned by this API response.", len(report.Models))
	}
	if report.Message == "" && len(report.Quotas) == 0 {
		report.Message = "Antigravity quota endpoint responded, but no quota windows were found."
	}
	return report, nil
}

func (e *AntigravityExecutor) fetchAntigravitySubscriptionInfo(ctx context.Context, client *http.Client, provider store.Provider, token string) map[string]any {
	loadURL := strings.TrimSpace(os.Getenv("ANTIGRAVITY_LOAD_PROJECT_URL"))
	if loadURL == "" {
		loadURL = defaultAntigravityLoadProjectURL
	}
	body := map[string]any{
		"metadata": map[string]any{"ideType": 1, "platform": 1, "pluginType": 2},
		"mode":     1,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loadURL, bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", antigravityUserAgent())
	req.Header.Set(antigravityRequestSourceHeader, antigravityRequestSource)
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil
	}
	return payload
}

func antigravitySubscriptionPlan(subscription map[string]any) string {
	if subscription == nil {
		return ""
	}
	if tier := objectValue(subscription["currentTier"]); tier != nil {
		if name := firstString(tier, "name", "id", "tierId"); name != "" {
			return name
		}
	}
	return firstString(subscription, "tierId", "tier", "plan")
}

// ParseAntigravityQuotaPayload normalizes known and likely Cloud Code Assist quota response shapes.
func ParseAntigravityQuotaPayload(data map[string]any) AntigravityQuotaReport {
	report := AntigravityQuotaReport{Plan: firstString(data, "tierId", "tier", "plan", "plan_type")}
	if report.Plan == "" {
		if user := objectValue(data["user"]); user != nil {
			report.Plan = firstString(user, "tierId", "tier", "plan")
		}
	}
	if report.Plan == "" {
		report.Plan = "unknown"
	}

	models := antigravityModelEntries(data)
	for _, model := range models {
		id := firstString(model, "id", "name", "model", "modelId")
		if id != "" {
			report.Models = append(report.Models, id)
		}
		if quota := firstObject(model["quotaInfo"], model["quota"], model["rateLimit"], model["rate_limit"], model["usage"]); quota != nil {
			name := id
			if name == "" {
				name = "Model quota"
			}
			report.Quotas = append(report.Quotas, formatAntigravityQuota(id, name, quota))
		}
	}

	for _, key := range []string{"quota", "quotas", "rateLimit", "rate_limit", "usage", "limits"} {
		if quota := objectValue(data[key]); quota != nil {
			report.Quotas = append(report.Quotas, formatAntigravityQuota(key, quotaDisplayName(key), quota))
			continue
		}
		if items, ok := data[key].([]any); ok {
			for i, item := range items {
				quota := objectValue(item)
				if quota == nil {
					continue
				}
				name := firstString(quota, "name", "displayName", "id", "key", "limitName")
				if name == "" {
					name = fmt.Sprintf("Quota %d", i+1)
				}
				report.Quotas = append(report.Quotas, formatAntigravityQuota(name, name, quota))
			}
		}
	}
	return report
}

func antigravityModelEntries(data map[string]any) []map[string]any {
	keys := []string{"models", "availableModels", "available_models", "modelMetadata", "model_metadata"}
	out := []map[string]any{}
	for _, key := range keys {
		if items, ok := data[key].([]any); ok {
			for _, item := range items {
				if object := objectValue(item); object != nil {
					out = append(out, object)
				}
			}
			continue
		}
		if byID := objectValue(data[key]); byID != nil {
			for id, item := range byID {
				object := objectValue(item)
				if object == nil {
					continue
				}
				if firstString(object, "id", "name", "model", "modelId") == "" {
					object["id"] = id
				}
				out = append(out, object)
			}
		}
	}
	return out
}

func formatAntigravityQuota(key string, name string, quota map[string]any) CodexQuota {
	used, hasUsed := finiteValue(quota["used"], quota["usage"], quota["consumed"], quota["current"], quota["usedCount"], quota["consumedCount"])
	total, hasTotal := finiteValue(quota["total"], quota["limit"], quota["maximum"], quota["max"], quota["limitCount"])
	remaining, hasRemaining := finiteValue(quota["remaining"], quota["available"], quota["remainingCount"])
	if remainingFraction, ok := finiteValue(quota["remainingFraction"]); ok && !hasRemaining {
		total = 100
		remaining = remainingFraction * 100
		hasTotal, hasRemaining = true, true
	}
	percentUsed, hasPercent := finiteValue(quota["used_percent"], quota["usage_percent"], quota["percentUsed"], quota["usedPercentage"])

	if hasPercent && !hasTotal {
		used = percentUsed
		total = 100
		remaining = 100 - used
		hasUsed, hasTotal, hasRemaining = true, true, true
	}
	if !hasRemaining && hasUsed && hasTotal {
		remaining = total - used
		hasRemaining = true
	}
	if !hasUsed && hasRemaining && hasTotal {
		used = total - remaining
		hasUsed = true
	}
	if !hasTotal {
		total = 100
	}
	if !hasUsed {
		used = 0
	}
	if !hasRemaining {
		remaining = total - used
	}
	if key == "" {
		key = strings.ToLower(strings.ReplaceAll(name, " ", "_"))
	}
	return CodexQuota{
		Key:       key,
		Name:      name,
		Used:      used,
		Total:     total,
		Remaining: remaining,
		ResetAt:   parseResetTimeAny(firstNonNil(quota["reset_at"], quota["resetAt"], quota["reset_time"], quota["resetsAt"], quota["resetTime"])),
		Unlimited: boolValue(quota["unlimited"]),
	}
}

func quotaDisplayName(key string) string {
	switch key {
	case "rateLimit", "rate_limit":
		return "Rate limit"
	case "usage":
		return "Usage"
	case "limits":
		return "Limits"
	default:
		return "Quota"
	}
}
