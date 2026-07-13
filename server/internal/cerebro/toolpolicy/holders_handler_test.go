package toolpolicy

// Integration tests for the per-permission holders endpoint (FIR-3091 punkt 8).
// They exercise the full path through the DB-backed Store, so they share the
// TestMain fixture in store_test.go and skip cleanly when no DB is reachable.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// holdersRequest builds a GET .../tool-policy/holders request with the chi "id"
// URL param and a member context of the given role, exactly as the router
// middleware would supply them at runtime.
func holdersRequest(role, toolKey string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if toolKey != "" {
		req.URL.RawQuery = url.Values{"tool_key": {toolKey}}.Encode()
	}
	wsID := util.UUIDToString(tpTestWorkspaceID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", wsID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = middleware.SetMemberContext(ctx, wsID, db.Member{Role: role, UserID: tpTestUserID})
	return req.WithContext(ctx)
}

func TestHandlerHolders(t *testing.T) {
	s := newTPStore(t) // skips when DATABASE_URL is not reachable
	clearAll(t, s)
	ctx := context.Background()

	h := NewHandler(s)

	const enforcedTool = "create_issue" // enforced by default
	agent, runtime := uuidByte(60), uuidByte(61)

	// Seed two explicit rows for the enforced tool across two layers/subjects.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: enforcedTool, Layer: LayerAgent, SubjectID: agent, Setting: SettingDeny, UpdatedBy: tpTestUserID}); err != nil {
		t.Fatalf("seed agent row: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: enforcedTool, Layer: LayerRuntime, SubjectID: runtime, Setting: SettingAsk, UpdatedBy: tpTestUserID}); err != nil {
		t.Fatalf("seed runtime row: %v", err)
	}

	t.Run("admin sees holders for an enforced key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Holders(rec, holdersRequest("admin", enforcedTool))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		var resp holdersResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.ToolKey != enforcedTool {
			t.Fatalf("tool_key = %q, want %q", resp.ToolKey, enforcedTool)
		}
		if !resp.Enforced {
			t.Fatalf("enforced = false, want true for %q", enforcedTool)
		}
		if len(resp.Holders) != 2 {
			t.Fatalf("holders len = %d, want 2 (%+v)", len(resp.Holders), resp.Holders)
		}
		// Rows are ordered by layer ASC: "agent" before "runtime".
		byLayer := map[string]holderResponse{}
		for _, hd := range resp.Holders {
			byLayer[hd.Layer] = hd
		}
		if got := byLayer["agent"]; got.Setting != string(SettingDeny) || got.SubjectID != util.UUIDToString(agent) {
			t.Fatalf("agent row = %+v, want setting=deny subject=%s", got, util.UUIDToString(agent))
		}
		if got := byLayer["runtime"]; got.Setting != string(SettingAsk) || got.SubjectID != util.UUIDToString(runtime) {
			t.Fatalf("runtime row = %+v, want setting=ask subject=%s", got, util.UUIDToString(runtime))
		}
	})

	t.Run("enforced is false for a surfaced-but-unwired key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Holders(rec, holdersRequest("owner", "rerun_issue"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp holdersResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Enforced {
			t.Fatalf("enforced = true, want false for rerun_issue")
		}
		// No rows seeded for this tool: holders must be an empty array, never null.
		if resp.Holders == nil {
			t.Fatalf("holders is nil, want empty array")
		}
		if len(resp.Holders) != 0 {
			t.Fatalf("holders len = %d, want 0", len(resp.Holders))
		}
	})

	t.Run("missing tool_key is a 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Holders(rec, holdersRequest("admin", ""))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("non-admin is forbidden", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Holders(rec, holdersRequest("member", enforcedTool))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}
