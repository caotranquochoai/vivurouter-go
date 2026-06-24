package provider

import (
	"testing"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

func TestKeyPoolFillFirst(t *testing.T) {
	pool := NewKeyPool()
	p := store.Provider{
		ID:          "p",
		KeyStrategy: store.ProviderKeyStrategyFillFirst,
		Keys: []store.ProviderKey{
			{ID: "k2", Key: "sk-two", Enabled: true, Priority: 2},
			{ID: "k1", Key: "sk-one", Enabled: true, Priority: 1},
		},
	}
	key := pool.SelectKey(p)
	if key == nil || key.ID != "k1" {
		t.Fatalf("selected %+v, want k1", key)
	}
}

func TestKeyPoolCooldownSkipsKey(t *testing.T) {
	pool := NewKeyPool()
	p := store.Provider{
		ID:          "p",
		KeyStrategy: store.ProviderKeyStrategyFillFirst,
		Keys: []store.ProviderKey{
			{ID: "k1", Key: "sk-one", Enabled: true, Priority: 1},
			{ID: "k2", Key: "sk-two", Enabled: true, Priority: 2},
		},
	}
	pool.MarkUnavailable("p", "k1", time.Minute)
	key := pool.SelectKey(p)
	if key == nil || key.ID != "k2" {
		t.Fatalf("selected %+v, want k2", key)
	}
}

func TestKeyPoolRoundRobinSticky(t *testing.T) {
	pool := NewKeyPool()
	p := store.Provider{
		ID:          "p",
		KeyStrategy: store.ProviderKeyStrategyRoundRobin,
		StickyLimit: 2,
		Keys: []store.ProviderKey{
			{ID: "k1", Key: "sk-one", Enabled: true, Priority: 1},
			{ID: "k2", Key: "sk-two", Enabled: true, Priority: 2},
		},
	}
	want := []string{"k1", "k1", "k2", "k2", "k1"}
	for i, id := range want {
		key := pool.SelectKey(p)
		if key == nil || key.ID != id {
			t.Fatalf("select %d = %+v, want %s", i, key, id)
		}
	}
}
