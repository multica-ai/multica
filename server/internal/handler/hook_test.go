package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/automation"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// hookSpecFromMap round-trips a test spec map through JSON into a typed HookSpec,
// for tests that drive the service layer directly.
func hookSpecFromMap(m map[string]any) automation.HookSpec {
	buf, _ := json.Marshal(m)
	var spec automation.HookSpec
	json.Unmarshal(buf, &spec)
	return spec
}

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

func newMemberHookRequest(method, path string, body any) *http.Request {
	return newUserHookRequest(method, path, body, testUserID)
}

// newUserHookRequest builds a member-authenticated request for the given user.
func newUserHookRequest(method, path string, body any, userID string) *http.Request {
	req := newJSONRequest(method, path, body)
	req.Header.Set("X-User-ID", userID)
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

// seedHookIssue inserts a real issue in the test workspace and returns its id.
func seedHookIssue(t *testing.T) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'hook target issue', 'todo', 'medium', 'member', $2,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id`, testWorkspaceID, testUserID).Scan(&id); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
	return id
}

// seededHookAgentID returns the workspace-visible agent seeded by the fixture.
func seededHookAgentID(t *testing.T) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent' LIMIT 1`,
		testWorkspaceID).Scan(&id); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}
	return id
}

// testOwnerMemberID returns the member row id of the fixture owner (testUserID).
func testOwnerMemberID(t *testing.T) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`,
		testWorkspaceID, testUserID).Scan(&id); err != nil {
		t.Fatalf("load owner member: %v", err)
	}
	return id
}

// hookSeedCounter makes each seeded user's email unique across calls.
var hookSeedCounter atomic.Int64

// seedHookMember inserts a user + workspace member with the given role and
// returns the user id.
func seedHookMember(t *testing.T, role string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	email := fmt.Sprintf("hook-%s-%d-%d@test.local", role, hookSeedCounter.Add(1), len(t.Name()))
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Hook "+role, email).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)`,
		testWorkspaceID, userID, role); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

// sampleHookSpec is a minimal valid per_event spec: comment an existing issue.
func sampleHookSpec(name, message, issueID string) map[string]any {
	return map[string]any{
		"name": name,
		"when": map[string]any{
			"event": "issue.status_changed",
			"match": map[string]any{"to": "done"},
		},
		"fire": map[string]any{"mode": "per_event"},
		"do": []any{
			map[string]any{"type": "add_comment", "issue_id": issueID, "message": message},
		},
	}
}

func createHookAs(t *testing.T, userID string, spec map[string]any) HookResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateHook(w, newUserHookRequest(http.MethodPost, "/api/hooks", spec, userID))
	if w.Code != http.StatusCreated {
		t.Fatalf("create hook as %s: status %d: %s", userID, w.Code, w.Body.String())
	}
	var resp HookResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM hook_revision WHERE hook_id = $1`, resp.ID)
		testPool.Exec(context.Background(), `DELETE FROM hook WHERE id = $1`, resp.ID)
	})
	return resp
}

func TestHookCRUDLifecycle(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	enableHooksFlag(t)
	ctx := context.Background()
	issueID := seedHookIssue(t)

	created := createHookAs(t, testUserID, sampleHookSpec("lifecycle hook", "first", issueID))
	if created.Revision.Revision != 1 || !created.Enabled || created.Revision.Event != "issue.status_changed" {
		t.Fatalf("unexpected create response: %+v", created)
	}
	hookID := created.ID

	// Get.
	w := httptest.NewRecorder()
	testHandler.GetHook(w, withURLParam(newMemberHookRequest(http.MethodGet, "/api/hooks/"+hookID, nil), "id", hookID))
	if w.Code != http.StatusOK {
		t.Fatalf("get: status %d: %s", w.Code, w.Body.String())
	}

	// Update → new immutable revision #2, active pointer moves, name updated.
	w = httptest.NewRecorder()
	testHandler.UpdateHook(w, withURLParam(newMemberHookRequest(http.MethodPatch, "/api/hooks/"+hookID, sampleHookSpec("renamed hook", "second", issueID)), "id", hookID))
	if w.Code != http.StatusOK {
		t.Fatalf("update: status %d: %s", w.Code, w.Body.String())
	}
	var updated HookResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Revision.Revision != 2 || updated.Name != "renamed hook" || updated.Revision.ID == created.Revision.ID {
		t.Fatalf("update did not append a new revision / rename: %+v", updated)
	}
	var revCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM hook_revision WHERE hook_id = $1`, hookID).Scan(&revCount)
	if revCount != 2 {
		t.Errorf("hook_revision count = %d, want 2 (revisions are immutable)", revCount)
	}

	// List.
	w = httptest.NewRecorder()
	testHandler.ListHooks(w, newMemberHookRequest(http.MethodGet, "/api/hooks", nil))
	var list []HookResponse
	json.NewDecoder(w.Body).Decode(&list)
	if !containsHook(list, hookID) {
		t.Errorf("list does not contain created hook %s", hookID)
	}

	// Disable → enabled=false with reason.
	w = httptest.NewRecorder()
	testHandler.DisableHook(w, withURLParam(newMemberHookRequest(http.MethodPost, "/api/hooks/"+hookID+"/disable", map[string]any{"reason": "paused"}), "id", hookID))
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

	// Executions trace is empty in store-only PR2 but the endpoint works.
	w = httptest.NewRecorder()
	testHandler.ListHookExecutions(w, withURLParam(newMemberHookRequest(http.MethodGet, "/api/hooks/"+hookID+"/executions", nil), "id", hookID))
	if w.Code != http.StatusOK {
		t.Fatalf("executions: status %d", w.Code)
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
}

func TestHookRequiresFeatureFlag(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	prev := testHandler.FeatureFlags
	testHandler.FeatureFlags = nil // nil service → every flag resolves to its default (off)
	t.Cleanup(func() { testHandler.FeatureFlags = prev })

	w := httptest.NewRecorder()
	testHandler.CreateHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks", sampleHookSpec("blocked", "x", "55555555-5555-5555-5555-555555555555")))
	if w.Code != http.StatusNotFound {
		t.Errorf("create with flag off: status %d, want 404", w.Code)
	}
}

// Strict schema: unknown fields and per-action disallowed fields and bad status
// are all rejected at the boundary with 400, never persisted (review point 3).
func TestHookStrictSchemaRejections(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	enableHooksFlag(t)
	issueID := seedHookIssue(t)

	cases := map[string]func() map[string]any{
		"unknown top-level field": func() map[string]any {
			s := sampleHookSpec("x", "hi", issueID)
			s["unexpected"] = true
			return s
		},
		"disallowed action field": func() map[string]any {
			s := sampleHookSpec("x", "hi", issueID)
			s["do"] = []any{map[string]any{"type": "add_comment", "issue_id": issueID, "message": "hi", "agent_id": "66666666-6666-6666-6666-666666666666"}}
			return s
		},
		"invalid status enum": func() map[string]any {
			s := sampleHookSpec("x", "hi", issueID)
			s["do"] = []any{map[string]any{"type": "set_issue_status", "issue_id": issueID, "status": "ascended"}}
			return s
		},
		"system-only action": func() map[string]any {
			s := sampleHookSpec("x", "hi", issueID)
			s["do"] = []any{map[string]any{"type": "set_issue_status_many"}}
			return s
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			testHandler.CreateHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks", build()))
			if w.Code != http.StatusBadRequest {
				t.Errorf("status %d, want 400: %s", w.Code, w.Body.String())
			}
		})
	}
}

// Fail-closed target validation: a spec that references a target absent from the
// workspace is rejected with 400, not persisted (review point 2).
func TestHookFailClosedTargets(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	enableHooksFlag(t)
	realIssue := seedHookIssue(t)
	const ghost = "77777777-7777-7777-7777-777777777777"

	action := func(a map[string]any) map[string]any {
		s := sampleHookSpec("targets", "hi", realIssue)
		s["do"] = []any{a}
		return s
	}
	cases := map[string]map[string]any{
		"nonexistent issue":     action(map[string]any{"type": "add_comment", "issue_id": ghost, "message": "hi"}),
		"nonexistent member":    action(map[string]any{"type": "send_inbox", "member_id": ghost, "message": "hi"}),
		"nonexistent agent":     action(map[string]any{"type": "trigger_agent", "issue_id": realIssue, "agent_id": ghost}),
		"nonexistent autopilot": action(map[string]any{"type": "run_autopilot", "autopilot_id": ghost}),
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			testHandler.CreateHook(w, newMemberHookRequest(http.MethodPost, "/api/hooks", spec))
			if w.Code != http.StatusBadRequest {
				t.Errorf("status %d, want 400 (fail-closed): %s", w.Code, w.Body.String())
			}
		})
	}

	// A real, invokable agent target is accepted.
	t.Run("real invokable agent accepted", func(t *testing.T) {
		spec := action(map[string]any{"type": "trigger_agent", "issue_id": realIssue, "agent_id": seededHookAgentID(t)})
		createHookAs(t, testUserID, spec) // fails the test if not 201
	})
}

// Only the hook's principal or a workspace owner/admin may edit it; an arbitrary
// member cannot rewrite a rule that keeps running under someone else's authority
// (review point 1).
func TestHookEditAuthorization(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	enableHooksFlag(t)
	issueID := seedHookIssue(t)

	author := seedHookMember(t, "member") // principal, non-admin
	other := seedHookMember(t, "member")  // non-principal, non-admin

	hook := createHookAs(t, author, sampleHookSpec("owned by author", "hi", issueID))

	// A different non-admin member cannot edit / disable / delete it.
	patch := withURLParam(newUserHookRequest(http.MethodPatch, "/api/hooks/"+hook.ID, sampleHookSpec("hijacked", "x", issueID), other), "id", hook.ID)
	w := httptest.NewRecorder()
	testHandler.UpdateHook(w, patch)
	if w.Code != http.StatusForbidden {
		t.Errorf("other member PATCH: status %d, want 403", w.Code)
	}
	w = httptest.NewRecorder()
	testHandler.DisableHook(w, withURLParam(newUserHookRequest(http.MethodPost, "/api/hooks/"+hook.ID+"/disable", nil, other), "id", hook.ID))
	if w.Code != http.StatusForbidden {
		t.Errorf("other member disable: status %d, want 403", w.Code)
	}
	w = httptest.NewRecorder()
	testHandler.DeleteHook(w, withURLParam(newUserHookRequest(http.MethodDelete, "/api/hooks/"+hook.ID, nil, other), "id", hook.ID))
	if w.Code != http.StatusForbidden {
		t.Errorf("other member delete: status %d, want 403", w.Code)
	}

	// The principal can edit their own hook.
	w = httptest.NewRecorder()
	testHandler.UpdateHook(w, withURLParam(newUserHookRequest(http.MethodPatch, "/api/hooks/"+hook.ID, sampleHookSpec("by principal", "x", issueID), author), "id", hook.ID))
	if w.Code != http.StatusOK {
		t.Errorf("principal PATCH: status %d, want 200: %s", w.Code, w.Body.String())
	}

	// A workspace owner/admin (the fixture owner) can edit any hook.
	w = httptest.NewRecorder()
	testHandler.UpdateHook(w, withURLParam(newMemberHookRequest(http.MethodPatch, "/api/hooks/"+hook.ID, sampleHookSpec("by admin", "x", issueID)), "id", hook.ID))
	if w.Code != http.StatusOK {
		t.Errorf("admin PATCH: status %d, want 200: %s", w.Code, w.Body.String())
	}

	// The principal was NOT transferred by any edit — it is still the author.
	var principal string
	testPool.QueryRow(context.Background(),
		`SELECT authorization_principal_user_id FROM hook WHERE id = $1`, hook.ID).Scan(&principal)
	if principal != author {
		t.Errorf("principal = %s, want %s (edits must not transfer the principal)", principal, author)
	}
}

// Concurrent PATCHes on the same hook must each append a distinct, contiguous
// revision without a MAX+1 unique-index collision surfacing as a 500 (review
// point 4). Driven at the service layer so all workers hit the pool concurrently.
func TestHookConcurrentPatchAppendsContiguousRevisions(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	enableHooksFlag(t)
	ctx := context.Background()
	issueID := seedHookIssue(t)
	hook := createHookAs(t, testUserID, sampleHookSpec("concurrent", "hi", issueID))

	admin := service.HookAuthor{
		ActorType:        "member",
		ActorID:          parseUUID(testUserID),
		PrincipalUserID:  parseUUID(testUserID),
		IsWorkspaceAdmin: true,
	}
	hookUUID := parseUUID(hook.ID)
	wsUUID := parseUUID(testWorkspaceID)

	const workers = 8
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			spec := hookSpecFromMap(sampleHookSpec(fmt.Sprintf("rev-%d", n), "x", issueID))
			_, err := testHandler.HookService.UpdateHook(ctx, wsUUID, hookUUID, spec, admin, nil)
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent PATCH errored (revision race not serialized): %v", err)
		}
	}

	// Revisions must be exactly 1..(workers+1), contiguous and unique.
	rows, err := testPool.Query(ctx, `SELECT revision FROM hook_revision WHERE hook_id = $1 ORDER BY revision`, hook.ID)
	if err != nil {
		t.Fatalf("query revisions: %v", err)
	}
	defer rows.Close()
	var revs []int
	for rows.Next() {
		var r int
		rows.Scan(&r)
		revs = append(revs, r)
	}
	if len(revs) != workers+1 {
		t.Fatalf("revision count = %d, want %d", len(revs), workers+1)
	}
	for i, r := range revs {
		if r != i+1 {
			t.Fatalf("revisions not contiguous at index %d: got %d, want %d (revs=%v)", i, r, i+1, revs)
		}
	}
}

// An agent author with no resolvable human principal (§8) is refused.
func TestHookAgentRequiresPrincipal(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	enableHooksFlag(t)
	issueID := seedHookIssue(t)
	req := newMemberHookRequest(http.MethodPost, "/api/hooks", sampleHookSpec("agent hook", "x", issueID))
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
