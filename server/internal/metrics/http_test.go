package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestHTTPMiddlewareUsesRoutePatternLabels(t *testing.T) {
	registry := NewRegistry(RegistryOptions{
		Version: "v-test",
		Commit:  "abc123",
	})

	r := chi.NewRouter()
	r.Use(registry.HTTP.Middleware)
	r.Get("/api/issues/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/api/issues/secret-issue-id?token=secret-token", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("request status = %d, want %d", rec.Code, http.StatusCreated)
	}

	metricsRec := httptest.NewRecorder()
	NewHandler(registry.Gatherer).ServeHTTP(metricsRec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := metricsRec.Body.String()

	for _, want := range []string{
		`multica_http_requests_total{method="GET",route="/api/issues/{id}",status="201"} 1`,
		`multica_build_info{commit="abc123",version="v-test"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\n%s", want, body)
		}
	}
	for _, leaked := range []string{"secret-issue-id", "secret-token"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("metrics body leaked %q\n%s", leaked, body)
		}
	}
}

func TestMetricsHandlerOnlyServesMetricsPath(t *testing.T) {
	registry := NewRegistry(RegistryOptions{})
	handler := NewHandler(registry.Gatherer)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body, _ := io.ReadAll(rec.Body); !strings.Contains(string(body), "multica_build_info") {
		t.Fatalf("/metrics body missing build info: %s", body)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/health status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
