package store

import (
	"math"
	"os"
	"strings"
	"time"
)

const (
	ProviderOpenAICompatible = "openai-compatible"
	ProviderCodex            = "codex"
	ProviderMimoFree         = "mimo-free"
	ProviderOpenCodeFree     = "opencode"
	ProviderAntigravity      = "antigravity"

	ProviderKeyStrategyFillFirst  = "fill-first"
	ProviderKeyStrategyRoundRobin = "round-robin"

	PromptRouterCompressionPreview = "preview"
	PromptRouterCompressionFull    = "full"
	PromptRouterCompressionPerType = "per-type"

	FusionModeParallel   = "parallel"
	FusionModeSequential = "sequential"

	GuardrailResponseBuffered = "buffered"
	GuardrailResponseStream   = "buffered-stream"
)

// Settings stores local gateway settings.
type Settings struct {
	BindHost                        string           `json:"bind_host,omitempty"`
	BindPort                        string           `json:"bind_port,omitempty"`
	RequireAPIKey                   bool             `json:"require_api_key"`
	LocalAPIKey                     string           `json:"local_api_key"`
	DefaultProvider                 string           `json:"default_provider"`
	DefaultCodexID                  string           `json:"default_codex_id"`
	KeepRequestLogs                 int              `json:"keep_request_logs"`
	ObservabilityEnabled            bool             `json:"observability_enabled"`
	SaveRawPrompt                   bool             `json:"save_raw_prompt"`
	SaveRawToolResult               bool             `json:"save_raw_tool_result"`
	SaveRawResponse                 bool             `json:"save_raw_response"`
	MaskDebugSecrets                bool             `json:"mask_debug_secrets"`
	CompactDebugPayloads            bool             `json:"compact_debug_payloads"`
	MaxDebugPayloadBytes            int              `json:"max_debug_payload_bytes"`
	TokenOptimizeToolResults        bool             `json:"token_optimize_tool_results"`
	TokenOptimizeSystem             bool             `json:"token_optimize_system"`
	TokenOptimizeDeveloper          bool             `json:"token_optimize_developer"`
	TokenOptimizeText               bool             `json:"token_optimize_text"`
	TokenOptimizeToolSchemas        bool             `json:"token_optimize_tool_schemas"`
	TokenOptimizeToolCalls          bool             `json:"token_optimize_tool_calls"`
	TokenOptimizeMinChars           int              `json:"token_optimize_min_chars"`
	TokenOptimizeMaxChars           int              `json:"token_optimize_max_chars"`
	PromptRouterCompressionMode     string           `json:"prompt_router_compression_mode"`
	PromptRouterCompressSystem      bool             `json:"prompt_router_compress_system"`
	PromptRouterCompressDeveloper   bool             `json:"prompt_router_compress_developer"`
	PromptRouterCompressMessages    bool             `json:"prompt_router_compress_messages"`
	PromptRouterCompressToolResults bool             `json:"prompt_router_compress_tool_results"`
	PromptRouterCompressToolSchemas bool             `json:"prompt_router_compress_tool_schemas"`
	PromptRouterCompressImages      bool             `json:"prompt_router_compress_images"`
	RTKEnabled                      bool             `json:"rtk_enabled"`
	RTKPath                         string           `json:"rtk_path"`
	DashboardMessage                string           `json:"dashboard_message"`
	AdminSecurityEnabled            bool             `json:"admin_security_enabled"`
	AdminPasscode                   string           `json:"admin_passcode"`
	APIKeys                         []APIKeyPolicy   `json:"api_keys"`
	ModelPrices                     []ModelPriceRule `json:"model_prices"`
	Combos                          []Combo          `json:"combos"`
	PromptRouters                   []PromptRouter   `json:"prompt_routers"`
	Fusions                         []Fusion         `json:"fusions"`
	Guardrails                      []Guardrail      `json:"guardrails"`
	DailyBudgetUSD                  float64          `json:"daily_budget_usd"`
	MonthlyBudgetUSD                float64          `json:"monthly_budget_usd"`
	BudgetAlertPct                  int              `json:"budget_alert_pct"`
}

// APIKeyPolicy controls one local gateway key with optional quota and model restrictions.
type APIKeyPolicy struct {
	ID            string   `json:"id"`
	Key           string   `json:"key"`
	Enabled       bool     `json:"enabled"`
	AllowedModels []string `json:"allowed_models"`
	MaxRequests   int      `json:"max_requests"`
	MaxTokens     int      `json:"max_tokens"`
	MaxCostUSD    float64  `json:"max_cost_usd"`
	MaxRPM        int      `json:"max_rpm"`
	MaxConcurrent int      `json:"max_concurrent"`
	UsedRequests  int      `json:"used_requests"`
	UsedTokens    int      `json:"used_tokens"`
	UsedCostUSD   float64  `json:"used_cost_usd"`
}

type APIKeyUsageDelta struct {
	Requests int
	Tokens   int
	CostUSD  float64
}

// ModelPriceRule overrides per-provider/model prices in USD per 1M tokens, context metadata and optional rate limits.
type ModelPriceRule struct {
	ProviderID       string  `json:"provider_id"`
	Model            string  `json:"model"`
	InputPer1M       float64 `json:"input_per_1m"`
	OutputPer1M      float64 `json:"output_per_1m"`
	CachedInputPer1M float64 `json:"cached_input_per_1m"`
	ReasoningPer1M   float64 `json:"reasoning_per_1m"`
	ContextLength    int     `json:"context_length"`
	RPM              int     `json:"rpm"`
	TPM              int     `json:"tpm"`
	DailyRequests    int     `json:"daily_requests"`
	DailyTokens      int     `json:"daily_tokens"`
}

// Combo describes a virtual model backed by multiple concrete model IDs.
type Combo struct {
	Name          string   `json:"name"`
	Models        []string `json:"models"`
	Strategy      string   `json:"strategy"`
	StickyLimit   int      `json:"sticky_limit"`
	Enabled       bool     `json:"enabled"`
	ContextLength int      `json:"context_length"`
	Description   string   `json:"description,omitempty"`
}

// PromptRoute maps a classifier role to a concrete model or combo target.
type PromptRoute struct {
	Role              string `json:"role"`
	Complexity        string `json:"complexity,omitempty"`
	Risk              string `json:"risk,omitempty"`
	Target            string `json:"target"`
	InjectInstruction bool   `json:"inject_instruction"`
	Instruction       string `json:"instruction,omitempty"`
}

// PromptRouter describes a virtual model that classifies raw prompts before routing.
type PromptRouter struct {
	Name                     string        `json:"name"`
	Enabled                  bool          `json:"enabled"`
	ClassifierModel          string        `json:"classifier_model"`
	FallbackTarget           string        `json:"fallback_target"`
	FallbackRole             string        `json:"fallback_role"`
	Routes                   []PromptRoute `json:"routes"`
	UseRawPrompt             bool          `json:"use_raw_prompt,omitempty"`
	ClassifierPromptTemplate string        `json:"classifier_prompt_template,omitempty"`
	Description              string        `json:"description,omitempty"`
}

// FusionExpert describes one expert participant in a Fusion virtual model.
type FusionExpert struct {
	Name           string `json:"name"`
	Target         string `json:"target"`
	Role           string `json:"role"`
	PromptTemplate string `json:"prompt_template,omitempty"`
	Enabled        bool   `json:"enabled"`
	Weight         int    `json:"weight,omitempty"`
}

// Fusion describes a virtual model that fans a request out to experts, then synthesizes and reviews the result.
type Fusion struct {
	Name                    string         `json:"name"`
	Description             string         `json:"description,omitempty"`
	Enabled                 bool           `json:"enabled"`
	Experts                 []FusionExpert `json:"experts"`
	Mode                    string         `json:"mode"`
	TimeoutMS               int            `json:"timeout_ms"`
	MinSuccessfulExperts    int            `json:"min_successful_experts"`
	MaxOutputTokens         int            `json:"max_output_tokens"`
	SynthesizerTarget       string         `json:"synthesizer_target"`
	ReviewerTarget          string         `json:"reviewer_target"`
	RequireReviewer         bool           `json:"require_reviewer"`
	SynthesisPromptTemplate string         `json:"synthesis_prompt_template,omitempty"`
	ReviewerPromptTemplate  string         `json:"reviewer_prompt_template,omitempty"`
	IncludeExpertRawOutputs bool           `json:"include_expert_raw_outputs"`
}

// Guardrail describes a virtual model that optimizes eligible input text and validates the buffered output.
type Guardrail struct {
	Name               string   `json:"name"`
	Description        string   `json:"description,omitempty"`
	Enabled            bool     `json:"enabled"`
	SchemaVersion      int      `json:"schema_version,omitempty"`
	OptimizerEnabled   bool     `json:"optimizer_enabled"`
	ValidatorEnabled   bool     `json:"validator_enabled"`
	OptimizerTarget    string   `json:"optimizer_target,omitempty"`
	MainTarget         string   `json:"main_target"`
	ValidatorTarget    string   `json:"validator_target"`
	ResponseMode       string   `json:"response_mode"`
	PolicyPresets      []string `json:"policy_presets,omitempty"`
	CustomPolicy       string   `json:"custom_policy,omitempty"`
	OptimizerTimeoutMS int      `json:"optimizer_timeout_ms"`
	MainTimeoutMS      int      `json:"main_timeout_ms"`
	ValidatorTimeoutMS int      `json:"validator_timeout_ms"`
	MaxPatchCount      int      `json:"max_patch_count"`
	MaxPatchBytes      int      `json:"max_patch_bytes"`
	MaxBufferedBytes   int      `json:"max_buffered_bytes"`
	OptimizerFailOpen  bool     `json:"optimizer_fail_open"`
	ValidatorFailOpen  bool     `json:"validator_fail_open"`
}

// ProviderKey holds one API key / credential entry for multi-key provider support.
type ProviderKey struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Key      string `json:"key"`
	Enabled  bool   `json:"enabled"`
	Priority int    `json:"priority"`
}

// ProviderAccount holds one independently routable upstream credential set under a logical provider.
// Credentials are secret fields and must be redacted before dashboard/API responses.
type ProviderAccount struct {
	ID         string `json:"id"`
	ProviderID string `json:"provider_id"`
	Name       string `json:"name"`
	AuthType   string `json:"auth_type"`

	APIKey       string `json:"api_key,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`

	ProxyURL          string  `json:"proxy_url,omitempty"`
	ProxyID           string  `json:"proxy_id,omitempty"`
	Enabled           bool    `json:"enabled"`
	Priority          int     `json:"priority"`
	QuotaLimitPercent float64 `json:"quota_limit_percent,omitempty"`

	FailureStreak  int       `json:"failure_streak"`
	CooldownUntil  time.Time `json:"cooldown_until,omitempty"`
	CooldownReason string    `json:"cooldown_reason,omitempty"`
	LastUsedAt     time.Time `json:"last_used_at,omitempty"`
	LastSuccessAt  time.Time `json:"last_success_at,omitempty"`
	LastFailureAt  time.Time `json:"last_failure_at,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ProviderAccountOutcome is an atomic operational update recorded after an account attempt.
type ProviderAccountOutcome struct {
	Success          bool
	IncrementFailure bool
	FailureStreak    int
	CooldownUntil    time.Time
	CooldownReason   string
	At               time.Time
}

// Proxy describes one reusable outbound proxy in the shared proxy pool.
type Proxy struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Invoice tracks manually-entered provider/vendor bills for cost management.
type Invoice struct {
	ID         string    `json:"id"`
	ProviderID string    `json:"provider_id"`
	Vendor     string    `json:"vendor"`
	InvoiceNo  string    `json:"invoice_no"`
	Status     string    `json:"status"`
	IssueDate  time.Time `json:"issue_date"`
	DueDate    time.Time `json:"due_date"`
	PaidDate   time.Time `json:"paid_date"`
	Currency   string    `json:"currency"`
	Amount     float64   `json:"amount"`
	Tax        float64   `json:"tax"`
	Total      float64   `json:"total"`
	Note       string    `json:"note"`
	Attachment string    `json:"attachment"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Provider describes one upstream account or compatible endpoint.
type Provider struct {
	ID           string        `json:"id"`
	Type         string        `json:"type"`
	Name         string        `json:"name"`
	BaseURL      string        `json:"base_url"`
	APIKey       string        `json:"api_key"`
	AccessToken  string        `json:"access_token"`
	RefreshToken string        `json:"refresh_token"`
	ProxyURL     string        `json:"proxy_url"`
	ProxyID      string        `json:"proxy_id,omitempty"`
	Enabled      bool          `json:"enabled"`
	Models       []string      `json:"models"`
	HiddenModels []string      `json:"hidden_models,omitempty"`
	Keys         []ProviderKey `json:"keys,omitempty"`
	KeyStrategy  string        `json:"key_strategy"`
	StickyLimit  int           `json:"sticky_limit"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// RequestLogDebugPayload stores optional, explicitly enabled diagnostic payloads.
type RequestLogDebugPayload struct {
	RawPrompt                    string `json:"raw_prompt,omitempty"`
	RawToolResult                string `json:"raw_tool_result,omitempty"`
	RawResponse                  string `json:"raw_response,omitempty"`
	CompactPrompt                string `json:"compact_prompt,omitempty"`
	CompactToolResult            string `json:"compact_tool_result,omitempty"`
	CompactResponse              string `json:"compact_response,omitempty"`
	RawPromptBytes               int    `json:"raw_prompt_bytes,omitempty"`
	RawToolResultBytes           int    `json:"raw_tool_result_bytes,omitempty"`
	RawResponseBytes             int    `json:"raw_response_bytes,omitempty"`
	CompactPromptBytes           int    `json:"compact_prompt_bytes,omitempty"`
	CompactToolResultBytes       int    `json:"compact_tool_result_bytes,omitempty"`
	CompactResponseBytes         int    `json:"compact_response_bytes,omitempty"`
	EstimatedPromptTokensSaved   int    `json:"estimated_prompt_tokens_saved,omitempty"`
	EstimatedToolTokensSaved     int    `json:"estimated_tool_tokens_saved,omitempty"`
	EstimatedResponseTokensSaved int    `json:"estimated_response_tokens_saved,omitempty"`
	RawPromptTruncated           bool   `json:"raw_prompt_truncated,omitempty"`
	RawToolTruncated             bool   `json:"raw_tool_truncated,omitempty"`
	RawResponseTruncated         bool   `json:"raw_response_truncated,omitempty"`
	CompactPromptApplied         bool   `json:"compact_prompt_applied,omitempty"`
	CompactToolApplied           bool   `json:"compact_tool_applied,omitempty"`
	CompactResponseApplied       bool   `json:"compact_response_applied,omitempty"`
	Redacted                     bool   `json:"redacted,omitempty"`
}

type MediaMetrics struct {
	Source          string  `json:"source,omitempty"`
	Operation       string  `json:"operation,omitempty"`
	RequestBytes    int64   `json:"request_bytes,omitempty"`
	ResponseBytes   int64   `json:"response_bytes,omitempty"`
	InputFileCount  int     `json:"input_file_count,omitempty"`
	ImageCount      int     `json:"image_count,omitempty"`
	ImageWidth      int     `json:"image_width,omitempty"`
	ImageHeight     int     `json:"image_height,omitempty"`
	ImageSize       string  `json:"image_size,omitempty"`
	ImageQuality    string  `json:"image_quality,omitempty"`
	TTSCharacters   int     `json:"tts_characters,omitempty"`
	AudioDurationMS int64   `json:"audio_duration_ms,omitempty"`
	STTDurationMS   int64   `json:"stt_duration_ms,omitempty"`
	CostBasis       string  `json:"cost_basis,omitempty"`
	CostUnits       float64 `json:"cost_units,omitempty"`
	UnitPriceUSD    float64 `json:"unit_price_usd,omitempty"`
	Estimated       bool    `json:"estimated,omitempty"`
}

// RequestLog is a compact operational log for dashboard inspection.
type RequestLog struct {
	ID                         string                  `json:"id"`
	Timestamp                  time.Time               `json:"timestamp"`
	Endpoint                   string                  `json:"endpoint"`
	ProviderID                 string                  `json:"provider_id"`
	ProviderAccountID          string                  `json:"provider_account_id,omitempty"`
	Model                      string                  `json:"model"`
	Media                      MediaMetrics            `json:"media,omitempty"`
	Status                     string                  `json:"status"`
	DurationMS                 int64                   `json:"duration_ms"`
	Stream                     bool                    `json:"stream"`
	PromptTokens               int                     `json:"prompt_tokens"`
	CompletionTokens           int                     `json:"completion_tokens"`
	TotalTokens                int                     `json:"total_tokens"`
	CachedTokens               int                     `json:"cached_tokens"`
	ReasoningTokens            int                     `json:"reasoning_tokens"`
	OptimizeDurationMS         int64                   `json:"optimize_duration_ms,omitempty"`
	ProviderDurationMS         int64                   `json:"provider_duration_ms,omitempty"`
	DebugLogDurationMS         int64                   `json:"debug_log_duration_ms,omitempty"`
	EstimatedTokens            bool                    `json:"estimated_tokens"`
	RawInputTokens             int                     `json:"raw_input_tokens,omitempty"`
	UpstreamTokensSaved        int                     `json:"upstream_tokens_saved,omitempty"`
	UpstreamOptimizerEngine    string                  `json:"upstream_optimizer_engine,omitempty"`
	UpstreamOptimizedParts     int                     `json:"upstream_optimized_parts,omitempty"`
	EstimatedTokensSaved       int                     `json:"estimated_tokens_saved,omitempty"`
	EstimatedPromptTokensSaved int                     `json:"estimated_prompt_tokens_saved,omitempty"`
	EstimatedToolTokensSaved   int                     `json:"estimated_tool_tokens_saved,omitempty"`
	CostUSD                    float64                 `json:"cost_usd"`
	APIKeyID                   string                  `json:"api_key_id,omitempty"`
	APIKeyMasked               string                  `json:"api_key_masked,omitempty"`
	APIKeyPrefix               string                  `json:"api_key_prefix,omitempty"`
	APIKeySuffix               string                  `json:"api_key_suffix,omitempty"`
	Error                      string                  `json:"error,omitempty"`
	RouterName                 string                  `json:"router_name,omitempty"`
	RouterRole                 string                  `json:"router_role,omitempty"`
	RouterComplexity           string                  `json:"router_complexity,omitempty"`
	RouterRisk                 string                  `json:"router_risk,omitempty"`
	RouterTarget               string                  `json:"router_target,omitempty"`
	RouterClassifierModel      string                  `json:"router_classifier_model,omitempty"`
	RouterConfidence           float64                 `json:"router_confidence,omitempty"`
	RouterReason               string                  `json:"router_reason,omitempty"`
	RouterDurationMS           int64                   `json:"router_duration_ms,omitempty"`
	RouterUsedFallback         bool                    `json:"router_used_fallback,omitempty"`
	FusionName                 string                  `json:"fusion_name,omitempty"`
	FusionMode                 string                  `json:"fusion_mode,omitempty"`
	FusionExpertCount          int                     `json:"fusion_expert_count,omitempty"`
	FusionSuccessfulExperts    int                     `json:"fusion_successful_experts,omitempty"`
	FusionSynthesizerTarget    string                  `json:"fusion_synthesizer_target,omitempty"`
	FusionReviewerTarget       string                  `json:"fusion_reviewer_target,omitempty"`
	FusionDurationMS           int64                   `json:"fusion_duration_ms,omitempty"`
	FusionUsedReviewer         bool                    `json:"fusion_used_reviewer,omitempty"`
	FusionError                string                  `json:"fusion_error,omitempty"`
	FusionTrace                string                  `json:"fusion_trace,omitempty"`
	GuardrailName              string                  `json:"guardrail_name,omitempty"`
	GuardrailDecision          string                  `json:"guardrail_decision,omitempty"`
	GuardrailFinalAction       string                  `json:"guardrail_final_action,omitempty"`
	GuardrailDurationMS        int64                   `json:"guardrail_duration_ms,omitempty"`
	GuardrailTrace             string                  `json:"guardrail_trace,omitempty"`
	Debug                      *RequestLogDebugPayload `json:"debug,omitempty"`
}

type RequestLogBatchWriter interface {
	AddRequestLogs([]RequestLog) error
}

// Store is the persistence contract used by gateway and dashboard.
type Store interface {
	GetSettings() (Settings, error)
	SaveSettings(Settings) error
	RecordAPIKeyUsage(id string, delta APIKeyUsageDelta) error
	ListProviders() ([]Provider, error)
	GetProvider(id string) (Provider, bool, error)
	UpsertProvider(Provider) error
	DeleteProvider(id string) error
	ListProviderAccounts(providerID string) ([]ProviderAccount, error)
	GetProviderAccount(id string) (ProviderAccount, bool, error)
	UpsertProviderAccount(ProviderAccount) error
	DeleteProviderAccount(id string) error
	RecordProviderAccountOutcome(id string, outcome ProviderAccountOutcome) error
	AddRequestLog(RequestLog) error
	RecentRequestLogs(limit int) ([]RequestLog, error)
	GetRequestDebugPayload(id string) (*RequestLogDebugPayload, bool, error)
	DeleteRequestDebugPayloads() (int, error)
	ResetAllData() error

	ListProxies() ([]Proxy, error)
	GetProxy(id string) (Proxy, bool, error)
	UpsertProxy(Proxy) error
	DeleteProxy(id string) error

	ListInvoices() ([]Invoice, error)
	GetInvoice(id string) (Invoice, bool, error)
	UpsertInvoice(Invoice) error
	DeleteInvoice(id string) error
}

func HydrateRequestLogMetrics(log RequestLog) RequestLog {
	if log.RawInputTokens <= 0 && log.PromptTokens > 0 && log.EstimatedTokensSaved > 0 {
		log.RawInputTokens = log.PromptTokens + log.EstimatedTokensSaved
	}
	return log
}

func StripRequestDebugPayload(log RequestLog) RequestLog {
	log = HydrateRequestLogMetrics(log)
	if log.Debug == nil {
		return log
	}
	debug := *log.Debug
	debug.RawPrompt = ""
	debug.RawToolResult = ""
	debug.RawResponse = ""
	debug.CompactPrompt = ""
	debug.CompactToolResult = ""
	debug.CompactResponse = ""
	log.Debug = &debug
	return HydrateRequestLogMetrics(log)
}

func CloneRequestDebugPayload(payload *RequestLogDebugPayload) *RequestLogDebugPayload {
	if payload == nil {
		return nil
	}
	out := *payload
	return &out
}

func PreserveAPIKeyUsage(items []APIKeyPolicy, current []APIKeyPolicy) []APIKeyPolicy {
	usage := make(map[string]APIKeyPolicy, len(current))
	for _, item := range current {
		usage[item.ID] = item
	}
	for i := range items {
		if previous, ok := usage[items[i].ID]; ok {
			items[i].UsedRequests = previous.UsedRequests
			items[i].UsedTokens = previous.UsedTokens
			items[i].UsedCostUSD = previous.UsedCostUSD
		}
	}
	return items
}

func NormalizeAPIKeyPolicies(items []APIKeyPolicy) []APIKeyPolicy {
	out := []APIKeyPolicy{}
	for _, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		item.Key = strings.TrimSpace(item.Key)
		item.AllowedModels = NormalizeModels(item.AllowedModels)
		if item.MaxRequests < 0 {
			item.MaxRequests = 0
		}
		if item.MaxTokens < 0 {
			item.MaxTokens = 0
		}
		if item.MaxCostUSD < 0 {
			item.MaxCostUSD = 0
		}
		if item.MaxRPM < 0 {
			item.MaxRPM = 0
		}
		if item.MaxConcurrent < 0 {
			item.MaxConcurrent = 0
		}
		if item.ID == "" && item.Key != "" {
			item.ID = item.Key
		}
		if item.ID == "" && item.Key == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func NormalizeModelPriceRules(items []ModelPriceRule) []ModelPriceRule {
	out := []ModelPriceRule{}
	for _, item := range items {
		item.ProviderID = strings.TrimSpace(item.ProviderID)
		item.Model = strings.TrimSpace(item.Model)
		if item.ProviderID == "" && item.Model == "" {
			continue
		}
		if item.ContextLength < 0 {
			item.ContextLength = 0
		}
		out = append(out, item)
	}
	return out
}

func NormalizeCombos(items []Combo) []Combo {
	out := []Combo{}
	seen := map[string]bool{}
	for _, item := range items {
		item.Name = strings.TrimSpace(item.Name)
		item.Models = NormalizeModels(item.Models)
		item.Strategy = strings.ToLower(strings.TrimSpace(item.Strategy))
		item.Description = strings.TrimSpace(item.Description)
		if item.Name == "" || len(item.Models) == 0 || seen[item.Name] {
			continue
		}
		if item.Strategy != "round-robin" {
			item.Strategy = "fallback"
		}
		if item.StickyLimit <= 0 {
			item.StickyLimit = 1
		}
		if item.ContextLength < 0 {
			item.ContextLength = 0
		}
		seen[item.Name] = true
		out = append(out, item)
	}
	return out
}

func NormalizePromptRouters(items []PromptRouter) []PromptRouter {
	out := []PromptRouter{}
	seen := map[string]bool{}
	for _, item := range items {
		item.Name = strings.TrimSpace(item.Name)
		item.ClassifierModel = strings.TrimSpace(item.ClassifierModel)
		item.FallbackTarget = strings.TrimSpace(item.FallbackTarget)
		item.FallbackRole = strings.TrimSpace(item.FallbackRole)
		item.Description = strings.TrimSpace(item.Description)
		item.ClassifierPromptTemplate = strings.TrimSpace(item.ClassifierPromptTemplate)
		if item.Name == "" || item.ClassifierModel == "" || seen[item.Name] {
			continue
		}
		routes := []PromptRoute{}
		routeSeen := map[string]bool{}
		for _, route := range item.Routes {
			route.Role = strings.ToLower(strings.TrimSpace(route.Role))
			route.Complexity = normalizePromptRouterLevel(route.Complexity)
			route.Risk = normalizePromptRouterLevel(route.Risk)
			route.Target = strings.TrimSpace(route.Target)
			route.Instruction = strings.TrimSpace(route.Instruction)
			key := route.Role + "|" + route.Complexity + "|" + route.Risk
			if route.Role == "" || route.Target == "" || routeSeen[key] {
				continue
			}
			routeSeen[key] = true
			routes = append(routes, route)
		}
		if len(routes) == 0 {
			continue
		}
		if item.FallbackTarget == "" {
			item.FallbackTarget = routes[0].Target
		}
		if item.FallbackRole == "" {
			item.FallbackRole = routes[0].Role
		}
		item.Routes = routes
		seen[item.Name] = true
		out = append(out, item)
	}
	return out
}

func normalizePromptRouterLevel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "low", "medium", "high":
		return value
	default:
		return ""
	}
}

func NormalizeGuardrails(items []Guardrail) []Guardrail {
	seen := map[string]bool{}
	out := []Guardrail{}
	for _, item := range items {
		item.Name = strings.TrimSpace(item.Name)
		item.Description = strings.TrimSpace(item.Description)
		item.OptimizerTarget = strings.TrimSpace(item.OptimizerTarget)
		item.MainTarget = strings.TrimSpace(item.MainTarget)
		item.ValidatorTarget = strings.TrimSpace(item.ValidatorTarget)
		item.CustomPolicy = strings.TrimSpace(item.CustomPolicy)
		if item.SchemaVersion <= 0 {
			// Guardrails created before independent stage toggles always ran validation.
			item.ValidatorEnabled = true
		}
		item.SchemaVersion = 1
		if item.Name == "" || item.MainTarget == "" || (item.ValidatorEnabled && item.ValidatorTarget == "") || seen[item.Name] {
			continue
		}
		if item.Name == item.MainTarget || (item.ValidatorEnabled && item.Name == item.ValidatorTarget) || (item.OptimizerEnabled && item.Name == item.OptimizerTarget) {
			continue
		}
		if item.ResponseMode != GuardrailResponseBuffered {
			item.ResponseMode = GuardrailResponseStream
		}
		item.PolicyPresets = normalizeGuardrailPolicies(item.PolicyPresets)
		if item.OptimizerEnabled && item.OptimizerTarget == "" {
			item.OptimizerEnabled = false
		}
		item.OptimizerTimeoutMS = normalizeGuardrailTimeout(item.OptimizerTimeoutMS, 30000)
		item.MainTimeoutMS = normalizeGuardrailTimeout(item.MainTimeoutMS, 120000)
		item.ValidatorTimeoutMS = normalizeGuardrailTimeout(item.ValidatorTimeoutMS, 30000)
		if item.MaxPatchCount <= 0 {
			item.MaxPatchCount = 128
		} else if item.MaxPatchCount > 1024 {
			item.MaxPatchCount = 1024
		}
		if item.MaxPatchBytes <= 0 {
			item.MaxPatchBytes = 256 * 1024
		} else if item.MaxPatchBytes > 1024*1024 {
			item.MaxPatchBytes = 1024 * 1024
		}
		if item.MaxBufferedBytes <= 0 {
			item.MaxBufferedBytes = 4 * 1024 * 1024
		} else if item.MaxBufferedBytes > 16*1024*1024 {
			item.MaxBufferedBytes = 16 * 1024 * 1024
		}
		// Guardrails are fail-open by default for backward-compatible zero-value settings.
		if !item.OptimizerFailOpen && !item.ValidatorFailOpen {
			item.OptimizerFailOpen = true
			item.ValidatorFailOpen = true
		}
		seen[item.Name] = true
		out = append(out, item)
	}
	return out
}

func normalizeGuardrailTimeout(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	if value > 180000 {
		return 180000
	}
	return value
}

func normalizeGuardrailPolicies(items []string) []string {
	allowed := map[string]bool{"safety": true, "quality": true, "format": true, "privacy": true}
	seen := map[string]bool{}
	out := []string{}
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if !allowed[item] || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	if len(out) == 0 {
		out = []string{"safety", "quality", "format", "privacy"}
	}
	return out
}

// NormalizeBudgetSettings clamps budget alert threshold and budget amounts to
// safe values. A zero budget means unlimited; the alert threshold defaults to
// 80% so existing stores without the field still get sensible behavior.
func NormalizeFusions(items []Fusion) []Fusion {
	seen := map[string]bool{}
	out := []Fusion{}
	for _, fusion := range items {
		fusion.Name = strings.TrimSpace(fusion.Name)
		if fusion.Name == "" || seen[fusion.Name] {
			continue
		}
		seen[fusion.Name] = true
		fusion.Description = strings.TrimSpace(fusion.Description)
		fusion.Mode = strings.TrimSpace(fusion.Mode)
		if fusion.Mode != FusionModeSequential {
			fusion.Mode = FusionModeParallel
		}
		if fusion.TimeoutMS <= 0 {
			fusion.TimeoutMS = 120000
		}
		if fusion.MaxOutputTokens < 0 {
			fusion.MaxOutputTokens = 0
		}
		fusion.SynthesizerTarget = strings.TrimSpace(fusion.SynthesizerTarget)
		fusion.ReviewerTarget = strings.TrimSpace(fusion.ReviewerTarget)
		fusion.SynthesisPromptTemplate = strings.TrimSpace(fusion.SynthesisPromptTemplate)
		fusion.ReviewerPromptTemplate = strings.TrimSpace(fusion.ReviewerPromptTemplate)
		experts := []FusionExpert{}
		for _, expert := range fusion.Experts {
			expert.Name = strings.TrimSpace(expert.Name)
			expert.Target = strings.TrimSpace(expert.Target)
			expert.Role = strings.TrimSpace(expert.Role)
			expert.PromptTemplate = strings.TrimSpace(expert.PromptTemplate)
			if expert.Target == "" || expert.Target == fusion.Name {
				continue
			}
			if expert.Name == "" {
				expert.Name = expert.Target
			}
			if expert.Weight < 0 {
				expert.Weight = 0
			}
			experts = append(experts, expert)
		}
		fusion.Experts = experts
		if fusion.MinSuccessfulExperts < 0 {
			fusion.MinSuccessfulExperts = 0
		}
		if fusion.MinSuccessfulExperts > len(fusion.Experts) {
			fusion.MinSuccessfulExperts = len(fusion.Experts)
		}
		out = append(out, fusion)
	}
	return out
}

func NormalizeBudgetSettings(s *Settings) {
	if s.BudgetAlertPct <= 0 {
		s.BudgetAlertPct = 80
	}
	if s.BudgetAlertPct > 100 {
		s.BudgetAlertPct = 100
	}
	if s.DailyBudgetUSD < 0 {
		s.DailyBudgetUSD = 0
	}
	if s.MonthlyBudgetUSD < 0 {
		s.MonthlyBudgetUSD = 0
	}
}

// NormalizeDebugSettings keeps diagnostic payload capture safe by default.
func NormalizeDebugSettings(s *Settings) {
	if s.MaxDebugPayloadBytes <= 0 {
		s.MaxDebugPayloadBytes = 128 * 1024
		s.MaskDebugSecrets = true
	}
	if s.MaxDebugPayloadBytes > 512*1024 {
		s.MaxDebugPayloadBytes = 512 * 1024
	}
}

func NormalizeTokenOptimizationSettings(s *Settings) {
	if s.TokenOptimizeMinChars <= 0 {
		s.TokenOptimizeMinChars = 12000
	}
	if s.TokenOptimizeMaxChars <= 0 {
		s.TokenOptimizeMaxChars = 12000
	}
	if s.TokenOptimizeMinChars < 1000 {
		s.TokenOptimizeMinChars = 1000
	}
	if s.TokenOptimizeMaxChars < 2000 {
		s.TokenOptimizeMaxChars = 2000
	}
	if s.TokenOptimizeMaxChars > 128*1024 {
		s.TokenOptimizeMaxChars = 128 * 1024
	}
	originalCompressionMode := strings.TrimSpace(s.PromptRouterCompressionMode)
	s.PromptRouterCompressionMode = originalCompressionMode
	switch s.PromptRouterCompressionMode {
	case PromptRouterCompressionFull, PromptRouterCompressionPerType:
	case "":
		s.PromptRouterCompressionMode = PromptRouterCompressionPreview
	default:
		s.PromptRouterCompressionMode = PromptRouterCompressionPreview
	}
	if originalCompressionMode == "" && !s.PromptRouterCompressSystem && !s.PromptRouterCompressDeveloper && !s.PromptRouterCompressMessages && !s.PromptRouterCompressToolResults && !s.PromptRouterCompressToolSchemas && !s.PromptRouterCompressImages {
		s.PromptRouterCompressSystem = true
		s.PromptRouterCompressDeveloper = true
		s.PromptRouterCompressMessages = true
		s.PromptRouterCompressToolResults = true
		s.PromptRouterCompressToolSchemas = true
		s.PromptRouterCompressImages = true
	}
	s.RTKPath = strings.TrimSpace(s.RTKPath)
}

func DefaultSettings() Settings {
	return Settings{
		RequireAPIKey:                   false,
		LocalAPIKey:                     "",
		DefaultProvider:                 "openai",
		DefaultCodexID:                  "codex",
		KeepRequestLogs:                 1000,
		ObservabilityEnabled:            true,
		SaveRawPrompt:                   false,
		SaveRawToolResult:               false,
		MaskDebugSecrets:                true,
		CompactDebugPayloads:            true,
		MaxDebugPayloadBytes:            128 * 1024,
		TokenOptimizeToolResults:        false,
		TokenOptimizeSystem:             false,
		TokenOptimizeDeveloper:          false,
		TokenOptimizeText:               false,
		TokenOptimizeToolSchemas:        false,
		TokenOptimizeToolCalls:          false,
		TokenOptimizeMinChars:           12000,
		TokenOptimizeMaxChars:           12000,
		PromptRouterCompressionMode:     PromptRouterCompressionPreview,
		PromptRouterCompressSystem:      true,
		PromptRouterCompressDeveloper:   true,
		PromptRouterCompressMessages:    true,
		PromptRouterCompressToolResults: true,
		PromptRouterCompressToolSchemas: true,
		PromptRouterCompressImages:      true,
		RTKEnabled:                      false,
		RTKPath:                         "",
		DashboardMessage:                "VivuRouter local AI gateway for OpenAI-compatible and Codex endpoints",
		AdminSecurityEnabled:            false,
		AdminPasscode:                   "",
		APIKeys:                         []APIKeyPolicy{},
		ModelPrices:                     []ModelPriceRule{},
		Combos:                          []Combo{},
		PromptRouters:                   []PromptRouter{},
		Fusions:                         []Fusion{},
		Guardrails:                      []Guardrail{},
		BudgetAlertPct:                  80,
	}
}

// SeedProviders builds the default provider set from environment variables. It
// is shared by every Store backend so first-run seeding is consistent.
func SeedProviders() []Provider {
	now := time.Now().UTC()
	openAIModels := splitCSV(os.Getenv("OPENAI_MODELS"), []string{"gpt-4.1", "gpt-4o-mini"})
	codexModels := splitCSV(os.Getenv("CODEX_MODELS"), []string{"cx/gpt-5.5", "cx/gpt-5.4", "cx/gpt-5.3-codex"})
	return []Provider{
		{
			ID:        "openai",
			Type:      ProviderOpenAICompatible,
			Name:      "OpenAI Compatible",
			BaseURL:   envOr("OPENAI_BASE_URL", "https://api.openai.com/v1"),
			APIKey:    os.Getenv("OPENAI_API_KEY"),
			ProxyURL:  os.Getenv("OPENAI_PROXY_URL"),
			Enabled:   true,
			Models:    openAIModels,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:          "codex",
			Type:        ProviderCodex,
			Name:        "Codex Responses",
			BaseURL:     envOr("CODEX_BASE_URL", "https://chatgpt.com/backend-api/codex/responses"),
			AccessToken: os.Getenv("CODEX_ACCESS_TOKEN"),
			ProxyURL:    os.Getenv("CODEX_PROXY_URL"),
			Enabled:     os.Getenv("CODEX_ACCESS_TOKEN") != "",
			Models:      codexModels,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:        "mimo-free",
			Type:      ProviderMimoFree,
			Name:      "MiMo Code Free",
			BaseURL:   envOr("MIMO_FREE_BASE_URL", "https://api.xiaomimimo.com/api/free-ai/openai/chat"),
			ProxyURL:  os.Getenv("MIMO_FREE_PROXY_URL"),
			Enabled:   strings.EqualFold(os.Getenv("MIMO_FREE_ENABLED"), "true"),
			Models:    splitCSV(os.Getenv("MIMO_FREE_MODELS"), []string{"mimo-auto"}),
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        "opencode",
			Type:      ProviderOpenCodeFree,
			Name:      "OpenCode Free",
			BaseURL:   envOr("OPENCODE_FREE_BASE_URL", "https://opencode.ai"),
			ProxyURL:  os.Getenv("OPENCODE_FREE_PROXY_URL"),
			Enabled:   strings.EqualFold(os.Getenv("OPENCODE_FREE_ENABLED"), "true"),
			Models:    splitCSV(os.Getenv("OPENCODE_FREE_MODELS"), []string{"big-pickle"}),
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:           "antigravity",
			Type:         ProviderAntigravity,
			Name:         "Antigravity",
			BaseURL:      envOr("ANTIGRAVITY_BASE_URL", "https://daily-cloudcode-pa.googleapis.com"),
			AccessToken:  os.Getenv("ANTIGRAVITY_ACCESS_TOKEN"),
			RefreshToken: os.Getenv("ANTIGRAVITY_REFRESH_TOKEN"),
			ProxyURL:     os.Getenv("ANTIGRAVITY_PROXY_URL"),
			Enabled:      strings.EqualFold(os.Getenv("ANTIGRAVITY_ENABLED"), "true") || os.Getenv("ANTIGRAVITY_ACCESS_TOKEN") != "",
			Models:       splitCSV(os.Getenv("ANTIGRAVITY_MODELS"), antigravityDefaultProviderModels()),
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}
}

func antigravityDefaultProviderModels() []string {
	return []string{
		"gemini-3-flash-agent",
		"gemini-3.5-flash-low",
		"gemini-3.5-flash-extra-low",
		"gemini-pro-agent",
		"gemini-3.1-pro-low",
		"claude-sonnet-4-6",
		"claude-opus-4-6-thinking",
		"gpt-oss-120b-medium",
		"gemini-3-flash",
	}
}

// NormalizeProvider trims core fields, normalizes models and migrates legacy APIKey into Keys.
func NormalizeProvider(provider Provider) Provider {
	provider.ID = strings.TrimSpace(provider.ID)
	provider.Type = strings.TrimSpace(provider.Type)
	provider.Name = strings.TrimSpace(provider.Name)
	provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	provider.APIKey = strings.TrimSpace(provider.APIKey)
	provider.AccessToken = strings.TrimSpace(provider.AccessToken)
	provider.RefreshToken = strings.TrimSpace(provider.RefreshToken)
	provider.ProxyURL = strings.TrimSpace(provider.ProxyURL)
	provider.ProxyID = strings.TrimSpace(provider.ProxyID)
	if provider.ProxyID != "" {
		provider.ProxyURL = ""
	}
	provider.Models = NormalizeModels(provider.Models)
	provider.HiddenModels = NormalizeModels(provider.HiddenModels)
	provider.Keys = NormalizeProviderKeys(provider.Keys)
	if len(provider.Keys) == 0 && provider.APIKey != "" {
		provider.Keys = []ProviderKey{{ID: "default", Name: "Default Key", Key: provider.APIKey, Enabled: true, Priority: 1}}
	}
	switch provider.KeyStrategy {
	case ProviderKeyStrategyRoundRobin:
	case "":
		provider.KeyStrategy = ProviderKeyStrategyFillFirst
	default:
		provider.KeyStrategy = ProviderKeyStrategyFillFirst
	}
	if provider.StickyLimit <= 0 {
		provider.StickyLimit = 1
	}
	return provider
}

func NormalizeProviders(items []Provider) []Provider {
	out := make([]Provider, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		item = NormalizeProvider(item)
		if item.ID == "" || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		out = append(out, item)
	}
	return out
}

func NormalizeProviderKeys(items []ProviderKey) []ProviderKey {
	out := make([]ProviderKey, 0, len(items))
	seen := map[string]bool{}
	for i, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		item.Name = strings.TrimSpace(item.Name)
		item.Key = strings.TrimSpace(item.Key)
		if item.Key == "" {
			continue
		}
		if item.ID == "" {
			item.ID = "key-" + strings.ToLower(strings.ReplaceAll(item.Name, " ", "-"))
			if item.ID == "key-" {
				item.ID = "key"
			}
		}
		if seen[item.ID] {
			item.ID = item.ID + "-" + strings.TrimSpace(time.Now().UTC().Format("20060102150405"))
		}
		if item.Name == "" {
			item.Name = "Key " + strings.TrimSpace(item.ID)
		}
		if item.Priority <= 0 {
			item.Priority = i + 1
		}
		seen[item.ID] = true
		out = append(out, item)
	}
	return out
}

func NormalizeProviderAccount(item ProviderAccount) ProviderAccount {
	item.ID = strings.TrimSpace(item.ID)
	item.ProviderID = strings.TrimSpace(item.ProviderID)
	item.Name = strings.TrimSpace(item.Name)
	item.AuthType = strings.ToLower(strings.TrimSpace(item.AuthType))
	switch item.AuthType {
	case "api_key", "bearer", "oauth", "none":
	default:
		item.AuthType = "api_key"
	}
	item.APIKey = strings.TrimSpace(item.APIKey)
	item.AccessToken = strings.TrimSpace(item.AccessToken)
	item.RefreshToken = strings.TrimSpace(item.RefreshToken)
	item.ProxyID = strings.TrimSpace(item.ProxyID)
	item.ProxyURL = strings.TrimSpace(item.ProxyURL)
	if item.ProxyID != "" {
		item.ProxyURL = ""
	}
	if item.Priority <= 0 {
		item.Priority = 1
	}
	if math.IsNaN(item.QuotaLimitPercent) || math.IsInf(item.QuotaLimitPercent, 0) || item.QuotaLimitPercent < 0 {
		item.QuotaLimitPercent = 0
	}
	if item.QuotaLimitPercent > 100 {
		item.QuotaLimitPercent = 100
	}
	if item.FailureStreak < 0 {
		item.FailureStreak = 0
	}
	return item
}

func NormalizeProviderAccounts(items []ProviderAccount) []ProviderAccount {
	out := make([]ProviderAccount, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		item = NormalizeProviderAccount(item)
		if item.ID == "" || item.ProviderID == "" || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		out = append(out, item)
	}
	return out
}

func NormalizeProxy(p Proxy) Proxy {
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)
	p.URL = strings.TrimSpace(p.URL)
	return p
}

func NormalizeProxies(items []Proxy) []Proxy {
	out := make([]Proxy, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		item = NormalizeProxy(item)
		if item.ID == "" || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		out = append(out, item)
	}
	return out
}

func NormalizeInvoice(item Invoice) Invoice {
	item.ID = strings.TrimSpace(item.ID)
	item.ProviderID = strings.TrimSpace(item.ProviderID)
	item.Vendor = strings.TrimSpace(item.Vendor)
	item.InvoiceNo = strings.TrimSpace(item.InvoiceNo)
	item.Status = strings.ToLower(strings.TrimSpace(item.Status))
	switch item.Status {
	case "draft", "unpaid", "paid", "overdue", "void":
	default:
		item.Status = "unpaid"
	}
	item.Currency = strings.ToUpper(strings.TrimSpace(item.Currency))
	if item.Currency == "" {
		item.Currency = "USD"
	}
	if item.Amount < 0 {
		item.Amount = 0
	}
	if item.Tax < 0 {
		item.Tax = 0
	}
	if item.Total < 0 {
		item.Total = 0
	}
	if item.Total == 0 {
		item.Total = item.Amount + item.Tax
	}
	item.Note = strings.TrimSpace(item.Note)
	item.Attachment = strings.TrimSpace(item.Attachment)
	return item
}

func NormalizeInvoices(items []Invoice) []Invoice {
	out := make([]Invoice, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		item = NormalizeInvoice(item)
		if item.ID == "" || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		out = append(out, item)
	}
	return out
}

// NormalizeModels trims, dedups and drops empty model identifiers.
func NormalizeModels(items []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range items {
		model := strings.TrimSpace(item)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

func splitCSV(value string, fallback []string) []string {
	if strings.TrimSpace(value) == "" {
		return append([]string(nil), fallback...)
	}
	return NormalizeModels(strings.Split(value, ","))
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
