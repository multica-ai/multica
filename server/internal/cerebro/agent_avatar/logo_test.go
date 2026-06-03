package agentavatar

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestBuildLogoPrompt(t *testing.T) {
	got := buildLogoPrompt("a clever fox", 0)

	// The user's idea must survive into the prompt verbatim.
	if !strings.Contains(got, "a clever fox") {
		t.Fatalf("prompt dropped the user idea: %s", got)
	}
	// Icon-shaping clauses keep the model on a clean square logo, not a photo.
	for _, want := range []string{"app icon", "no text", "square 1:1 app-icon"} {
		if !strings.Contains(strings.ToLower(got), want) {
			t.Fatalf("prompt missing %q clause: %s", want, got)
		}
	}

	// Variations must differ so the five options aren't near-identical.
	if buildLogoPrompt("a fox", 0) == buildLogoPrompt("a fox", 1) {
		t.Fatal("variant 0 and 1 produced identical prompts")
	}
}

func logoGateway(t *testing.T) *httptest.Server {
	t.Helper()
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"images": []string{dataURL}}},
			},
		})
	}))
}

func newLogoRequest(t *testing.T, body string) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/11111111-1111-1111-1111-111111111111/generate-logo", strings.NewReader(body))
	ctx := middleware.SetMemberContext(req.Context(), "11111111-1111-1111-1111-111111111111", db.Member{Role: "owner"})
	return req.WithContext(ctx), httptest.NewRecorder()
}

func TestGenerateLogosReturnsRequestedCount(t *testing.T) {
	gateway := logoGateway(t)
	defer gateway.Close()
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", gateway.URL)
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", "rk_test")

	h := New(&fakeStorage{}, nil)
	req, w := newLogoRequest(t, `{"prompt":"a clever fox","count":5}`)
	h.GenerateLogos(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var resp generateLogosResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.URLs) != 5 {
		t.Fatalf("urls = %d, want 5", len(resp.URLs))
	}
}

func TestGenerateLogosClampsCount(t *testing.T) {
	gateway := logoGateway(t)
	defer gateway.Close()
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", gateway.URL)
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", "rk_test")

	h := New(&fakeStorage{}, nil)
	req, w := newLogoRequest(t, `{"prompt":"a fox","count":50}`)
	h.GenerateLogos(w, req)

	var resp generateLogosResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.URLs) != maxLogoCount {
		t.Fatalf("urls = %d, want clamp to %d", len(resp.URLs), maxLogoCount)
	}
}

func TestGenerateLogosRejectsEmptyPrompt(t *testing.T) {
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", "https://env.example.com")
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", "rk_env")

	h := New(&fakeStorage{}, nil)
	req, w := newLogoRequest(t, `{"prompt":"   "}`)
	h.GenerateLogos(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestGenerateLogosUnconfiguredGateway(t *testing.T) {
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", "")
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", "")

	h := New(&fakeStorage{}, nil)
	req, w := newLogoRequest(t, `{"prompt":"a fox"}`)
	h.GenerateLogos(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}