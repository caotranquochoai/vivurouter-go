package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type databaseFile struct {
	Settings    Settings     `json:"settings"`
	Providers   []Provider   `json:"providers"`
	Proxies     []Proxy      `json:"proxies,omitempty"`
	RequestLogs []RequestLog `json:"request_logs,omitempty"`
}

type requestLogsFile struct {
	RequestLogs []RequestLog `json:"request_logs"`
}

// FileStore persists sample state in a JSON file. It keeps the demo dependency-free.
type FileStore struct {
	mu       sync.RWMutex
	path     string
	logsPath string
	dataDir  string
	debugDir string
	db       databaseFile
}

func NewFileStore(dataDir string) (*FileStore, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	fs := &FileStore{path: filepath.Join(dataDir, "sample-db.json"), logsPath: filepath.Join(dataDir, "request-logs.json"), dataDir: dataDir, debugDir: filepath.Join(dataDir, "debug-payloads")}
	if err := fs.load(); err != nil {
		return nil, err
	}
	return fs, nil
}

func (s *FileStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.db.Settings = DefaultSettings()
	if raw, err := os.ReadFile(s.path); err == nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &s.db); err != nil {
			return err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	legacyLogs := append([]RequestLog(nil), s.db.RequestLogs...)
	if raw, err := os.ReadFile(s.logsPath); err == nil && len(raw) > 0 {
		var logs requestLogsFile
		if err := json.Unmarshal(raw, &logs); err != nil {
			return err
		}
		s.db.RequestLogs = logs.RequestLogs
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		s.db.RequestLogs = legacyLogs
	}

	if s.db.Settings.KeepRequestLogs <= 0 {
		s.db.Settings.KeepRequestLogs = 200
	}
	s.db.Settings.APIKeys = NormalizeAPIKeyPolicies(s.db.Settings.APIKeys)
	s.db.Settings.ModelPrices = NormalizeModelPriceRules(s.db.Settings.ModelPrices)
	s.db.Settings.Combos = NormalizeCombos(s.db.Settings.Combos)
	s.db.Settings.PromptRouters = NormalizePromptRouters(s.db.Settings.PromptRouters)
	s.db.Settings.Fusions = NormalizeFusions(s.db.Settings.Fusions)
	NormalizeBudgetSettings(&s.db.Settings)
	NormalizeDebugSettings(&s.db.Settings)
	NormalizeTokenOptimizationSettings(&s.db.Settings)
	if len(s.db.Providers) == 0 {
		s.db.Providers = NormalizeProviders(SeedProviders())
	} else {
		s.db.Providers = NormalizeProviders(s.db.Providers)
	}
	s.db.Proxies = NormalizeProxies(s.db.Proxies)
	if err := s.migrateDebugPayloadsLocked(); err != nil {
		return err
	}
	if err := s.saveLocked(); err != nil {
		return err
	}
	return s.saveRequestLogsLocked()
}

func (s *FileStore) GetSettings() (Settings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.Settings, nil
}

func (s *FileStore) SaveSettings(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.db.Settings = settings
	return s.saveLocked()
}

func (s *FileStore) ListProviders() ([]Provider, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := cloneProviders(s.db.Providers)
	for i := range items {
		s.resolveProxyURL(&items[i])
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *FileStore) resolveProxyURL(p *Provider) {
	if p.ProxyID == "" {
		return
	}
	for _, px := range s.db.Proxies {
		if px.ID == p.ProxyID && px.Enabled {
			p.ProxyURL = px.URL
			return
		}
	}
	p.ProxyURL = ""
}

func (s *FileStore) GetProvider(id string) (Provider, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, provider := range s.db.Providers {
		if provider.ID == id {
			p := cloneProvider(provider)
			s.resolveProxyURL(&p)
			return p, true, nil
		}
	}
	return Provider{}, false, nil
}

func (s *FileStore) UpsertProvider(provider Provider) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	for i := range s.db.Providers {
		if s.db.Providers[i].ID == provider.ID {
			provider.CreatedAt = s.db.Providers[i].CreatedAt
			if provider.CreatedAt.IsZero() {
				provider.CreatedAt = provider.UpdatedAt
			}
			s.db.Providers[i] = provider
			return s.saveLocked()
		}
	}
	provider.CreatedAt = provider.UpdatedAt
	s.db.Providers = append(s.db.Providers, provider)
	return s.saveLocked()
}

func (s *FileStore) DeleteProvider(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.db.Providers {
		if s.db.Providers[i].ID == id {
			s.db.Providers = append(s.db.Providers[:i], s.db.Providers[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

func (s *FileStore) ListProxies() ([]Proxy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := cloneProxies(s.db.Proxies)
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *FileStore) GetProxy(id string) (Proxy, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, proxy := range s.db.Proxies {
		if proxy.ID == id {
			return cloneProxy(proxy), true, nil
		}
	}
	return Proxy{}, false, nil
}

func (s *FileStore) UpsertProxy(proxy Proxy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	proxy = NormalizeProxy(proxy)
	if proxy.ID == "" {
		proxy.ID = randomID("proxy")
	}
	if proxy.Name == "" {
		proxy.Name = proxy.ID
	}
	proxy.UpdatedAt = time.Now().UTC()

	for i := range s.db.Proxies {
		if s.db.Proxies[i].ID == proxy.ID {
			proxy.CreatedAt = s.db.Proxies[i].CreatedAt
			if proxy.CreatedAt.IsZero() {
				proxy.CreatedAt = proxy.UpdatedAt
			}
			s.db.Proxies[i] = proxy
			return s.saveLocked()
		}
	}
	proxy.CreatedAt = proxy.UpdatedAt
	s.db.Proxies = append(s.db.Proxies, proxy)
	return s.saveLocked()
}

func (s *FileStore) DeleteProxy(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.db.Proxies {
		if s.db.Proxies[i].ID == id {
			s.db.Proxies = append(s.db.Proxies[:i], s.db.Proxies[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

func (s *FileStore) AddRequestLog(log RequestLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if log.ID == "" {
		log.ID = randomID("req")
	}
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now().UTC()
	}
	if log.Debug != nil {
		if err := s.saveDebugPayloadLocked(log.ID, log.Debug); err != nil {
			return err
		}
		log = StripRequestDebugPayload(log)
	}
	s.db.RequestLogs = append([]RequestLog{log}, s.db.RequestLogs...)
	keep := s.db.Settings.KeepRequestLogs
	if keep <= 0 {
		keep = 200
	}
	if len(s.db.RequestLogs) > keep {
		s.db.RequestLogs = s.db.RequestLogs[:keep]
	}
	return s.saveRequestLogsLocked()
}

func (s *FileStore) RecentRequestLogs(limit int) ([]RequestLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.db.RequestLogs) {
		limit = len(s.db.RequestLogs)
	}
	out := make([]RequestLog, limit)
	for i := 0; i < limit; i++ {
		out[i] = StripRequestDebugPayload(s.db.RequestLogs[i])
	}
	return out, nil
}

func (s *FileStore) GetRequestDebugPayload(id string) (*RequestLogDebugPayload, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := s.debugPayloadPath(id)
	raw, err := os.ReadFile(path)
	if err == nil {
		var payload RequestLogDebugPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, false, err
		}
		return &payload, true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	for _, log := range s.db.RequestLogs {
		if log.ID == id {
			return CloneRequestDebugPayload(log.Debug), log.Debug != nil, nil
		}
	}
	return nil, false, nil
}

func (s *FileStore) migrateDebugPayloadsLocked() error {
	changed := false
	for i := range s.db.RequestLogs {
		if strings.TrimSpace(s.db.RequestLogs[i].ID) == "" {
			s.db.RequestLogs[i].ID = randomID("req")
			changed = true
		}
		debug := s.db.RequestLogs[i].Debug
		if debug == nil || (debug.RawPrompt == "" && debug.RawToolResult == "") {
			continue
		}
		if err := s.saveDebugPayloadLocked(s.db.RequestLogs[i].ID, debug); err != nil {
			return err
		}
		s.db.RequestLogs[i] = StripRequestDebugPayload(s.db.RequestLogs[i])
		changed = true
	}
	if changed {
		return s.saveRequestLogsLocked()
	}
	return nil
}

func (s *FileStore) DeleteRequestDebugPayloads() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := 0
	entries, err := os.ReadDir(s.debugDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
				continue
			}
			if err := os.Remove(filepath.Join(s.debugDir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				return deleted, err
			}
			deleted++
		}
	}
	changed := false
	for i := range s.db.RequestLogs {
		if s.db.RequestLogs[i].Debug != nil {
			s.db.RequestLogs[i].Debug = nil
			changed = true
		}
	}
	if changed {
		if err := s.saveRequestLogsLocked(); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func (s *FileStore) ResetAllData() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.RemoveAll(s.debugDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.db = databaseFile{
		Settings:    DefaultSettings(),
		Providers:   SeedProviders(),
		RequestLogs: []RequestLog{},
	}
	if err := s.saveLocked(); err != nil {
		return err
	}
	return s.saveRequestLogsLocked()
}

func (s *FileStore) saveDebugPayloadLocked(id string, payload *RequestLogDebugPayload) error {
	if id == "" || payload == nil || (payload.RawPrompt == "" && payload.RawToolResult == "") {
		return nil
	}
	if err := os.MkdirAll(s.debugDir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.debugPayloadPath(id) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.debugPayloadPath(id))
}

func (s *FileStore) debugPayloadPath(id string) string {
	id = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, strings.TrimSpace(id))
	return filepath.Join(s.debugDir, id+".json")
}

func (s *FileStore) saveLocked() error {
	main := databaseFile{
		Settings:  s.db.Settings,
		Providers: cloneProviders(s.db.Providers),
		Proxies:   cloneProxies(s.db.Proxies),
	}
	raw, err := json.MarshalIndent(main, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *FileStore) saveRequestLogsLocked() error {
	raw, err := json.MarshalIndent(requestLogsFile{RequestLogs: s.db.RequestLogs}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.logsPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.logsPath)
}

func cloneProviders(items []Provider) []Provider {
	out := make([]Provider, len(items))
	for i, item := range items {
		out[i] = cloneProvider(item)
	}
	return out
}

func cloneProvider(provider Provider) Provider {
	provider.Models = append([]string(nil), provider.Models...)
	provider.Keys = append([]ProviderKey(nil), provider.Keys...)
	return provider
}

func cloneProxies(items []Proxy) []Proxy {
	out := make([]Proxy, len(items))
	for i, item := range items {
		out[i] = cloneProxy(item)
	}
	return out
}

func cloneProxy(proxy Proxy) Proxy {
	return proxy
}

func randomID(prefix string) string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return prefix + "-" + time.Now().UTC().Format("20060102150405")
	}
	return prefix + "-" + hex.EncodeToString(buf)
}
