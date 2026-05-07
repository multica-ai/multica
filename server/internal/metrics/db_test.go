package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDBCollectorExposesPoolStats(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://multica:multica@127.0.0.1:1/multica?sslmode=disable")
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()

	registry := NewRegistry(RegistryOptions{Pool: pool})
	rec := httptest.NewRecorder()
	NewHandler(registry.Gatherer).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	for _, want := range []string{
		"multica_db_pool_acquired_conns",
		"multica_db_pool_idle_conns",
		"multica_db_pool_max_conns",
		"multica_db_pool_acquire_duration_seconds_total",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\n%s", want, body)
		}
	}
}
