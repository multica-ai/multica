package toolpolicy

// Integration tests for the per-permission usage log (FIR-3091 punkt 8 fase
// 3): RecordUsage must leave rows, and the Usage endpoint must return them
// newest-first with the enforcement point, subject, resource and decision
// intact. They exercise the full path through the DB-backed Store, so they
// share the TestMain fixture in store_test.go and skip cleanly when no DB is
// reachable.

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

// usageRequest builds a GET .../tool-policy/usage request with the chi "id"
// URL param and a member context of the given role, exactly as the router
// middleware would supply them at runtime.
func usageRequest(role, toolKey string) *http.Request {
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

// clearUsageRows drops the usage-log rows for the test workspace so each test
// starts from an empty history.
func clearUsageRows(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(),
		`DELETE FROM cerebro_tool_policy_usage WHERE workspace_id = $1`, tpTestWorkspaceID); err != nil {
		t.Fatalf("clear usage rows: %v", err)
	}
}

func TestHandlerUsage(t *testing.T) {
	s := newTPStore(t) // skips when DATABASE_URL is not reachable
	clearUsageRows(t, s)
	ctx := context.Background()

	h := NewHandler(s)

	const tool = "repo.checkout"
	agent := uuidByte(80)

	// Three applied decisions at different gates. Recorded oldest to newest, so
	// the endpoint must return them in reverse.
	s.RecordUsage(ctx, UsageParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool,
		EnforcementPoint: "repo_checkout", SubjectType: "agent", SubjectID: agent,
		Resource: "https://github.com/acme/repo", Decision: SettingAllow, DecidedBy: "workspace",
	})
	s.RecordUsage(ctx, UsageParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool,
		EnforcementPoint: "repo_checkout", SubjectType: "agent", SubjectID: agent,
		Resource: "https://github.com/acme/secret", Decision: SettingDeny, DecidedBy: "group",
	})
	s.RecordUsage(ctx, UsageParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool,
		EnforcementPoint: "http_action", SubjectType: "member", SubjectID: tpTestUserID,
		Decision: SettingAsk,
	})

	t.Run("admin sees the usage log newest-first", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Usage(rec, usageRequest("admin", tool))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		var resp usageResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.ToolKey != tool {
			t.Fatalf("tool_key = %q, want %q", resp.ToolKey, tool)
		}
		if len(resp.Usage) != 3 {
			t.Fatalf("usage len = %d, want 3 (%+v)", len(resp.Usage), resp.Usage)
		}
		want := []struct {
			point, subjectType, subjectID, resource, decision, decidedBy string
		}{
			{"http_action", "member", util.UUIDToString(tpTestUserID), "", "ask", ""},
			{"repo_checkout", "agent", util.UUIDToString(agent), "https://github.com/acme/secret", "deny", "group"},
			{"repo_checkout", "agent", util.UUIDToString(agent), "https://github.com/acme/repo", "allow", "workspace"},
		}
		for i, w := range want {
			got := resp.Usage[i]
			if got.EnforcementPoint != w.point || got.SubjectType != w.subjectType || got.SubjectID != w.subjectID {
				t.Fatalf("usage[%d] subject = %+v, want point=%q type=%q id=%q", i, got, w.point, w.subjectType, w.subjectID)
			}
			if got.Resource != w.resource || got.Decision != w.decision || got.DecidedBy != w.decidedBy {
				t.Fatalf("usage[%d] = %+v, want resource=%q decision=%q decided_by=%q", i, got, w.resource, w.decision, w.decidedBy)
			}
			if got.CreatedAt == "" {
				t.Fatalf("usage[%d] created_at is empty", i)
			}
		}
	})

	t.Run("an invalid subject id records a system subject", func(t *testing.T) {
		const sysTool = "trigger_autopilot"
		s.RecordUsage(ctx, UsageParams{
			WorkspaceID: tpTestWorkspaceID, ToolKey: sysTool,
			EnforcementPoint: "gateway_tool", SubjectType: "agent", SubjectID: pgtype.UUID{},
			Decision: SettingDeny,
		})
		rec := httptest.NewRecorder()
		h.Usage(rec, usageRequest("owner", sysTool))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp usageResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Usage) != 1 {
			t.Fatalf("usage len = %d, want 1", len(resp.Usage))
		}
		if got := resp.Usage[0]; got.SubjectType != "system" || got.SubjectID != "" {
			t.Fatalf("subject = %+v, want system with empty subject_id", got)
		}
	})

	t.Run("a Disable verdict is recorded as the deny it acts as", func(t *testing.T) {
		const disTool = "tools:agent-browser"
		s.RecordUsage(ctx, UsageParams{
			WorkspaceID: tpTestWorkspaceID, ToolKey: disTool,
			EnforcementPoint: "agent_browser_sandbox", SubjectType: "agent", SubjectID: agent,
			Decision: SettingDisable, DecidedBy: "workspace",
		})
		rec := httptest.NewRecorder()
		h.Usage(rec, usageRequest("admin", disTool))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp usageResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Usage) != 1 {
			t.Fatalf("usage len = %d, want 1", len(resp.Usage))
		}
		if got := resp.Usage[0]; got.Decision != string(SettingDeny) {
			t.Fatalf("decision = %q, want %q", got.Decision, SettingDeny)
		}
	})

	t.Run("a tool with no history returns an empty array, never null", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Usage(rec, usageRequest("admin", "rerun_issue"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp usageResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Usage == nil {
			t.Fatalf("usage is nil, want empty array")
		}
		if len(resp.Usage) != 0 {
			t.Fatalf("usage len = %d, want 0", len(resp.Usage))
		}
	})

	t.Run("missing tool_key is a 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Usage(rec, usageRequest("admin", ""))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("non-admin is forbidden", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Usage(rec, usageRequest("member", tool))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}
