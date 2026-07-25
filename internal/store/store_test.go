package store

import (
	"sync"
	"testing"
	"time"
)

// storeFactory builds a fresh store rooted at a temp dir.
type storeFactory struct {
	name string
	open func(dir string) (Store, func(), error)
}

func factories() []storeFactory {
	return []storeFactory{
		{
			name: "file",
			open: func(dir string) (Store, func(), error) {
				s, err := NewFileStore(dir)
				return s, func() {}, err
			},
		},
		{
			name: "sqlite",
			open: func(dir string) (Store, func(), error) {
				s, err := NewSQLiteStore(dir)
				if err != nil {
					return nil, func() {}, err
				}
				return s, func() { _ = s.Close() }, nil
			},
		},
	}
}

func TestStoreCRUD(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			st, closeFn, err := f.open(t.TempDir())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer closeFn()

			// Seeded providers should be present.
			providers, err := st.ListProviders()
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(providers) < 2 {
				t.Fatalf("seeded providers = %d, want >= 2", len(providers))
			}

			// Upsert (create).
			if err := st.UpsertProvider(Provider{ID: "extra", Type: ProviderOpenAICompatible, BaseURL: "https://x/", Models: []string{"m1", "m1", " m2 "}}); err != nil {
				t.Fatalf("upsert create: %v", err)
			}
			got, found, err := st.GetProvider("extra")
			if err != nil || !found {
				t.Fatalf("get extra: found=%v err=%v", found, err)
			}
			if got.BaseURL != "https://x" {
				t.Fatalf("base url = %q, want trailing slash trimmed", got.BaseURL)
			}
			if len(got.Models) != 2 || got.Models[0] != "m1" || got.Models[1] != "m2" {
				t.Fatalf("models = %v, want [m1 m2] normalized", got.Models)
			}
			if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
				t.Fatal("timestamps should be set")
			}

			// Upsert (update) preserves CreatedAt.
			created := got.CreatedAt
			if err := st.UpsertProvider(Provider{ID: "extra", Type: ProviderCodex, Name: "Renamed", Models: []string{"m3"}, Enabled: true}); err != nil {
				t.Fatalf("upsert update: %v", err)
			}
			got2, _, _ := st.GetProvider("extra")
			if got2.Name != "Renamed" || got2.Type != ProviderCodex {
				t.Fatalf("update not applied: %+v", got2)
			}
			if !got2.CreatedAt.Equal(created) {
				t.Fatalf("CreatedAt changed on update: %v -> %v", created, got2.CreatedAt)
			}

			// Delete.
			if err := st.DeleteProvider("extra"); err != nil {
				t.Fatalf("delete: %v", err)
			}
			if _, found, _ := st.GetProvider("extra"); found {
				t.Fatal("provider should be gone after delete")
			}
		})
	}
}

func TestStoreSettings(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			st, closeFn, err := f.open(t.TempDir())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer closeFn()

			settings, err := st.GetSettings()
			if err != nil {
				t.Fatalf("get settings: %v", err)
			}
			settings.DefaultProvider = "custom"
			settings.KeepRequestLogs = 7
			settings.TokenOptimizeToolResults = true
			settings.TokenOptimizeSystem = true
			settings.TokenOptimizeDeveloper = true
			settings.TokenOptimizeText = true
			settings.TokenOptimizeToolSchemas = true
			settings.TokenOptimizeToolCalls = true
			settings.TokenOptimizeMinChars = 10
			settings.TokenOptimizeMaxChars = 20
			settings.RTKEnabled = true
			settings.RTKPath = " ./rtk.exe "
			settings.PromptRouterCompressionMode = PromptRouterCompressionPerType
			settings.PromptRouterCompressToolSchemas = false
			settings.PromptRouterCompressToolResults = false
			settings.Combos = []Combo{{Name: "fast", Models: []string{" openai/gpt-4.1 ", "openai/gpt-4.1"}, Strategy: "round-robin", StickyLimit: 2, Enabled: true, ContextLength: 999}}
			settings.PromptRouters = []PromptRouter{{Name: "auto", Enabled: true, ClassifierModel: "openai/gpt-4.1-mini", FallbackTarget: "openai/gpt-4.1", FallbackRole: "planner", ClassifierPromptTemplate: "Pick {{roles}}. {{json_schema}}", Routes: []PromptRoute{{Role: "planner", Target: "openai/gpt-4.1"}}}}
			if err := st.SaveSettings(settings); err != nil {
				t.Fatalf("save settings: %v", err)
			}
			got, _ := st.GetSettings()
			if got.DefaultProvider != "custom" {
				t.Fatalf("default provider = %q, want custom", got.DefaultProvider)
			}
			if got.KeepRequestLogs != 7 {
				t.Fatalf("keep logs = %d, want 7", got.KeepRequestLogs)
			}
			if len(got.Combos) != 1 || got.Combos[0].Name != "fast" || got.Combos[0].Strategy != "round-robin" || got.Combos[0].StickyLimit != 2 || len(got.Combos[0].Models) != 1 {
				t.Fatalf("combos not normalized/persisted: %+v", got.Combos)
			}
			if len(got.PromptRouters) != 1 || got.PromptRouters[0].ClassifierPromptTemplate != "Pick {{roles}}. {{json_schema}}" {
				t.Fatalf("prompt router classifier template not persisted: %+v", got.PromptRouters)
			}
			if !got.TokenOptimizeToolResults || !got.TokenOptimizeSystem || !got.TokenOptimizeDeveloper || !got.TokenOptimizeText || !got.TokenOptimizeToolSchemas || !got.TokenOptimizeToolCalls || got.TokenOptimizeMinChars != 1000 || got.TokenOptimizeMaxChars != 2000 || !got.RTKEnabled || got.RTKPath != "./rtk.exe" {
				t.Fatalf("token optimization settings not normalized/persisted: %+v", got)
			}
			if got.PromptRouterCompressionMode != PromptRouterCompressionPerType || got.PromptRouterCompressToolSchemas || got.PromptRouterCompressToolResults {
				t.Fatalf("prompt router compression settings not persisted: %+v", got)
			}
		})
	}
}

func TestStoreRequestLogRouterFields(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			st, closeFn, err := f.open(t.TempDir())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer closeFn()

			if err := st.AddRequestLog(RequestLog{
				Endpoint:              "/v1/chat/completions",
				ProviderID:            "openai",
				Model:                 "gpt-test",
				Status:                "200",
				RouterName:            "auto",
				RouterRole:            "planner",
				RouterComplexity:      "high",
				RouterRisk:            "medium",
				RouterTarget:          "openai/gpt-4.1",
				RouterClassifierModel: "openai/gpt-4.1-mini",
				RouterConfidence:      0.82,
				RouterReason:          "matched planning prompt",
				RouterDurationMS:      123,
				RouterUsedFallback:    true,
			}); err != nil {
				t.Fatalf("add log: %v", err)
			}

			logs, err := st.RecentRequestLogs(1)
			if err != nil {
				t.Fatalf("recent logs: %v", err)
			}
			if len(logs) != 1 {
				t.Fatalf("logs = %d, want 1", len(logs))
			}
			got := logs[0]
			if got.RouterName != "auto" || got.RouterRole != "planner" || got.RouterComplexity != "high" || got.RouterRisk != "medium" || got.RouterTarget != "openai/gpt-4.1" || got.RouterClassifierModel != "openai/gpt-4.1-mini" || got.RouterConfidence != 0.82 || got.RouterReason != "matched planning prompt" || got.RouterDurationMS != 123 || !got.RouterUsedFallback {
				t.Fatalf("router fields were not persisted: %+v", got)
			}
		})
	}
}

func TestStoreGuardrailSettings(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			st, closeFn, err := f.open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer closeFn()
			settings, err := st.GetSettings()
			if err != nil {
				t.Fatal(err)
			}
			settings.Guardrails = []Guardrail{{Name: "guard", Enabled: true, OptimizerEnabled: true, OptimizerTarget: "cheap/model", MainTarget: "main/model", ValidatorTarget: "cheap/model", CustomPolicy: "no secrets"}}
			if err := st.SaveSettings(settings); err != nil {
				t.Fatal(err)
			}
			got, err := st.GetSettings()
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Guardrails) != 1 || got.Guardrails[0].Name != "guard" || got.Guardrails[0].MaxBufferedBytes == 0 {
				t.Fatalf("guardrails = %#v", got.Guardrails)
			}
		})
	}
}

func TestNormalizeGuardrailsIndependentStageToggles(t *testing.T) {
	items := NormalizeGuardrails([]Guardrail{
		{Name: "legacy", MainTarget: "main", ValidatorTarget: "check"},
		{Name: "optimize-only", SchemaVersion: 1, OptimizerEnabled: true, ValidatorEnabled: false, OptimizerTarget: "cheap", MainTarget: "main"},
		{Name: "validate-only", SchemaVersion: 1, OptimizerEnabled: false, ValidatorEnabled: true, MainTarget: "main", ValidatorTarget: "check"},
		{Name: "main-only", SchemaVersion: 1, MainTarget: "main"},
	})
	if len(items) != 4 {
		t.Fatalf("len = %d, want 4", len(items))
	}
	if !items[0].ValidatorEnabled {
		t.Fatal("legacy guardrail did not retain validator behavior")
	}
	if items[1].ValidatorEnabled || !items[1].OptimizerEnabled {
		t.Fatalf("optimize-only toggles = %#v", items[1])
	}
	if !items[2].ValidatorEnabled || items[2].OptimizerEnabled {
		t.Fatalf("validate-only toggles = %#v", items[2])
	}
	if items[3].ValidatorEnabled || items[3].OptimizerEnabled {
		t.Fatalf("main-only toggles = %#v", items[3])
	}
}

func TestNormalizeGuardrails(t *testing.T) {
	items := []Guardrail{
		{Name: " guard ", Enabled: true, OptimizerEnabled: true, OptimizerTarget: " cheap ", MainTarget: " main ", ValidatorTarget: " validate ", PolicyPresets: []string{"SAFETY", "unknown", "safety"}},
		{Name: "guard", MainTarget: "other", ValidatorTarget: "validate"},
		{Name: "self", MainTarget: "self", ValidatorTarget: "validate"},
	}
	got := NormalizeGuardrails(items)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	item := got[0]
	if item.Name != "guard" || item.OptimizerTarget != "cheap" || item.MainTarget != "main" || item.ValidatorTarget != "validate" {
		t.Fatalf("unexpected normalized guardrail: %#v", item)
	}
	if item.ResponseMode != GuardrailResponseStream || !item.OptimizerFailOpen || !item.ValidatorFailOpen {
		t.Fatalf("defaults not applied: %#v", item)
	}
	if len(item.PolicyPresets) != 1 || item.PolicyPresets[0] != "safety" {
		t.Fatalf("policies = %#v", item.PolicyPresets)
	}
}

func TestNormalizeFusions(t *testing.T) {
	items := []Fusion{
		{Name: "dup", Experts: []FusionExpert{{Name: "a", Target: "m"}}},
		{Name: "dup", Experts: []FusionExpert{{Name: "b", Target: "m"}}},
		{Name: "", Experts: []FusionExpert{{Name: "c", Target: "m"}}},
		{Name: "self", Experts: []FusionExpert{{Name: "c", Target: "self"}}},
		{Name: "ok", Mode: "sequential", TimeoutMS: 5000, MinSuccessfulExperts: 2, MaxOutputTokens: -1, Experts: []FusionExpert{{Name: "a", Target: "m", Weight: -1}}},
	}
	got := NormalizeFusions(items)
	if len(got) != 3 {
		t.Fatalf("normalize len = %d, want 3: %+v", len(got), got)
	}
	if got[0].Name != "dup" || got[0].Mode != "parallel" || got[0].TimeoutMS != 120000 {
		t.Fatalf("default fusion not normalized: %+v", got[0])
	}
	if got[1].Name != "self" || len(got[1].Experts) != 0 {
		t.Fatalf("self-reference not cleaned: %+v", got[1])
	}
	if got[2].Name != "ok" || got[2].Mode != "sequential" || got[2].TimeoutMS != 5000 || got[2].MinSuccessfulExperts != 1 || got[2].MaxOutputTokens != 0 || len(got[2].Experts) != 1 || got[2].Experts[0].Weight != 0 {
		t.Fatalf("custom fusion not normalized: %+v", got[2])
	}
}

func TestStoreFusionSettings(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			st, closeFn, err := f.open(t.TempDir())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer closeFn()
			settings, _ := st.GetSettings()
			settings.Fusions = []Fusion{
				{Name: "review", Enabled: true, Mode: "sequential", TimeoutMS: 60000, MinSuccessfulExperts: 1, MaxOutputTokens: 2048, SynthesizerTarget: "gpt-4.1", ReviewerTarget: "claude-sonnet", RequireReviewer: true, Experts: []FusionExpert{{Name: "sec", Target: "claude-sonnet", Role: "security", PromptTemplate: "be careful", Enabled: true, Weight: 2}, {Name: "perf", Target: "gpt-4.1", Role: "performance", Enabled: false, Weight: 1}}},
			}
			if err := st.SaveSettings(settings); err != nil {
				t.Fatalf("save: %v", err)
			}
			got, _ := st.GetSettings()
			if len(got.Fusions) != 1 {
				t.Fatalf("fusions len = %d, want 1: %+v", len(got.Fusions), got.Fusions)
			}
			fusion := got.Fusions[0]
			if fusion.Name != "review" || !fusion.Enabled || fusion.Mode != "sequential" || fusion.TimeoutMS != 60000 || fusion.MinSuccessfulExperts != 1 || fusion.MaxOutputTokens != 2048 || fusion.SynthesizerTarget != "gpt-4.1" || fusion.ReviewerTarget != "claude-sonnet" || !fusion.RequireReviewer || len(fusion.Experts) != 2 || fusion.Experts[0].Name != "sec" || fusion.Experts[0].Weight != 2 || fusion.Experts[1].Name != "perf" || fusion.Experts[1].Enabled {
				t.Fatalf("fusion not persisted/normalized: %+v", fusion)
			}
		})
	}
}

func TestStoreProviderKeysRoundTrip(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			st, closeFn, err := f.open(t.TempDir())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer closeFn()

			if err := st.UpsertProvider(Provider{
				ID:          "multi",
				Type:        ProviderOpenAICompatible,
				Name:        "Multi Key",
				BaseURL:     "https://x/",
				APIKey:      "sk-legacy",
				KeyStrategy: ProviderKeyStrategyRoundRobin,
				StickyLimit: 3,
				Keys: []ProviderKey{
					{ID: "k1", Name: "Primary", Key: "sk-one", Enabled: true, Priority: 1},
					{ID: "k2", Name: "Backup", Key: "sk-two", Enabled: true, Priority: 2},
				},
				Models: []string{"gpt-4o-mini"},
			}); err != nil {
				t.Fatalf("upsert: %v", err)
			}
			got, found, err := st.GetProvider("multi")
			if err != nil || !found {
				t.Fatalf("get multi: found=%v err=%v", found, err)
			}
			if len(got.Keys) != 2 {
				t.Fatalf("keys = %d, want 2: %+v", len(got.Keys), got.Keys)
			}
			if got.Keys[0].Key != "sk-one" || got.Keys[1].Key != "sk-two" {
				t.Fatalf("key values not persisted: %+v", got.Keys)
			}
			if got.KeyStrategy != ProviderKeyStrategyRoundRobin || got.StickyLimit != 3 {
				t.Fatalf("strategy/sticky not persisted: %q/%d", got.KeyStrategy, got.StickyLimit)
			}
			if got.APIKey != "sk-legacy" {
				t.Fatalf("legacy api_key not preserved: %q", got.APIKey)
			}
		})
	}
}

func TestStoreProviderLegacyAPIKeyMigratesToKeys(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			st, closeFn, err := f.open(t.TempDir())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer closeFn()

			// Single-key provider: no Keys slice, only legacy APIKey.
			if err := st.UpsertProvider(Provider{
				ID:      "legacy",
				Type:    ProviderOpenAICompatible,
				Name:    "Legacy",
				BaseURL: "https://x/",
				APIKey:  "sk-legacy-only",
				Models:  []string{"m1"},
			}); err != nil {
				t.Fatalf("upsert: %v", err)
			}
			got, found, err := st.GetProvider("legacy")
			if err != nil || !found {
				t.Fatalf("get legacy: found=%v err=%v", found, err)
			}
			if len(got.Keys) != 1 {
				t.Fatalf("expected 1 auto-migrated key, got %d: %+v", len(got.Keys), got.Keys)
			}
			if got.Keys[0].Key != "sk-legacy-only" || !got.Keys[0].Enabled {
				t.Fatalf("auto-migrated key wrong: %+v", got.Keys[0])
			}
			if got.KeyStrategy != ProviderKeyStrategyFillFirst {
				t.Fatalf("default strategy = %q, want fill-first", got.KeyStrategy)
			}
		})
	}
}

func TestStoreProxyCRUD(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			st, closeFn, err := f.open(t.TempDir())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer closeFn()

			if err := st.UpsertProxy(Proxy{
				ID:      "pool-main",
				Name:    "Pool Main",
				URL:     "http://user:pass@127.0.0.1:8080",
				Enabled: true,
			}); err != nil {
				t.Fatalf("upsert proxy: %v", err)
			}

			proxies, err := st.ListProxies()
			if err != nil {
				t.Fatalf("list proxies: %v", err)
			}
			if len(proxies) != 1 {
				t.Fatalf("proxy len = %d, want 1: %+v", len(proxies), proxies)
			}
			if proxies[0].ID != "pool-main" || proxies[0].Name != "Pool Main" || proxies[0].URL != "http://user:pass@127.0.0.1:8080" || !proxies[0].Enabled {
				t.Fatalf("proxy not persisted: %+v", proxies[0])
			}

			got, found, err := st.GetProxy("pool-main")
			if err != nil || !found {
				t.Fatalf("get proxy: found=%v err=%v", found, err)
			}
			created := got.CreatedAt
			if created.IsZero() || got.UpdatedAt.IsZero() {
				t.Fatalf("proxy timestamps missing: %+v", got)
			}

			got.Name = "Pool Main Updated"
			got.URL = "socks5://127.0.0.1:9050"
			got.Enabled = false
			if err := st.UpsertProxy(got); err != nil {
				t.Fatalf("update proxy: %v", err)
			}

			got2, found, err := st.GetProxy("pool-main")
			if err != nil || !found {
				t.Fatalf("get updated proxy: found=%v err=%v", found, err)
			}
			if got2.Name != "Pool Main Updated" || got2.URL != "socks5://127.0.0.1:9050" || got2.Enabled {
				t.Fatalf("proxy update not persisted: %+v", got2)
			}
			if !got2.CreatedAt.Equal(created) {
				t.Fatalf("proxy CreatedAt changed on update: %v -> %v", created, got2.CreatedAt)
			}

			if err := st.DeleteProxy("pool-main"); err != nil {
				t.Fatalf("delete proxy: %v", err)
			}
			if _, found, _ := st.GetProxy("pool-main"); found {
				t.Fatal("proxy should be gone after delete")
			}
		})
	}
}

func TestSQLiteListProviderAccountsWithProxyReturnsPromptly(t *testing.T) {
	st, err := NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertProvider(Provider{ID: "accounts-timeout", Type: ProviderOpenAICompatible, BaseURL: "https://example.test", Enabled: true}); err != nil {
		_ = st.Close()
		t.Fatal(err)
	}
	if err := st.UpsertProxy(Proxy{ID: "pool-timeout", Name: "Pool", URL: "http://proxy.test:8080", Enabled: true}); err != nil {
		_ = st.Close()
		t.Fatal(err)
	}
	if err := st.UpsertProviderAccount(ProviderAccount{ID: "acc-timeout", ProviderID: "accounts-timeout", ProxyID: "pool-timeout", Enabled: true}); err != nil {
		_ = st.Close()
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := st.ListProviderAccounts("accounts-timeout")
		done <- err
	}()
	select {
	case err := <-done:
		if closeErr := st.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			t.Fatalf("list provider accounts: %v", err)
		}
	case <-time.After(2 * time.Second):
		// Do not close a store blocked waiting for its own only connection.
		t.Fatal("ListProviderAccounts blocked while resolving a proxy")
	}
}

func TestStoreRecordAPIKeyUsageConcurrent(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			st, closeFn, err := f.open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer closeFn()
			settings, err := st.GetSettings()
			if err != nil {
				t.Fatal(err)
			}
			settings.DashboardMessage = "preserve-me"
			settings.APIKeys = []APIKeyPolicy{{ID: "usage-key", Key: "secret", Enabled: true, MaxRequests: 10000}}
			if err := st.SaveSettings(settings); err != nil {
				t.Fatal(err)
			}

			writers, increments := 1, 10
			if f.name == "sqlite" {
				writers, increments = 12, 5
			}
			var wg sync.WaitGroup
			for i := 0; i < writers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < increments; j++ {
						if err := st.RecordAPIKeyUsage("usage-key", APIKeyUsageDelta{Requests: 1, Tokens: 3, CostUSD: 0.25}); err != nil {
							t.Errorf("record usage: %v", err)
							return
						}
					}
				}()
			}
			wg.Wait()
			got, err := st.GetSettings()
			if err != nil {
				t.Fatal(err)
			}
			key := got.APIKeys[0]
			want := writers * increments
			if key.UsedRequests != want || key.UsedTokens != want*3 || key.UsedCostUSD != float64(want)*0.25 {
				t.Fatalf("usage=%+v, want requests=%d tokens=%d cost=%.2f", key, want, want*3, float64(want)*0.25)
			}
			if got.DashboardMessage != "preserve-me" || key.MaxRequests != 10000 || key.Key != "secret" {
				t.Fatalf("unrelated settings changed: %+v", got)
			}
			if err := st.RecordAPIKeyUsage("usage-key", APIKeyUsageDelta{Requests: -10, Tokens: -10, CostUSD: -1}); err != nil {
				t.Fatal(err)
			}
			afterNegative, _ := st.GetSettings()
			if afterNegative.APIKeys[0].UsedRequests != want || afterNegative.APIKeys[0].UsedTokens != want*3 {
				t.Fatalf("negative delta reduced usage: %+v", afterNegative.APIKeys[0])
			}
		})
	}
}

func TestStoreProviderAccountsRoundTrip(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			st, closeFn, err := f.open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer closeFn()
			if err := st.UpsertProvider(Provider{ID: "accounts", Type: ProviderOpenAICompatible, BaseURL: "https://example.test", Enabled: true, Models: []string{"m"}}); err != nil {
				t.Fatal(err)
			}
			if err := st.UpsertProxy(Proxy{ID: "pool", Name: "Pool", URL: "http://user:pass@proxy.test:8080", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			account := ProviderAccount{ID: "acc-a", ProviderID: "accounts", Name: "Primary", AuthType: "api_key", APIKey: "secret", ProxyID: "pool", Enabled: true, Priority: 2, QuotaLimitPercent: 70}
			if err := st.UpsertProviderAccount(account); err != nil {
				t.Fatal(err)
			}
			items, err := st.ListProviderAccounts("accounts")
			if err != nil || len(items) != 1 {
				t.Fatalf("accounts=%+v err=%v", items, err)
			}
			if items[0].ProxyURL != "http://user:pass@proxy.test:8080" || items[0].APIKey != "secret" || items[0].Priority != 2 || items[0].QuotaLimitPercent != 70 {
				t.Fatalf("account not persisted/resolved: %+v", items[0])
			}
			now := time.Now().UTC()
			if err := st.RecordProviderAccountOutcome("acc-a", ProviderAccountOutcome{FailureStreak: 2, CooldownUntil: now.Add(time.Minute), CooldownReason: "rate_limited", At: now}); err != nil {
				t.Fatal(err)
			}
			got, found, err := st.GetProviderAccount("acc-a")
			if err != nil || !found || got.FailureStreak != 2 || got.CooldownReason != "rate_limited" {
				t.Fatalf("failure outcome=%+v found=%v err=%v", got, found, err)
			}
			if err := st.RecordProviderAccountOutcome("acc-a", ProviderAccountOutcome{Success: true, At: now}); err != nil {
				t.Fatal(err)
			}
			got, _, _ = st.GetProviderAccount("acc-a")
			if got.FailureStreak != 0 || !got.CooldownUntil.IsZero() || got.LastSuccessAt.IsZero() {
				t.Fatalf("success did not reset account: %+v", got)
			}
		})
	}
}

func TestStoreProviderProxyIDRoundTrip(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			st, closeFn, err := f.open(t.TempDir())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer closeFn()

			if err := st.UpsertProxy(Proxy{ID: "proxy-a", Name: "Proxy A", URL: "http://127.0.0.1:8080", Enabled: true}); err != nil {
				t.Fatalf("upsert proxy: %v", err)
			}
			if err := st.UpsertProvider(Provider{
				ID:      "proxy-provider",
				Type:    ProviderOpenAICompatible,
				Name:    "Proxy Provider",
				BaseURL: "https://x/",
				APIKey:  "sk-test",
				ProxyID: "proxy-a",
				Models:  []string{"m1"},
				Enabled: true,
			}); err != nil {
				t.Fatalf("upsert provider with proxy id: %v", err)
			}

			got, found, err := st.GetProvider("proxy-provider")
			if err != nil || !found {
				t.Fatalf("get provider: found=%v err=%v", found, err)
			}
			if got.ProxyID != "proxy-a" || got.ProxyURL != "http://127.0.0.1:8080" {
				t.Fatalf("provider proxy resolution = id %q url %q", got.ProxyID, got.ProxyURL)
			}
			providers, err := st.ListProviders()
			if err != nil {
				t.Fatalf("list providers: %v", err)
			}
			found = false
			for _, provider := range providers {
				if provider.ID == "proxy-provider" {
					found = true
					if provider.ProxyID != "proxy-a" || provider.ProxyURL != "http://127.0.0.1:8080" {
						t.Fatalf("listed provider proxy resolution = id %q url %q", provider.ProxyID, provider.ProxyURL)
					}
				}
			}
			if !found {
				t.Fatal("proxy-provider missing from ListProviders")
			}
		})
	}
}

func TestStoreRequestLogFusionFields(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			st, closeFn, err := f.open(t.TempDir())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer closeFn()
			if err := st.AddRequestLog(RequestLog{
				Endpoint: "/v1/chat/completions", ProviderID: "fusion", Model: "review",
				Status: "200", DurationMS: 1000,
				FusionName: "review", FusionMode: "parallel", FusionExpertCount: 3, FusionSuccessfulExperts: 2,
				FusionSynthesizerTarget: "gpt-4.1", FusionReviewerTarget: "claude-sonnet",
				FusionDurationMS: 1000, FusionUsedReviewer: true, FusionError: "", FusionTrace: `{"experts":[]}`,
			}); err != nil {
				t.Fatalf("add log: %v", err)
			}
			logs, err := st.RecentRequestLogs(1)
			if err != nil {
				t.Fatalf("recent logs: %v", err)
			}
			if len(logs) != 1 {
				t.Fatalf("logs = %d, want 1", len(logs))
			}
			got := logs[0]
			if got.FusionName != "review" || got.FusionMode != "parallel" || got.FusionExpertCount != 3 || got.FusionSuccessfulExperts != 2 || got.FusionSynthesizerTarget != "gpt-4.1" || got.FusionReviewerTarget != "claude-sonnet" || got.FusionDurationMS != 1000 || !got.FusionUsedReviewer || got.FusionTrace != `{"experts":[]}` {
				t.Fatalf("fusion log fields not persisted: %+v", got)
			}
		})
	}
}

func TestStoreRequestLogRetention(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			st, closeFn, err := f.open(t.TempDir())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer closeFn()

			settings, _ := st.GetSettings()
			settings.KeepRequestLogs = 3
			if err := st.SaveSettings(settings); err != nil {
				t.Fatalf("save settings: %v", err)
			}

			for i := 0; i < 10; i++ {
				if err := st.AddRequestLog(RequestLog{
					Endpoint:         "/v1/chat",
					Model:            "m",
					Status:           "200",
					PromptTokens:     10 + i,
					CompletionTokens: 5,
					TotalTokens:      15 + i,
					CachedTokens:     2,
					ReasoningTokens:  1,
					EstimatedTokens:  true,
					CostUSD:          0.001,
				}); err != nil {
					t.Fatalf("add log: %v", err)
				}
			}
			logs, err := st.RecentRequestLogs(0)
			if err != nil {
				t.Fatalf("recent logs: %v", err)
			}
			if len(logs) != 3 {
				t.Fatalf("retained logs = %d, want 3", len(logs))
			}
			if logs[0].PromptTokens == 0 || logs[0].CompletionTokens == 0 || logs[0].TotalTokens == 0 || logs[0].CostUSD == 0 {
				t.Fatalf("usage fields were not persisted: %+v", logs[0])
			}
			if !logs[0].EstimatedTokens || logs[0].CachedTokens != 2 || logs[0].ReasoningTokens != 1 {
				t.Fatalf("usage detail fields were not persisted: %+v", logs[0])
			}
		})
	}
}
