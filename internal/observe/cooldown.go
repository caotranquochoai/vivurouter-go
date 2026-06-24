package observe

import (
	"sort"
	"sync"
	"time"
)

// CooldownTracker records per-provider cooldown windows after fallback-eligible
// upstream failures. It is in-memory only and resets on restart.
type CooldownTracker struct {
	mu    sync.Mutex
	until map[string]cooldownEntry
}

type cooldownEntry struct {
	until  time.Time
	reason string
}

// CooldownStatus is a snapshot row for one provider still in cooldown.
type CooldownStatus struct {
	ProviderID  string    `json:"provider_id"`
	Until       time.Time `json:"until"`
	RemainingMS int64     `json:"remaining_ms"`
	Reason      string    `json:"reason"`
}

func newCooldownTracker() *CooldownTracker {
	return &CooldownTracker{until: map[string]cooldownEntry{}}
}

// Penalize marks a provider unavailable for the given duration. A longer
// existing cooldown is never shortened.
func (c *CooldownTracker) Penalize(providerID string, now time.Time, dur time.Duration, reason string) {
	if providerID == "" || dur <= 0 {
		return
	}
	until := now.Add(dur)
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.until[providerID]; ok && existing.until.After(until) {
		return
	}
	c.until[providerID] = cooldownEntry{until: until, reason: reason}
}

// Available reports whether the provider may currently receive traffic.
func (c *CooldownTracker) Available(providerID string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.until[providerID]
	if !ok {
		return true
	}
	if !now.Before(entry.until) {
		delete(c.until, providerID)
		return true
	}
	return false
}

// Snapshot returns active cooldowns sorted by provider ID, pruning expired ones.
func (c *CooldownTracker) Snapshot(now time.Time) []CooldownStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []CooldownStatus{}
	for id, entry := range c.until {
		if !now.Before(entry.until) {
			delete(c.until, id)
			continue
		}
		out = append(out, CooldownStatus{
			ProviderID:  id,
			Until:       entry.until,
			RemainingMS: entry.until.Sub(now).Milliseconds(),
			Reason:      entry.reason,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProviderID < out[j].ProviderID })
	return out
}
