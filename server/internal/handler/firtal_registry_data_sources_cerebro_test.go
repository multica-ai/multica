package handler

// CEREBRO-PATCH(firtal-registry-data-sources-test): TECH-2925 admin proxy coverage.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeFirtalRegistryDataSources_ArrayShape(t *testing.T) {
	raw := []byte(`[
		{"id":"ds-1","name":"Orders","description":"Order lines"},
		{"id":"ds-2","name":"Products"}
	]`)
	items, err := normalizeFirtalRegistryDataSources(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].ID != "ds-1" || items[0].Name != "Orders" || items[0].Description == nil || *items[0].Description != "Order lines" {
		t.Fatalf("first item: %+v", items[0])
	}
}

func TestNormalizeFirtalRegistryDataSources_EnvelopeShape(t *testing.T) {
	raw := []byte(`{"data":[{"id":"abc","name":"ABC"}]}`)
	items, err := normalizeFirtalRegistryDataSources(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(items) != 1 || items[0].ID != "abc" {
		t.Fatalf("got %+v", items)
	}
}

func TestListFirtalRegistryDataSources_HappyPath(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != firtalRegistryListPath {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-registry-key" {
			t.Fatalf("x-api-key = %q", got)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "ds-a", "name": "Source A"},
		})
	}))
	defer upstream.Close()

	ctx := context.Background()
	settings, _ := json.Marshal(map[string]any{
		"data_registry": map[string]string{
			"base_url": upstream.URL,
			"api_key":  "test-registry-key",
		},
	})
	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET settings = $1::jsonb WHERE id = $2`,
		settings, testWorkspaceID,
	); err != nil {
		t.Fatalf("patch workspace settings: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `UPDATE workspace SET settings = '{}'::jsonb WHERE id = $1`, testWorkspaceID)
		firtalRegistryDataSourcesCacheMu.Lock()
		delete(firtalRegistryDataSourcesCache, testWorkspaceID)
		firtalRegistryDataSourcesCacheMu.Unlock()
	})

	agentID := createHandlerTestAgent(t, "firtal-registry-ds", []byte(`{}`))

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/firtal-registry/data-sources", nil), "id", agentID)
	testHandler.ListFirtalRegistryDataSources(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var items []FirtalRegistryDataSource
	if err := json.NewDecoder(w.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 1 || items[0].ID != "ds-a" || items[0].Name != "Source A" {
		t.Fatalf("items = %+v", items)
	}
}

func TestListFirtalRegistryDataSources_MissingCredentials(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET settings = '{}'::jsonb WHERE id = $1`,
		testWorkspaceID,
	); err != nil {
		t.Fatalf("clear workspace settings: %v", err)
	}
	t.Cleanup(func() {
		firtalRegistryDataSourcesCacheMu.Lock()
		delete(firtalRegistryDataSourcesCache, testWorkspaceID)
		firtalRegistryDataSourcesCacheMu.Unlock()
	})

	agentID := createHandlerTestAgent(t, "firtal-registry-no-creds", []byte(`{}`))

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/firtal-registry/data-sources", nil), "id", agentID)
	testHandler.ListFirtalRegistryDataSources(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}
