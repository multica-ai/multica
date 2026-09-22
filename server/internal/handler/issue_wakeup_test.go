package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueWakeupAPIAndTrustedOrigin(t *testing.T) {
	issue := dbfx.Issue(t, "wake api")
	agent := dbfx.Agent(t, "wake api", testRuntimeID)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN(SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	body := map[string]any{"agent_id": agent, "kind": "at", "after_seconds": 600, "instruction": "check deployment"}
	req := withURLParam(newRequest("POST", "/api/issues/"+issue+"/wakeups", body), "id", issue)
	// An untrusted task header must not get stamped as delegation provenance.
	forged := dbfx.Task(t, agent, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issue})
	req.Header.Set("X-Task-ID", forged)
	rec := httptest.NewRecorder()
	testHandler.CreateIssueWakeup(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create %d: %s", rec.Code, rec.Body.String())
	}
	var result db.IssueWakeup
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.SourceTaskID.Valid || uuidToString(result.CreatedBy) != testUserID {
		t.Fatal("untrusted source identity")
	}
	update := withURLParams(newRequest("PUT", "/", body), "id", issue, "wakeupID", uuidToString(result.ID))
	updated := httptest.NewRecorder()
	testHandler.CreateIssueWakeup(updated, update)
	if updated.Code != http.StatusOK {
		t.Fatalf("update %d: %s", updated.Code, updated.Body.String())
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	req = withURLParam(newRequest("GET", "/", nil), "id", issue)
	rec = httptest.NewRecorder()
	testHandler.ListIssueWakeups(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	outsider := dbfx.User(t, "wake outsider", "wake-outside@multica.test")
	dbfx.Member(t, testWorkspaceID, outsider, "member")
	// A task-scoped caller must use the initiating human's rights, never the
	// runtime owner's rights, even when registering a wakeup for itself.
	dbfx.Exec(t, "UPDATE agent_task_queue SET originator_user_id=$2,accountable_user_id=$2,status='running',started_at=now() WHERE id=$1", forged, outsider)
	req = withURLParam(newRequest("POST", "/", map[string]any{"kind": "at", "after_seconds": 600, "instruction": "check"}), "id", issue)
	req.Header.Set("X-Agent-ID", agent)
	req.Header.Set("X-Task-ID", forged)
	req.Header.Set("X-Actor-Source", "task_token")
	rec = httptest.NewRecorder()
	testHandler.CreateIssueWakeup(rec, req)
	if rec.Code != 403 {
		t.Fatalf("borrowed runtime owner permission: %d %s", rec.Code, rec.Body.String())
	}
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	if _, err := svc.Disable(context.Background(), parseUUID(issue), result.ID, parseUUID(outsider)); err != service.ErrWakeupForbidden {
		t.Fatalf("other member disabled: %v", err)
	}
	if _, err := svc.Disable(context.Background(), parseUUID(issue), result.ID, parseUUID(testUserID)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		trusted  bool
		revision int64
		want     int
	}{
		{false, 0, 400}, {true, result.Revision, 403}, {false, result.Revision, 200}, {false, result.Revision, 409},
	} {
		req := withURLParams(newRequest("POST", "/", map[string]any{"revision": tc.revision}), "id", issue, "wakeupID", uuidToString(result.ID))
		req.Header.Set("X-Task-ID", forged)
		if tc.trusted {
			req.Header.Set("X-Agent-ID", agent)
			req.Header.Set("X-Actor-Source", "task_token")
		}
		rec := httptest.NewRecorder()
		testHandler.EnableIssueWakeup(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("enable %d: %s", rec.Code, rec.Body.String())
		}
		if tc.want == 200 {
			var enabled db.IssueWakeup
			if err := json.Unmarshal(rec.Body.Bytes(), &enabled); err != nil {
				t.Fatal(err)
			}
			if !enabled.Enabled || enabled.SourceTaskID.Valid {
				t.Fatal("enable trusted a forged source")
			}
		}
	}
}

func TestWorkspaceWakeupSummariesScopeAndBounds(t *testing.T) {
	issue := dbfx.Issue(t, "wakeup summary")
	agent := dbfx.Agent(t, "summary target", testRuntimeID)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN(SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	var first db.IssueWakeup
	for i := 0; i < 5; i++ {
		in := service.WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "PRIVATE PROMPT"}
		if i == 0 {
			in.Kind = "at"
			in.EventTypes = nil
			in.AfterSeconds = 3600
		}
		w, err := svc.Create(context.Background(), parseUUID(issue), parseUUID(testUserID), pgtype.UUID{}, in)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = w
		}
	}
	read := func(req *http.Request, want int) []db.ListWorkspaceWakeupSummaryRowsRow {
		t.Helper()
		rec := httptest.NewRecorder()
		testHandler.ListWorkspaceWakeupSummaries(rec, req)
		if rec.Code != want {
			t.Fatalf("summary %d: %s", rec.Code, rec.Body.String())
		}
		if want != 200 {
			return nil
		}
		if strings.Contains(rec.Body.String(), "PRIVATE PROMPT") || strings.Contains(rec.Body.String(), "instruction") {
			t.Fatal("prompt leaked to summary")
		}
		var rows []db.ListWorkspaceWakeupSummaryRowsRow
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	rows := read(newRequest("GET", "/api/issue-wakeup-summaries", nil), 200)
	if len(rows) != 3 || rows[0].ID != first.ID || rows[0].ActiveCount != 5 || rows[0].EventCount != 4 {
		t.Fatalf("wrong bounded summary: %+v", rows)
	}
	outsider := dbfx.User(t, "summary outsider", "summary-outsider@multica.test")
	req := newRequest("GET", "/", nil)
	req.Header.Set("X-User-ID", outsider)
	read(req, 404)
	dbfx.Member(t, testWorkspaceID, outsider, "member")
	if got := read(req, 200); len(got) != 3 || got[0].ActiveCount != 5 {
		t.Fatal("shared issue rules disappeared from summary")
	}
	// Rule visibility follows the shared issue; all read surfaces consistently
	// redact private source-agent and source-run references.
	sourceRun := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": testRuntimeID, "status": "completed"})
	dbfx.Exec(t, "UPDATE issue_wakeup SET filter_agent_id=$2,filter_task_id=$3 WHERE id=$1", first.ID, agent, sourceRun)
	for _, row := range read(req, 200) {
		if row.FilterAgentName.Valid || row.FilterTaskID.Valid {
			t.Fatal("private source exposed in summary")
		}
	}
	detail := httptest.NewRecorder()
	testHandler.ListIssueWakeups(detail, withURLParam(req, "id", issue))
	var details []db.ListIssueWakeupsRow
	if detail.Code != 200 || json.Unmarshal(detail.Body.Bytes(), &details) != nil || len(details) != 5 {
		t.Fatalf("detail response: %d %s", detail.Code, detail.Body.String())
	}
	for _, row := range details {
		if row.FilterAgentName.Valid || row.FilterAgentID.Valid || row.FilterTaskID.Valid {
			t.Fatal("private source exposed in detail")
		}
		if row.Instruction != "PRIVATE PROMPT" {
			t.Fatal("shared issue instruction missing")
		}
	}
	other := dbfx.Workspace(t, "other summary", "other-summary")
	dbfx.Member(t, other, testUserID, "owner")
	req = newRequest("GET", "/", nil)
	req.Header.Set("X-Workspace-ID", other)
	if got := read(req, 200); len(got) != 0 {
		t.Fatal("cross-workspace summary exposed")
	}
	if _, err := svc.Disable(context.Background(), parseUUID(issue), first.ID, parseUUID(testUserID)); err != nil {
		t.Fatal(err)
	}
	rows = read(newRequest("GET", "/", nil), 200)
	if len(rows) != 3 || rows[0].ActiveCount != 4 || rows[0].EventCount != 4 {
		t.Fatalf("disabled entry retained: %+v", rows)
	}
	dbfx.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", issue)
	if got := read(newRequest("GET", "/", nil), 200); len(got) != 0 {
		t.Fatal("closed issue summary retained")
	}
}

func TestWakeupDeferredRunSnapshotPreservesOrigin(t *testing.T) {
	issue := dbfx.Issue(t, "deferred wakeup snapshot")
	agent := dbfx.Agent(t, "deferred wakeup", testRuntimeID)
	for _, contextJSON := range []string{`{}`, `{"wakeup_id":"01900000-0000-7000-8000-000000000001"}`} {
		dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "status": "deferred", "runtime_id": testRuntimeID, "context": contextJSON})
	}
	rec := httptest.NewRecorder()
	testHandler.ListWorkspaceAgentTaskSnapshot(rec, newRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var tasks []AgentTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &tasks); err != nil {
		t.Fatal(err)
	}
	var found int
	for _, task := range tasks {
		if task.IssueID == issue {
			found++
			if task.WakeupID != "01900000-0000-7000-8000-000000000001" || task.Status != "deferred" {
				t.Fatalf("bad wakeup origin: %+v", task)
			}
		}
	}
	if found != 1 {
		t.Fatalf("wrong deferred snapshot count: %d", found)
	}
}

func TestIssueWakeupMutationTrustedActor(t *testing.T) {
	issue := dbfx.Issue(t, "wake mutation actor")
	agent := dbfx.Agent(t, "wake mutation actor", testRuntimeID)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN(SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	comment := dbfx.Comment(t, issue, "agent original", testutil.Cols{"author_type": "agent", "author_id": agent})
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	w, err := svc.Create(context.Background(), parseUUID(issue), parseUUID(testUserID), pgtype.UUID{}, service.WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.updated", "issue.metadata_changed", "reaction.added", "reaction.removed"}, Instruction: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	run := dbfx.Task(t, agent, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issue, "status": "running", "started_at": testutil.Raw("now()"), "originator_user_id": testUserID, "accountable_user_id": testUserID})
	call := func(handler http.HandlerFunc, req *http.Request, trusted bool) {
		t.Helper()
		req.Header.Set("X-Task-ID", run)
		if trusted {
			req.Header.Set("X-Agent-ID", agent)
			req.Header.Set("X-Actor-Source", "task_token")
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code < 200 || rec.Code >= 300 {
			t.Fatalf("mutation %d: %s", rec.Code, rec.Body.String())
		}
	}
	// Admin editing another agent's comment: author stays agent; actor is member,
	// and a forged task header cannot suppress the event as the wakeup's own run.
	dbfx.Exec(t, "UPDATE agent_task_queue SET context=jsonb_build_object('wakeup_id',$2::text) WHERE id=$1", run, uuidToString(w.ID))
	call(testHandler.UpdateComment, withURLParam(newRequest("PATCH", "/", map[string]any{"content": "admin edit"}), "commentId", comment), false)
	var actorType, actorID string
	var source *string
	dbfx.QueryRow(t, "SELECT payload->>'actor_type',payload->>'actor_id',payload->>'source_task_id' FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID).Scan(&actorType, &actorID, &source)
	if actorType != "member" || actorID != testUserID || source != nil {
		t.Fatalf("wrong editor identity %s %s %v", actorType, actorID, source)
	}
	count := func() int { return dbfx.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID) }
	call(testHandler.UpdateComment, withURLParam(newRequest("PATCH", "/", map[string]any{"content": "own wakeup edit"}), "commentId", comment), true)
	req := withURLParams(newRequest("PUT", "/", map[string]any{"value": "secret"}), "id", issue, "key", "test")
	call(testHandler.SetIssueMetadataKey, req, true)
	call(testHandler.AddIssueReaction, withURLParam(newRequest("POST", "/", map[string]any{"emoji": "👍"}), "id", issue), true)
	call(testHandler.RemoveIssueReaction, withURLParam(newRequest("DELETE", "/", map[string]any{"emoji": "👍"}), "id", issue), true)
	if count() != 1 {
		t.Fatalf("own run caused wakeup: %d", count())
	}
	dbfx.Exec(t, "UPDATE agent_task_queue SET context='{}' WHERE id=$1", run)
	req = withURLParams(newRequest("PUT", "/", map[string]any{"value": "changed"}), "id", issue, "key", "test")
	call(testHandler.SetIssueMetadataKey, req, true)
	if count() != 2 {
		t.Fatalf("external run not captured: %d", count())
	}
	dbfx.QueryRow(t, "SELECT payload->>'actor_type',payload->>'actor_id',payload->>'source_task_id' FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND event_type='issue.metadata_changed'", w.ID).Scan(&actorType, &actorID, &source)
	if actorType != "agent" || actorID != agent || source == nil || *source != run {
		t.Fatalf("wrong source identity %s %s %v", actorType, actorID, source)
	}
}

func TestIssueWakeupCapacityReturnsActionableError(t *testing.T) {
	issue := dbfx.Issue(t, "wakeup capacity response")
	agent := dbfx.Agent(t, "wakeup capacity target", testRuntimeID)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	dbfx.Exec(t, `INSERT INTO issue_wakeup(id,workspace_id,issue_id,agent_id,created_by,instruction,kind,mode,event_types)
 SELECT gen_random_uuid(),$1,$2,$3,$4,'check','event','continuous',ARRAY['comment.created'] FROM generate_series(1,32)`, testWorkspaceID, issue, agent, testUserID)
	req := withURLParam(newRequest("POST", "/", map[string]any{"agent_id": agent, "kind": "at", "after_seconds": 600, "instruction": "check"}), "id", issue)
	rec := httptest.NewRecorder()
	testHandler.CreateIssueWakeup(rec, req)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "wakeup_capacity_exceeded") || !strings.Contains(rec.Body.String(), "32") {
		t.Fatalf("capacity error %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIssueWakeupActorFilterAPIAndProjection(t *testing.T) {
	issue := dbfx.Issue(t, "actor filter API")
	target := dbfx.Agent(t, "actor filter target", testRuntimeID)
	source := dbfx.Agent(t, "hidden actor name", testRuntimeID)
	person := dbfx.User(t, "Monitored Person", "actor-projection@multica.test")
	dbfx.Member(t, testWorkspaceID, person, "member")
	reader := dbfx.User(t, "actor reader", "actor-reader@multica.test")
	dbfx.Member(t, testWorkspaceID, reader, "member")
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN(SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	for _, actor := range []struct{ kind, id string }{{"member", person}, {"agent", source}} {
		req := withURLParam(newRequest("POST", "/", map[string]any{"agent_id": target, "kind": "event", "event_types": []string{"comment.created"}, "filter_actor_type": actor.kind, "filter_actor_id": actor.id, "instruction": "wait for this actor"}), "id", issue)
		rec := httptest.NewRecorder()
		testHandler.CreateIssueWakeup(rec, req)
		if rec.Code != 201 {
			t.Fatalf("actor create %d: %s", rec.Code, rec.Body.String())
		}
		var rule db.IssueWakeup
		if err := json.Unmarshal(rec.Body.Bytes(), &rule); err != nil {
			t.Fatal(err)
		}
		if rule.FilterActorType.String != actor.kind || uuidToString(rule.FilterActorID) != actor.id {
			t.Fatal("actor filter not persisted")
		}
	}
	for _, endpoint := range []func(http.ResponseWriter, *http.Request){testHandler.ListIssueWakeups, testHandler.ListWorkspaceWakeupSummaries, testHandler.ListWorkspaceWakeups} {
		req := withURLParam(newRequest("GET", "/?scope=all", nil), "id", issue)
		req.Header.Set("X-User-ID", reader)
		rec := httptest.NewRecorder()
		endpoint(rec, req)
		if rec.Code != 200 {
			t.Fatal(rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Monitored Person") || !strings.Contains(body, person) || !strings.Contains(body, `"filter_actor_type":"agent"`) {
			t.Fatalf("missing actor projection: %s", body)
		}
		if strings.Contains(body, "hidden actor name") || strings.Contains(body, source) {
			t.Fatalf("private actor exposed: %s", body)
		}
	}
}

func TestIssueWakeupInstructionAPI(t *testing.T) {
	issue := dbfx.Issue(t, "edit prompt api")
	agent := dbfx.Agent(t, "edit prompt", testRuntimeID)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	rule, err := svc.Create(context.Background(), parseUUID(issue), parseUUID(testUserID), pgtype.UUID{}, service.WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 600, Instruction: "old"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		body map[string]any
		want int
	}{
		{uuidToString(rule.ID), map[string]any{"instruction": "new", "expected_instruction": "old", "revision": rule.Revision}, 204},
		{uuidToString(rule.ID), map[string]any{"instruction": "overwritten", "expected_instruction": "old", "revision": rule.Revision}, 409},
		{uuidToString(rule.ID), map[string]any{"instruction": " ", "expected_instruction": "new", "revision": rule.Revision}, 400},
		{uuidToString(rule.ID), map[string]any{"instruction": "new", "expected_instruction": "new", "revision": rule.Revision, "enabled": true}, 400},
		{"not-a-uuid", map[string]any{"instruction": "new", "expected_instruction": "new", "revision": rule.Revision}, 400},
	} {
		req := withURLParams(newRequest("PATCH", "/", tc.body), "id", issue, "wakeupID", tc.id)
		testutil.Call(t, testHandler.EditIssueWakeupInstruction, req).Want(tc.want)
	}
}
