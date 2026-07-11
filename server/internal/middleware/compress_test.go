package middleware

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// TestCompressMiddlewareGzipsLargeDaemonResponse verifies the gzip compression
// wired onto the /api/daemon route group for #4072: a large daemon response
// (the task-claim payload inlines every bound skill's full content and can
// reach many MB) is returned gzip-compressed when the client advertises
// Accept-Encoding: gzip. The daemon client uses http.DefaultTransport.Clone(),
// which sends that header transparently and decompresses the response, so
// compression keeps large claim bodies inside the 30s read-timeout window.
//
// The assertion is over the middleware wiring used in cmd/server/router.go
// (chimw.Compress(5) applied to the daemon route group), reproduced here with a
// minimal router so the test does not require a database.
func TestCompressMiddlewareGzipsLargeDaemonResponse(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/daemon", func(r chi.Router) {
		r.Use(chimw.Compress(5))
		r.Post("/runtimes/{runtimeId}/tasks/claim", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Simulate a large claim payload (e.g. many inlined skills).
			_ = json.NewEncoder(w).Encode(map[string]any{
				"skills": strings.Repeat("a", 64*1024),
			})
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/runtimes/rt-1/tasks/claim", nil)
	req.Header.Set("Accept-Encoding", "gzip") // sent by the daemon's default Go transport
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected Content-Encoding gzip for large daemon response, got %q (compression not applied?)", got)
	}

	gr, err := gzip.NewReader(rec.Result().Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()
	decoded, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	if !strings.Contains(string(decoded), "skills") {
		t.Fatalf("decompressed body missing payload: %q", decoded)
	}
}

// TestCompressMiddlewareRespectsAcceptEncoding confirms the wiring only
// compresses when the client advertises Accept-Encoding: gzip — a request
// without it (e.g. a plain curl, or a client that opts out) gets an
// uncompressed response, so compression never breaks non-gzip clients.
func TestCompressMiddlewareRespectsAcceptEncoding(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/daemon", func(r chi.Router) {
		r.Use(chimw.Compress(5))
		r.Get("/tasks/{taskId}/status", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "running"})
		})
	})

	// No Accept-Encoding header -> the response must stay uncompressed.
	req := httptest.NewRequest(http.MethodGet, "/api/daemon/tasks/t-1/status", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got == "gzip" {
		t.Fatalf("response should not be gzip-compressed without Accept-Encoding: gzip, got Content-Encoding: gzip")
	}
}
