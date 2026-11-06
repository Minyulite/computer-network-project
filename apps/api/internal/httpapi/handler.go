package httpapi

import (
	"context"
	"net/http"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/config"
)

// Pinger reports whether the database is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps are what the HTTP API routes to.
type Deps struct {
	DB        Pinger
	Auth      *auth.Service
	Config    config.Config
	WebSocket http.Handler // Optional; mounted at GET /ws when set.
}

func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler(d.DB))
	mux.HandleFunc("POST /auth/signup", signupHandler(d.Auth, d.Config))
	mux.HandleFunc("POST /auth/login", loginHandler(d.Auth, d.Config))
	mux.HandleFunc("GET /auth/me", meHandler(d.Auth))
	mux.HandleFunc("POST /auth/logout", logoutHandler(d.Auth, d.Config))
	if d.WebSocket != nil {
		mux.Handle("GET /ws", d.WebSocket)
	}
	return withAuthOrigin(d.Config.AllowedOrigins, mux)
}

func healthHandler(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, code := "ok", http.StatusOK
		if err := db.Ping(r.Context()); err != nil {
			status, code = "unavailable", http.StatusServiceUnavailable
		}
		writeJSON(w, code, struct {
			Status string `json:"status"`
		}{Status: status})
	}
}
