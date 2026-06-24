package observe

import (
	"testing"
	"time"
)

func TestMetricsBeginAndSnapshot(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := newMetrics(now)

	done := m.BeginRequest()
	snap := m.Snapshot(now)
	if snap.InFlight != 1 {
		t.Fatalf("in-flight = %d, want 1", snap.InFlight)
	}
	if snap.TotalRequests != 1 {
		t.Fatalf("total = %d, want 1", snap.TotalRequests)
	}
	done(200)

	m.RecordUpstreamFailure()
	m.RecordOutcome("fallback")

	snap = m.Snapshot(now.Add(5 * time.Second))
	if snap.InFlight != 0 {
		t.Fatalf("in-flight after done = %d, want 0", snap.InFlight)
	}
	if snap.ByStatus["2xx"] != 1 {
		t.Fatalf("2xx = %d, want 1", snap.ByStatus["2xx"])
	}
	if snap.UpstreamFails != 1 {
		t.Fatalf("upstream fails = %d, want 1", snap.UpstreamFails)
	}
	if snap.ByOutcome["fallback"] != 1 {
		t.Fatalf("fallback outcome = %d, want 1", snap.ByOutcome["fallback"])
	}
	if snap.UptimeSeconds != 5 {
		t.Fatalf("uptime = %d, want 5", snap.UptimeSeconds)
	}
}

func TestCooldownPenalizeAndAvailable(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := newCooldownTracker()

	if !c.Available("p1", now) {
		t.Fatal("fresh provider should be available")
	}

	c.Penalize("p1", now, 30*time.Second, "status_429")
	if c.Available("p1", now) {
		t.Fatal("provider should be in cooldown")
	}
	if !c.Available("p1", now.Add(31*time.Second)) {
		t.Fatal("provider should recover after cooldown window")
	}
}

func TestCooldownDoesNotShorten(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := newCooldownTracker()

	c.Penalize("p1", now, 60*time.Second, "long")
	c.Penalize("p1", now, 5*time.Second, "short")

	if c.Available("p1", now.Add(10*time.Second)) {
		t.Fatal("shorter penalty must not override longer cooldown")
	}
}

func TestCooldownSnapshotPrunes(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := newCooldownTracker()
	c.Penalize("a", now, 10*time.Second, "x")
	c.Penalize("b", now, 1*time.Second, "y")

	snap := c.Snapshot(now.Add(2 * time.Second))
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1 (b pruned)", len(snap))
	}
	if snap[0].ProviderID != "a" {
		t.Fatalf("remaining provider = %s, want a", snap[0].ProviderID)
	}
}

func TestParseRetryAfterViaCooldownStatus(t *testing.T) {
	// Indirect coverage lives in the gateway package; here we just ensure the
	// tracker accepts the durations the gateway derives.
	now := time.Now().UTC()
	c := newCooldownTracker()
	c.Penalize("p", now, 0, "noop") // zero duration is a no-op
	if !c.Available("p", now) {
		t.Fatal("zero-duration penalty should not start a cooldown")
	}
}
