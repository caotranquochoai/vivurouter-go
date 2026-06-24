package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store on top of a pure-Go SQLite database. It mirrors
// FileStore semantics (seeding, model normalization, log retention) so both
// backends are interchangeable behind the Store interface.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (and migrates) a SQLite database under dataDir.
func NewSQLiteStore(dataDir string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "sample.sqlite")
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.seed(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying database handle.
func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  data TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS providers (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  name TEXT,
  base_url TEXT,
  api_key TEXT,
  access_token TEXT,
  refresh_token TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  models TEXT NOT NULL,
  proxy_url TEXT,
  proxy_id TEXT DEFAULT '',
  keys TEXT DEFAULT '[]',
  key_strategy TEXT DEFAULT 'fill-first',
  sticky_limit INTEGER DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS proxies (
  id TEXT PRIMARY KEY,
  name TEXT,
  url TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS request_logs (
  id TEXT PRIMARY KEY,
  timestamp TEXT NOT NULL,
  endpoint TEXT,
  provider_id TEXT,
  model TEXT,
  status TEXT,
  duration_ms INTEGER,
  stream INTEGER,
  prompt_tokens INTEGER DEFAULT 0,
  completion_tokens INTEGER DEFAULT 0,
  total_tokens INTEGER DEFAULT 0,
  cached_tokens INTEGER DEFAULT 0,
  reasoning_tokens INTEGER DEFAULT 0,
  optimize_duration_ms INTEGER DEFAULT 0,
  provider_duration_ms INTEGER DEFAULT 0,
  debug_log_duration_ms INTEGER DEFAULT 0,
  estimated_tokens INTEGER DEFAULT 0,
  estimated_tokens_saved INTEGER DEFAULT 0,
  upstream_tokens_saved INTEGER DEFAULT 0,
  upstream_optimizer_engine TEXT,
  upstream_optimized_parts INTEGER DEFAULT 0,
  estimated_prompt_tokens_saved INTEGER DEFAULT 0,
  estimated_tool_tokens_saved INTEGER DEFAULT 0,
  cost_usd REAL DEFAULT 0,
  router_name TEXT,
  router_role TEXT,
  router_complexity TEXT,
  router_risk TEXT,
  router_target TEXT,
  router_classifier_model TEXT,
  router_confidence REAL DEFAULT 0,
  router_reason TEXT,
  router_duration_ms INTEGER DEFAULT 0,
  router_used_fallback INTEGER DEFAULT 0,
  fusion_name TEXT,
  fusion_mode TEXT,
  fusion_expert_count INTEGER DEFAULT 0,
  fusion_successful_experts INTEGER DEFAULT 0,
  fusion_synthesizer_target TEXT,
  fusion_reviewer_target TEXT,
  fusion_duration_ms INTEGER DEFAULT 0,
  fusion_used_reviewer INTEGER DEFAULT 0,
  fusion_error TEXT,
  fusion_trace TEXT,
  error TEXT
);
CREATE TABLE IF NOT EXISTS request_debug_payloads (
  request_id TEXT PRIMARY KEY,
  payload TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_request_debug_payloads_created ON request_debug_payloads(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_request_logs_ts ON request_logs(timestamp DESC);
`)
	if err != nil {
		return err
	}
	if err := s.migrateUsageColumns(); err != nil {
		return err
	}
	if err := s.migrateProviderColumns(); err != nil {
		return err
	}
	return s.migrateDebugPayloads()
}

func (s *SQLiteStore) migrateUsageColumns() error {
	columns := map[string]string{
		"prompt_tokens":                 "INTEGER DEFAULT 0",
		"completion_tokens":             "INTEGER DEFAULT 0",
		"total_tokens":                  "INTEGER DEFAULT 0",
		"cached_tokens":                 "INTEGER DEFAULT 0",
		"reasoning_tokens":              "INTEGER DEFAULT 0",
		"estimated_tokens":              "INTEGER DEFAULT 0",
		"estimated_tokens_saved":        "INTEGER DEFAULT 0",
		"upstream_tokens_saved":         "INTEGER DEFAULT 0",
		"upstream_optimizer_engine":     "TEXT",
		"upstream_optimized_parts":      "INTEGER DEFAULT 0",
		"estimated_prompt_tokens_saved": "INTEGER DEFAULT 0",
		"estimated_tool_tokens_saved":   "INTEGER DEFAULT 0",
		"cost_usd":                      "REAL DEFAULT 0",
		"api_key_id":                    "TEXT",
		"api_key_masked":                "TEXT",
		"api_key_prefix":                "TEXT",
		"api_key_suffix":                "TEXT",
		"debug_payload":                 "TEXT",
		"debug_redacted":                "INTEGER DEFAULT 0",
		"raw_prompt_bytes":              "INTEGER DEFAULT 0",
		"raw_tool_result_bytes":         "INTEGER DEFAULT 0",
		"raw_prompt_truncated":          "INTEGER DEFAULT 0",
		"raw_tool_truncated":            "INTEGER DEFAULT 0",
		"router_complexity":             "TEXT",
		"router_risk":                   "TEXT",
		"router_used_fallback":          "INTEGER DEFAULT 0",
		"fusion_name":                   "TEXT",
		"fusion_mode":                   "TEXT",
		"fusion_expert_count":           "INTEGER DEFAULT 0",
		"fusion_successful_experts":     "INTEGER DEFAULT 0",
		"fusion_synthesizer_target":     "TEXT",
		"fusion_reviewer_target":        "TEXT",
		"fusion_duration_ms":            "INTEGER DEFAULT 0",
		"fusion_used_reviewer":          "INTEGER DEFAULT 0",
		"fusion_error":                  "TEXT",
		"fusion_trace":                  "TEXT",
	}
	for name, typ := range columns {
		var exists int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('request_logs') WHERE name = ?`, name).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			if _, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE request_logs ADD COLUMN %s %s`, name, typ)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SQLiteStore) migrateProviderColumns() error {
	columns := map[string]string{
		"proxy_url":    "TEXT",
		"proxy_id":     "TEXT DEFAULT ''",
		"keys":         "TEXT DEFAULT '[]'",
		"key_strategy": "TEXT DEFAULT 'fill-first'",
		"sticky_limit": "INTEGER DEFAULT 1",
	}
	for name, typ := range columns {
		var exists int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('providers') WHERE name = ?`, name).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			if _, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE providers ADD COLUMN %s %s`, name, typ)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SQLiteStore) seed() error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		if err := s.SaveSettings(DefaultSettings()); err != nil {
			return err
		}
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM providers`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		for _, p := range SeedProviders() {
			if err := s.insertProvider(p); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SQLiteStore) GetSettings() (Settings, error) {
	var data string
	err := s.db.QueryRow(`SELECT data FROM settings WHERE id = 1`).Scan(&data)
	if err == sql.ErrNoRows {
		return DefaultSettings(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	settings := DefaultSettings()
	if err := json.Unmarshal([]byte(data), &settings); err != nil {
		return Settings{}, err
	}
	if settings.KeepRequestLogs <= 0 {
		settings.KeepRequestLogs = 200
	}
	settings.APIKeys = NormalizeAPIKeyPolicies(settings.APIKeys)
	settings.ModelPrices = NormalizeModelPriceRules(settings.ModelPrices)
	settings.Combos = NormalizeCombos(settings.Combos)
	settings.PromptRouters = NormalizePromptRouters(settings.PromptRouters)
	settings.Fusions = NormalizeFusions(settings.Fusions)
	NormalizeBudgetSettings(&settings)
	NormalizeDebugSettings(&settings)
	NormalizeTokenOptimizationSettings(&settings)
	return settings, nil
}

func (s *SQLiteStore) SaveSettings(settings Settings) error {
	if settings.KeepRequestLogs <= 0 {
		settings.KeepRequestLogs = 200
	}
	settings.APIKeys = NormalizeAPIKeyPolicies(settings.APIKeys)
	settings.ModelPrices = NormalizeModelPriceRules(settings.ModelPrices)
	settings.Combos = NormalizeCombos(settings.Combos)
	settings.PromptRouters = NormalizePromptRouters(settings.PromptRouters)
	settings.Fusions = NormalizeFusions(settings.Fusions)
	NormalizeBudgetSettings(&settings)
	NormalizeDebugSettings(&settings)
	NormalizeTokenOptimizationSettings(&settings)
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO settings (id, data) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data`, string(raw))
	return err
}

func (s *SQLiteStore) ListProviders() ([]Provider, error) {
	rows, err := s.db.Query(`SELECT id, type, name, base_url, api_key, access_token, refresh_token, proxy_url, proxy_id, enabled, models, keys, key_strategy, sticky_limit, created_at, updated_at FROM providers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Provider{}
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	proxies, err := s.enabledProxyMap()
	if err != nil {
		return out, nil
	}
	for i := range out {
		s.resolveProxyURL(&out[i], proxies)
	}
	return out, nil
}

func (s *SQLiteStore) resolveProxyURL(p *Provider, proxies map[string]string) {
	if p.ProxyID == "" {
		return
	}
	if u, ok := proxies[p.ProxyID]; ok {
		p.ProxyURL = u
	} else {
		p.ProxyURL = ""
	}
}

func (s *SQLiteStore) enabledProxyMap() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT id, url FROM proxies WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var id, url string
		if err := rows.Scan(&id, &url); err != nil {
			return nil, err
		}
		m[id] = url
	}
	return m, rows.Err()
}

func (s *SQLiteStore) GetProvider(id string) (Provider, bool, error) {
	row := s.db.QueryRow(`SELECT id, type, name, base_url, api_key, access_token, refresh_token, proxy_url, proxy_id, enabled, models, keys, key_strategy, sticky_limit, created_at, updated_at FROM providers WHERE id = ?`, id)
	p, err := scanProvider(row)
	if err == sql.ErrNoRows {
		return Provider{}, false, nil
	}
	if err != nil {
		return Provider{}, false, err
	}
	if p.ProxyID != "" {
		var u string
		if err := s.db.QueryRow(`SELECT url FROM proxies WHERE id = ? AND enabled = 1`, p.ProxyID).Scan(&u); err == nil {
			p.ProxyURL = u
		} else {
			p.ProxyURL = ""
		}
	}
	return p, true, nil
}

func (s *SQLiteStore) UpsertProvider(provider Provider) error {
	provider = NormalizeProvider(provider)
	if provider.ID == "" {
		provider.ID = randomID("provider")
	}
	if provider.Name == "" {
		provider.Name = provider.ID
	}
	if provider.Type == "" {
		provider.Type = ProviderOpenAICompatible
	}
	provider.UpdatedAt = time.Now().UTC()

	var existingCreated string
	err := s.db.QueryRow(`SELECT created_at FROM providers WHERE id = ?`, provider.ID).Scan(&existingCreated)
	switch {
	case err == sql.ErrNoRows:
		provider.CreatedAt = provider.UpdatedAt
	case err != nil:
		return err
	default:
		if t, perr := time.Parse(time.RFC3339Nano, existingCreated); perr == nil {
			provider.CreatedAt = t
		} else {
			provider.CreatedAt = provider.UpdatedAt
		}
	}
	return s.insertProvider(provider)
}

func (s *SQLiteStore) insertProvider(provider Provider) error {
	if provider.CreatedAt.IsZero() {
		provider.CreatedAt = time.Now().UTC()
	}
	if provider.UpdatedAt.IsZero() {
		provider.UpdatedAt = provider.CreatedAt
	}
	provider = NormalizeProvider(provider)
	models, err := json.Marshal(provider.Models)
	if err != nil {
		return err
	}
	keys, err := json.Marshal(provider.Keys)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO providers
			(id, type, name, base_url, api_key, access_token, refresh_token, proxy_url, proxy_id, enabled, models, keys, key_strategy, sticky_limit, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				type = excluded.type, name = excluded.name, base_url = excluded.base_url,
				api_key = excluded.api_key, access_token = excluded.access_token,
				refresh_token = excluded.refresh_token, proxy_url = excluded.proxy_url, proxy_id = excluded.proxy_id,
				enabled = excluded.enabled, models = excluded.models, keys = excluded.keys,
				key_strategy = excluded.key_strategy, sticky_limit = excluded.sticky_limit,
				updated_at = excluded.updated_at`,
		provider.ID, provider.Type, provider.Name, provider.BaseURL,
		provider.APIKey, provider.AccessToken, provider.RefreshToken, provider.ProxyURL, provider.ProxyID,
		boolToInt(provider.Enabled), string(models), string(keys), provider.KeyStrategy, provider.StickyLimit,
		provider.CreatedAt.Format(time.RFC3339Nano), provider.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteStore) DeleteProvider(id string) error {
	_, err := s.db.Exec(`DELETE FROM providers WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) ListProxies() ([]Proxy, error) {
	rows, err := s.db.Query(`SELECT id, name, url, enabled, created_at, updated_at FROM proxies ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Proxy{}
	for rows.Next() {
		proxy, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, proxy)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetProxy(id string) (Proxy, bool, error) {
	row := s.db.QueryRow(`SELECT id, name, url, enabled, created_at, updated_at FROM proxies WHERE id = ?`, id)
	proxy, err := scanProxy(row)
	if err == sql.ErrNoRows {
		return Proxy{}, false, nil
	}
	if err != nil {
		return Proxy{}, false, err
	}
	return proxy, true, nil
}

func (s *SQLiteStore) UpsertProxy(proxy Proxy) error {
	proxy = NormalizeProxy(proxy)
	if proxy.ID == "" {
		proxy.ID = randomID("proxy")
	}
	if proxy.Name == "" {
		proxy.Name = proxy.ID
	}
	proxy.UpdatedAt = time.Now().UTC()

	var existingCreated string
	err := s.db.QueryRow(`SELECT created_at FROM proxies WHERE id = ?`, proxy.ID).Scan(&existingCreated)
	switch {
	case err == sql.ErrNoRows:
		proxy.CreatedAt = proxy.UpdatedAt
	case err != nil:
		return err
	default:
		if t, perr := time.Parse(time.RFC3339Nano, existingCreated); perr == nil {
			proxy.CreatedAt = t
		} else {
			proxy.CreatedAt = proxy.UpdatedAt
		}
	}
	return s.insertProxy(proxy)
}

func (s *SQLiteStore) insertProxy(proxy Proxy) error {
	if proxy.CreatedAt.IsZero() {
		proxy.CreatedAt = time.Now().UTC()
	}
	if proxy.UpdatedAt.IsZero() {
		proxy.UpdatedAt = proxy.CreatedAt
	}
	proxy = NormalizeProxy(proxy)
	_, err := s.db.Exec(`INSERT INTO proxies
				(id, name, url, enabled, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET
					name = excluded.name, url = excluded.url, enabled = excluded.enabled,
					updated_at = excluded.updated_at`,
		proxy.ID, proxy.Name, proxy.URL, boolToInt(proxy.Enabled),
		proxy.CreatedAt.Format(time.RFC3339Nano), proxy.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteStore) DeleteProxy(id string) error {
	_, err := s.db.Exec(`DELETE FROM proxies WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) AddRequestLog(log RequestLog) error {
	if log.ID == "" {
		log.ID = randomID("req")
	}
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now().UTC()
	}
	debugPayload := ""
	debugRedacted, rawPromptBytes, rawToolResultBytes, rawPromptTruncated, rawToolTruncated := 0, 0, 0, 0, 0
	if log.Debug != nil {
		if raw, err := json.Marshal(log.Debug); err == nil && (log.Debug.RawPrompt != "" || log.Debug.RawToolResult != "") {
			debugPayload = string(raw)
		}
		debugRedacted = boolToInt(log.Debug.Redacted)
		rawPromptBytes = log.Debug.RawPromptBytes
		rawToolResultBytes = log.Debug.RawToolResultBytes
		rawPromptTruncated = boolToInt(log.Debug.RawPromptTruncated)
		rawToolTruncated = boolToInt(log.Debug.RawToolTruncated)
	}
	if debugPayload != "" {
		if _, err := s.db.Exec(`INSERT INTO request_debug_payloads (request_id, payload, created_at) VALUES (?, ?, ?)
			ON CONFLICT(request_id) DO UPDATE SET payload = excluded.payload, created_at = excluded.created_at`,
			log.ID, debugPayload, log.Timestamp.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`INSERT INTO request_logs
		(id, timestamp, endpoint, provider_id, model, status, duration_ms, stream,
		 prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, optimize_duration_ms, provider_duration_ms, debug_log_duration_ms, estimated_tokens, estimated_tokens_saved, upstream_tokens_saved, upstream_optimizer_engine, upstream_optimized_parts, estimated_prompt_tokens_saved, estimated_tool_tokens_saved, cost_usd,
			 api_key_id, api_key_masked, api_key_prefix, api_key_suffix, router_name, router_role, router_complexity, router_risk, router_target, router_classifier_model, router_confidence, router_reason, router_duration_ms, router_used_fallback,
			 fusion_name, fusion_mode, fusion_expert_count, fusion_successful_experts, fusion_synthesizer_target, fusion_reviewer_target, fusion_duration_ms, fusion_used_reviewer, fusion_error, fusion_trace, debug_payload, debug_redacted,
		 raw_prompt_bytes, raw_tool_result_bytes, raw_prompt_truncated, raw_tool_truncated, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.Timestamp.Format(time.RFC3339Nano), log.Endpoint, log.ProviderID,
		log.Model, log.Status, log.DurationMS, boolToInt(log.Stream),
		log.PromptTokens, log.CompletionTokens, log.TotalTokens, log.CachedTokens,
		log.ReasoningTokens, log.OptimizeDurationMS, log.ProviderDurationMS, log.DebugLogDurationMS, boolToInt(log.EstimatedTokens), log.EstimatedTokensSaved, log.UpstreamTokensSaved, log.UpstreamOptimizerEngine, log.UpstreamOptimizedParts, log.EstimatedPromptTokensSaved, log.EstimatedToolTokensSaved, log.CostUSD,
		log.APIKeyID, log.APIKeyMasked, log.APIKeyPrefix, log.APIKeySuffix, log.RouterName, log.RouterRole, log.RouterComplexity, log.RouterRisk, log.RouterTarget, log.RouterClassifierModel, log.RouterConfidence, log.RouterReason, log.RouterDurationMS, boolToInt(log.RouterUsedFallback),
		log.FusionName, log.FusionMode, log.FusionExpertCount, log.FusionSuccessfulExperts, log.FusionSynthesizerTarget, log.FusionReviewerTarget, log.FusionDurationMS, boolToInt(log.FusionUsedReviewer), log.FusionError, log.FusionTrace, "", debugRedacted,
		rawPromptBytes, rawToolResultBytes, rawPromptTruncated, rawToolTruncated, log.Error); err != nil {
		return err
	}

	keep := 200
	if settings, err := s.GetSettings(); err == nil && settings.KeepRequestLogs > 0 {
		keep = settings.KeepRequestLogs
	}
	_, err := s.db.Exec(`DELETE FROM request_logs WHERE id NOT IN (
		SELECT id FROM request_logs ORDER BY timestamp DESC, id DESC LIMIT ?)`, keep)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM request_debug_payloads WHERE request_id NOT IN (SELECT id FROM request_logs)`)
	return err
}

func (s *SQLiteStore) RecentRequestLogs(limit int) ([]RequestLog, error) {
	query := `SELECT id, timestamp, endpoint, provider_id, model, status, duration_ms, stream,
		prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, optimize_duration_ms, provider_duration_ms, debug_log_duration_ms, estimated_tokens, estimated_tokens_saved, COALESCE(upstream_tokens_saved, 0), COALESCE(upstream_optimizer_engine, ''), COALESCE(upstream_optimized_parts, 0), estimated_prompt_tokens_saved, estimated_tool_tokens_saved, cost_usd,
		COALESCE(api_key_id, ''), COALESCE(api_key_masked, ''), COALESCE(api_key_prefix, ''), COALESCE(api_key_suffix, ''),
		COALESCE(router_name, ''), COALESCE(router_role, ''), COALESCE(router_complexity, ''), COALESCE(router_risk, ''), COALESCE(router_target, ''), COALESCE(router_classifier_model, ''), COALESCE(router_confidence, 0), COALESCE(router_reason, ''), COALESCE(router_duration_ms, 0), COALESCE(router_used_fallback, 0),
			COALESCE(fusion_name, ''), COALESCE(fusion_mode, ''), COALESCE(fusion_expert_count, 0), COALESCE(fusion_successful_experts, 0), COALESCE(fusion_synthesizer_target, ''), COALESCE(fusion_reviewer_target, ''), COALESCE(fusion_duration_ms, 0), COALESCE(fusion_used_reviewer, 0), COALESCE(fusion_error, ''), COALESCE(fusion_trace, ''),
		COALESCE(debug_redacted, 0), COALESCE(raw_prompt_bytes, 0), COALESCE(raw_tool_result_bytes, 0),
		COALESCE(raw_prompt_truncated, 0), COALESCE(raw_tool_truncated, 0), COALESCE(error, '')
		FROM request_logs ORDER BY timestamp DESC, id DESC`
	args := []any{}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RequestLog{}
	for rows.Next() {
		var (
			logItem                              RequestLog
			ts                                   string
			stream                               int
			estimate                             int
			routerUsedFallback                   int
			fusionUsedReviewer                   int
			debugRedacted                        int
			rawPromptBytes                       int
			rawToolResultBytes                   int
			rawPromptTruncated, rawToolTruncated int
		)
		if err := rows.Scan(&logItem.ID, &ts, &logItem.Endpoint, &logItem.ProviderID,
			&logItem.Model, &logItem.Status, &logItem.DurationMS, &stream,
			&logItem.PromptTokens, &logItem.CompletionTokens, &logItem.TotalTokens,
			&logItem.CachedTokens, &logItem.ReasoningTokens, &logItem.OptimizeDurationMS,
			&logItem.ProviderDurationMS, &logItem.DebugLogDurationMS, &estimate, &logItem.EstimatedTokensSaved,
			&logItem.UpstreamTokensSaved, &logItem.UpstreamOptimizerEngine, &logItem.UpstreamOptimizedParts,
			&logItem.EstimatedPromptTokensSaved, &logItem.EstimatedToolTokensSaved, &logItem.CostUSD,
			&logItem.APIKeyID, &logItem.APIKeyMasked, &logItem.APIKeyPrefix, &logItem.APIKeySuffix,
			&logItem.RouterName, &logItem.RouterRole, &logItem.RouterComplexity, &logItem.RouterRisk, &logItem.RouterTarget, &logItem.RouterClassifierModel, &logItem.RouterConfidence, &logItem.RouterReason, &logItem.RouterDurationMS, &routerUsedFallback,
			&logItem.FusionName, &logItem.FusionMode, &logItem.FusionExpertCount, &logItem.FusionSuccessfulExperts, &logItem.FusionSynthesizerTarget, &logItem.FusionReviewerTarget, &logItem.FusionDurationMS, &fusionUsedReviewer, &logItem.FusionError, &logItem.FusionTrace,
			&debugRedacted, &rawPromptBytes, &rawToolResultBytes, &rawPromptTruncated, &rawToolTruncated, &logItem.Error); err != nil {
			return nil, err
		}
		if rawPromptBytes > 0 || rawToolResultBytes > 0 || logItem.EstimatedPromptTokensSaved > 0 || logItem.EstimatedToolTokensSaved > 0 {
			logItem.Debug = &RequestLogDebugPayload{
				RawPromptBytes:             rawPromptBytes,
				RawToolResultBytes:         rawToolResultBytes,
				RawPromptTruncated:         rawPromptTruncated != 0,
				RawToolTruncated:           rawToolTruncated != 0,
				EstimatedPromptTokensSaved: logItem.EstimatedPromptTokensSaved,
				EstimatedToolTokensSaved:   logItem.EstimatedToolTokensSaved,
				CompactPromptApplied:       logItem.EstimatedPromptTokensSaved > 0,
				CompactToolApplied:         logItem.EstimatedToolTokensSaved > 0,
				Redacted:                   debugRedacted != 0,
			}
		}
		if t, perr := time.Parse(time.RFC3339Nano, ts); perr == nil {
			logItem.Timestamp = t
		}
		logItem.Stream = stream != 0
		logItem.EstimatedTokens = estimate != 0
		logItem.RouterUsedFallback = routerUsedFallback != 0
		logItem.FusionUsedReviewer = fusionUsedReviewer != 0
		logItem.FusionUsedReviewer = fusionUsedReviewer != 0
		logItem = HydrateRequestLogMetrics(logItem)
		out = append(out, logItem)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) migrateDebugPayloads() error {
	rows, err := s.db.Query(`SELECT id, timestamp, COALESCE(debug_payload, '') FROM request_logs WHERE COALESCE(debug_payload, '') != ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type item struct{ id, ts, payload string }
	items := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.ts, &it.payload); err != nil {
			return err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, it := range items {
		if _, err := s.db.Exec(`INSERT INTO request_debug_payloads (request_id, payload, created_at) VALUES (?, ?, ?)
			ON CONFLICT(request_id) DO UPDATE SET payload = excluded.payload, created_at = excluded.created_at`, it.id, it.payload, it.ts); err != nil {
			return err
		}
	}
	if len(items) > 0 {
		_, err = s.db.Exec(`UPDATE request_logs SET debug_payload = '' WHERE COALESCE(debug_payload, '') != ''`)
	}
	return err
}

func (s *SQLiteStore) GetRequestDebugPayload(id string) (*RequestLogDebugPayload, bool, error) {
	var payloadJSON string
	err := s.db.QueryRow(`SELECT payload FROM request_debug_payloads WHERE request_id = ?`, id).Scan(&payloadJSON)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if payloadJSON == "" {
		return nil, false, nil
	}
	var payload RequestLogDebugPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return nil, false, err
	}
	return &payload, true, nil
}

func (s *SQLiteStore) DeleteRequestDebugPayloads() (int, error) {
	result, err := s.db.Exec(`DELETE FROM request_debug_payloads`)
	if err != nil {
		return 0, err
	}
	if _, err := s.db.Exec(`UPDATE request_logs SET debug_payload = '', debug_redacted = 0, raw_prompt_bytes = 0, raw_tool_result_bytes = 0, raw_prompt_truncated = 0, raw_tool_truncated = 0`); err != nil {
		return 0, err
	}
	rows, _ := result.RowsAffected()
	return int(rows), nil
}

func (s *SQLiteStore) ResetAllData() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, stmt := range []string{
		`DELETE FROM request_debug_payloads`,
		`DELETE FROM request_logs`,
		`DELETE FROM providers`,
		`DELETE FROM proxies`,
		`DELETE FROM settings`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := s.SaveSettings(DefaultSettings()); err != nil {
		return err
	}
	for _, provider := range SeedProviders() {
		if err := s.insertProvider(provider); err != nil {
			return err
		}
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProvider(row rowScanner) (Provider, error) {
	var (
		p          Provider
		enabled    int
		modelsJSON string
		keysJSON   string
		created    string
		updated    string
	)
	if err := row.Scan(&p.ID, &p.Type, &p.Name, &p.BaseURL, &p.APIKey, &p.AccessToken,
		&p.RefreshToken, &p.ProxyURL, &p.ProxyID, &enabled, &modelsJSON, &keysJSON, &p.KeyStrategy, &p.StickyLimit, &created, &updated); err != nil {
		return Provider{}, err
	}
	p.Enabled = enabled != 0
	if modelsJSON != "" {
		if err := json.Unmarshal([]byte(modelsJSON), &p.Models); err != nil {
			return Provider{}, fmt.Errorf("decode models for provider %s: %w", p.ID, err)
		}
	}
	if p.Models == nil {
		p.Models = []string{}
	}
	if keysJSON != "" {
		if err := json.Unmarshal([]byte(keysJSON), &p.Keys); err != nil {
			return Provider{}, fmt.Errorf("decode keys for provider %s: %w", p.ID, err)
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
		p.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updated); err == nil {
		p.UpdatedAt = t
	}
	return NormalizeProvider(p), nil
}

func scanProxy(row rowScanner) (Proxy, error) {
	var (
		p       Proxy
		enabled int
		created string
		updated string
	)
	if err := row.Scan(&p.ID, &p.Name, &p.URL, &enabled, &created, &updated); err != nil {
		return Proxy{}, err
	}
	p.Enabled = enabled != 0
	if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
		p.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updated); err == nil {
		p.UpdatedAt = t
	}
	return NormalizeProxy(p), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
