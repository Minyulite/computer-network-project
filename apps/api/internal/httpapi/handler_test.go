package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/computer-network/project/apps/api/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHealthUnavailable(t *testing.T) {
	config, err := pgxpool.ParseConfig("postgres://nobody:unused@127.0.0.1:1/absent?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	recorder := httptest.NewRecorder()
	NewHandler(&store.DB{Pool: pool}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusServiceUnavailable || recorder.Body.String() != "{\"status\":\"unavailable\"}\n" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}
