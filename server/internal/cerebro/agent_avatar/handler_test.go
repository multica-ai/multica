package agentavatar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestBuildPrompt(t *testing.T) {
	t.Run("custom prompt is used verbatim", func(t *testing.T) {
		got := buildPrompt("Sara", "a watercolor fox")
		if got != "a watercolor fox" {
			t.Fatalf("custom prompt = %q, want passthrough", got)
		}
	})

	t.Run("auto prompt is photorealistic, not an illustration", func(t *testing.T) {
		got := strings.ToLower(buildPrompt("Sara", ""))
		for _, want := range []string{"photorealistic", "scandinavian", "headshot", "studio background"} {
			if !strings.Contains(got, want) {
				t.Fatalf("prompt missing %q: %s", want, got)
			}
		}
		// The anti-illustration clause is what stops the model drifting to flat
		// cartoons — regressing it reintroduces the "håbløs" avatar.
		if !strings.Contains(got, "not an illustration") {
			t.Fatalf("prompt missing anti-illustration clause: %s", got)
		}
	})

	t.Run("deterministic for the same name", func(t *testing.T) {
		if buildPrompt("Mia", "") != buildPrompt("Mia", "") {
			t.Fatal("buildPrompt is not deterministic")
		}
	})

	t.Run("different names get different backgrounds", func(t *testing.T) {
		// Backgrounds are the at-a-glance identifier, so a spread of names must
		// land on more than one colour.
		names := []string{"Sara", "Mia", "Sofie", "Franz", "Rasp", "Tine", "GPT-Boy", "Trump"}
		seen := map[string]bool{}
		for _, n := range names {
			for _, bg := range backgroundColors {
				if strings.Contains(buildPrompt(n, ""), " solid "+bg+" studio background") {
					seen[bg] = true
				}
			}
		}
		if len(seen) < 2 {
			t.Fatalf("expected varied backgrounds across names, got %d distinct", len(seen))
		}
	})
}

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

type fakeStorage struct {
	uploaded []byte
}

func (s *fakeStorage) Upload(_ context.Context, _ string, data []byte, _ string, _ string) (string, error) {
	s.uploaded = append([]byte(nil), data...)
	return "https://cdn.example.com/avatar.png", nil
}

func (s *fakeStorage) Delete(_ context.Context, _ string)       {}
func (s *fakeStorage) DeleteKeys(_ context.Context, _ []string) {}
func (s *fakeStorage) KeyFromURL(string) string                 { return "" }
func (s *fakeStorage) CdnDomain() string                        { return "" }
func (s *fakeStorage) GetReader(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

type fakeAgentStore struct {
	agents  []db.Agent
	updates []db.UpdateAgentParams
}

func (s *fakeAgentStore) ListAgents(context.Context, pgtype.UUID) ([]db.Agent, error) {
	return append([]db.Agent(nil), s.agents...), nil
}

func (s *fakeAgentStore) UpdateAgent(_ context.Context, arg db.UpdateAgentParams) (db.Agent, error) {
	s.updates = append(s.updates, arg)
	for i := range s.agents {
		if s.agents[i].ID == arg.ID {
			s.agents[i].AvatarUrl = arg.AvatarUrl
			return s.agents[i], nil
		}
	}
	return db.Agent{ID: arg.ID, AvatarUrl: arg.AvatarUrl}, nil
}

func TestGenerateForAgentStoresAndUpdatesOnlyAvatarURL(t *testing.T) {
	wantImage := []byte("png-bytes")
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(wantImage)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"images": []string{dataURL}}},
			},
		})
	}))
	defer gateway.Close()

	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", gateway.URL)
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", "rk_test")

	store := &fakeStorage{}
	agents := &fakeAgentStore{}
	h := New(store, nil)
	h.agents = agents
	agentID := util.MustParseUUID("22222222-2222-2222-2222-222222222222")

	url, err := h.GenerateForAgent(context.Background(), "11111111-1111-1111-1111-111111111111", db.Agent{
		ID:   agentID,
		Name: "Charlotte",
	})
	if err != nil {
		t.Fatalf("GenerateForAgent returned error: %v", err)
	}
	if url != "https://cdn.example.com/avatar.png" {
		t.Fatalf("url = %q", url)
	}
	if string(store.uploaded) != string(wantImage) {
		t.Fatalf("uploaded image = %q", store.uploaded)
	}
	if len(agents.updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(agents.updates))
	}
	update := agents.updates[0]
	if update.ID != agentID || update.AvatarUrl.String != url || !update.AvatarUrl.Valid {
		t.Fatalf("avatar update = %#v", update)
	}
	if update.Name.Valid || update.Description.Valid || update.RuntimeID.Valid || update.Model.Valid {
		t.Fatalf("GenerateForAgent should only set avatar_url, got update %#v", update)
	}
}

func TestBackfillStartsBackgroundJobAndTracksProgress(t *testing.T) {
	wantImage := []byte("png-bytes")
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(wantImage)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"images": []string{dataURL}}},
			},
		})
	}))
	defer gateway.Close()

	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", gateway.URL)
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", "rk_test")

	agents := &fakeAgentStore{agents: []db.Agent{
		{ID: util.MustParseUUID("22222222-2222-2222-2222-222222222222"), WorkspaceID: util.MustParseUUID("11111111-1111-1111-1111-111111111111"), Name: "Missing"},
		{ID: util.MustParseUUID("33333333-3333-3333-3333-333333333333"), WorkspaceID: util.MustParseUUID("11111111-1111-1111-1111-111111111111"), Name: "Done", AvatarUrl: pgtype.Text{String: "https://cdn/existing.png", Valid: true}},
	}}
	h := New(&fakeStorage{}, nil)
	h.agents = agents

	req := httptest.NewRequest(http.MethodPost, "/api/agents/backfill-avatars", nil)
	ctx := middleware.SetMemberContext(req.Context(), "11111111-1111-1111-1111-111111111111", db.Member{Role: "admin"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Backfill(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("Backfill status = %d, body %s", w.Code, w.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(agents.updates) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(agents.updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(agents.updates))
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/agents/backfill-avatars", nil).WithContext(ctx)
	statusW := httptest.NewRecorder()
	h.BackfillStatus(statusW, statusReq)
	if statusW.Code != http.StatusOK {
		t.Fatalf("BackfillStatus = %d, body %s", statusW.Code, statusW.Body.String())
	}
	var status backfillStatusResponse
	if err := json.NewDecoder(statusW.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Status != "done" || status.Generated != 1 || status.Missing != 0 {
		t.Fatalf("status = %#v", status)
	}
}
