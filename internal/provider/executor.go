package provider

import (
	"context"
	"net/http"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

// Executors holds upstream executors used by the gateway.
type Executors struct {
	OpenAI       *OpenAIExecutor
	Codex        *CodexExecutor
	MimoFree     *MimoFreeExecutor
	OpenCodeFree *OpenCodeFreeExecutor
	Antigravity  *AntigravityExecutor
	KeyPool      *KeyPool
}

func NewExecutors() *Executors {
	return NewExecutorsWithStore(nil)
}

func NewExecutorsWithStore(st store.Store) *Executors {
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          512,
			MaxIdleConnsPerHost:   128,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 120 * time.Second,
		},
	}
	keyPool := NewKeyPool()
	return &Executors{
		OpenAI:       &OpenAIExecutor{Client: client, KeyPool: keyPool},
		Codex:        &CodexExecutor{Client: client, Store: st},
		MimoFree:     &MimoFreeExecutor{Client: client},
		OpenCodeFree: &OpenCodeFreeExecutor{Client: client},
		Antigravity:  &AntigravityExecutor{Client: client, Store: st},
		KeyPool:      keyPool,
	}
}

type ExecuteResult struct {
	Response        *http.Response
	URL             string
	TransformedBody map[string]any
	UsedKeyID       string // ID of the provider key used, "legacy" for single-key, empty if unknown
}

func (e *Executors) ExecuteChat(ctx context.Context, provider store.Provider, model string, body map[string]any) (*ExecuteResult, error) {
	switch provider.Type {
	case store.ProviderMimoFree:
		return e.MimoFree.ExecuteChat(ctx, provider, model, body)
	case store.ProviderOpenCodeFree:
		return e.OpenCodeFree.ExecuteChat(ctx, provider, model, body)
	case store.ProviderAntigravity:
		return e.Antigravity.ExecuteChat(ctx, provider, model, body)
	default:
		return e.OpenAI.ExecuteChat(ctx, provider, model, body)
	}
}

type ChatExecutor interface {
	ExecuteChat(ctx context.Context, provider store.Provider, model string, body map[string]any) (*ExecuteResult, error)
}

type ResponsesExecutor interface {
	ExecuteResponses(ctx context.Context, provider store.Provider, model string, body map[string]any) (*ExecuteResult, error)
}
