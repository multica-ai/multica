package agentavatar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
				{
					"message": map[string]any{
						"images": []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(wantImage)},
					},
				},
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
}
