package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/agentmemory"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestCerebroMemoryEnabled_NilQueriesReturnsFalse mirrors
// TestCerebroRequireLocalRuntimePolicy_NilQueriesPasses's shape: with no
// CerebroQueries wired (upstream-only test fixtures), the flag must resolve
// to OFF rather than panic or fail open — memory ships dormant by default.
func TestCerebroMemoryEnabled_NilQueriesReturnsFalse(t *testing.T) {
	h := &Handler{}
	if h.cerebroMemoryEnabled(context.Background(), pgtype.UUID{}) {
		t.Fatalf("nil CerebroQueries: flag must resolve to false")
	}
}

func TestGetAgentMemorySettings_NilServiceReturns404(t *testing.T) {
	h := &Handler{}
	r := httptest.NewRequest(http.MethodGet, "/api/agents/x/memory-settings", nil)
	w := httptest.NewRecorder()
	h.GetAgentMemorySettings(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("nil AgentMemory: expected 404, got %d", w.Code)
	}
}

func TestSetAgentMemorySettings_NilServiceReturns404(t *testing.T) {
	h := &Handler{}
	r := httptest.NewRequest(http.MethodPut, "/api/agents/x/memory-settings", nil)
	w := httptest.NewRecorder()
	h.SetAgentMemorySettings(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("nil AgentMemory: expected 404, got %d", w.Code)
	}
}

// fakeAgentMemoryService is a minimal in-memory double satisfying
// AgentMemorySettingsService, used to test the flag/capability gate ordering
// without a DB. Mirrors the fake Querier pattern in agentmemory/service_test.go.
type fakeAgentMemoryService struct {
	settings agentmemory.Settings
}

func (f *fakeAgentMemoryService) GetSettings(ctx context.Context, userID, agentID pgtype.UUID) (agentmemory.Settings, error) {
	return f.settings, nil
}

func (f *fakeAgentMemoryService) SetSettings(ctx context.Context, workspaceID, userID, agentID pgtype.UUID, next agentmemory.Settings) (agentmemory.Settings, error) {
	f.settings = next
	return next, nil
}

// TestGetAgentMemorySettings_FlagOffReturns404 pins that the workspace flag
// gates GET too — a workspace with cerebro_memory off must not leak even the
// (always-false-by-default) settings shape.
func TestGetAgentMemorySettings_FlagOffReturns404(t *testing.T) {
	h := &Handler{AgentMemory: &fakeAgentMemoryService{}}
	r := makeAuthedRequest("00000000-0000-0000-0000-0000000000aa", "00000000-0000-0000-0000-0000000000bb")
	ctx := middleware.SetMemberContext(r.Context(), "00000000-0000-0000-0000-0000000000bb", db.Member{})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.GetAgentMemorySettings(w, r)
	// h.CerebroQueries is nil here, so cerebroMemoryEnabled fails closed (false)
	// regardless of workspace — the handler must surface that as 404, not 200.
	if w.Code != http.StatusNotFound {
		t.Fatalf("flag off (nil CerebroQueries): expected 404, got %d body=%q", w.Code, w.Body.String())
	}
}
