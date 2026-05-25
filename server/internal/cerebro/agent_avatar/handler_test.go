package agentavatar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type stubWorkspaceLoader struct {
	settings []byte
	err      error
}

func (s stubWorkspaceLoader) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	return db.Workspace{Settings: s.settings}, s.err
}

func TestGatewayConfigPrefersWorkspaceSettings(t *testing.T) {
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", "https://env.example.com")
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", "rk_env")
	t.Setenv("FIRTAL_DATA_REGISTRY_AVATAR_MODEL", "")

	wsID := "11111111-1111-1111-1111-111111111111"

	t.Run("workspace settings override env", func(t *testing.T) {
		h := New(nil, stubWorkspaceLoader{settings: []byte(
			`{"firtal_gateway":{"gateway_url":"https://ws.example.com/","api_key":"rk_workspace"}}`)})
		cfg := h.gatewayConfig(context.Background(), wsID)
		if cfg.baseURL != "https://ws.example.com" {
			t.Fatalf("baseURL = %q, want workspace value", cfg.baseURL)
		}
		if cfg.apiKey != "rk_workspace" {
			t.Fatalf("apiKey = %q, want workspace value", cfg.apiKey)
		}
		if cfg.model != defaultAvatarModel {
			t.Fatalf("model = %q, want default", cfg.model)
		}
	})

	t.Run("falls back to env when settings empty", func(t *testing.T) {
		h := New(nil, stubWorkspaceLoader{settings: []byte(`{}`)})
		cfg := h.gatewayConfig(context.Background(), wsID)
		if cfg.baseURL != "https://env.example.com" || cfg.apiKey != "rk_env" {
			t.Fatalf("expected env fallback, got url=%q key=%q", cfg.baseURL, cfg.apiKey)
		}
	})

	t.Run("falls back to env when no loader", func(t *testing.T) {
		h := New(nil, nil)
		cfg := h.gatewayConfig(context.Background(), wsID)
		if cfg.baseURL != "https://env.example.com" || cfg.apiKey != "rk_env" {
			t.Fatalf("expected env fallback, got url=%q key=%q", cfg.baseURL, cfg.apiKey)
		}
	})
}

func TestGatewayConfigFromEnvUsesAvatarModelDefault(t *testing.T) {
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", " https://registry.example.com/ ")
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", " rk_test ")
	t.Setenv("FIRTAL_DATA_REGISTRY_AVATAR_MODEL", "")

	cfg := gatewayConfigFromEnv()

	if cfg.baseURL != "https://registry.example.com" {
		t.Fatalf("baseURL = %q", cfg.baseURL)
	}
	if cfg.apiKey != "rk_test" {
		t.Fatalf("apiKey = %q", cfg.apiKey)
	}
	if cfg.model != defaultAvatarModel {
		t.Fatalf("model = %q, want %q", cfg.model, defaultAvatarModel)
	}
}

func TestCallGatewayPostsToDataRegistryAndDecodesImage(t *testing.T) {
	wantImage := []byte("png-bytes")
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(wantImage)

	// The Data Registry gateway proxies OpenRouter, which returns each image as
	// an {"type":"image_url","image_url":{"url":"data:…"}} object. A few
	// providers emit a bare string instead, so both shapes must decode.
	cases := []struct {
		name   string
		images any
	}{
		{
			name: "openrouter_object_form",
			images: []map[string]any{
				{"type": "image_url", "image_url": map[string]any{"url": dataURL}},
			},
		},
		{
			name:   "bare_string_form",
			images: []string{dataURL},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != gatewayChatCompletionsPath {
					t.Fatalf("path = %q", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer rk_test" {
					t.Fatalf("Authorization = %q", got)
				}
				if got := r.Header.Get("x-trace-name"); got != "agent-avatar-generate" {
					t.Fatalf("x-trace-name = %q", got)
				}

				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if body["model"] != "openai/gpt-5-image-mini" {
					t.Fatalf("model = %v", body["model"])
				}
				messages, ok := body["messages"].([]any)
				if !ok || len(messages) != 1 {
					t.Fatalf("messages = %#v", body["messages"])
				}

				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []map[string]any{
						{"message": map[string]any{"images": tc.images}},
					},
				})
			}))
			defer server.Close()

			got, err := callGateway(context.Background(), server.Client(), gatewayConfig{
				baseURL: server.URL,
				apiKey:  "rk_test",
				model:   "openai/gpt-5-image-mini",
			}, "make an avatar")
			if err != nil {
				t.Fatalf("callGateway returned error: %v", err)
			}
			if string(got) != string(wantImage) {
				t.Fatalf("image = %q, want %q", got, wantImage)
			}
		})
	}
}
