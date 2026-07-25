// Command loadtest drives a load/throughput test against the VivuRouter gateway
// using sample data instead of real provider APIs. It can either spin up the
// whole gateway in-process wired to a mock upstream (default, self-contained,
// costs no tokens), or fire load at an already-running gateway (--target).
//
// Usage:
//
//	go run ./cmd/loadtest --concurrency 50 --duration 10s
//	go run ./cmd/loadtest --concurrency 100 --duration 15s --upstream-delay 20ms
//	go run ./cmd/loadtest --target http://127.0.0.1:20129 --requests 5000
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/local/vivurouter-go/internal/app"
	"github.com/local/vivurouter-go/internal/config"
	"github.com/local/vivurouter-go/internal/store"
)

func main() {
	var (
		concurrency   = flag.Int("concurrency", 50, "number of concurrent workers")
		duration      = flag.Duration("duration", 10*time.Second, "how long to send load (ignored if --requests > 0)")
		requests      = flag.Int("requests", 0, "total requests to send (0 = run for --duration)")
		upstreamDelay = flag.Duration("upstream-delay", 0, "artificial latency the mock upstream adds per response")
		warmup        = flag.Duration("warmup", 0, "warmup period whose results are discarded")
		target        = flag.String("target", "", "gateway base URL to hit; empty spins an in-process gateway + mock upstream")
		apiKey        = flag.String("api-key", "load-test-key", "API key sent to the gateway")
		model         = flag.String("model", "mock/gpt-4o", "model field in the chat request")
		timeout       = flag.Duration("timeout", 30*time.Second, "per-request HTTP timeout")
		verbose       = flag.Bool("verbose", false, "keep the gateway's per-request logging (self-contained mode only)")
	)
	flag.Parse()

	baseURL := *target
	if baseURL == "" {
		// Self-contained mode: mock upstream + in-process gateway. Silence the
		// gateway's per-request logging middleware so it does not drown the
		// stats output; pass --verbose to keep it.
		if !*verbose {
			log.SetOutput(io.Discard)
		}
		mock := startMockUpstream(*upstreamDelay)
		defer mock.Close()

		gw, cleanup, err := startInProcessGateway(mock.URL, *apiKey)
		if err != nil {
			log.SetOutput(os.Stderr)
			fmt.Fprintf(os.Stderr, "failed to start in-process gateway: %v\n", err)
			os.Exit(1)
		}
		defer cleanup()
		baseURL = gw
		fmt.Printf("Self-contained mode: mock upstream + in-process gateway\n")
		fmt.Printf("  mock upstream: %s\n  gateway:       %s\n", mock.URL, baseURL)
	} else {
		fmt.Printf("Target mode: hitting %s\n", baseURL)
	}

	cfg := loadConfig{
		baseURL:     baseURL,
		apiKey:      *apiKey,
		model:       *model,
		concurrency: *concurrency,
		duration:    *duration,
		requests:    *requests,
		warmup:      *warmup,
		timeout:     *timeout,
	}
	fmt.Printf("\nLoad: concurrency=%d ", cfg.concurrency)
	if cfg.requests > 0 {
		fmt.Printf("requests=%d\n", cfg.requests)
	} else {
		fmt.Printf("duration=%s\n", cfg.duration)
	}
	if *upstreamDelay > 0 {
		fmt.Printf("Upstream delay: %s\n", *upstreamDelay)
	}

	stats := runLoad(cfg)
	stats.print()
}

// ---------------------------------------------------------------------------
// Mock upstream: pretends to be an OpenAI-compatible provider returning sample
// data, so the gateway never reaches out to a real API.
// ---------------------------------------------------------------------------

const sampleAssistantText = "This is a sample assistant response returned by the mock upstream. " +
	"It contains a few sentences so the payload size is realistic for load testing purposes. " +
	"No real provider was contacted; this content is entirely synthetic sample data."

func startMockUpstream(delay time.Duration) *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		// Echo the requested model back if present, else a default.
		modelName := "gpt-4o"
		var reqBody map[string]any
		if raw, err := io.ReadAll(r.Body); err == nil {
			_ = json.Unmarshal(raw, &reqBody)
			if m, ok := reqBody["model"].(string); ok && m != "" {
				modelName = m
			}
		}
		resp := map[string]any{
			"id":      "chatcmpl-mock",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   modelName,
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": sampleAssistantText},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     1200,
				"completion_tokens": 350,
				"total_tokens":      1550,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{{"id": "gpt-4o", "object": "model"}},
		})
	})

	return httptest.NewServer(mux)
}

// ---------------------------------------------------------------------------
// In-process gateway wired to the mock upstream.
// ---------------------------------------------------------------------------

func startInProcessGateway(upstreamURL, apiKey string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "vr-loadtest")
	if err != nil {
		return "", nil, err
	}
	cleanupDir := func() { _ = os.RemoveAll(dir) }

	fs, err := store.NewFileStore(dir)
	if err != nil {
		cleanupDir()
		return "", nil, err
	}

	// Require an API key + a valid unlimited policy. With RequireAPIKey=false the
	// gateway resolves an empty policy whose KeyWithinQuota() fails, returning 429
	// — so auth must be ON with a real key to exercise the happy path.
	settings := store.DefaultSettings()
	settings.RequireAPIKey = true
	settings.ObservabilityEnabled = true
	settings.DefaultProvider = "mock"
	settings.APIKeys = []store.APIKeyPolicy{{
		ID:      "load",
		Key:     apiKey,
		Enabled: true,
	}}
	if err := fs.SaveSettings(settings); err != nil {
		cleanupDir()
		return "", nil, err
	}

	if err := fs.UpsertProvider(store.Provider{
		ID:      "mock",
		Type:    store.ProviderOpenAICompatible,
		Name:    "Mock Upstream",
		BaseURL: upstreamURL,
		APIKey:  "mock-provider-key",
		Enabled: true,
		Models:  []string{"gpt-4o"},
	}); err != nil {
		cleanupDir()
		return "", nil, err
	}

	// Wrap in the async log store exactly like production (cmd/vivurouter-go).
	asyncStore := store.NewAsyncLogStore(fs, 0)

	// The gateway's NewServer builds dashboard handlers which ParseGlob the HTML
	// templates, so AssetsDir must point at a real web/ dir even though the load
	// test only exercises /v1/*. config.Load()'s resolver finds ./web or
	// ./vivurouter-go/web; fall back to the temp dir only if neither exists.
	assetsDir := resolveWebDir()

	cfg := config.Config{
		Host:         "127.0.0.1",
		Port:         "0",
		DataDir:      dir,
		AssetsDir:    assetsDir,
		StoreBackend: "file",
	}
	srv, err := app.NewServer(cfg, asyncStore)
	if err != nil {
		asyncStore.Flush()
		cleanupDir()
		return "", nil, err
	}

	ts := httptest.NewServer(srv.Handler)
	cleanup := func() {
		ts.Close()
		asyncStore.Flush()
		cleanupDir()
	}
	return ts.URL, cleanup, nil
}

// resolveWebDir returns a directory containing templates/*.html for the gateway
// to parse. It prefers a real web/ tree (so /v1 behaves exactly like production)
// and otherwise creates a throwaway one with a stub template, letting the load
// test run from anywhere without the dashboard assets.
func resolveWebDir() string {
	for _, p := range []string{"web", filepath.Join("vivurouter-go", "web")} {
		if info, err := os.Stat(filepath.Join(p, "templates")); err == nil && info.IsDir() {
			return p
		}
	}
	// Fallback: synthesize a minimal templates dir the load test never renders.
	stub, err := os.MkdirTemp("", "vr-loadtest-web")
	if err != nil {
		return "web"
	}
	tplDir := filepath.Join(stub, "templates")
	if err := os.MkdirAll(tplDir, 0o755); err != nil {
		return "web"
	}
	_ = os.WriteFile(filepath.Join(tplDir, "stub.html"), []byte(`{{define "stub"}}ok{{end}}`), 0o644)
	return stub
}

// ---------------------------------------------------------------------------
// Load generator + statistics.
// ---------------------------------------------------------------------------

type loadConfig struct {
	baseURL     string
	apiKey      string
	model       string
	concurrency int
	duration    time.Duration
	requests    int
	warmup      time.Duration
	timeout     time.Duration
}

type result struct {
	latency time.Duration
	status  int
	bytes   int
	err     bool
}

type stats struct {
	latencies []time.Duration
	total     int
	success   int
	byStatus  map[int]int
	errs      int
	totBytes  int64
	wall      time.Duration
}

func runLoad(cfg loadConfig) stats {
	client := &http.Client{
		Timeout: cfg.timeout,
		Transport: &http.Transport{
			MaxIdleConns:        cfg.concurrency * 2,
			MaxIdleConnsPerHost: cfg.concurrency * 2,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	reqBody := buildChatBody(cfg.model)

	// Optional warmup: fire a short burst whose results are discarded.
	if cfg.warmup > 0 {
		warmCtx, cancel := context.WithTimeout(context.Background(), cfg.warmup)
		runWorkers(warmCtx, cfg, client, reqBody, nil, nil)
		cancel()
	}

	results := make(chan result, 4096)
	var collected []result
	var collectWG sync.WaitGroup
	collectWG.Add(1)
	go func() {
		defer collectWG.Done()
		for r := range results {
			collected = append(collected, r)
		}
	}()

	var ctx context.Context
	var cancel context.CancelFunc
	var budget *int64
	if cfg.requests > 0 {
		ctx, cancel = context.WithCancel(context.Background())
		n := int64(cfg.requests)
		budget = &n
	} else {
		ctx, cancel = context.WithTimeout(context.Background(), cfg.duration)
	}

	start := time.Now()
	runWorkers(ctx, cfg, client, reqBody, budget, results)
	wall := time.Since(start)
	cancel()

	close(results)
	collectWG.Wait()

	return summarize(collected, wall)
}

func runWorkers(ctx context.Context, cfg loadConfig, client *http.Client, body []byte, budget *int64, out chan<- result) {
	var wg sync.WaitGroup
	for i := 0; i < cfg.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				// In requests mode, claim a slot from the shared budget.
				if budget != nil {
					if atomic.AddInt64(budget, -1) < 0 {
						return
					}
				}
				res := doRequest(ctx, cfg, client, body)
				if out != nil {
					out <- res
				}
			}
		}()
	}
	wg.Wait()
}

func doRequest(ctx context.Context, cfg loadConfig, client *http.Client, body []byte) result {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return result{err: true}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.apiKey)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		// Context cancellation at the deadline is expected, not a real error.
		if ctx.Err() != nil {
			return result{err: false, status: -1}
		}
		return result{err: true, latency: time.Since(start)}
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return result{
		latency: time.Since(start),
		status:  resp.StatusCode,
		bytes:   int(n),
	}
}

func buildChatBody(model string) []byte {
	body := map[string]any{
		"model":  model,
		"stream": false,
		"messages": []map[string]any{
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Summarize the benefits of load testing in two sentences."},
		},
	}
	raw, _ := json.Marshal(body)
	return raw
}

func summarize(results []result, wall time.Duration) stats {
	s := stats{byStatus: map[int]int{}, wall: wall}
	for _, r := range results {
		if r.status == -1 {
			// Deadline-cancelled in-flight request; ignore entirely.
			continue
		}
		s.total++
		if r.err {
			s.errs++
			continue
		}
		s.byStatus[r.status]++
		s.totBytes += int64(r.bytes)
		if r.status >= 200 && r.status < 300 {
			s.success++
			s.latencies = append(s.latencies, r.latency)
		}
	}
	sort.Slice(s.latencies, func(i, j int) bool { return s.latencies[i] < s.latencies[j] })
	return s
}

func (s stats) print() {
	fmt.Printf("\n=== Load test results ===\n")
	fmt.Printf("Wall time:        %s\n", s.wall.Round(time.Millisecond))
	fmt.Printf("Total requests:   %d\n", s.total)
	fmt.Printf("Successful (2xx): %d\n", s.success)
	fmt.Printf("Transport errors: %d\n", s.errs)
	if s.total > 0 {
		fmt.Printf("Success rate:     %.1f%%\n", float64(s.success)/float64(s.total)*100)
	}
	if len(s.byStatus) > 0 {
		fmt.Printf("Status breakdown:\n")
		codes := make([]int, 0, len(s.byStatus))
		for c := range s.byStatus {
			codes = append(codes, c)
		}
		sort.Ints(codes)
		for _, c := range codes {
			fmt.Printf("  %d: %d\n", c, s.byStatus[c])
		}
	}
	if s.wall > 0 {
		fmt.Printf("Throughput:       %.1f req/s\n", float64(s.success)/s.wall.Seconds())
		fmt.Printf("Data received:    %.2f MB\n", float64(s.totBytes)/(1024*1024))
	}
	if len(s.latencies) > 0 {
		fmt.Printf("Latency (2xx):\n")
		fmt.Printf("  mean: %s\n", meanDuration(s.latencies).Round(time.Microsecond))
		fmt.Printf("  p50:  %s\n", percentile(s.latencies, 50).Round(time.Microsecond))
		fmt.Printf("  p90:  %s\n", percentile(s.latencies, 90).Round(time.Microsecond))
		fmt.Printf("  p99:  %s\n", percentile(s.latencies, 99).Round(time.Microsecond))
		fmt.Printf("  max:  %s\n", s.latencies[len(s.latencies)-1].Round(time.Microsecond))
	}
	if s.success == 0 {
		fmt.Printf("\nWARNING: no successful (2xx) responses. Check auth/provider config.\n")
	}
}

func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func meanDuration(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range ds {
		total += d
	}
	return total / time.Duration(len(ds))
}
