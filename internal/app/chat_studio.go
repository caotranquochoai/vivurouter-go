package app

import (
	"net/http"

	"github.com/local/vivurouter-go/internal/dashboard"
	"github.com/local/vivurouter-go/internal/gateway"
)

// chatStudioCompletions lets an authenticated dashboard session use the normal
// gateway path without exposing a gateway or provider credential to JavaScript.
func chatStudioCompletions(dash *dashboard.Handlers, gw *gateway.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !dash.RequireAdminAPI(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// The dashboard session is the authorization boundary for this endpoint.
		// Gateway's internal entry point bypasses only API-key authentication while
		// preserving routing, quotas, request logging, deadlines and cancellation.
		gw.ChatCompletionsFromDashboard(w, r)
	}
}
