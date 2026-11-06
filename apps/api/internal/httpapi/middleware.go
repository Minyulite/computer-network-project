package httpapi

import (
	"net/http"
	"strings"
)

// withAuthOrigin guards /auth/ routes against cross-site requests and answers
// CORS for the allowed origins. A POST or preflight must name its origin;
// other paths pass through untouched (the WebSocket checks its own origin).
func withAuthOrigin(allowedOrigins []string, next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		origins := r.Header.Values("Origin")
		origin := ""
		if len(origins) == 1 {
			origin = origins[0]
		}
		if len(origins) > 1 || (origin != "" && !allowed[origin]) ||
			((r.Method == http.MethodPost || r.Method == http.MethodOptions) && origin == "") {
			writeError(w, http.StatusForbidden, "origin_forbidden", "Origin is not allowed")
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
