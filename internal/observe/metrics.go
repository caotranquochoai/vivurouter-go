package observe

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics holds lightweight runtime counters for dashboard observability.
// All counters are process-local and reset on restart.
type Metrics struct {
	startedAt     time.Time
	totalRequests atomic.Int64
	inFlight      atomic.Int64
	upstreamFails atomic.Int64

	mu        sync.Mutex
	byStatus  map[int]int64
	byOutcome map[string]int64
}

// MetricsSnapshot is a point-in-time copy safe to hand to templates/JSON.
type MetricsSnapshot struct {
	StartedAt     time.Time        `json:"started_at"`
	UptimeSeconds int64            `json:"uptime_seconds"`
	TotalRequests int64            `json:"total_requests"`
	InFlight      int64            `json:"in_flight"`
	UpstreamFails int64            `json:"upstream_fails"`
	ByStatus      map[string]int64 `json:"by_status"`
	ByOutcome     map[string]int64 `json:"by_outcome"`
}

func newMetrics(now time.Time) *Metrics {
	return &Metrics{
		startedAt: now,
		byStatus:  map[int]int64{},
		byOutcome: map[string]int64{},
	}
}

// BeginRequest increments in-flight and total counters and returns a done func.
func (m *Metrics) BeginRequest() func(status int) {
	m.inFlight.Add(1)
	m.totalRequests.Add(1)
	return func(status int) {
		m.inFlight.Add(-1)
		if status <= 0 {
			return
		}
		bucket := (status / 100) * 100
		m.mu.Lock()
		m.byStatus[bucket]++
		m.mu.Unlock()
	}
}

// RecordOutcome tags a gateway-level outcome such as "fallback" or "exhausted".
func (m *Metrics) RecordOutcome(outcome string) {
	if outcome == "" {
		return
	}
	m.mu.Lock()
	m.byOutcome[outcome]++
	m.mu.Unlock()
}

// RecordUpstreamFailure counts a single failed upstream attempt.
func (m *Metrics) RecordUpstreamFailure() {
	m.upstreamFails.Add(1)
}

// Snapshot copies the current counters.
func (m *Metrics) Snapshot(now time.Time) MetricsSnapshot {
	m.mu.Lock()
	byStatus := make(map[string]int64, len(m.byStatus))
	for bucket, count := range m.byStatus {
		byStatus[statusLabel(bucket)] = count
	}
	byOutcome := make(map[string]int64, len(m.byOutcome))
	for outcome, count := range m.byOutcome {
		byOutcome[outcome] = count
	}
	m.mu.Unlock()

	return MetricsSnapshot{
		StartedAt:     m.startedAt,
		UptimeSeconds: int64(now.Sub(m.startedAt).Seconds()),
		TotalRequests: m.totalRequests.Load(),
		InFlight:      m.inFlight.Load(),
		UpstreamFails: m.upstreamFails.Load(),
		ByStatus:      byStatus,
		ByOutcome:     byOutcome,
	}
}

func statusLabel(bucket int) string {
	switch bucket {
	case 200:
		return "2xx"
	case 300:
		return "3xx"
	case 400:
		return "4xx"
	case 500:
		return "5xx"
	default:
		if bucket <= 0 {
			return "other"
		}
		return statusDigits(bucket)
	}
}

func statusDigits(bucket int) string {
	// Render e.g. 100 -> "1xx".
	first := bucket / 100
	return string(rune('0'+first)) + "xx"
}
