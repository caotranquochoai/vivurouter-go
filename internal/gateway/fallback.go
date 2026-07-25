package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
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

// perProviderAttemptTimeout caps one provider attempt. The request-level
// deadline remains authoritative and includes all retries and fallbacks.
var perProviderAttemptTimeout = 120 * time.Second

type attemptFunc func(ctx context.Context, cand resolvedModel) (*provider.ExecuteResult, error)
type commitFunc func(w http.ResponseWriter, r *http.Request, result *provider.ExecuteResult, cand resolvedModel) (usageInfo, error)
type requestBodyFunc func(cand resolvedModel, result *provider.ExecuteResult) map[string]any

// runWithFallback drives a request through ordered candidates using a single
// deadline budget. It never starts a retry/fallback which would consume the
// reserved budget needed to make a useful next attempt.
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
	settings store.Settings,
) {
	requestCtx, cancelRequest := withGatewayDeadline(r.Context(), h.requestTimeout)
	defer cancelRequest()

	candidates = h.expandAccountCandidates(requestCtx, candidates, time.Now().UTC())
	if strictSingleAttempt(requestCtx) && len(candidates) > 1 {
		candidates = candidates[:1]
	}
	attempted := 0
	trace := []attemptTraceEntry{}
	for i := range candidates {
		cand := candidates[i]
		now := time.Now().UTC()
		if !h.observe.Cooldowns.Available(cand.Provider.ID, now) {
			trace = appendTrace(trace, cand, "skipped_cooldown", failureDecision{}, 0, now, "")
			continue
		}
		if !hasRemainingBudget(requestCtx, h.minFallbackBudget) {
			break
		}
		attempted++
		providerStarted := time.Now()

		result, err, decision := h.executeCandidate(requestCtx, cand, attempt)
		if err != nil {
			if requestCtx.Err() != nil {
				if r.Context().Err() != nil {
					return // the caller disconnected; no response can be committed.
				}
				writeGatewayError(w, failurePublicError(failureDecision{Class: FailureTimeout}))
				return
			}
			h.observe.Metrics.RecordUpstreamFailure()
			if decision.Cooldown > 0 && cand.AccountID == "" {
				h.observe.Cooldowns.Penalize(cand.Provider.ID, time.Now().UTC(), decision.Cooldown, decision.CooldownReason)
			}
			h.recordAccountOutcome(cand, decision, false, time.Now().UTC())
			trace = appendTrace(trace, cand, "failed", decision, 0, providerStarted, err.Error())
			usage := usageInfo{}.ensureEstimated(requestBodyForUsage(cand, nil), 0).withCost(cand.Provider, cand.Model)
			h.logRequest(settings, endpoint, cand, stream, started, "FAILED", err.Error(), usage, apiKey, debugBody, upstreamMeta, optimizeDurationMS, time.Since(providerStarted).Milliseconds(), routerDecision)
			if decision.Fallback && hasAvailableAfter(candidates, i, h.observe.Cooldowns) && hasRemainingBudget(requestCtx, h.minFallbackBudget) {
				h.observe.Metrics.RecordOutcome("fallback")
				continue
			}
			writeGatewayError(w, failurePublicError(decision))
			return
		}
		if result == nil || result.Response == nil {
			err = fmt.Errorf("provider %s returned no response", cand.Provider.ID)
			decision = failureDecision{Class: FailureProtocol, Fallback: true, Cooldown: networkCooldown, CooldownReason: "empty_response"}
			h.observe.Metrics.RecordUpstreamFailure()
			h.observe.Cooldowns.Penalize(cand.Provider.ID, time.Now().UTC(), decision.Cooldown, decision.CooldownReason)
			trace = appendTrace(trace, cand, "failed", decision, 0, providerStarted, err.Error())
			usage := usageInfo{}.ensureEstimated(requestBodyForUsage(cand, nil), 0).withCost(cand.Provider, cand.Model)
			h.logRequest(settings, endpoint, cand, stream, started, "FAILED", err.Error(), usage, apiKey, debugBody, upstreamMeta, optimizeDurationMS, time.Since(providerStarted).Milliseconds(), routerDecision)
			if hasAvailableAfter(candidates, i, h.observe.Cooldowns) && hasRemainingBudget(requestCtx, h.minFallbackBudget) {
				h.observe.Metrics.RecordOutcome("fallback")
				continue
			}
			writeGatewayError(w, failurePublicError(decision))
			return
		}

		resp := result.Response
		status := resp.StatusCode
		decision = classifyResponse(status, resp.Header)
		if decision.Class == FailureInvalidRequest {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
			if readErr == nil && isContextOverflowMessage(string(body)) {
				decision = failureDecision{Class: FailureContextOverflow, Fallback: true}
				resp.Body = io.NopCloser(bytes.NewReader(body))
			}
		}
		if decision.Class != "" {
			h.observe.Metrics.RecordUpstreamFailure()
			if decision.Cooldown > 0 && cand.AccountID == "" {
				h.observe.Cooldowns.Penalize(cand.Provider.ID, time.Now().UTC(), decision.Cooldown, decision.CooldownReason)
			}
			h.recordAccountOutcome(cand, decision, false, time.Now().UTC())
			trace = appendTrace(trace, cand, "response_failed", decision, status, providerStarted, "")
			usage := usageInfo{}.ensureEstimated(requestBodyForUsage(cand, result), 0).withCost(cand.Provider, cand.Model)
			h.logRequest(settings, endpoint, cand, stream, started, strconv.Itoa(status), string(decision.Class), usage, apiKey, debugBody, upstreamMeta, optimizeDurationMS, time.Since(providerStarted).Milliseconds(), routerDecision)
			resp.Body.Close()
			if decision.Fallback && hasAvailableAfter(candidates, i, h.observe.Cooldowns) && hasRemainingBudget(requestCtx, h.minFallbackBudget) {
				h.observe.Metrics.RecordOutcome("fallback")
				continue
			}
			writeGatewayError(w, failurePublicError(decision))
			return
		}

		usage, commitErr := commit(w, r, result, cand)
		usage = usage.ensureEstimated(requestBodyForUsage(cand, result), 0).withCost(cand.Provider, cand.Model)
		resp.Body.Close()
		statusStr := strconv.Itoa(status)
		if commitErr != nil {
			statusStr = "STREAM_ERROR"
		}
		trace = appendTrace(trace, cand, "success", failureDecision{}, status, providerStarted, "")
		h.recordAccountOutcome(cand, failureDecision{}, true, time.Now().UTC())
		h.logRequest(settings, endpoint, cand, stream, started, statusStr, errString(commitErr), usage, apiKey, debugBody, upstreamMeta, optimizeDurationMS, time.Since(providerStarted).Milliseconds(), routerDecision)
		return
	}

	_ = trace // structured trace is retained in-process for Phase 3 request-log enrichment.
	if requestCtx.Err() != nil {
		if r.Context().Err() == nil {
			writeGatewayError(w, failurePublicError(failureDecision{Class: FailureTimeout}))
		}
		return
	}
	if attempted == 0 {
		h.observe.Metrics.RecordOutcome("all_cooldown")
		writeErrorCode(w, http.StatusServiceUnavailable, "all candidate providers are in cooldown", "all_candidates_unavailable")
		return
	}
	writeErrorCode(w, http.StatusServiceUnavailable, "no provider could serve the request", "no_provider_available")
}

func (h *Handler) executeCandidate(ctx context.Context, cand resolvedModel, attempt attemptFunc) (*provider.ExecuteResult, error, failureDecision) {
	attemptCtx, cancel, accept := withAttemptTimeout(ctx, perProviderAttemptTimeout)
	result, err := attempt(attemptCtx, cand)
	decision := classifyAttemptError(attemptCtx, err)
	if err == nil {
		if result != nil && result.Response != nil && result.Response.Body != nil {
			accept()
			result.Response.Body = &cancelOnClose{ReadCloser: result.Response.Body, cancel: cancel}
		} else {
			cancel()
		}
		return result, nil, decision
	}
	cancel()
	if strictSingleAttempt(ctx) || !decision.RetrySame || !hasRemainingBudget(ctx, h.minFallbackBudget) {
		return result, err, decision
	}

	retryCtx, cancelRetry, acceptRetry := withAttemptTimeout(ctx, perProviderAttemptTimeout)
	result, err = attempt(retryCtx, cand)
	decision = classifyAttemptError(retryCtx, err)
	if err == nil {
		if result != nil && result.Response != nil && result.Response.Body != nil {
			acceptRetry()
			result.Response.Body = &cancelOnClose{ReadCloser: result.Response.Body, cancel: cancelRetry}
		} else {
			cancelRetry()
		}
		return result, nil, decision
	}
	cancelRetry()
	return result, err, decision
}

// cancelOnClose retains client/request cancellation while a successful upstream
// body is consumed. The shorter provider-attempt timer is stopped once headers
// are accepted so it cannot truncate a healthy long-running stream.
type cancelOnClose struct {
	io.ReadCloser
	cancel func()
	once   sync.Once
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.once.Do(c.cancel)
	return err
}

func (h *Handler) expandAccountCandidates(ctx context.Context, candidates []resolvedModel, now time.Time) []resolvedModel {
	out := make([]resolvedModel, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Provider.Type != store.ProviderOpenAICompatible && candidate.Provider.Type != store.ProviderCodex && candidate.Provider.Type != store.ProviderAntigravity {
			out = append(out, candidate)
			continue
		}
		accounts, err := h.store.ListProviderAccounts(candidate.Provider.ID)
		if err != nil || len(accounts) == 0 {
			out = append(out, candidate) // legacy provider credentials remain supported.
			continue
		}
		added := 0
		for _, account := range accounts {
			if !account.Enabled || (!account.CooldownUntil.IsZero() && now.Before(account.CooldownUntil)) {
				continue
			}
			if account.ProxyID != "" && strings.TrimSpace(account.ProxyURL) == "" {
				continue // never silently bypass a disabled or missing pool proxy.
			}
			effective := candidate.Provider
			effective.APIKey = account.APIKey
			effective.AccessToken = account.AccessToken
			effective.RefreshToken = account.RefreshToken
			if account.ProxyID != "" || account.ProxyURL != "" {
				effective.ProxyID = account.ProxyID
				effective.ProxyURL = account.ProxyURL
			}
			// Accounts own credentials. Do not accidentally select the logical
			// provider's legacy key pool after selecting an account.
			effective.Keys = nil
			if candidate.Provider.Type == store.ProviderCodex && !h.codexQuotaGate.allows(ctx, account, effective, now) {
				continue
			}
			out = append(out, resolvedModel{Provider: effective, Model: candidate.Model, IsCodex: candidate.IsCodex, AccountID: account.ID, Account: account.Name})
			added++
		}
		if added == 0 {
			continue
		}
	}
	return out
}

func (h *Handler) recordAccountOutcome(cand resolvedModel, decision failureDecision, success bool, now time.Time) {
	if cand.AccountID == "" {
		return
	}
	if success {
		_ = h.store.RecordProviderAccountOutcome(cand.AccountID, store.ProviderAccountOutcome{Success: true, At: now})
		return
	}
	if !accountFailureEligible(decision.Class) {
		return
	}
	account, found, err := h.store.GetProviderAccount(cand.AccountID)
	if err != nil || !found {
		return
	}
	streak := account.FailureStreak + 1
	cooldown := accountCooldown(streak, decision, time.Now())
	_ = h.store.RecordProviderAccountOutcome(cand.AccountID, store.ProviderAccountOutcome{
		IncrementFailure: true,
		CooldownUntil:    now.Add(cooldown),
		CooldownReason:   decision.CooldownReason,
		At:               now,
	})
}

func accountFailureEligible(class FailureClass) bool {
	switch class {
	case FailureAuthentication, FailureRateLimited, FailureQuotaExhausted, FailureTransient, FailureUpstreamServer, FailureTimeout:
		return true
	default:
		return false
	}
}

func accountCooldown(streak int, decision failureDecision, now time.Time) time.Duration {
	if retryAfter := decision.RetryAfter; retryAfter > 0 {
		if retryAfter > 5*time.Minute {
			return 5 * time.Minute
		}
		return retryAfter
	}
	if streak <= 1 {
		return 15 * time.Second
	}
	if streak == 2 {
		return 30 * time.Second
	}
	if streak == 3 {
		return 60 * time.Second
	}
	return 120 * time.Second
}

func withGatewayDeadline(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	if deadline, ok := parent.Deadline(); ok && time.Until(deadline) <= timeout {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

// withAttemptTimeout applies the short provider-attempt timeout while waiting for
// a response. accept stops only that timer; parent cancellation remains attached
// to ctx until cancel is called after the accepted body is closed.
func withAttemptTimeout(parent context.Context, maxAttempt time.Duration) (ctx context.Context, cancel context.CancelFunc, accept func()) {
	ctx, cancelCause := context.WithCancelCause(parent)
	cancel = func() { cancelCause(context.Canceled) }
	if maxAttempt <= 0 {
		return ctx, cancel, func() {}
	}
	if deadline, ok := parent.Deadline(); ok && time.Until(deadline) <= maxAttempt {
		return ctx, cancel, func() {}
	}
	timer := time.AfterFunc(maxAttempt, func() { cancelCause(context.DeadlineExceeded) })
	var once sync.Once
	accept = func() {
		once.Do(func() { timer.Stop() })
	}
	wrappedCancel := func() {
		accept()
		cancelCause(context.Canceled)
	}
	return ctx, wrappedCancel, accept
}

func hasRemainingBudget(ctx context.Context, reserve time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	deadline, ok := ctx.Deadline()
	return !ok || time.Until(deadline) > reserve
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
