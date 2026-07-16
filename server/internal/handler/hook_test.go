package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// enableHooksFlag flips automation_event_hooks on for the shared test handler and
// restores the previous flag service when the test ends.
func enableHooksFlag(t *testing.T) {
	t.Helper()
	prev := testHandler.FeatureFlags
	p := featureflag.NewStaticProvider()
	p.Set(featureflags.EventHooks, featureflag.Rule{Default: true})
	testHandler.FeatureFlags = featureflag.NewService(p)
	t.Cleanup(func() { testHandler.FeatureFlags = prev })
}

// newMemberHookRequest builds a member-authenticated request (X-User-ID +
// X-Workspace-ID), the same identity a member hits the REST API with.
func newMemberHookRequest(method, path string, body any) *http.Request {
	req := newJSONRequest(method, path, body)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	return req
}

func newJSONRequest(method, path string, body any) *http.Request {
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, path, nil)
	} else {
		buf, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(buf))
	}
	r.Header.Set("Content-Type", "application/json")
	return r
}

// A minimal valid per_event spec; action targets are arbitrary uuids (PR2
// validates uuid shape, not existence — matching/execution is a later slice).
func sampleHookSpec(name, message string) map[string]any {
	return map[string]any{
		"name": name,
		"when": map[string]any{
			"event": "issue.status_changed",
			"match": map[string]any{"to": "done"},
		},
		"fire": map[string]any{"mode": "per_event"},
		"do": []any{
			map[string]any{"type": "add_comment", "issue_id": "55555555-5555-5555-5555-555555555555", "message": message},
		},
	}
}

func TestHookCRUDLifecycle(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	enableHooksFlag(t)
	ctx := context.Background()

	// Create → revision #1.
	w := httptest.NewRecorder()
	testHandler.CreateHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks", sampleHookSpec("lifecycle hook", "first")))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status %d: %s", w.Code, w.Body.String())
	}
	var created HookResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID == "" || created.Revision.Revision != 1 || !created.Enabled {
		t.Fatalf("unexpected create response: %+v", created)
	}
	if created.Revision.Event != "issue.status_changed" || created.Revision.FireMode != "per_event" {
		t.Errorf("revision fields wrong: %+v", created.Revision)
	}
	hookID := created.ID
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM hook_revision WHERE hook_id = $1`, hookID)
		testPool.Exec(context.Background(), `DELETE FROM hook WHERE id = $1`, hookID)
	})

	// Get.
	w = httptest.NewRecorder()
	testHandler.GetHook(w, withURLParam(newMemberHookRequest(http.MethodGet, "/api/hooks/"+hookID, nil), "id", hookID))
	if w.Code != http.StatusOK {
		t.Fatalf("get: status %d: %s", w.Code, w.Body.String())
	}

	// Update → new immutable revision #2, active pointer moves, name updated.
	w = httptest.NewRecorder()
	testHandler.UpdateHook(w, withURLParam(newMemberHookRequest(http.MethodPatch, "/api/hooks/"+hookID, sampleHookSpec("renamed hook", "second")), "id", hookID))
	if w.Code != http.StatusOK {
		t.Fatalf("update: status %d: %s", w.Code, w.Body.String())
	}
	var updated HookResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Revision.Revision != 2 || updated.Name != "renamed hook" {
		t.Fatalf("update did not append revision / rename: %+v", updated)
	}
	if updated.Revision.ID == created.Revision.ID {
		t.Errorf("update must create a NEW revision id, got same %s", updated.Revision.ID)
	}
	// The original revision row is retained (immutable history).
	var revCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM hook_revision WHERE hook_id = $1`, hookID).Scan(&revCount)
	if revCount != 2 {
		t.Errorf("hook_revision count = %d, want 2 (revisions are immutable, never overwritten)", revCount)
	}

	// List includes it.
	w = httptest.NewRecorder()
	testHandler.ListHooks(w, newMemberHookRequest(http.MethodGet, "/api/hooks", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: status %d", w.Code)
	}
	var list []HookResponse
	json.NewDecoder(w.Body).Decode(&list)
	if !containsHook(list, hookID) {
		t.Errorf("list does not contain created hook %s", hookID)
	}

	// Disable → enabled=false with reason.
	w = httptest.NewRecorder()
	testHandler.DisableHook(w, withURLParam(newMemberHookRequest(http.MethodPost, "/api/hooks/"+hookID+"/disable", map[string]any{"reason": "paused"}), "id", hookID))
	if w.Code != http.StatusOK {
		t.Fatalf("disable: status %d: %s", w.Code, w.Body.String())
	}
	var disabled HookResponse
	json.NewDecoder(w.Body).Decode(&disabled)
	if disabled.Enabled || disabled.DisabledReason != "paused" {
		t.Errorf("disable did not take: %+v", disabled)
	}

	// Enable again.
	w = httptest.NewRecorder()
	testHandler.EnableHook(w, withURLParam(newMemberHookRequest(http.MethodPost, "/api/hooks/"+hookID+"/enable", nil), "id", hookID))
	var reenabled HookResponse
	json.NewDecoder(w.Body).Decode(&reenabled)
	if !reenabled.Enabled {
		t.Errorf("enable did not take: %+v", reenabled)
	}

	// Executions trace is empty (no matcher runs in PR2) but the endpoint works.
	w = httptest.NewRecorder()
	testHandler.ListHookExecutions(w, withURLParam(newMemberHookRequest(http.MethodGet, "/api/hooks/"+hookID+"/executions", nil), "id", hookID))
	if w.Code != http.StatusOK {
		t.Fatalf("executions: status %d", w.Code)
	}
	var execs []HookExecutionResponse
	json.NewDecoder(w.Body).Decode(&execs)
	if len(execs) != 0 {
		t.Errorf("executions = %d, want 0 in store-only PR2", len(execs))
	}

	// Delete (soft archive) → subsequent get 404s.
	w = httptest.NewRecorder()
	testHandler.DeleteHook(w, withURLParam(newMemberHookRequest(http.MethodDelete, "/api/hooks/"+hookID, nil), "id", hookID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	testHandler.GetHook(w, withURLParam(newMemberHookRequest(http.MethodGet, "/api/hooks/"+hookID, nil), "id", hookID))
	if w.Code != http.StatusNotFound {
		t.Errorf("get after archive: status %d, want 404", w.Code)
	}
	// The row is soft-archived, not physically deleted.
	var archived bool
	testPool.QueryRow(ctx, `SELECT archived_at IS NOT NULL FROM hook WHERE id = $1`, hookID).Scan(&archived)
	if !archived {
		t.Errorf("hook should be soft-archived, not deleted")
	}
}

// The whole surface is invisible unless the feature flag is on.
func TestHookRequiresFeatureFlag(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	prev := testHandler.FeatureFlags
	testHandler.FeatureFlags = nil // nil service → every flag resolves to its default (off)
	t.Cleanup(func() { testHandler.FeatureFlags = prev })

	w := httptest.NewRecorder()
	testHandler.CreateHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks", sampleHookSpec("blocked", "x")))
	if w.Code != http.StatusNotFound {
		t.Errorf("create with flag off: status %d, want 404", w.Code)
	}
}

// A bad spec is rejected at the API boundary with 400, never reaching the store.
func TestHookCreateRejectsInvalidSpec(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	enableHooksFlag(t)
	bad := sampleHookSpec("bad", "x")
	bad["do"] = []any{map[string]any{"type": "set_issue_status_many"}} // system-only
	w := httptest.NewRecorder()
	testHandler.CreateHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks", bad))
	if w.Code != http.StatusBadRequest {
		t.Errorf("create with system-only action: status %d, want 400", w.Code)
	}
}

// An agent author with no resolvable human principal (§8) is refused.
func TestHookAgentRequiresPrincipal(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	enableHooksFlag(t)
	req := newMemberHookRequest(http.MethodPost, "/api/hooks", sampleHookSpec("agent hook", "x"))
	// Trusted agent identity (task_token), valid agent uuid, but no X-Task-ID means
	// no originator can be resolved → no accountable principal.
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", "66666666-6666-6666-6666-666666666666")
	w := httptest.NewRecorder()
	testHandler.CreateHook(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("agent create without principal: status %d, want 403", w.Code)
	}
}

func containsHook(list []HookResponse, id string) bool {
	for _, h := range list {
		if h.ID == id {
			return true
		}
	}
	return false
}
