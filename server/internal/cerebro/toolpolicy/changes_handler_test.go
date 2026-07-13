package toolpolicy

// Integration tests for the per-permission change log (FIR-3091 punkt 8 fase
// 2): Set/Clear must leave audit rows, and the Changes endpoint must return
// them newest-first with the old -> new transition intact. They exercise the
// full path through the DB-backed Store, so they share the TestMain fixture in
// store_test.go and skip cleanly when no DB is reachable.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// changesRequest builds a GET .../tool-policy/changes request with the chi "id"
// URL param and a member context of the given role, exactly as the router
// middleware would supply them at runtime.
func changesRequest(role, toolKey string) *http.Request {
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

// clearAuditRows drops the change-log rows for the test workspace so each test
// starts from an empty history.
func clearAuditRows(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(),
		`DELETE FROM cerebro_tool_policy_audit WHERE workspace_id = $1`, tpTestWorkspaceID); err != nil {
		t.Fatalf("clear audit rows: %v", err)
	}
}

func TestHandlerChanges(t *testing.T) {
	s := newTPStore(t) // skips when DATABASE_URL is not reachable
	clearAll(t, s)
	clearAuditRows(t, s)
	ctx := context.Background()

	h := NewHandler(s)

	const tool = "create_issue"
	agent := uuidByte(70)

	// One full lifecycle: no row -> deny -> allow -> cleared. Each write must
	// leave exactly one audit row recording the transition.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerAgent, SubjectID: agent, Setting: SettingDeny, UpdatedBy: tpTestUserID}); err != nil {
		t.Fatalf("set deny: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerAgent, SubjectID: agent, Setting: SettingAllow, UpdatedBy: tpTestUserID}); err != nil {
		t.Fatalf("set allow: %v", err)
	}
	if err := s.Clear(ctx, tpTestWorkspaceID, tool, LayerAgent, agent, "", tpTestUserID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	// Clearing the already-absent row is a no-op and must NOT add a fourth row.
	if err := s.Clear(ctx, tpTestWorkspaceID, tool, LayerAgent, agent, "", tpTestUserID); err != nil {
		t.Fatalf("second clear: %v", err)
	}

	t.Run("admin sees the full transition history newest-first", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Changes(rec, changesRequest("admin", tool))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		var resp changesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.ToolKey != tool {
			t.Fatalf("tool_key = %q, want %q", resp.ToolKey, tool)
		}
		if len(resp.Changes) != 3 {
			t.Fatalf("changes len = %d, want 3 (%+v)", len(resp.Changes), resp.Changes)
		}
		// Newest first: clear(allow -> ""), set(deny -> allow), set("" -> deny).
		want := []struct{ action, old, new string }{
			{"clear", string(SettingAllow), ""},
			{"set", string(SettingDeny), string(SettingAllow)},
			{"set", "", string(SettingDeny)},
		}
		for i, w := range want {
			got := resp.Changes[i]
			if got.Action != w.action || got.OldSetting != w.old || got.NewSetting != w.new {
				t.Fatalf("changes[%d] = %+v, want action=%q old=%q new=%q", i, got, w.action, w.old, w.new)
			}
			if got.Layer != string(LayerAgent) || got.SubjectID != util.UUIDToString(agent) {
				t.Fatalf("changes[%d] subject = %+v, want layer=agent subject=%s", i, got, util.UUIDToString(agent))
			}
			if got.ActorType != "member" || got.ActorID != util.UUIDToString(tpTestUserID) {
				t.Fatalf("changes[%d] actor = %+v, want member %s", i, got, util.UUIDToString(tpTestUserID))
			}
			if got.CreatedAt == "" {
				t.Fatalf("changes[%d] created_at is empty", i)
			}
		}
	})

	t.Run("a system write records actor_type system", func(t *testing.T) {
		const sysTool = "web_fetch"
		if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: sysTool, Layer: LayerWorkspace, SubjectID: tpTestWorkspaceID, Setting: SettingAllow, UpdatedBy: pgtype.UUID{}}); err != nil {
			t.Fatalf("system set: %v", err)
		}
		rec := httptest.NewRecorder()
		h.Changes(rec, changesRequest("owner", sysTool))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp changesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Changes) != 1 {
			t.Fatalf("changes len = %d, want 1", len(resp.Changes))
		}
		if got := resp.Changes[0]; got.ActorType != "system" || got.ActorID != "" {
			t.Fatalf("actor = %+v, want system with empty actor_id", got)
		}
	})

	t.Run("a tool with no history returns an empty array, never null", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Changes(rec, changesRequest("admin", "rerun_issue"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp changesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Changes == nil {
			t.Fatalf("changes is nil, want empty array")
		}
		if len(resp.Changes) != 0 {
			t.Fatalf("changes len = %d, want 0", len(resp.Changes))
		}
	})

	t.Run("missing tool_key is a 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Changes(rec, changesRequest("admin", ""))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("non-admin is forbidden", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Changes(rec, changesRequest("member", tool))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}
