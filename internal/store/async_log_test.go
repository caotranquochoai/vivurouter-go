package store

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingStore is a minimal Store used to observe AddRequestLog calls from the
// async wrapper. Only AddRequestLog and Close carry behavior; the rest satisfy
// the interface and are unused by these tests.
type countingStore struct {
	mu     sync.Mutex
	logs   []RequestLog
	closed atomic.Bool
	// delay lets a test hold the worker inside AddRequestLog to force the
	// caller's buffer-full fallback path.
	delay time.Duration
	block <-chan struct{}
	fail  bool
}

func (c *countingStore) AddRequestLog(log RequestLog) error {
	if c.block != nil {
		<-c.block
	}
	if c.delay > 0 {
		time.Sleep(c.delay)
	}
	if c.fail {
		return errors.New("persistence unavailable")
	}
	c.mu.Lock()
	c.logs = append(c.logs, log)
	c.mu.Unlock()
	return nil
}

func (c *countingStore) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.logs)
}

func (c *countingStore) Close() error {
	c.closed.Store(true)
	return nil
}

// Unused Store methods.
func (c *countingStore) GetSettings() (Settings, error)                   { return Settings{}, nil }
func (c *countingStore) SaveSettings(Settings) error                      { return nil }
func (c *countingStore) RecordAPIKeyUsage(string, APIKeyUsageDelta) error { return nil }
func (c *countingStore) ListProviders() ([]Provider, error)               { return nil, nil }
func (c *countingStore) GetProvider(string) (Provider, bool, error) {
	return Provider{}, false, nil
}
func (c *countingStore) UpsertProvider(Provider) error                          { return nil }
func (c *countingStore) DeleteProvider(string) error                            { return nil }
func (c *countingStore) ListProviderAccounts(string) ([]ProviderAccount, error) { return nil, nil }
func (c *countingStore) GetProviderAccount(string) (ProviderAccount, bool, error) {
	return ProviderAccount{}, false, nil
}
func (c *countingStore) UpsertProviderAccount(ProviderAccount) error { return nil }
func (c *countingStore) DeleteProviderAccount(string) error          { return nil }
func (c *countingStore) RecordProviderAccountOutcome(string, ProviderAccountOutcome) error {
	return nil
}
func (c *countingStore) RecentRequestLogs(int) ([]RequestLog, error) {
	return nil, nil
}
func (c *countingStore) GetRequestDebugPayload(string) (*RequestLogDebugPayload, bool, error) {
	return nil, false, nil
}
func (c *countingStore) DeleteRequestDebugPayloads() (int, error) { return 0, nil }
func (c *countingStore) ResetAllData() error                      { return nil }
func (c *countingStore) ListProxies() ([]Proxy, error)            { return nil, nil }
func (c *countingStore) GetProxy(string) (Proxy, bool, error)     { return Proxy{}, false, nil }
func (c *countingStore) UpsertProxy(Proxy) error                  { return nil }
func (c *countingStore) DeleteProxy(string) error                 { return nil }
func (c *countingStore) ListInvoices() ([]Invoice, error)         { return nil, nil }
func (c *countingStore) GetInvoice(string) (Invoice, bool, error) { return Invoice{}, false, nil }
func (c *countingStore) UpsertInvoice(Invoice) error              { return nil }
func (c *countingStore) DeleteInvoice(string) error               { return nil }

func TestAsyncLogStoreDrainsAllOnFlush(t *testing.T) {
	backing := &countingStore{}
	async := NewAsyncLogStore(backing, 1024)

	const n = 500
	for i := 0; i < n; i++ {
		if err := async.AddRequestLog(RequestLog{Endpoint: "/v1/chat/completions"}); err != nil {
			t.Fatalf("AddRequestLog: %v", err)
		}
	}
	async.Flush()

	if got := backing.count(); got != n {
		t.Fatalf("drained %d logs, want %d", got, n)
	}
}

func TestAsyncLogStoreFallsBackWhenBufferFull(t *testing.T) {
	// Tiny buffer + a slow worker forces the non-blocking send to miss and take
	// the synchronous fallback. No log may be dropped.
	backing := &countingStore{delay: 5 * time.Millisecond}
	async := NewAsyncLogStore(backing, 1)

	const n = 50
	for i := 0; i < n; i++ {
		if err := async.AddRequestLog(RequestLog{Endpoint: "/v1/messages"}); err != nil {
			t.Fatalf("AddRequestLog: %v", err)
		}
	}
	async.Flush()

	if got := backing.count(); got != n {
		t.Fatalf("got %d logs, want %d (none may be dropped)", got, n)
	}
	if health := async.Health(); health.SyncFallbacks == 0 {
		t.Fatalf("expected queue-full sync fallback metric, got %+v", health)
	}
}

func TestAsyncLogStoreSyncFallbackDoesNotHoldLifecycleMutex(t *testing.T) {
	unblock := make(chan struct{})
	backing := &countingStore{block: unblock}
	async := NewAsyncLogStore(backing, 1)
	if err := async.AddRequestLog(RequestLog{Endpoint: "/worker"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := async.AddRequestLog(RequestLog{Endpoint: "/queued"}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = async.AddRequestLog(RequestLog{Endpoint: "/sync-fallback"})
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	if !async.mu.TryLock() {
		close(unblock)
		<-done
		t.Fatal("synchronous persistence held the lifecycle mutex")
	}
	async.mu.Unlock()
	close(unblock)
	<-done
	async.Flush()
}

func TestAsyncLogStoreRecordsWorkerFailure(t *testing.T) {
	async := NewAsyncLogStore(&countingStore{fail: true}, 8)
	if err := async.AddRequestLog(RequestLog{Endpoint: "/failure"}); err != nil {
		t.Fatalf("async enqueue should not fail: %v", err)
	}
	async.Flush()
	if health := async.Health(); health.WriteFailures != 1 || health.LastFailureUnixMilli == 0 {
		t.Fatalf("worker failure health = %+v", health)
	}
}

func TestAsyncLogStoreCloseFlushesAndClosesUnderlying(t *testing.T) {
	backing := &countingStore{}
	async := NewAsyncLogStore(backing, 16)

	for i := 0; i < 10; i++ {
		_ = async.AddRequestLog(RequestLog{Endpoint: "/v1/responses"})
	}
	if err := async.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := backing.count(); got != 10 {
		t.Fatalf("Close did not drain: got %d, want 10", got)
	}
	if !backing.closed.Load() {
		t.Fatalf("Close did not close the underlying store")
	}
}

func TestAsyncLogStoreAddAfterFlushIsSynchronous(t *testing.T) {
	backing := &countingStore{}
	async := NewAsyncLogStore(backing, 8)
	async.Flush()

	// After flush the channel is closed; AddRequestLog must not panic on a
	// closed channel and must still persist synchronously.
	if err := async.AddRequestLog(RequestLog{Endpoint: "/late"}); err != nil {
		t.Fatalf("AddRequestLog after flush: %v", err)
	}
	if got := backing.count(); got != 1 {
		t.Fatalf("post-flush log not written synchronously: got %d", got)
	}
}

func TestAsyncLogStoreConcurrentWriters(t *testing.T) {
	backing := &countingStore{}
	async := NewAsyncLogStore(backing, 256)

	const writers = 16
	const each = 100
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				_ = async.AddRequestLog(RequestLog{Endpoint: "/concurrent"})
			}
		}()
	}
	wg.Wait()
	async.Flush()

	if got := backing.count(); got != writers*each {
		t.Fatalf("concurrent writes lost: got %d, want %d", got, writers*each)
	}
}

// TestAsyncLogStoreFlushPersistsToDisk is the end-to-end durability proof for
// graceful shutdown: logs enqueued through the async wrapper over a real
// FileStore must be readable from disk after Flush (mirrors what closeStore does
// on SIGINT/SIGTERM).
func TestAsyncLogStoreFlushPersistsToDisk(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	async := NewAsyncLogStore(fs, 64)

	const n = 25
	for i := 0; i < n; i++ {
		if err := async.AddRequestLog(RequestLog{Endpoint: "/v1/chat/completions", Status: "200"}); err != nil {
			t.Fatalf("AddRequestLog: %v", err)
		}
	}
	async.Flush() // graceful shutdown drains the queue to disk

	// Re-read via the underlying store (same as a fresh process would).
	logs, err := fs.RecentRequestLogs(0)
	if err != nil {
		t.Fatalf("RecentRequestLogs: %v", err)
	}
	if len(logs) != n {
		t.Fatalf("after flush, disk has %d logs, want %d", len(logs), n)
	}
}
