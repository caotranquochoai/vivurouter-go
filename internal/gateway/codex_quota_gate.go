package gateway

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

var (
	codexQuotaCacheTTL     = 60 * time.Second
	codexQuotaFetchTimeout = 5 * time.Second
)

type codexQuotaSnapshot struct {
	used      float64
	fetchedAt time.Time
	valid     bool
}

type codexQuotaFlight struct {
	done chan struct{}
}

type codexQuotaGate struct {
	executor *provider.CodexExecutor

	mu      sync.Mutex
	cache   map[string]codexQuotaSnapshot
	flights map[string]*codexQuotaFlight
}

func newCodexQuotaGate(executor *provider.CodexExecutor) *codexQuotaGate {
	return &codexQuotaGate{
		executor: executor,
		cache:    map[string]codexQuotaSnapshot{},
		flights:  map[string]*codexQuotaFlight{},
	}
}

func (g *codexQuotaGate) allows(ctx context.Context, account store.ProviderAccount, target store.Provider, now time.Time) bool {
	threshold := account.QuotaLimitPercent
	if threshold <= 0 || g == nil || g.executor == nil {
		return true
	}
	if threshold > 100 {
		threshold = 100
	}

	snapshot := g.snapshot(account.ID)
	if snapshot.valid && now.Sub(snapshot.fetchedAt) < codexQuotaCacheTTL {
		return snapshot.used < threshold
	}

	snapshot, ok := g.refresh(ctx, account.ID, target, now)
	if ok {
		return snapshot.used < threshold
	}
	// Quota observation must not make the gateway unavailable. A stale valid
	// snapshot remains useful; without one, fail open and let real upstream
	// rate-limit/quota errors drive the normal fallback path.
	if snapshot.valid {
		return snapshot.used < threshold
	}
	return true
}

func (g *codexQuotaGate) snapshot(accountID string) codexQuotaSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cache[accountID]
}

func (g *codexQuotaGate) refresh(ctx context.Context, accountID string, target store.Provider, now time.Time) (codexQuotaSnapshot, bool) {
	g.mu.Lock()
	if flight := g.flights[accountID]; flight != nil {
		stale := g.cache[accountID]
		g.mu.Unlock()
		select {
		case <-flight.done:
			fresh := g.snapshot(accountID)
			return fresh, fresh.valid && fresh.fetchedAt.After(stale.fetchedAt)
		case <-ctx.Done():
			return stale, false
		}
	}
	flight := &codexQuotaFlight{done: make(chan struct{})}
	g.flights[accountID] = flight
	stale := g.cache[accountID]
	g.mu.Unlock()

	fetchCtx := ctx
	cancel := func() {}
	if codexQuotaFetchTimeout > 0 {
		fetchCtx, cancel = context.WithTimeout(ctx, codexQuotaFetchTimeout)
	}
	report, err := g.executor.FetchQuota(fetchCtx, target)
	cancel()
	used, valid := codexRoutingQuotaUsage(report)
	fresh := codexQuotaSnapshot{used: used, fetchedAt: now, valid: valid && err == nil}

	g.mu.Lock()
	if fresh.valid {
		g.cache[accountID] = fresh
	} else {
		fresh = stale
	}
	delete(g.flights, accountID)
	close(flight.done)
	g.mu.Unlock()
	return fresh, valid && err == nil
}

func codexRoutingQuotaUsage(report provider.CodexQuotaReport) (float64, bool) {
	used := 0.0
	valid := false
	for _, quota := range report.Quotas {
		if quota.Key != "session" && quota.Key != "weekly" {
			continue
		}
		if quota.Unlimited || math.IsNaN(quota.Used) || math.IsInf(quota.Used, 0) {
			continue
		}
		value := math.Max(0, math.Min(100, quota.Used))
		if !valid || value > used {
			used = value
		}
		valid = true
	}
	return used, valid
}
