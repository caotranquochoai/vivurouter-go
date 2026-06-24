package dashboard

import (
	"time"

	"github.com/local/vivurouter-go/internal/config"
	"github.com/local/vivurouter-go/internal/observe"
	"github.com/local/vivurouter-go/internal/store"
)

type codexQuotaSeed struct {
	ProviderID string `json:"provider_id"`
}

type pageData struct {
	Title               string
	Lang                string
	T                   map[string]string
	Now                 time.Time
	Config              config.Config
	Settings            store.Settings
	Providers           []store.Provider
	Proxies             []store.Proxy
	ProxyCards          []proxyCard
	ProxySummary        proxySummary
	ProviderSummary     providerSummary
	ProviderGroups      []providerGroup
	SelectedProvider    *providerCard
	Requests            []store.RequestLog
	RequestViews        []requestLogView
	Usage               usageSummary
	UsageTable          usageTableData
	ProviderUsagePeriod string
	CodexOAuth          any
	AntigravityOAuth    any
	CodexQuota          codexQuotaSeed
	Metrics             observe.MetricsSnapshot
	Cooldowns           []observe.CooldownStatus
	EditProvider        *store.Provider
	EditProxy           *store.Proxy
	Error               string
	Saved               bool
	APIKeysText         string
	ModelPricesText     string
	PricingGroups       []pricingRuleGroup
	CombosText          string
	PromptRoutersText   string
	FusionsText         string
	AvailableModels     []comboModelOption
	RequestTotal        int
	RequestShown        int
	RequestPage         int
	RequestLimit        int
	RequestPrevPage     int
	RequestNextPage     int
	RequestHasPrev      bool
	RequestHasNext      bool
	UptimeLabel         string
	CostNote            string
	Budget              BudgetStatus
	UsageSeries         UsageSeries
}

type requestLogView struct {
	store.RequestLog
	Fusion *fusionTraceView
}

type fusionTraceView struct {
	Experts          []fusionExpertTraceView
	Synthesizer      *fusionStageTraceView
	Reviewer         *fusionStageTraceView
	SynthesisPreview string
	FinalPreview     string
}

type fusionStageTraceView struct {
	Name           string
	Target         string
	Success        bool
	Error          string
	ContentPreview string
	DurationMS     int64
	PromptTokens   int
	OutputTokens   int
}

type fusionExpertTraceView struct {
	Name           string
	Target         string
	Role           string
	Success        bool
	Error          string
	ContentPreview string
	DurationMS     int64
	PromptTokens   int
	OutputTokens   int
}

func (v fusionTraceView) HasDetails() bool {
	return len(v.Experts) > 0 || v.Synthesizer != nil || v.Reviewer != nil || v.SynthesisPreview != "" || v.FinalPreview != ""
}

type pricingRuleGroup struct {
	ProviderID       string
	Model            string
	PairsText        string
	Count            int
	InputPer1M       float64
	OutputPer1M      float64
	CachedInputPer1M float64
	ReasoningPer1M   float64
	ContextLength    int
	RPM              int
	TPM              int
	DailyRequests    int
	DailyTokens      int
}

type providerSummary struct {
	Total           int
	Enabled         int
	Disabled        int
	WithCredentials int
	OAuthConnected  int
	InCooldown      int
}

type providerGroup struct {
	Key      string
	Title    string
	Subtitle string
	Cards    []providerCard
}

type providerCapabilityBadge struct {
	Label string
	Class string
	Title string
}

type providerCard struct {
	ID                string
	Type              string
	Name              string
	BaseURL           string
	IconText          string
	ProxyURL          string
	ProxyID           string
	ProxyName         string
	ProxyEnabled      bool
	ProxyLabel        string
	ProxyClass        string
	AuthLabel         string
	AuthClass         string
	StatusLabel       string
	StatusClass       string
	Enabled           bool
	IsDefault         bool
	HasCredential     bool
	SecretLabel       string
	SecretClass       string
	Models            []string
	VisibleModels     []providerModelUsage
	HiddenModels      []providerModelUsage
	HiddenModelCount  int
	ModelCount        int
	RequestCount      int
	SuccessCount      int
	ErrorCount        int
	UsageTokens       int
	UsageRequests     int
	UsagePeriodLabel  string
	LastStatus        string
	LastError         string
	LastSeen          string
	Cooldown          bool
	CooldownRemaining string
	CooldownReason    string
	DefaultWarning    string
	DefaultLabel      string
	DefaultTitle      string
	SecretTitle       string
	KeyCount          int
	KeyStrategy       string
	StickyLimit       int
	Keys              []store.ProviderKey
	CapabilityBadges  []providerCapabilityBadge
}

type providerModelUsage struct {
	Name      string
	Tokens    int
	Requests  int
	HasUsage  bool
	UsageRank int
}

type comboModelOption struct {
	ProviderID string
	Provider   string
	Model      string
	Value      string
}

type UsageCounter struct {
	Requests           int     `json:"requests"`
	PromptTokens       int     `json:"prompt_tokens"`
	CompletionTokens   int     `json:"completion_tokens"`
	TotalTokens        int     `json:"total_tokens"`
	CachedTokens       int     `json:"cached_tokens"`
	ReasoningTokens    int     `json:"reasoning_tokens"`
	Estimated          int     `json:"estimated"`
	UpstreamSaved      int     `json:"upstream_saved"`
	DebugSaved         int     `json:"debug_saved"`
	OptimizeDurationMS int64   `json:"optimize_duration_ms"`
	ProviderDurationMS int64   `json:"provider_duration_ms"`
	DebugLogDurationMS int64   `json:"debug_log_duration_ms"`
	CostUSD            float64 `json:"cost_usd"`
}

type usageSummary struct {
	UsageCounter
	ByProvider map[string]UsageCounter `json:"by_provider"`
	ByModel    map[string]UsageCounter `json:"by_model"`
	ByEndpoint map[string]UsageCounter `json:"by_endpoint"`
	ByAPIKey   map[string]UsageCounter `json:"by_api_key"`
}

type usageTableRow struct {
	Name             string          `json:"name"`
	Provider         string          `json:"provider"`
	Requests         int             `json:"requests"`
	LastUsedSecsAgo  int             `json:"last_used_secs_ago"`
	LastUsedLabel    string          `json:"last_used_label"`
	InputCost        float64         `json:"input_cost"`
	OutputCost       float64         `json:"output_cost"`
	TotalCost        float64         `json:"total_cost"`
	PromptTokens     int             `json:"prompt_tokens"`
	CompletionTokens int             `json:"completion_tokens"`
	TotalTokens      int             `json:"total_tokens"`
	Children         []usageTableRow `json:"children,omitempty"`
}

type usageTableData struct {
	ByModel    []usageTableRow `json:"by_model"`
	ByAccount  []usageTableRow `json:"by_account"`
	ByAPIKey   []usageTableRow `json:"by_api_key"`
	ByEndpoint []usageTableRow `json:"by_endpoint"`
}

type proxySummary struct {
	Total    int
	Enabled  int
	Disabled int
}

type proxyCard struct {
	ID        string
	Name      string
	URL       string
	Redacted  string
	Enabled   bool
	CreatedAt string
	UpdatedAt string
	UseCount  int
}
