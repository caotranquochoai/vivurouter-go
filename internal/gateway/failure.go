package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// FailureClass is the normalized failure category the gateway policy uses for
// retries, cooldowns, and candidate fallback. Provider implementations retain
// ownership of request serialization and auth refresh; the core owns decisions.
type FailureClass string

const (
	FailureInvalidRequest  FailureClass = "invalid_request"
	FailureAuthentication  FailureClass = "authentication"
	FailureRateLimited     FailureClass = "rate_limited"
	FailureQuotaExhausted  FailureClass = "quota_exhausted"
	FailureTransient       FailureClass = "transient"
	FailureUpstreamServer  FailureClass = "upstream_server"
	FailureTimeout         FailureClass = "timeout"
	FailureCancelled       FailureClass = "cancelled"
	FailureProtocol        FailureClass = "protocol"
	FailureUnsupported     FailureClass = "unsupported"
	FailureContextOverflow FailureClass = "context_overflow"
)

type failureDecision struct {
	Class          FailureClass
	RetrySame      bool
	Fallback       bool
	Cooldown       time.Duration
	RetryAfter     time.Duration
	CooldownReason string
}

type attemptTraceEntry struct {
	ProviderID string       `json:"provider_id"`
	AccountID  string       `json:"account_id,omitempty"`
	Account    string       `json:"account,omitempty"`
	Model      string       `json:"model"`
	Outcome    string       `json:"outcome"`
	Failure    FailureClass `json:"failure,omitempty"`
	Status     int          `json:"status,omitempty"`
	DurationMS int64        `json:"duration_ms"`
	Detail     string       `json:"detail,omitempty"`
}

func classifyAttemptError(ctx context.Context, err error) failureDecision {
	if err == nil {
		return failureDecision{}
	}
	cause := context.Cause(ctx)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(cause, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return failureDecision{Class: FailureTimeout, Fallback: true, Cooldown: networkCooldown, CooldownReason: "timeout"}
	}
	if errors.Is(err, context.Canceled) || errors.Is(cause, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return failureDecision{Class: FailureCancelled}
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) {
		return failureDecision{Class: FailureTransient, RetrySame: true, Fallback: true, Cooldown: networkCooldown, CooldownReason: "network"}
	}
	return failureDecision{Class: FailureTransient, RetrySame: true, Fallback: true, Cooldown: networkCooldown, CooldownReason: "network"}
}

func isContextOverflowMessage(message string) bool {
	message = strings.ToLower(message)
	for _, marker := range []string{
		"context_length_exceeded", "context length exceeded", "maximum context length",
		"context window", "too many tokens", "prompt is too long", "input is too long",
		"maximum number of tokens",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func classifyResponse(status int, header http.Header) failureDecision {
	switch {
	case status >= 200 && status < 300:
		return failureDecision{}
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		return failureDecision{Class: FailureInvalidRequest}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return failureDecision{Class: FailureAuthentication, Fallback: true, Cooldown: 2 * time.Minute, CooldownReason: "authentication"}
	case status == http.StatusTooManyRequests:
		cooldown := cooldownForStatus(status, header)
		return failureDecision{Class: FailureRateLimited, Fallback: true, Cooldown: cooldown, RetryAfter: parseRetryAfter(header.Get("Retry-After")), CooldownReason: "rate_limited"}
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return failureDecision{Class: FailureTimeout, Fallback: true, Cooldown: networkCooldown, CooldownReason: "timeout"}
	case status >= 500:
		return failureDecision{Class: FailureUpstreamServer, RetrySame: true, Fallback: true, Cooldown: cooldownForStatus(status, header), CooldownReason: fmt.Sprintf("status_%d", status)}
	default:
		return failureDecision{Class: FailureProtocol}
	}
}

func failurePublicError(decision failureDecision) error {
	switch decision.Class {
	case FailureInvalidRequest:
		return newGatewayError(http.StatusBadRequest, "invalid_upstream_request", "request was rejected by the selected provider", nil)
	case FailureAuthentication:
		return newGatewayError(http.StatusBadGateway, "upstream_authentication_failed", "selected provider authentication failed", nil)
	case FailureRateLimited, FailureQuotaExhausted:
		return newGatewayError(http.StatusTooManyRequests, "upstream_rate_limited", "provider capacity is temporarily unavailable", nil)
	case FailureTimeout:
		return newGatewayError(http.StatusGatewayTimeout, "gateway_timeout", "gateway request deadline exceeded", nil)
	case FailureContextOverflow:
		return newGatewayError(http.StatusBadRequest, "context_length_exceeded", "request exceeds the selected provider context window", nil)
	default:
		return newGatewayError(http.StatusBadGateway, "upstream_error", "upstream request failed", nil)
	}
}

func appendTrace(entries []attemptTraceEntry, cand resolvedModel, outcome string, decision failureDecision, status int, started time.Time, detail string) []attemptTraceEntry {
	entry := attemptTraceEntry{
		ProviderID: cand.Provider.ID,
		AccountID:  cand.AccountID,
		Account:    cand.Account,
		Model:      cand.Model,
		Outcome:    outcome,
		Failure:    decision.Class,
		Status:     status,
		DurationMS: time.Since(started).Milliseconds(),
		Detail:     strings.TrimSpace(detail),
	}
	return append(entries, entry)
}
