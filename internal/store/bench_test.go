package store

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// sampleRequestLog is a representative log entry for write benchmarks.
func sampleRequestLog() RequestLog {
	return RequestLog{
		Endpoint:         "/v1/chat/completions",
		ProviderID:       "openai",
		Model:            "gpt-4o",
		Status:           "200",
		DurationMS:       842,
		Stream:           true,
		PromptTokens:     1200,
		CompletionTokens: 350,
		TotalTokens:      1550,
		CachedTokens:     128,
		ReasoningTokens:  64,
		Timestamp:        time.Unix(1_700_000_000, 0).UTC(),
		APIKeyID:         "key-abcd",
		APIKeyMasked:     "sk-***-***wxyz",
	}
}

// BenchmarkFileAddRequestLogSync measures the synchronous write path: every
// AddRequestLog rewrites the whole request-logs file. This ~ms/op cost is what
// sat on the gateway response path before the async wrapper moved it to a worker.
func BenchmarkFileAddRequestLogSync(b *testing.B) {
	// Use a manually managed dir: b.TempDir()'s automatic RemoveAll can race with
	// the store's .tmp rename on Windows and fail cleanup spuriously.
	dir, err := os.MkdirTemp("", "vr-bench-sync")
	if err != nil {
		b.Fatalf("MkdirTemp: %v", err)
	}
	b.Cleanup(func() { _ = os.RemoveAll(dir) })
	fs, err := NewFileStore(dir)
	if err != nil {
		b.Fatalf("NewFileStore: %v", err)
	}
	log := sampleRequestLog()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := fs.AddRequestLog(log); err != nil {
			b.Fatalf("AddRequestLog: %v", err)
		}
	}
	b.StopTimer()
}

// BenchmarkAsyncEnqueueCost measures the caller-observed cost of AddRequestLog
// through the async wrapper when the backend is instant (in-memory). This
// isolates the latency the gateway actually pays per request once disk I/O is
// off the critical path — the whole point of the wrapper. Under real (non-
// saturating) traffic the enqueue returns in this time instead of the ~ms sync
// file write above.
func BenchmarkAsyncEnqueueCost(b *testing.B) {
	async := NewAsyncLogStore(&countingStore{}, b.N+1)
	defer async.Flush()
	log := sampleRequestLog()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := async.AddRequestLog(log); err != nil {
			b.Fatalf("AddRequestLog: %v", err)
		}
	}
}

// BenchmarkMarshalLogsIndent and BenchmarkMarshalLogsCompact measure the B2
// change directly: request logs are serialized on every write, and compact
// Marshal is cheaper and smaller than MarshalIndent.
func benchLogs() requestLogsFile {
	logs := make([]RequestLog, 200)
	for i := range logs {
		logs[i] = sampleRequestLog()
	}
	return requestLogsFile{RequestLogs: logs}
}

func BenchmarkMarshalLogsIndent(b *testing.B) {
	data := benchLogs()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.MarshalIndent(data, "", "  "); err != nil {
			b.Fatalf("MarshalIndent: %v", err)
		}
	}
}

func BenchmarkMarshalLogsCompact(b *testing.B) {
	data := benchLogs()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(data); err != nil {
			b.Fatalf("Marshal: %v", err)
		}
	}
}
