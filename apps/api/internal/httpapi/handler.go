package httpapi

import (
	"encoding/json"
	"net/http"

	"example.com/computer-network/project/apps/api/internal/store"
)

func NewHandler(db *store.DB) http.Handler {
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
	return mux
}
