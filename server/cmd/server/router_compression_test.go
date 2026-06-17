package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestDaemonJSONCompressorGzipsJSONWhenAccepted(t *testing.T) {
	router := chi.NewRouter()
	router.Use(daemonJSONCompressor())
	router.Get("/api/daemon/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"payload":"` + strings.Repeat("x", 256) + `"}`))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/daemon/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want Accept-Encoding", got)
	}

	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("open gzip body: %v", err)
	}
	defer zr.Close()
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	if !strings.Contains(string(body), `"payload":"xxx`) {
		t.Fatalf("decoded body missing payload: %s", string(body))
	}
}

func TestDaemonJSONCompressorLeavesPlainJSONWhenGzipNotAccepted(t *testing.T) {
	router := chi.NewRouter()
	router.Use(daemonJSONCompressor())
	router.Get("/api/daemon/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"task":null}`))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/daemon/test", nil)
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if got := rec.Body.String(); got != `{"task":null}` {
		t.Fatalf("body = %q, want plain JSON", got)
	}
}

func TestDaemonJSONCompressorSkipsWebsocketUpgrade(t *testing.T) {
	router := chi.NewRouter()
	router.Use(daemonJSONCompressor())
	router.Get("/api/daemon/ws", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Upgrade"); got != "websocket" {
			t.Fatalf("Upgrade header = %q, want websocket", got)
		}
		w.WriteHeader(http.StatusSwitchingProtocols)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/daemon/ws", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty for websocket upgrade", got)
	}
	if rec.Code != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSwitchingProtocols)
	}
}
