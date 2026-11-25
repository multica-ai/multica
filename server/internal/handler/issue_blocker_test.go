package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func blockerTestIssue(t *testing.T, body map[string]any) IssueResponse {
	t.Helper()
	if body["title"] == nil {
		body["title"] = "blocker API " + uuid.NewString()
	}
	var out IssueResponse
	testutil.Call(t, testHandler.CreateIssue, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body)).Want(http.StatusCreated).JSON(&out)
	t.Cleanup(func() {
		req := withURLParam(newRequest("DELETE", "/api/issues/"+out.ID, nil), "id", out.ID)
		testutil.Call(t, testHandler.DeleteIssue, req).Want(http.StatusNoContent)
	})
	return out
}

func TestIssueBlockerReleaseKeepsWorkflowBrief(t *testing.T) {
	seedTestCatalog(t)
	agent := seededReadyAgentID(t)
	project := deliveryProject(t, "Blocked delivery "+workflowTestSuffix(), agent)
	prerequisite := blockerTestIssue(t, map[string]any{"status": "todo"})
	issue := blockerTestIssue(t, map[string]any{"status": "in_progress", "project_id": project, "blocked_by": []string{prerequisite.ID}})
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", issue.ID); n != 0 {
		t.Fatalf("blocked handoff queued %d runs", n)
	}
	updateIssueForTest(t, prerequisite.ID, map[string]any{"status": "done"}).Want(200)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	if err := svc.TickWorkspaces(context.Background(), parseUUID(testWorkspaceID)); err != nil {
		t.Fatal(err)
	}
	rule, err := testHandler.Queries.GetSystemWakeup(context.Background(), db.GetSystemWakeupParams{IssueID: parseUUID(issue.ID), SystemRule: pgtype.Text{String: service.SystemRuleBlockersDone, Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := testHandler.Queries.GetAgentTask(context.Background(), rule.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.WorkflowStep.String != "in_progress" {
		t.Fatalf("wakeup workflow step = %s", task.WorkflowStep.String)
	}
	if brief := claimBriefForTest(t, issue.ID, task); !strings.Contains(brief, "Implement it and open a PR.") {
		t.Fatalf("missing handoff instructions: %s", brief)
	}
}

func TestIssueBlockerAtomicAPIAndFilter(t *testing.T) {
	a := blockerTestIssue(t, map[string]any{"status": "todo"})
	b := blockerTestIssue(t, map[string]any{"status": "todo", "blocked_by": []string{a.ID}, "assignee_type": "agent", "assignee_id": handlerTestAgentID(t)})
	if b.BlockedCount != 1 || len(b.BlockedBy) != 1 || b.BlockedBy[0].Via != "manual" {
		t.Fatalf("missing blockers: %+v", b)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", b.ID); n != 0 {
		t.Fatalf("blocked create queued %d tasks", n)
	}
	// The rejected cycle rolls back the status write as well as the edge.
	req := withURLParam(newRequest("PUT", "/api/issues/"+a.ID, map[string]any{"status": "in_review", "blocked_by": []string{b.ID}}), "id", a.ID)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusBadRequest)
	var status string
	dbfx.QueryRow(t, "SELECT status FROM issue WHERE id=$1", a.ID).Scan(&status)
	if status != "todo" {
		t.Fatalf("rejected edge changed status: %s", status)
	}
	var list struct {
		Issues []IssueResponse `json:"issues"`
		Total  int             `json:"total"`
	}
	testutil.Call(t, testHandler.ListIssues, newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&blocked=true", nil)).Want(200).JSON(&list)
	found := false
	for _, row := range list.Issues {
		if row.ID == b.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("blocked list omitted waiting issue")
	}
	// Parent edits must also validate existing manual edges.
	req = withURLParam(newRequest("PUT", "/api/issues/"+b.ID, map[string]any{"parent_issue_id": a.ID}), "id", b.ID)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusBadRequest)
}

// A bulk insert of top-level issues took minutes under stale planner statistics
// when every row ran the blocker checks.
func TestIssueBlockerChecksSkipNewTopLevelIssues(t *testing.T) {
	ctx := context.Background()
	parent := blockerTestIssue(t, map[string]any{})
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Both checks fail loudly inside this transaction only.
	if _, err := tx.Exec(ctx, `
		CREATE OR REPLACE FUNCTION check_issue_blocker_graph(ws uuid, target uuid) RETURNS void
		LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'blocker checks ran'; END $$;
		CREATE OR REPLACE FUNCTION arm_issue_blocker_wakeup(ws uuid, target uuid) RETURNS void
		LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'blocker checks ran'; END $$;
	`); err != nil {
		t.Fatal(err)
	}
	insert := func(parentID any) error {
		sub, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer sub.Rollback(ctx)
		_, err = sub.Exec(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, parent_issue_id)
			SELECT $1, 'blocker checks', 'todo', 'none', 'member', $2, 0, COALESCE(MAX(number), 0) + 1, $3::uuid
			FROM issue WHERE workspace_id = $1
		`, testWorkspaceID, testUserID, parentID)
		return err
	}
	if err := insert(nil); err != nil {
		t.Fatalf("top-level insert: %v", err)
	}
	if err := insert(parent.ID); err == nil || !strings.Contains(err.Error(), "blocker checks ran") {
		t.Fatalf("sub-issue insert skipped the blocker checks: %v", err)
	}
}

func TestIssueBlockerRejectsMissingReferenceAtomically(t *testing.T) {
	title := "missing blocker " + uuid.NewString()
	testutil.Call(t, testHandler.CreateIssue, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{"title": title, "blocked_by": []string{uuid.NewString()}})).Want(400)
	if n := dbfx.Count(t, "SELECT count(*) FROM issue WHERE workspace_id=$1 AND title=$2", testWorkspaceID, title); n != 0 {
		t.Fatalf("failed create left %d issues", n)
	}
	a := blockerTestIssue(t, map[string]any{})
	req := withURLParam(newRequest("PUT", "/api/issues/"+a.ID, map[string]any{"blocked_by": []string{"malformed"}}), "id", a.ID)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(400)
}

func TestIssueBlockerPrivateReferencesStayPrivate(t *testing.T) {
	f := newPrivateProjectFixture(t)
	ctx := context.Background()
	if err := service.AddIssueBlocker(ctx, testHandler.Queries, parseUUID(testWorkspaceID), parseUUID(f.publicIssue), parseUUID(f.secretIssue), "member", parseUUID(testUserID)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dbfx.Exec(t, "DELETE FROM issue_blocker WHERE issue_id=$1", f.publicIssue)
		cleanupChildDoneIssue(f.publicIssue)
	})
	get := func(user string) IssueResponse {
		t.Helper()
		var issue IssueResponse
		req := withURLParam(asUser(newRequest("GET", "/api/issues/"+f.publicIssue, nil), user), "id", f.publicIssue)
		testutil.Call(t, testHandler.GetIssue, req).Want(200).JSON(&issue)
		return issue
	}
	owner := get(testUserID)
	if len(owner.BlockedBy) != 1 || owner.BlockedBy[0].ID != f.secretIssue {
		t.Fatalf("authorized owner lost the reference: %+v", owner.BlockedBy)
	}
	outsider := get(f.outsider)
	if len(outsider.BlockedBy) != 0 || outsider.BlockedCount != 1 {
		t.Fatalf("outsider projection = %+v", outsider)
	}
	publisher := *testHandler
	publisher.Bus = events.New()
	var broadcast string
	publisher.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) { raw, _ := json.Marshal(e.Payload); broadcast = string(raw) })
	update := withURLParam(newRequest("PUT", "/api/issues/"+f.publicIssue, map[string]any{"title": f.marker + " updated public issue"}), "id", f.publicIssue)
	testutil.Call(t, publisher.UpdateIssue, update).Want(200)
	if broadcast == "" || strings.Contains(broadcast, f.secretIssue) || strings.Contains(broadcast, f.marker+" secret issue") {
		t.Fatalf("public broadcast leaked or was missing: %s", broadcast)
	}
	req := withURLParam(asUser(newRequest("PUT", "/api/issues/"+f.publicIssue, map[string]any{"blocked_by": []string{f.secretIssue}}), f.outsider), "id", f.publicIssue)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(400)
	dbfx.Exec(t, "UPDATE issue SET assignee_type='member',assignee_id=$2 WHERE id=$1", f.publicIssue, f.outsider)
	dbfx.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", f.secretIssue)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	if err := svc.TickWorkspaces(ctx, parseUUID(testWorkspaceID)); err != nil {
		t.Fatal(err)
	}
	var details string
	dbfx.QueryRow(t, "SELECT details::text FROM inbox_item WHERE issue_id=$1 AND type='blockers_done'", f.publicIssue).Scan(&details)
	if strings.Contains(details, f.secretIssue) || strings.Contains(details, f.marker+" secret issue") {
		t.Fatalf("inbox leaked a private prerequisite: %s", details)
	}
	var activity string
	dbfx.QueryRow(t, "SELECT details::text FROM activity_log WHERE issue_id=$1 AND action='wakeup_triggered' ORDER BY created_at DESC LIMIT 1", f.publicIssue).Scan(&activity)
	if strings.Contains(activity, f.secretIssue) || strings.Contains(activity, f.marker+" secret issue") {
		t.Fatalf("activity leaked a private prerequisite: %s", activity)
	}
}

func TestIssueBlockerDeletingStagePredecessorReleasesWaitingIssue(t *testing.T) {
	parent := blockerTestIssue(t, map[string]any{"status": "todo"})
	earlier := dbfx.Issue(t, "deleted stage predecessor", testutil.Cols{"parent_issue_id": parent.ID, "stage": 1})
	waiting := blockerTestIssue(t, map[string]any{"status": "todo", "parent_issue_id": parent.ID, "stage": 2, "assignee_type": "agent", "assignee_id": handlerTestAgentID(t)})
	if waiting.BlockedCount != 1 {
		t.Fatalf("later stage was not waiting: %+v", waiting)
	}
	req := withURLParam(newRequest("DELETE", "/api/issues/"+earlier, nil), "id", earlier)
	testutil.Call(t, testHandler.DeleteIssue, req).Want(204)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	if err := svc.TickWorkspaces(context.Background(), parseUUID(testWorkspaceID)); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND context->>'wakeup_system'='blockers_done'", waiting.ID); n != 1 {
		t.Fatalf("deleting predecessor queued %d runs", n)
	}
	var current IssueResponse
	req = withURLParam(newRequest("GET", "/api/issues/"+waiting.ID, nil), "id", waiting.ID)
	testutil.Call(t, testHandler.GetIssue, req).Want(200).JSON(&current)
	if current.BlockedCount != 0 || current.Status != "todo" {
		t.Fatalf("release changed status or left a blocker: %+v", current)
	}
}

func TestIssueBlockerRejectsSelfWithReadableError(t *testing.T) {
	a := blockerTestIssue(t, map[string]any{"status": "todo"})
	req := withURLParam(newRequest("PUT", "/api/issues/"+a.ID, map[string]any{"title": "renamed " + uuid.NewString(), "blocked_by": []string{a.ID}}), "id", a.ID)
	body := testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusBadRequest).Text()
	if !strings.Contains(body, "an issue cannot block itself") || strings.Contains(body, "constraint") {
		t.Fatalf("self blocker error = %s", body)
	}
	var title string
	dbfx.QueryRow(t, "SELECT title FROM issue WHERE id=$1", a.ID).Scan(&title)
	if title != a.Title {
		t.Fatalf("rejected self blocker saved title %q", title)
	}
}

func TestIssueBlockerFilterSkipsFinishedIssues(t *testing.T) {
	open := blockerTestIssue(t, map[string]any{"status": "todo"})
	waiting := blockerTestIssue(t, map[string]any{"status": "todo", "blocked_by": []string{open.ID}})
	finished := map[string]string{}
	for _, status := range []string{"done", "cancelled"} {
		issue := blockerTestIssue(t, map[string]any{"status": "todo", "blocked_by": []string{open.ID}})
		updateIssueForTest(t, issue.ID, map[string]any{"status": status}).Want(200)
		finished[issue.ID] = status
	}
	var list struct {
		Issues []IssueResponse `json:"issues"`
	}
	testutil.Call(t, testHandler.ListIssues, newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&blocked=true&limit=100", nil)).Want(200).JSON(&list)
	var grouped GroupedIssuesResponse
	testutil.Call(t, testHandler.ListGroupedIssues, newRequest("GET", "/api/issues/grouped?workspace_id="+testWorkspaceID+"&group_by=assignee&blocked=true", nil)).Want(200).JSON(&grouped)
	rows := map[string][]IssueResponse{"list": list.Issues}
	for _, group := range grouped.Groups {
		rows["grouped"] = append(rows["grouped"], group.Issues...)
	}
	for surface, issues := range rows {
		found := false
		for _, row := range issues {
			if row.ID == waiting.ID {
				found = true
			}
			if status, ok := finished[row.ID]; ok {
				t.Fatalf("%s blocked filter listed a %s issue", surface, status)
			}
		}
		if !found {
			t.Fatalf("%s blocked filter omitted the waiting issue", surface)
		}
	}
}

func TestIssueBlockerDetailListsBlocking(t *testing.T) {
	prerequisite := blockerTestIssue(t, map[string]any{"status": "todo"})
	waiting := blockerTestIssue(t, map[string]any{"status": "todo", "blocked_by": []string{prerequisite.ID}})
	parent := blockerTestIssue(t, map[string]any{"status": "todo"})
	earlier := blockerTestIssue(t, map[string]any{"status": "todo", "parent_issue_id": parent.ID, "stage": 1})
	later := blockerTestIssue(t, map[string]any{"status": "todo", "parent_issue_id": parent.ID, "stage": 2})
	detail := func(id string) IssueResponse {
		t.Helper()
		var issue IssueResponse
		req := withURLParam(newRequest("GET", "/api/issues/"+id, nil), "id", id)
		testutil.Call(t, testHandler.GetIssue, req).Want(200).JSON(&issue)
		if issue.Blocking == nil {
			t.Fatalf("detail of %s omitted blocking", id)
		}
		return issue
	}
	if got := *detail(prerequisite.ID).Blocking; len(got) != 1 || got[0].ID != waiting.ID || got[0].Via != "manual" || got[0].Closed {
		t.Fatalf("prerequisite blocking = %+v", got)
	}
	if got := *detail(earlier.ID).Blocking; len(got) != 1 || got[0].ID != later.ID || got[0].Via != "stage" {
		t.Fatalf("earlier stage blocking = %+v", got)
	}
	if got := *detail(waiting.ID).Blocking; len(got) != 0 {
		t.Fatalf("waiting issue blocks %+v", got)
	}
	updateIssueForTest(t, waiting.ID, map[string]any{"status": "done"}).Want(200)
	if got := *detail(prerequisite.ID).Blocking; len(got) != 1 || !got[0].Closed {
		t.Fatalf("finished dependent not marked closed: %+v", got)
	}
	// Lists and broadcasts stay as they were: only the detail read pays for it.
	body := testutil.Call(t, testHandler.ListIssues, newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&limit=100", nil)).Want(200).Text()
	if strings.Contains(body, `"blocking"`) {
		t.Fatal("issue list carried blocking")
	}
}

func TestIssueBlockerBlockingHidesPrivateDependents(t *testing.T) {
	f := newPrivateProjectFixture(t)
	if err := service.AddIssueBlocker(context.Background(), testHandler.Queries, parseUUID(testWorkspaceID), parseUUID(f.secretIssue), parseUUID(f.publicIssue), "member", parseUUID(testUserID)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dbfx.Exec(t, "DELETE FROM issue_blocker WHERE issue_id=$1", f.secretIssue) })
	get := func(user string) []IssueBlockerResponse {
		t.Helper()
		var issue IssueResponse
		req := withURLParam(asUser(newRequest("GET", "/api/issues/"+f.publicIssue, nil), user), "id", f.publicIssue)
		testutil.Call(t, testHandler.GetIssue, req).Want(200).JSON(&issue)
		return *issue.Blocking
	}
	if got := get(testUserID); len(got) != 1 || got[0].ID != f.secretIssue {
		t.Fatalf("authorized owner lost the dependent: %+v", got)
	}
	if got := get(f.outsider); len(got) != 0 {
		t.Fatalf("outsider saw a private dependent: %+v", got)
	}
}

func TestIssueBlockerUpdateFlagsBlockersChanged(t *testing.T) {
	a := blockerTestIssue(t, map[string]any{"status": "todo"})
	b := blockerTestIssue(t, map[string]any{"status": "todo"})
	publisher := *testHandler
	publisher.Bus = events.New()
	var changed []bool
	publisher.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, _ := e.Payload.(map[string]any)
		flag, _ := payload["blockers_changed"].(bool)
		changed = append(changed, flag)
	})
	for _, body := range []map[string]any{
		{"blocked_by": []string{a.ID}},
		{"title": "renamed " + uuid.NewString()},
		{"remove_blocked_by": []string{a.ID}},
	} {
		req := withURLParam(newRequest("PUT", "/api/issues/"+b.ID, body), "id", b.ID)
		testutil.Call(t, publisher.UpdateIssue, req).Want(200)
	}
	batch := newRequest("POST", "/api/issues/batch-update?workspace_id="+testWorkspaceID, map[string]any{"issue_ids": []string{b.ID}, "updates": map[string]any{"blocked_by": []string{a.ID}}})
	testutil.Call(t, publisher.BatchUpdateIssues, batch).Want(200)
	if want := []bool{true, false, true, true}; len(changed) != len(want) || changed[0] != want[0] || changed[1] != want[1] || changed[2] != want[2] || changed[3] != want[3] {
		t.Fatalf("blockers_changed = %v, want %v", changed, want)
	}
}

// A guest sees only its own projects, so blocker references to issues in
// other projects or outside any project are hidden in both directions, not
// only references into private projects.
func TestIssueBlockerGuestSeesOnlyItsProjectsReferences(t *testing.T) {
	f := newGuestFixture(t)
	ctx := context.Background()
	ws := parseUUID(testWorkspaceID)
	waiting := dbfx.Issue(t, f.marker+" waiting shared issue", testutil.Cols{"project_id": f.shared, "number": 790000 + int(time.Now().UnixNano()%9000)})
	edges := [][2]string{
		{f.otherIssue, f.sharedIssue}, {f.looseIssue, f.sharedIssue}, // shared blocks two issues the guest can't open
		{waiting, f.otherIssue}, {waiting, f.looseIssue}, // waiting is blocked by them
	}
	for _, e := range edges {
		if err := service.AddIssueBlocker(ctx, testHandler.Queries, ws, parseUUID(e[0]), parseUUID(e[1]), "member", parseUUID(testUserID)); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		dbfx.Exec(t, "DELETE FROM issue_blocker WHERE issue_id = ANY($1::uuid[])", []string{f.otherIssue, f.looseIssue, waiting})
	})
	get := func(user, id string) IssueResponse {
		t.Helper()
		var issue IssueResponse
		guestRequest(t, testHandler.GetIssue, user, http.MethodGet, "/api/issues/"+id, nil, "id", id).Want(http.StatusOK).JSON(&issue)
		return issue
	}
	if got := *get(f.guest, f.sharedIssue).Blocking; len(got) != 0 {
		t.Fatalf("guest saw dependents outside its project: %+v", got)
	}
	if got := *get(testUserID, f.sharedIssue).Blocking; len(got) != 2 {
		t.Fatalf("member lost dependents: %+v", got)
	}
	guestView := get(f.guest, waiting)
	if len(guestView.BlockedBy) != 0 || guestView.BlockedCount != 2 {
		t.Fatalf("guest blocked_by projection = %+v (count %d)", guestView.BlockedBy, guestView.BlockedCount)
	}
	if got := get(testUserID, waiting).BlockedBy; len(got) != 2 {
		t.Fatalf("member lost blockers: %+v", got)
	}
}

// "Don't start yet" on the write that makes a released issue startable is an
// answer to that release: the dependency rule must not start the run anyway.
func TestIssueBlockerReleaseHonorsSuppressedRun(t *testing.T) {
	agent := seededReadyAgentID(t)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	tick := func() {
		t.Helper()
		if err := svc.TickWorkspaces(context.Background(), parseUUID(testWorkspaceID)); err != nil {
			t.Fatal(err)
		}
	}
	writes := map[string]func(id string, updates map[string]any){
		"single": func(id string, updates map[string]any) {
			updateIssueForTest(t, id, updates).Want(http.StatusOK)
		},
		"batch": func(id string, updates map[string]any) {
			testutil.Call(t, testHandler.BatchUpdateIssues, newRequest("POST", "/api/issues/batch-update?workspace_id="+testWorkspaceID, map[string]any{
				"issue_ids": []string{id}, "updates": updates,
			})).Want(http.StatusOK)
		},
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			prerequisite := blockerTestIssue(t, map[string]any{"status": "todo"})
			issue := blockerTestIssue(t, map[string]any{"status": "backlog", "assignee_type": "agent", "assignee_id": agent, "blocked_by": []string{prerequisite.ID}})
			runs := func() int {
				return dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", issue.ID)
			}
			updateIssueForTest(t, prerequisite.ID, map[string]any{"status": "done"}).Want(http.StatusOK)
			tick()
			write(issue.ID, map[string]any{"status": "todo", "suppress_run": true})
			tick()
			if n := runs(); n != 0 {
				t.Fatalf("suppressed promotion started %d runs", n)
			}
			// Later moves between active statuses are not a new release.
			updateIssueForTest(t, issue.ID, map[string]any{"status": "in_progress"}).Want(http.StatusOK)
			tick()
			if n := runs(); n != 0 {
				t.Fatalf("status move after a suppressed promotion started %d runs", n)
			}
			// A new wait is delivered as usual.
			updateIssueForTest(t, prerequisite.ID, map[string]any{"status": "todo"}).Want(http.StatusOK)
			updateIssueForTest(t, prerequisite.ID, map[string]any{"status": "done"}).Want(http.StatusOK)
			tick()
			if n := runs(); n != 1 {
				t.Fatalf("new wait started %d runs, want 1", n)
			}
		})
	}
}

// A release names the blockers to the agent it starts. The timeline and the
// inbox are read by people with different access, so neither names them.
func TestIssueBlockerReleaseHidesReferencesFromGuest(t *testing.T) {
	f := newGuestFixture(t)
	ctx := context.Background()
	for _, blocker := range []string{f.otherIssue, f.looseIssue} {
		if err := service.AddIssueBlocker(ctx, testHandler.Queries, parseUUID(testWorkspaceID), parseUUID(f.sharedIssue), parseUUID(blocker), "member", parseUUID(testUserID)); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		dbfx.Exec(t, "DELETE FROM issue_blocker WHERE issue_id=$1", f.sharedIssue)
		dbfx.Exec(t, "DELETE FROM inbox_item WHERE issue_id=$1", f.sharedIssue)
		cleanupChildDoneIssue(f.sharedIssue)
	})
	dbfx.Exec(t, "UPDATE issue SET status='todo',assignee_type='member',assignee_id=$2 WHERE id=$1", f.sharedIssue, f.guest)
	dbfx.Exec(t, "UPDATE issue SET status='done' WHERE id = ANY($1::uuid[])", []string{f.otherIssue, f.looseIssue})
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	if err := svc.TickWorkspaces(ctx, parseUUID(testWorkspaceID)); err != nil {
		t.Fatal(err)
	}
	var inbox string
	dbfx.QueryRow(t, "SELECT details::text FROM inbox_item WHERE issue_id=$1 AND type='blockers_done'", f.sharedIssue).Scan(&inbox)
	timeline := guestRequest(t, testHandler.ListTimeline, f.guest, http.MethodGet, "/api/issues/"+f.sharedIssue+"/timeline", nil, "id", f.sharedIssue).Want(http.StatusOK).Body.String()
	if !strings.Contains(timeline, "wakeup_triggered") {
		t.Fatalf("release left no timeline entry: %s", timeline)
	}
	for name, got := range map[string]string{"timeline": timeline, "inbox": inbox} {
		for _, secret := range []string{f.otherIssue, f.looseIssue, f.marker + " other issue", f.marker + " loose issue"} {
			if strings.Contains(got, secret) {
				t.Errorf("guest %s names a blocker it can't open (%s): %s", name, secret, got)
			}
		}
	}
}

// Removing a dependency is gated like adding one: a member who can't open the
// private prerequisite can't drop it, even with its ID in hand — including a
// member who added it before losing access to the project.
func TestIssueBlockerRemovalRequiresBlockerVisibility(t *testing.T) {
	agent := seededReadyAgentID(t)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	// Bodies are raw JSON so a test can repeat a key in a chosen order.
	writes := map[string]func(user, id, updates string) *testutil.Response{
		"single": func(user, id, updates string) *testutil.Response {
			return testutil.Call(t, testHandler.UpdateIssue, withURLParam(asUser(newRequest("PUT", "/api/issues/"+id, json.RawMessage(updates)), user), "id", id))
		},
		"batch": func(user, id, updates string) *testutil.Response {
			return testutil.Call(t, testHandler.BatchUpdateIssues, asUser(newRequest("POST", "/api/issues/batch-update?workspace_id="+testWorkspaceID, map[string]any{
				"issue_ids": []string{id}, "updates": json.RawMessage(updates),
			}), user))
		},
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			f := newPrivateProjectFixture(t)
			t.Cleanup(func() {
				dbfx.Exec(t, "DELETE FROM issue_blocker WHERE issue_id=$1", f.publicIssue)
				cleanupChildDoneIssue(f.publicIssue)
			})
			dbfx.Exec(t, "UPDATE issue SET status='todo',assignee_type='agent',assignee_id=$2 WHERE id=$1", f.publicIssue, agent)
			edges := func() int {
				return dbfx.Count(t, "SELECT count(*) FROM issue_blocker WHERE issue_id=$1 AND blocker_issue_id=$2", f.publicIssue, f.secretIssue)
			}
			runs := func() int {
				return dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", f.publicIssue)
			}
			tick := func() {
				t.Helper()
				if err := svc.TickWorkspaces(context.Background(), parseUUID(testWorkspaceID)); err != nil {
					t.Fatal(err)
				}
			}
			write(f.insider, f.publicIssue, fmt.Sprintf(`{"blocked_by":[%q]}`, f.secretIssue)).Want(http.StatusOK)
			remove := fmt.Sprintf(`{"remove_blocked_by":[%q]}`, f.secretIssue)
			write(f.outsider, f.publicIssue, remove).Want(http.StatusBadRequest)
			// A second key that differs only in case must not swap the checked
			// list for another one before the write.
			for _, key := range []string{"blocked_by", "remove_blocked_by"} {
				body := fmt.Sprintf(`{%q:[%q],%q:[]}`, key, f.secretIssue, strings.ToUpper(key))
				write(f.outsider, f.publicIssue, body).Want(http.StatusBadRequest)
			}
			removeProjectMemberRow(t, f.projectID, f.insider)
			hidden := write(f.insider, f.publicIssue, remove).Want(http.StatusBadRequest).Text()
			// A missing ID gets the same answer as a hidden one, so a member who
			// lost access can't learn whether the private issue still exists.
			missing := write(f.insider, f.publicIssue, fmt.Sprintf(`{"remove_blocked_by":[%q]}`, uuid.NewString())).Want(http.StatusBadRequest).Text()
			if hidden != missing {
				t.Fatalf("hidden blocker answered %q, missing blocker %q", hidden, missing)
			}
			tick()
			if n := edges(); n != 1 {
				t.Fatalf("rejected removals left %d edges, want 1", n)
			}
			if n := runs(); n != 0 {
				t.Fatalf("rejected removals started %d runs", n)
			}
			// The edge was the only thing holding the run back: a member who
			// can see the prerequisite removes it and the issue starts.
			write(testUserID, f.publicIssue, remove).Want(http.StatusOK)
			tick()
			if n := edges(); n != 0 {
				t.Fatalf("authorized removal left %d edges", n)
			}
			if n := runs(); n != 1 {
				t.Fatalf("authorized removal started %d runs, want 1", n)
			}
		})
	}
}
