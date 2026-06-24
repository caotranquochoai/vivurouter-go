package observe

import "time"

// State bundles the runtime observability primitives shared across the server.
type State struct {
	Metrics   *Metrics
	Cooldowns *CooldownTracker
}

// New builds a fresh observability state stamped with the current time.
func New() *State {
	return &State{
		Metrics:   newMetrics(time.Now().UTC()),
		Cooldowns: newCooldownTracker(),
	}
}
