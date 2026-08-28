package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverOmniRouteModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer model-key" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":[{"id":"anthropic/claude-fable-5","owned_by":"anthropic"},{"id":"custom-model"}]}`)
	}))
	defer server.Close()

	oldGetenv := getenv
	getenv = func(key string) string {
		switch key {
		case omniRouteBaseURLKey:
			return server.URL + "/v1"
		case omniRouteAPIKeyKey:
			return "model-key"
		default:
			return ""
		}
	}
	t.Cleanup(func() { getenv = oldGetenv })

	models, err := discoverOmniRouteModels(context.Background())
	if err != nil {
		t.Fatalf("discover models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %#v, want 2 entries", models)
	}
	if models[0].ID != "anthropic/claude-fable-5" || models[0].Provider != "anthropic" {
		t.Fatalf("first model = %#v", models[0])
	}
	if models[1].Provider != "" {
		t.Fatalf("second model provider = %q, want empty", models[1].Provider)
	}
}
