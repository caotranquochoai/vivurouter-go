package store

import (
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
)

// Settings stores local gateway settings.
type Settings struct {
	RequireAPIKey                   bool             `json:"require_api_key"`
	LocalAPIKey                     string           `json:"local_api_key"`
	DefaultProvider                 string           `json:"default_provider"`
	DefaultCodexID                  string           `json:"default_codex_id"`
	KeepRequestLogs                 int              `json:"keep_request_logs"`
	ObservabilityEnabled            bool             `json:"observability_enabled"`
	SaveRawPrompt                   bool             `json:"save_raw_prompt"`
	SaveRawToolResult               bool             `json:"save_raw_tool_result"`
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
	UsedRequests  int      `json:"used_requests"`
	UsedTokens    int      `json:"used_tokens"`
	UsedCostUSD   float64  `json:"used_cost_usd"`
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

// ProviderKey holds one API key / credential entry for multi-key provider support.
type ProviderKey struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Key      string `json:"key"`
	Enabled  bool   `json:"enabled"`
	Priority int    `json:"priority"`
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
	Keys         []ProviderKey `json:"keys,omitempty"`
	KeyStrategy  string        `json:"key_strategy"`
	StickyLimit  int           `json:"sticky_limit"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// RequestLogDebugPayload stores optional, explicitly enabled diagnostic payloads.
type RequestLogDebugPayload struct {
	RawPrompt                  string `json:"raw_prompt,omitempty"`
	RawToolResult              string `json:"raw_tool_result,omitempty"`
	CompactPrompt              string `json:"compact_prompt,omitempty"`
	CompactToolResult          string `json:"compact_tool_result,omitempty"`
	RawPromptBytes             int    `json:"raw_prompt_bytes,omitempty"`
	RawToolResultBytes         int    `json:"raw_tool_result_bytes,omitempty"`
	CompactPromptBytes         int    `json:"compact_prompt_bytes,omitempty"`
	CompactToolResultBytes     int    `json:"compact_tool_result_bytes,omitempty"`
	EstimatedPromptTokensSaved int    `json:"estimated_prompt_tokens_saved,omitempty"`
	EstimatedToolTokensSaved   int    `json:"estimated_tool_tokens_saved,omitempty"`
	RawPromptTruncated         bool   `json:"raw_prompt_truncated,omitempty"`
	RawToolTruncated           bool   `json:"raw_tool_truncated,omitempty"`
	CompactPromptApplied       bool   `json:"compact_prompt_applied,omitempty"`
	CompactToolApplied         bool   `json:"compact_tool_applied,omitempty"`
	Redacted                   bool   `json:"redacted,omitempty"`
}

// RequestLog is a compact operational log for dashboard inspection.
type RequestLog struct {
	ID                         string                  `json:"id"`
	Timestamp                  time.Time               `json:"timestamp"`
	Endpoint                   string                  `json:"endpoint"`
	ProviderID                 string                  `json:"provider_id"`
	Model                      string                  `json:"model"`
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
	Debug                      *RequestLogDebugPayload `json:"debug,omitempty"`
}

// Store is the persistence contract used by gateway and dashboard.
type Store interface {
	GetSettings() (Settings, error)
	SaveSettings(Settings) error
	ListProviders() ([]Provider, error)
	GetProvider(id string) (Provider, bool, error)
	UpsertProvider(Provider) error
	DeleteProvider(id string) error
	AddRequestLog(RequestLog) error
	RecentRequestLogs(limit int) ([]RequestLog, error)
	GetRequestDebugPayload(id string) (*RequestLogDebugPayload, bool, error)
	DeleteRequestDebugPayloads() (int, error)
	ResetAllData() error

	ListProxies() ([]Proxy, error)
	GetProxy(id string) (Proxy, bool, error)
	UpsertProxy(Proxy) error
	DeleteProxy(id string) error
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
	debug.CompactPrompt = ""
	debug.CompactToolResult = ""
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

func NormalizeAPIKeyPolicies(items []APIKeyPolicy) []APIKeyPolicy {
	out := []APIKeyPolicy{}
	for _, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		item.Key = strings.TrimSpace(item.Key)
		item.AllowedModels = NormalizeModels(item.AllowedModels)
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
