package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQoderCatalogPaginationAndRedaction(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("missing credential")
		}
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"env_first","name":"Web","config":{"secret":"do-not-expose"}},{"id":"env_archived","name":"Archived","archived_at":"2026-01-01"}],"has_more":true,"last_id":"env_archived"}`))
		} else {
			if r.URL.Query().Get("after_id") != "env_archived" {
				t.Error("missing pagination cursor")
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"env_second","name":"Build"}],"has_more":false}`))
		}
	}))
	defer server.Close()
	items, err := fetchQoderCatalog(context.Background(), server.URL, "private-token", "environments")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(items) != 2 || items[1].ID != "env_second" {
		t.Fatalf("unexpected catalog: %+v", items)
	}
	raw, _ := json.Marshal(items)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "config") {
		t.Fatal("provider configuration leaked")
	}
}
func TestQoderCatalogRejectsMalformedAndRedactsErrors(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":null}`, `{"data":[{"id":"wrong"}]}`, `{"data":[],"has_more":true}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		if _, err := fetchQoderCatalog(context.Background(), server.URL, "private-token", "environments"); err == nil {
			t.Errorf("accepted %s", body)
		}
		server.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "private-token", 401) }))
	defer server.Close()
	_, err := fetchQoderCatalog(context.Background(), server.URL, "private-token", "agents")
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("provider error was not redacted")
	}
}
