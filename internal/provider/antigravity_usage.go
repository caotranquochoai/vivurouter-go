package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

const (
	defaultAntigravityQuotaURL       = "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	defaultAntigravityLoadProjectURL = "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	antigravityIDEVersion            = "2.1.1"
	antigravityIDEUserAgent          = "antigravity/ide/2.1.1 darwin/arm64"
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

// AntigravityUpstreamError preserves the status needed for an OAuth retry without exposing response data.
type AntigravityUpstreamError struct {
	Operation  string
	StatusCode int
}

func (e *AntigravityUpstreamError) Error() string {
	return fmt.Sprintf("Antigravity %s failed: HTTP %d", e.Operation, e.StatusCode)
}

// IsAntigravityAuthError reports whether a quota/subscription request failed with expired authorization.
func IsAntigravityAuthError(err error) bool {
	upstream, ok := err.(*AntigravityUpstreamError)
	return ok && (upstream.StatusCode == http.StatusUnauthorized || upstream.StatusCode == http.StatusForbidden)
}

// FetchQuota reads Antigravity / Cloud Code Assist model availability and quota metadata.
func (e *AntigravityExecutor) FetchQuota(ctx context.Context, provider store.Provider) (AntigravityQuotaReport, error) {
	token := providerBearerToken(provider)
	if token == "" {
		return AntigravityQuotaReport{}, fmt.Errorf("provider %s has no Antigravity access token", provider.ID)
	}

	client, err := clientForProvider(e.Client, provider)
	if err != nil {
		return AntigravityQuotaReport{}, err
	}
	subscription, err := e.fetchAntigravitySubscriptionInfo(ctx, client, token)
	if err != nil {
		return AntigravityQuotaReport{}, err
	}
	projectID := antigravityProjectID(subscription)

	body := map[string]any{}
	if projectID != "" {
		body["project"] = projectID
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return AntigravityQuotaReport{}, err
	}
	quotaURL := strings.TrimSpace(os.Getenv("ANTIGRAVITY_QUOTA_URL"))
	if quotaURL == "" {
		quotaURL = defaultAntigravityQuotaURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, quotaURL, bytes.NewReader(raw))
	if err != nil {
		return AntigravityQuotaReport{}, err
	}
	setAntigravityQuotaHeaders(req, token, true)
	resp, err := client.Do(req)
	if err != nil {
		return AntigravityQuotaReport{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AntigravityQuotaReport{}, &AntigravityUpstreamError{Operation: "quota request", StatusCode: resp.StatusCode}
	}

	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
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
		report.Message = "Antigravity quota endpoint responded, but no valid quota windows were found."
	}
	return report, nil
}

func (e *AntigravityExecutor) fetchAntigravitySubscriptionInfo(ctx context.Context, client *http.Client, token string) (map[string]any, error) {
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
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loadURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	setAntigravityQuotaHeaders(req, token, false)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &AntigravityUpstreamError{Operation: "subscription request", StatusCode: resp.StatusCode}
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func setAntigravityQuotaHeaders(req *http.Request, token string, quota bool) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", antigravityIDEUserAgent)
	if quota {
		req.Header.Set("X-Client-Name", "antigravity")
		req.Header.Set("X-Client-Version", antigravityIDEVersion)
	}
}

func antigravityProjectID(subscription map[string]any) string {
	if subscription == nil {
		return ""
	}
	for _, key := range []string{"cloudaicompanionProject", "project", "projectId"} {
		value := subscription[key]
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		if object := objectValue(value); object != nil {
			if id := firstString(object, "id", "projectId", "name"); id != "" {
				return id
			}
		}
	}
	return ""
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

// ParseAntigravityQuotaPayload normalizes Cloud Code Assist model quota response shapes.
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

	seenModels := map[string]bool{}
	seenQuotas := map[string]bool{}
	for _, model := range antigravityModelEntries(data) {
		id := firstString(model, "id", "name", "model", "modelId")
		displayName := firstString(model, "displayName", "display_name")
		if !includeAntigravityQuotaModel(id, displayName, boolValue(model["isInternal"])) {
			continue
		}
		if id != "" && !seenModels[id] {
			seenModels[id] = true
			report.Models = append(report.Models, id)
		}
		quota := firstObject(model["quotaInfo"], model["quota"], model["rateLimit"], model["rate_limit"], model["usage"])
		if quota == nil || id == "" || seenQuotas[id] {
			continue
		}
		formatted, ok := formatAntigravityQuota(id, displayName, quota)
		if !ok {
			continue
		}
		if formatted.Name == "" {
			formatted.Name = id
		}
		seenQuotas[id] = true
		report.Quotas = append(report.Quotas, formatted)
	}
	sort.Strings(report.Models)
	sort.Slice(report.Quotas, func(i, j int) bool { return report.Quotas[i].Key < report.Quotas[j].Key })
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

func includeAntigravityQuotaModel(id, displayName string, internal bool) bool {
	if internal || strings.TrimSpace(id) == "" {
		return false
	}
	upperID := strings.ToUpper(strings.TrimSpace(id))
	if strings.HasPrefix(upperID, "MODEL_PLACEHOLDER_") {
		return false
	}
	if strings.HasPrefix(upperID, "MODEL_CHAT_") && strings.TrimSpace(displayName) == "" {
		return false
	}
	return true
}

func formatAntigravityQuota(key, name string, quota map[string]any) (CodexQuota, bool) {
	fraction, ok := finiteValue(quota["remainingFraction"])
	if !ok || math.IsNaN(fraction) || math.IsInf(fraction, 0) {
		return CodexQuota{}, false
	}
	fraction = math.Max(0, math.Min(1, fraction))
	remaining := math.Round(fraction * 100)
	return CodexQuota{
		Key:       key,
		Name:      name,
		Used:      100 - remaining,
		Total:     100,
		Remaining: remaining,
		ResetAt:   parseAntigravityResetTime(firstNonNil(quota["reset_at"], quota["resetAt"], quota["reset_time"], quota["resetsAt"], quota["resetTime"])),
		Unlimited: boolValue(quota["unlimited"]),
	}, true
}

func parseAntigravityResetTime(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return ""
		}
		if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
		number, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return ""
		}
		return antigravityUnixTime(number)
	}
	switch number := value.(type) {
	case int:
		return antigravityUnixTime(float64(number))
	case int64:
		return antigravityUnixTime(float64(number))
	case int32:
		return antigravityUnixTime(float64(number))
	case uint:
		return antigravityUnixTime(float64(number))
	case uint64:
		return antigravityUnixTime(float64(number))
	case uint32:
		return antigravityUnixTime(float64(number))
	}
	number, ok := finiteValue(value)
	if !ok {
		return ""
	}
	return antigravityUnixTime(number)
}

func antigravityUnixTime(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return ""
	}
	seconds := value
	if value >= 1e12 {
		seconds = value / 1000
	}
	whole, fraction := math.Modf(seconds)
	parsed := time.Unix(int64(whole), int64(fraction*float64(time.Second))).UTC()
	if parsed.Year() < 2000 || parsed.Year() > 3000 {
		return ""
	}
	return parsed.Format(time.RFC3339Nano)
}
