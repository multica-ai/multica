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
		for _, want := range []string{"photorealistic", "scandinavian", "headshot"} {
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

	t.Run("background is forced bright and flat, not a dark studio backdrop", func(t *testing.T) {
		// The earlier prompt asked for a "studio background" + "soft studio
		// lighting", and the model rendered dark, dim backdrops where the colour
		// barely showed (FIR-2321). The fix forces a flat, saturated, evenly-lit
		// colour and explicitly forbids the dark studio look.
		got := strings.ToLower(buildPrompt("Sara", ""))
		for _, want := range []string{"saturated", "lit bright", "even", "no vignette", "no gradient"} {
			if !strings.Contains(got, want) {
				t.Fatalf("background clause missing %q: %s", want, got)
			}
		}
		if strings.Contains(got, "studio background") {
			t.Fatalf("prompt still asks for a studio background (renders dark): %s", got)
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
				if strings.Contains(buildPrompt(n, ""), "saturated "+bg+",") {
					seen[bg] = true
				}
			}
		}
		if len(seen) < 2 {
			t.Fatalf("expected varied backgrounds across names, got %d distinct", len(seen))
		}
	})

	t.Run("explicit background override wins over the name hash", func(t *testing.T) {
		got := buildPromptWithBackground("Sara", "", "lava orange")
		if !strings.Contains(got, "saturated lava orange,") {
			t.Fatalf("override colour not used: %s", got)
		}
	})

	t.Run("gender follows the agent name, not a hash", func(t *testing.T) {
		// The old code hashed the name to pick man/woman, so "Sara" could come
		// out male. Now a clearly-gendered first name must drive the subject.
		women := []string{"Sara", "Mia", "Sofie", "Charlotte - Senior Backend Developer"}
		for _, n := range women {
			got := buildPrompt(n, "")
			if !strings.Contains(got, "portrait of a young woman ") {
				t.Fatalf("%q should render a young woman: %s", n, got)
			}
		}
		men := []string{"Lars - Head of Legal", "Preben", "Brian - Senior Developer"}
		for _, n := range men {
			got := buildPrompt(n, "")
			if !strings.Contains(got, "portrait of a young man ") {
				t.Fatalf("%q should render a young man: %s", n, got)
			}
		}
	})

	t.Run("unknown name defers gender to the model via the name", func(t *testing.T) {
		// An unrecognised name must not force a gender; instead the first name is
		// handed to the model so it can infer the gender itself.
		got := buildPrompt("Zephyrax", "")
		if !strings.Contains(got, "portrait of a young person ") {
			t.Fatalf("unknown name should use a neutral subject: %s", got)
		}
		if !strings.Contains(got, `first name "Zephyrax"`) {
			t.Fatalf("unknown name should be passed to the model: %s", got)
		}
	})
}

func TestAssignDistinctBackgrounds(t *testing.T) {
	// The whole point of batch regeneration is distinct colours: every agent up
	// to the palette size must get a unique background (the name-hash collided —
	// six agents shared one green across 23 names).
	n := len(backgroundColors)
	got := assignDistinctBackgrounds(n)
	if len(got) != n {
		t.Fatalf("len = %d, want %d", len(got), n)
	}
	seen := map[string]bool{}
	for _, c := range got {
		if seen[c] {
			t.Fatalf("colour %q assigned twice within palette size", c)
		}
		seen[c] = true
	}
	// Beyond the palette size it wraps rather than panicking.
	if wrap := assignDistinctBackgrounds(n + 2); len(wrap) != n+2 {
		t.Fatalf("wrap len = %d, want %d", len(wrap), n+2)
	}
}

func TestGenderForName(t *testing.T) {
	cases := map[string]struct {
		want  string
		known bool
	}{
		"Sara":                 {"woman", true},
		"Sara - CTO":           {"woman", true},
		"Lars - Head of Legal": {"man", true},
		"philippa - Filip EA":  {"woman", true}, // leading token wins over "Filip"
		"Zephyrax":             {"", false},
		"GPT-Boy":              {"", false},
		"":                     {"", false},
	}
	for name, want := range cases {
		gender, known := genderForName(name)
		if gender != want.want || known != want.known {
			t.Fatalf("genderForName(%q) = (%q, %v), want (%q, %v)", name, gender, known, want.want, want.known)
		}
	}
}

type stubWorkspaceLoader struct {
	settings []byte
	err      error
}

func (s stubWorkspaceLoader) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	return db.Workspace{Settings: s.settings}, s.err
}

func TestGatewayConfigPrefersWorkspaceSettings(t *testing.T) {
	t.Setenv("FIRTAL_REGISTRY_URL", "https://env.example.com")
	t.Setenv("FIRTAL_REGISTRY_KEY", "rk_env")
	t.Setenv("FIRTAL_REGISTRY_AVATAR_MODEL", "")

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
	t.Setenv("FIRTAL_REGISTRY_URL", " https://registry.example.com/ ")
	t.Setenv("FIRTAL_REGISTRY_KEY", " rk_test ")
	t.Setenv("FIRTAL_REGISTRY_AVATAR_MODEL", "")

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
	b64Image := base64.StdEncoding.EncodeToString(wantImage)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != gatewayImagesPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, gatewayImagesPath)
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
		if body["model"] != defaultAvatarModel {
			t.Fatalf("model = %v, want %v", body["model"], defaultAvatarModel)
		}
		if body["prompt"] != "make an avatar" {
			t.Fatalf("prompt = %v", body["prompt"])
		}
		if body["output_format"] != "b64_json" {
			t.Fatalf("output_format = %v", body["output_format"])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"b64_json": b64Image},
			},
		})
	}))
	defer server.Close()

	got, err := callGateway(context.Background(), server.Client(), gatewayConfig{
		baseURL: server.URL,
		apiKey:  "rk_test",
		model:   defaultAvatarModel,
	}, "make an avatar")
	if err != nil {
		t.Fatalf("callGateway returned error: %v", err)
	}
	if string(got) != string(wantImage) {
		t.Fatalf("image = %q, want %q", got, wantImage)
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
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"b64_json": base64.StdEncoding.EncodeToString(wantImage)},
			},
		})
	}))
	defer gateway.Close()

	t.Setenv("FIRTAL_REGISTRY_URL", gateway.URL)
	t.Setenv("FIRTAL_REGISTRY_KEY", "rk_test")

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
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"b64_json": base64.StdEncoding.EncodeToString(wantImage)},
			},
		})
	}))
	defer gateway.Close()

	t.Setenv("FIRTAL_REGISTRY_URL", gateway.URL)
	t.Setenv("FIRTAL_REGISTRY_KEY", "rk_test")

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

func TestBackfillForceRegeneratesAllExceptExcluded(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"b64_json": base64.StdEncoding.EncodeToString([]byte("png-bytes"))},
			},
		})
	}))
	defer gateway.Close()

	t.Setenv("FIRTAL_REGISTRY_URL", gateway.URL)
	t.Setenv("FIRTAL_REGISTRY_KEY", "rk_test")

	ws := util.MustParseUUID("11111111-1111-1111-1111-111111111111")
	keep := util.MustParseUUID("44444444-4444-4444-4444-444444444444") // "Mia" — excluded
	// Both agents already have avatars; a default (missing-only) run would skip
	// both. Force must regenerate everyone except the excluded agent.
	agents := &fakeAgentStore{agents: []db.Agent{
		{ID: util.MustParseUUID("22222222-2222-2222-2222-222222222222"), WorkspaceID: ws, Name: "Sara", AvatarUrl: pgtype.Text{String: "https://cdn/old-sara.png", Valid: true}},
		{ID: util.MustParseUUID("33333333-3333-3333-3333-333333333333"), WorkspaceID: ws, Name: "Lars", AvatarUrl: pgtype.Text{String: "https://cdn/old-lars.png", Valid: true}},
		{ID: keep, WorkspaceID: ws, Name: "Mia", AvatarUrl: pgtype.Text{String: "https://cdn/keep-mia.png", Valid: true}},
	}}
	h := New(&fakeStorage{}, nil)
	h.agents = agents

	body, _ := json.Marshal(backfillRequest{Mode: "all", ExcludeAgentIDs: []string{util.UUIDToString(keep)}})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/backfill-avatars", strings.NewReader(string(body)))
	ctx := middleware.SetMemberContext(req.Context(), "11111111-1111-1111-1111-111111111111", db.Member{Role: "owner"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Backfill(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("Backfill status = %d, body %s", w.Code, w.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(agents.updates) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(agents.updates) != 2 {
		t.Fatalf("updates = %d, want 2 (Sara + Lars, not Mia)", len(agents.updates))
	}
	for _, u := range agents.updates {
		if u.ID == keep {
			t.Fatalf("excluded agent Mia was regenerated")
		}
	}
}
