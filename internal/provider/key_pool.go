package provider

import (
	"sync"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

// KeyPool manages key selection and cooldown tracking for multi-key providers.
type KeyPool struct {
	mu        sync.Mutex
	rotations map[string]*keyRotationEntry
	cooldowns map[string]time.Time // "providerID:keyID" -> expiry
}

type keyRotationEntry struct {
	LastIndex        int
	ConsecutiveCount int
}

// NewKeyPool creates a new KeyPool.
func NewKeyPool() *KeyPool {
	return &KeyPool{
		rotations: make(map[string]*keyRotationEntry),
		cooldowns: make(map[string]time.Time),
	}
}

// SelectKey picks the best available key from the provider's Keys slice.
// Returns nil if no keys are available (caller should fall back to provider.APIKey).
func (kp *KeyPool) SelectKey(p store.Provider) *store.ProviderKey {
	kp.mu.Lock()
	defer kp.mu.Unlock()

	now := time.Now()

	// Build list of available (enabled + not cooled down) keys.
	available := make([]store.ProviderKey, 0, len(p.Keys))
	for _, k := range p.Keys {
		if !k.Enabled {
			continue
		}
		cooldownKey := p.ID + ":" + k.ID
		if exp, ok := kp.cooldowns[cooldownKey]; ok {
			if now.Before(exp) {
				continue // still cooled down
			}
			delete(kp.cooldowns, cooldownKey)
		}
		available = append(available, k)
	}

	if len(available) == 0 {
		return nil
	}

	switch p.KeyStrategy {
	case store.ProviderKeyStrategyRoundRobin:
		return kp.selectRoundRobin(p.ID, available, p.StickyLimit)
	default: // fill-first
		return kp.selectFillFirst(available)
	}
}

// MarkUnavailable puts a key into cooldown for the given duration.
func (kp *KeyPool) MarkUnavailable(providerID, keyID string, cooldown time.Duration) {
	kp.mu.Lock()
	defer kp.mu.Unlock()
	kp.cooldowns[providerID+":"+keyID] = time.Now().Add(cooldown)
}

// ClearCooldowns removes all cooldowns for a provider (e.g. after provider settings change).
func (kp *KeyPool) ClearCooldowns(providerID string) {
	kp.mu.Lock()
	defer kp.mu.Unlock()
	for key := range kp.cooldowns {
		if len(key) > len(providerID)+1 && key[:len(providerID)+1] == providerID+":" {
			delete(kp.cooldowns, key)
		}
	}
	// Reset rotation state
	delete(kp.rotations, providerID)
}

// selectFillFirst picks the key with the lowest Priority value.
func (kp *KeyPool) selectFillFirst(keys []store.ProviderKey) *store.ProviderKey {
	best := &keys[0]
	for i := 1; i < len(keys); i++ {
		if keys[i].Priority < best.Priority {
			best = &keys[i]
		}
	}
	return best
}

// selectRoundRobin picks keys in rotation, sticking to the same key for StickyLimit requests.
func (kp *KeyPool) selectRoundRobin(providerID string, keys []store.ProviderKey, stickyLimit int) *store.ProviderKey {
	entry, ok := kp.rotations[providerID]
	if !ok || entry.LastIndex >= len(keys) {
		entry = &keyRotationEntry{LastIndex: 0, ConsecutiveCount: 0}
		kp.rotations[providerID] = entry
	}

	if entry.ConsecutiveCount < stickyLimit {
		entry.ConsecutiveCount++
		return &keys[entry.LastIndex]
	}

	// Advance to next key
	entry.LastIndex = (entry.LastIndex + 1) % len(keys)
	entry.ConsecutiveCount = 1
	kp.rotations[providerID] = entry
	return &keys[entry.LastIndex]
}
