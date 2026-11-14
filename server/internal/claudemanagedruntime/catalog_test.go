package claudemanagedruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCatalogPaginationAndRedaction(t *testing.T) {
	requests := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("missing credential")
		}
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"agent_first","name":"Reviewer","system":"do-not-expose","mcp_servers":[{"token":"do-not-expose"}]},{"id":"agent_archived","name":"Old","archived_at":"2026-01-01"},{"id":"agent_first","name":"Duplicate"}],"next_page":"page_2"}`))
			return
		}
		if r.URL.Query().Get("page") != "page_2" {
			t.Error("missing pagination cursor")
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"agent_second","name":"  "}]}`))
	}))
	defer remote.Close()

	items, err := ListCatalog(context.Background(), remote.URL, "private-token", "agents")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(items) != 2 || items[0].Name != "Reviewer" || items[1].ID != "agent_second" || items[1].Name != "agent_second" {
		t.Fatalf("unexpected catalog after %d requests: %+v", requests, items)
	}
	raw, _ := json.Marshal(items)
	if strings.Contains(string(raw), "do-not-expose") || strings.Contains(string(raw), "system") {
		t.Fatal("provider configuration leaked")
	}
}

func TestCatalogRejectsMalformedResponses(t *testing.T) {
	for name, tc := range map[string]struct{ kind, body string }{
		"missing data":     {"agents", `{}`},
		"null data":        {"agents", `{"data":null}`},
		"foreign identity": {"agents", `{"data":[{"id":"env_1","name":"wrong kind"}]}`},
		"empty identity":   {"environments", `{"data":[{"name":"no id"}]}`},
		"unknown catalog":  {"vaults", `{"data":[]}`},
	} {
		t.Run(name, func(t *testing.T) {
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer remote.Close()
			if _, err := ListCatalog(context.Background(), remote.URL, "private-token", tc.kind); err == nil {
				t.Fatalf("accepted %s", tc.body)
			}
		})
	}
}

func TestCatalogRejectsACursorThatDoesNotAdvance(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[],"next_page":"same"}`))
	}))
	defer remote.Close()
	if _, err := ListCatalog(context.Background(), remote.URL, "private-token", "agents"); err == nil || !strings.Contains(err.Error(), "did not advance") {
		t.Fatalf("stuck cursor accepted: %v", err)
	}
}

func TestCatalogBoundsPageCount(t *testing.T) {
	page := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		_, _ = w.Write([]byte(`{"data":[],"next_page":"page_` + string(rune('a'+page)) + `"}`))
	}))
	defer remote.Close()
	if _, err := ListCatalog(context.Background(), remote.URL, "private-token", "agents"); err == nil || !strings.Contains(err.Error(), "pages") {
		t.Fatalf("unbounded pagination accepted: %v", err)
	}
	if page != maxCatalogPages {
		t.Fatalf("requested %d pages, want %d", page, maxCatalogPages)
	}
}

func TestCatalogErrorsDoNotEchoTheKey(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key private-token", http.StatusUnauthorized)
	}))
	defer remote.Close()
	_, err := ListCatalog(context.Background(), remote.URL, "private-token", "agents")
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("provider error was not redacted: %v", err)
	}
}

func TestCheckAccessVerifiesIdentityOfEveryResource(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/environments/env_1":
			_, _ = w.Write([]byte(`{"id":"env_1"}`))
		case "/vaults/vlt_1":
			_, _ = w.Write([]byte(`{"id":"vlt_other"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()
	if err := CheckAccess(context.Background(), remote.URL, "k", "env_1", nil); err != nil {
		t.Fatalf("environment alone should pass: %v", err)
	}
	if err := CheckAccess(context.Background(), remote.URL, "k", "env_1", []string{"vlt_1"}); err == nil {
		t.Fatal("a vault answering with another identity was accepted")
	}
	if err := CheckAccess(context.Background(), remote.URL, "k", "env_1", []string{"not-a-vault"}); err == nil {
		t.Fatal("malformed vault ID accepted")
	}
	if err := CheckAccess(context.Background(), remote.URL, "k", "env_missing", nil); err == nil {
		t.Fatal("unknown environment accepted")
	}
}
