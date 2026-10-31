package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/config"
	"example.com/computer-network/project/apps/api/internal/store"
)

func NewHandler(db *store.DB, authService *auth.Service, cfg config.Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		status := "ok"
		w.Header().Set("Content-Type", "application/json")
		if err := db.Ping(r.Context()); err != nil {
			status = "unavailable"
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		if err := json.NewEncoder(w).Encode(struct {
			Status string `json:"status"`
		}{Status: status}); err != nil {
			return
		}
	})
	mux.HandleFunc("POST /auth/signup", signupHandler(authService, cfg))
	mux.HandleFunc("POST /auth/login", loginHandler(authService, cfg))
	mux.HandleFunc("GET /auth/me", meHandler(authService))
	mux.HandleFunc("POST /auth/logout", logoutHandler(authService, cfg))
	allowed := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		allowed[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/auth/") {
			mux.ServeHTTP(w, r)
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
		mux.ServeHTTP(w, r)
	})
}
