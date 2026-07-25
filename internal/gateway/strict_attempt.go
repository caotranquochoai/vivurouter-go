package gateway

import "context"

type strictSingleAttemptContextKey struct{}

func withStrictSingleAttempt(ctx context.Context, enabled bool) context.Context {
	if !enabled {
		return ctx
	}
	return context.WithValue(ctx, strictSingleAttemptContextKey{}, true)
}

func strictSingleAttempt(ctx context.Context) bool {
	enabled, _ := ctx.Value(strictSingleAttemptContextKey{}).(bool)
	return enabled
}

func consumeStrictSingleAttempt(body map[string]any, dashboardRequest bool) bool {
	enabled, _ := body["vivurouter_disable_fallback"].(bool)
	delete(body, "vivurouter_disable_fallback")
	return dashboardRequest && enabled
}
