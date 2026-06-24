package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/observe"
	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

const (
	networkCooldown = 10 * time.Second
	serverCooldown  = 5 * time.Second
	rateLimitFloor  = 30 * time.Second
	maxCooldown     = 5 * time.Minute
)

type attemptFunc func(ctx context.Context, cand resolvedModel) (*provider.ExecuteResult, error)
type commitFunc func(w http.ResponseWriter, r *http.Request, result *provider.ExecuteResult, cand resolvedModel) (usageInfo, error)
type requestBodyFunc func(cand resolvedModel, result *provider.ExecuteResult) map[string]any

// runWithFallback drives one logical gateway request across an ordered list of
// provider candidates. It commits the first usable response to the client and
// only falls through on connection errors or fallback-eligible status codes
// (429/5xx), recording cooldowns so a failing provider is skipped next time.
func (h *Handler) runWithFallback(
	w http.ResponseWriter,
	r *http.Request,
	started time.Time,
	endpoint string,
	stream bool,
	candidates []resolvedModel,
	attempt attemptFunc,
	commit commitFunc,
	apiKey store.APIKeyPolicy,
	requestBodyForUsage requestBodyFunc,
	debugBody map[string]any,
	upstreamMeta upstreamOptimizationMeta,
	optimizeDurationMS int64,
	routerDecision promptRouterDecision,
) {
	attempted := 0
	for i := range candidates {
		cand := candidates[i]
		if !h.observe.Cooldowns.Available(cand.Provider.ID, time.Now().UTC()) {
			continue
		}
		attempted++
		providerStarted := time.Now()

		result, err := attempt(r.Context(), cand)
		if err != nil && r.Context().Err() == nil {
			// One in-place retry for transient connection failures.
			result, err = attempt(r.Context(), cand)
		}
		if err != nil {
			if r.Context().Err() != nil {
				return // client disconnected; nothing to send
			}
			h.observe.Metrics.RecordUpstreamFailure()
			h.observe.Cooldowns.Penalize(cand.Provider.ID, time.Now().UTC(), networkCooldown, "network")
			usage := usageInfo{}.ensureEstimated(requestBodyForUsage(cand, nil), 0).withCost(cand.Provider, cand.Model)
			h.logRequest(endpoint, cand, stream, started, "FAILED", err.Error(), usage, apiKey, debugBody, upstreamMeta, optimizeDurationMS, time.Since(providerStarted).Milliseconds(), routerDecision)
			if hasAvailableAfter(candidates, i, h.observe.Cooldowns) {
				h.observe.Metrics.RecordOutcome("fallback")
				continue
			}
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}

		resp := result.Response
		status := resp.StatusCode
		if isFallbackEligible(status) && hasAvailableAfter(candidates, i, h.observe.Cooldowns) {
			h.observe.Metrics.RecordUpstreamFailure()
			h.observe.Cooldowns.Penalize(cand.Provider.ID, time.Now().UTC(), cooldownForStatus(status, resp.Header), fmt.Sprintf("status_%d", status))
			h.observe.Metrics.RecordOutcome("fallback")
			usage := usageInfo{}.ensureEstimated(requestBodyForUsage(cand, result), 0).withCost(cand.Provider, cand.Model)
			h.logRequest(endpoint, cand, stream, started, strconv.Itoa(status), "", usage, apiKey, debugBody, upstreamMeta, optimizeDurationMS, time.Since(providerStarted).Milliseconds(), routerDecision)
			resp.Body.Close()
			continue
		}

		usage, commitErr := commit(w, r, result, cand)
		usage = usage.ensureEstimated(requestBodyForUsage(cand, result), 0).withCost(cand.Provider, cand.Model)
		resp.Body.Close()
		statusStr := strconv.Itoa(status)
		if commitErr != nil {
			statusStr = "STREAM_ERROR"
		}
		h.logRequest(endpoint, cand, stream, started, statusStr, errString(commitErr), usage, apiKey, debugBody, upstreamMeta, optimizeDurationMS, time.Since(providerStarted).Milliseconds(), routerDecision)
		return
	}

	if attempted == 0 {
		h.observe.Metrics.RecordOutcome("all_cooldown")
		writeError(w, http.StatusServiceUnavailable, "all candidate providers are in cooldown")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "no provider could serve the request")
}

func hasAvailableAfter(candidates []resolvedModel, idx int, cooldowns *observe.CooldownTracker) bool {
	now := time.Now().UTC()
	for j := idx + 1; j < len(candidates); j++ {
		if cooldowns.Available(candidates[j].Provider.ID, now) {
			return true
		}
	}
	return false
}

func isFallbackEligible(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func cooldownForStatus(status int, header http.Header) time.Duration {
	var dur time.Duration
	if status == http.StatusTooManyRequests {
		if d := parseRetryAfter(header.Get("Retry-After")); d > 0 {
			dur = d
		} else {
			dur = rateLimitFloor
		}
	} else {
		dur = serverCooldown
	}
	if dur > maxCooldown {
		dur = maxCooldown
	}
	return dur
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(value); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
