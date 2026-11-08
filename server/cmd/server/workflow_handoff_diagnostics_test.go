package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Observe the real handler, activity listener and enqueue path against the test DB.
// Faults are limited to named queries; all fixtures and successful inserts are real.
type handoffDiagnosticDB struct {
	db.DBTX
	fault       string
	noticeFault bool
	counts      map[string]int
	queryErrors []string
}
type handoffDiagnosticErrorRow struct{ err error }

func (r handoffDiagnosticErrorRow) Scan(...any) error { return r.err }

type handoffDiagnosticObservedRow struct {
	pgx.Row
	db  *handoffDiagnosticDB
	key string
}

func (r handoffDiagnosticObservedRow) Scan(args ...any) error {
	err := r.Row.Scan(args...)
	if err != nil {
		r.db.queryErrors = append(r.db.queryErrors, r.key+": "+err.Error())
	}
	return err
}
func (d *handoffDiagnosticDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	name := strings.Split(strings.Split(sql, "\n")[0], " ")
	key := sql
	if len(name) > 2 {
		key = name[2]
	}
	d.counts[key]++
	var row pgx.Row
	if (d.fault != "" && key == d.fault) || (d.noticeFault && key == "CreateComment") {
		row = handoffDiagnosticErrorRow{fmt.Errorf("injected diagnostic query failure: SQL secret-token private-runtime")}
	} else {
		row = d.DBTX.QueryRow(ctx, sql, args...)
	}
	return handoffDiagnosticObservedRow{row, d, key}
}

func TestWorkflowHandoffRunDiagnostics(t *testing.T) {
	if testPool == nil {
		t.Fatal("explicit isolated database required")
	}
	cases := []struct {
		name                 string
		batch                bool
		mode                 string
		preview, code, tasks int
		hand                 string
	}{
		{"idle_single", false, "idle", 1, 200, 1, "true"},
		{"idle_batch", true, "idle", 1, 200, 1, "true"},
		{"agent_from_other_issue", false, "agent_other", 1, 200, 1, "true"},
		{"agent_batch_from_other_issue", true, "agent_other", 1, 200, 1, "true"},
		{"same_handler_idle", false, "same", 1, 200, 1, "true"},
		{"own_running_next_step", false, "selfrun", 1, 200, 2, "true"},
		{"queued_covers", false, "queued", 0, 200, 1, "false"},
		{"dispatched_covers", false, "dispatched", 0, 200, 1, "false"},
		{"suppressed_single", false, "suppress", 1, 200, 0, "false"},
		{"suppressed_batch", true, "suppress", 1, 200, 0, "false"},
		{"no_runtime", false, "no_runtime", 0, 200, 0, "false"},
		{"archived_agent", false, "archived", 0, 400, 0, ""},
		{"missing_agent", false, "missing_agent", 0, 400, 0, ""},
		{"unusable_runtime", false, "unusable_runtime", 1, 200, 1, "true"},
		{"pending_lookup_error", false, "pending_error", 0, 200, 0, "false"},
		{"pending_lookup_error_batch", true, "pending_error", 0, 200, 0, "false"},
		{"agent_lookup_error", false, "agent_error", 0, 200, 0, "false"},
		{"enqueue_error_single", false, "enqueue_error", 1, 200, 0, "true"},
		{"enqueue_error_batch", true, "enqueue_error", 1, 200, 0, "true"},
		{"concurrent_enqueue_wins", false, "race", 1, 200, 1, "true"},
		{"concurrent_enqueue_wins_batch", true, "race", 1, 200, 1, "true"},
		{"notice_write_failure", false, "notice_error", 1, 200, 0, "true"},
		{"later_status_move", false, "later_status", 1, 200, 0, "true"},
		{"queued_covers_batch", true, "queued", 0, 200, 1, "false"},
		{"dispatched_covers_batch", true, "dispatched", 0, 200, 1, "false"},
		{"head_advanced", false, "head_advanced", 1, 200, 1, "true"},
		{"thread_pending_keeps_existing_issue_guard", false, "thread_pending", 0, 200, 1, "false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fx := testutil.New(testPool, testWorkspaceID, testUserID)
			rt := fx.Insert(t, "agent_runtime", testutil.Cols{"workspace_id": testWorkspaceID, "owner_id": testUserID, "name": "HANDOFF_DIAGNOSTIC synthetic runtime", "runtime_mode": "cloud", "provider": "diagnostic_never_execute", "status": "online"})
			a := fx.Agent(t, "HANDOFF_DIAGNOSTIC A", rt)
			b := fx.Agent(t, "HANDOFF_DIAGNOSTIC B", rt)
			bus := events.New()
			tapped := &handoffDiagnosticDB{DBTX: testPool, counts: map[string]int{}}
			q := db.New(tapped)
			h := handler.New(q, testPool, nil, bus, service.NewEmailService(), nil, nil, analytics.NoopClient{}, handler.Config{})
			var evs []events.Event
			bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) { evs = append(evs, e) })
			registerActivityListeners(bus, db.New(testPool))
			bus.SubscribeAll(func(e events.Event) {
				if e.Type != protocol.EventIssueUpdated {
					evs = append(evs, e)
				}
			})
			var selfTask, callerAgent string
			req := func(method, path string, body any) *http.Request {
				r := testutil.WithHeaders(testutil.JSONRequest(method, path, body), "X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID)
				if selfTask != "" {
					testutil.WithHeaders(r, "X-Actor-Source", "task_token", "X-Agent-ID", callerAgent, "X-Task-ID", selfTask)
				}
				return r
			}
			call := func(fn http.HandlerFunc, method, path string, body any, params ...string) *testutil.Response {
				return testutil.Call(t, fn, testutil.WithURLParams(req(method, path, body), params...))
			}
			var wf handler.IssueWorkflowResponse
			call(h.CreateIssueWorkflow, "POST", "/api/issue-workflows", map[string]any{"name": "HANDOFF_DIAGNOSTIC " + tc.name, "steps": []map[string]any{{"status_key": "todo"}, {"status_key": "in_progress"}, {"status_key": "in_review"}}}).Want(201).JSON(&wf)
			t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue_workflow WHERE id=$1`, wf.ID) })
			p := fx.Project(t, "HANDOFF_DIAGNOSTIC synthetic project")
			call(h.SetProjectWorkflow, "POST", "/api/projects/"+p+"/workflow", map[string]any{"workflow_id": wf.ID}, "id", p).Want(200)
			call(h.SetIssueWorkflowHandoff, "PUT", "/api/issue-workflows/"+wf.ID+"/handoffs/in_review", map[string]any{"handler": map[string]any{"type": "agent", "id": b}, "instructions": "Synthetic regression only"}, "id", wf.ID, "status", "in_review").Want(200)
			owner := a
			if tc.mode == "same" || tc.mode == "selfrun" {
				owner = b
			}
			issueID := fx.Issue(t, "HANDOFF_DIAGNOSTIC "+tc.name, testutil.Cols{"project_id": p, "status": "in_progress", "assignee_type": "agent", "assignee_id": owner})
			t.Cleanup(func() {
				testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id=$1`, issueID)
				testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
			})
			issueUUID := util.MustParseUUID(issueID)
			bUUID := util.MustParseUUID(b)
			if tc.mode == "head_advanced" {
				pr := fx.Insert(t, "github_pull_request", testutil.Cols{"workspace_id": testWorkspaceID, "installation_id": 1, "repo_owner": "synthetic", "repo_name": "diagnostics", "pr_number": 9772, "title": "synthetic review", "state": "open", "html_url": "https://example.test/pr", "pr_created_at": testutil.Raw("now()"), "pr_updated_at": testutil.Raw("now()"), "head_sha": strings.Repeat("b", 40)})
				fx.InsertNoID(t, "issue_pull_request", testutil.Cols{"issue_id": issueID, "pull_request_id": pr}, "issue_id=$1", issueID)
				old := fx.Task(t, b, testutil.Cols{"issue_id": issueID, "runtime_id": rt, "context": `{"head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`})
				_ = old
			}
			if tc.mode == "thread_pending" {
				root := fx.Comment(t, issueID, "synthetic different thread")
				fx.Task(t, b, testutil.Cols{"issue_id": issueID, "runtime_id": rt, "trigger_comment_id": root})
			}
			if tc.mode == "queued" || tc.mode == "dispatched" {
				current, e := q.GetIssue(ctx, issueUUID)
				if e != nil {
					t.Fatal(e)
				}
				current.AssigneeID = bUUID
				task, e := h.TaskService.EnqueueTaskForIssueWithHandoff(ctx, current, "synthetic previous enqueue", util.MustParseUUID(testUserID))
				if e != nil {
					t.Fatal(e)
				}
				if tc.mode == "dispatched" {
					fx.Exec(t, `UPDATE agent_task_queue SET status='dispatched',dispatched_at=now() WHERE id=$1`, util.UUIDToString(task.ID))
				}
			}
			if tc.mode == "selfrun" {
				// A running record emulates a trusted request from a worker. No worker executes.
				selfTask = fx.Task(t, b, testutil.Cols{"issue_id": issueID, "runtime_id": rt, "status": "running", "started_at": testutil.Raw("now()"), "workflow_step": "in_progress", "originator_user_id": testUserID, "accountable_user_id": testUserID, "originator_source": "direct_human"})
				callerAgent = b
			}
			if tc.mode == "agent_other" {
				source := fx.Issue(t, "HANDOFF_DIAGNOSTIC synthetic source", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": a})
				selfTask = fx.Task(t, a, testutil.Cols{"issue_id": source, "runtime_id": rt, "status": "running", "started_at": testutil.Raw("now()"), "originator_user_id": testUserID, "accountable_user_id": testUserID, "originator_source": "direct_human"})
				callerAgent = a
			}
			switch tc.mode {
			case "no_runtime":
				fx.Exec(t, `UPDATE agent SET runtime_id=NULL WHERE id=$1`, b)
			case "archived":
				fx.Exec(t, `UPDATE agent SET archived_at=now() WHERE id=$1`, b)
			case "missing_agent":
				fx.Exec(t, `DELETE FROM agent WHERE id=$1`, b)
			case "unusable_runtime":
				fx.Exec(t, `UPDATE agent_runtime SET status='offline',metadata='{"offline_reason":{"code":"not_executable","detail":"synthetic broken CLI"}}' WHERE id=$1`, rt)
			case "pending_error":
				tapped.fault = "HasPendingTaskForIssueAndAgent"
			case "agent_error":
				tapped.fault = "GetAgent"
			}
			evs = nil
			tapped.counts = map[string]int{}
			previewBody := map[string]any{"issue_ids": []string{issueID}, "status": "in_review"}
			prev := call(h.PreviewIssueTrigger, "POST", "/api/issues/preview-trigger", previewBody).Want(200)
			var preview handler.IssueTriggerPreviewResponse
			prev.JSON(&preview)
			if preview.TotalCount != tc.preview {
				t.Fatalf("preview=%d want=%d body=%s", preview.TotalCount, tc.preview, prev.Body.String())
			}
			before, e := q.GetIssue(ctx, issueUUID)
			if e != nil {
				t.Fatal(e)
			}
			if before.Status != "in_progress" {
				t.Fatal("preview mutated status")
			}
			previewCounts := tapped.counts
			tapped.counts = map[string]int{}
			tapped.queryErrors = nil
			if tc.mode == "enqueue_error" || tc.mode == "notice_error" || tc.mode == "later_status" {
				tapped.fault = "CreateAgentTask"
			}
			if tc.mode == "notice_error" {
				tapped.noticeFault = true
			}
			if tc.mode == "later_status" {
				// A later write lands after this handoff's activity but before dispatch.
				// The notice must identify the original snapshot, not the newer status.
				bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
					_, err := testPool.Exec(ctx, `UPDATE issue SET status='todo',updated_at=now(),revision=revision+1 WHERE id=$1`, issueID)
					if err != nil {
						t.Error(err)
					}
				})
			}
			if tc.mode == "race" {
				bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
					current, err := db.New(testPool).GetIssue(ctx, issueUUID)
					if err != nil {
						t.Error(err)
						return
					}
					winner := service.NewTaskService(db.New(testPool), testPool, nil, bus)
					_, err = winner.EnqueueTaskForIssueWithHandoff(ctx, current, "synthetic concurrent winner", util.MustParseUUID(testUserID))
					if err != nil {
						t.Error(err)
					}
				})
			}
			update := map[string]any{"status": "in_review"}
			if tc.mode == "suppress" {
				update["suppress_run"] = true
			}
			var response *testutil.Response
			if tc.batch {
				response = call(h.BatchUpdateIssues, "PUT", "/api/issues/batch", map[string]any{"issue_ids": []string{issueID}, "updates": update})
			} else {
				r := testutil.WithURLParams(req("PUT", "/api/issues/"+issueID, update), "id", issueID)
				response = testutil.Call(t, h.UpdateIssue, r)
			}
			response.Want(tc.code)
			writeCounts := maps.Clone(tapped.counts)
			writeErrors := append([]string(nil), tapped.queryErrors...)
			tapped.fault = ""
			tapped.noticeFault = false
			after, e := q.GetIssue(ctx, issueUUID)
			if e != nil {
				t.Fatal(e)
			}
			var tasks []map[string]any
			rows, e := testPool.Query(ctx, `SELECT id::text,status,agent_id::text,context FROM agent_task_queue WHERE issue_id=$1 ORDER BY created_at`, issueID)
			if e != nil {
				t.Fatal(e)
			}
			for rows.Next() {
				var id, status, agent string
				var c []byte
				if e := rows.Scan(&id, &status, &agent, &c); e != nil {
					t.Fatal(e)
				}
				tasks = append(tasks, map[string]any{"id": id, "status": status, "agent_id": agent, "context": json.RawMessage(c)})
			}
			rows.Close()
			if len(tasks) != tc.tasks {
				t.Fatalf("tasks=%d want=%d", len(tasks), tc.tasks)
			}
			timeline := call(h.ListTimeline, "GET", "/api/issues/"+issueID+"/timeline", nil, "id", issueID).Want(200)
			runs := call(h.ListTasksByIssue, "GET", "/api/issues/"+issueID+"/tasks", nil, "id", issueID).Want(200)
			activities := listActivitiesForIssue(t, db.New(testPool), issueID)
			var hand, actor string
			for _, act := range activities {
				if act.Action == "status_changed" {
					var details map[string]string
					json.Unmarshal(act.Details, &details)
					hand = details["handoff_run"]
					actor = act.ActorType.String
				}
			}
			if hand != tc.hand {
				t.Fatalf("handoff_run=%q want %q", hand, tc.hand)
			}
			expectedStatus := "in_review"
			if tc.mode == "later_status" {
				expectedStatus = "todo"
			}
			if tc.code == 200 && (after.Status != expectedStatus || after.AssigneeID != bUUID) {
				t.Fatal("successful update did not persist handoff")
			}
			if tc.code != 200 && after.Status != "in_progress" {
				t.Fatal("rejected update persisted")
			}
			if selfTask != "" && actor != "agent" {
				t.Fatalf("actor=%s", actor)
			}
			if tc.hand != "" && selfTask == "" && actor != "member" {
				t.Fatalf("actor=%s", actor)
			}
			expectedReason := "enqueue_allowed"
			switch tc.mode {
			case "queued", "dispatched", "thread_pending":
				expectedReason = "pending_task"
			case "pending_error":
				expectedReason = "pending_check_failed"
			case "agent_error":
				expectedReason = "agent_check_failed"
			case "no_runtime":
				expectedReason = "no_runtime"
			case "suppress":
				expectedReason = "suppressed"
			}
			var statusDetails map[string]string
			for _, act := range activities {
				if act.Action == "status_changed" {
					json.Unmarshal(act.Details, &statusDetails)
				}
			}
			if tc.hand != "" && statusDetails["handoff_run_reason"] != expectedReason {
				t.Fatalf("reason=%q want %q", statusDetails["handoff_run_reason"], expectedReason)
			}
			if tc.hand != "" && !strings.Contains(timeline.Body.String(), expectedReason) {
				t.Fatal("reason missing from timeline API")
			}
			if tc.code == 200 && writeCounts["HasPendingTaskForIssueAndAgent"] > 1 {
				t.Fatal("diagnostic repeated pending query")
			}
			if previewCounts["HasPendingTaskForIssueAndAgent"] > 1 {
				t.Fatal("preview repeated pending query")
			}
			if previewCounts["GetAgent"] > 1 || previewCounts["GetRuntime"] != 0 {
				t.Fatal("preview added readiness queries")
			}
			if writeCounts["GetAgent"] > 2 || writeCounts["GetRuntime"] != 0 {
				t.Fatal("write added readiness queries")
			}
			expectedInserts := 0
			if tc.hand == "true" {
				expectedInserts = 1
			}
			if writeCounts["CreateAgentTask"] != expectedInserts {
				t.Fatalf("enqueue attempts=%d want %d", writeCounts["CreateAgentTask"], expectedInserts)
			}
			var notices int
			var eventTypes []string
			for _, e := range evs {
				eventTypes = append(eventTypes, string(e.Type))
				if e.Type == protocol.EventCommentCreated {
					notices++
					if e.ActorType != "system" || e.ActorID != "" {
						t.Fatal("notice actor is not system")
					}
					raw, _ := json.Marshal(e.Payload)
					if strings.Contains(string(raw), "mention://") || strings.Contains(string(raw), "injected diagnostic query failure") {
						t.Fatal("unsafe notice")
					}
				}
			}
			wantNotices := 0
			if tc.mode == "enqueue_error" || tc.mode == "race" || tc.mode == "later_status" || tc.mode == "head_advanced" {
				wantNotices = 1
			}
			if notices != wantNotices {
				t.Fatalf("notices=%d want %d", notices, wantNotices)
			}
			if tc.mode == "race" && !strings.Contains(timeline.Body.String(), "Another pending run occupied") {
				t.Fatal("real duplicate not explained")
			}
			if tc.mode == "enqueue_error" || tc.mode == "later_status" {
				if !strings.Contains(timeline.Body.String(), "Enqueue failed") {
					t.Fatal("failed enqueue not explained")
				}
			}
			if tc.mode == "later_status" {
				if !strings.Contains(strings.ReplaceAll(timeline.Body.String(), `\\_`, `_`), "handoff to **in_review** recorded at") {
					t.Fatal("notice attributed to later state")
				}
			}
			pos := func(kind string) int {
				for i, v := range eventTypes {
					if v == kind {
						return i
					}
				}
				return -1
			}
			if tc.code == 200 && (pos("issue:updated") < 0 || pos("activity:created") <= pos("issue:updated")) {
				t.Fatalf("event order: %v", eventTypes)
			}
			if pos("task:queued") >= 0 && pos("task:queued") <= pos("activity:created") {
				t.Fatalf("queue event before activity: %v", eventTypes)
			}
			statusActivities := 0
			for _, act := range activities {
				if act.Action == "status_changed" {
					statusActivities++
				}
			}
			if tc.code == 200 && statusActivities != 1 {
				t.Fatalf("status activities=%d", statusActivities)
			}
			if strings.Contains(timeline.Body.String(), "injected diagnostic query failure") {
				t.Fatal("injected error leaked to timeline")
			}
			if tc.mode == "race" {
				_, err := h.TaskService.EnqueueTaskForIssueWithHandoff(ctx, after, "typed sentinel verification", util.MustParseUUID(testUserID))
				if !errors.Is(err, service.ErrDuplicatePendingTask) {
					t.Fatalf("expected real duplicate sentinel, got %v", err)
				}
				t.Logf("HANDOFF_DIAGNOSTIC duplicate service sentinel = %v; not a new task", err)
			}
			record := map[string]any{"case": tc.name, "preview": json.RawMessage(prev.Body.Bytes()), "preview_query_counts": previewCounts, "write_query_counts": writeCounts, "write_query_errors": writeErrors, "http_code": response.Code, "response": json.RawMessage(response.Body.Bytes()), "timeline": json.RawMessage(timeline.Body.Bytes()), "runs": json.RawMessage(runs.Body.Bytes()), "tasks": tasks, "events": evs}
			raw, e := json.Marshal(record)
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("HANDOFF_DIAGNOSTIC_RECORD %s", raw)
		})
	}
}

func TestWorkflowHandoffRunAccessBoundary(t *testing.T) {
	ctx := context.Background()
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	rt := fx.Insert(t, "agent_runtime", testutil.Cols{"workspace_id": testWorkspaceID, "owner_id": testUserID, "name": "HANDOFF_DIAGNOSTIC private runtime", "runtime_mode": "cloud", "provider": "diagnostic_never_execute", "status": "online"})
	target := fx.Agent(t, "HANDOFF_DIAGNOSTIC private target", rt)
	outsider := fx.User(t, "HANDOFF_DIAGNOSTIC other member", "workflow-diagnostic-other@example.test")
	fx.Member(t, testWorkspaceID, outsider, "member")
	bus := events.New()
	q := db.New(testPool)
	h := handler.New(q, testPool, nil, bus, service.NewEmailService(), nil, nil, analytics.NoopClient{}, handler.Config{})
	registerActivityListeners(bus, q)
	req := func(as, method, path string, body any, params ...string) *http.Request {
		return testutil.WithURLParams(testutil.WithHeaders(testutil.JSONRequest(method, path, body), "X-User-ID", as, "X-Workspace-ID", testWorkspaceID), params...)
	}
	var wf handler.IssueWorkflowResponse
	testutil.Call(t, h.CreateIssueWorkflow, req(testUserID, "POST", "/api/issue-workflows", map[string]any{"name": "HANDOFF_DIAGNOSTIC access boundary", "steps": []map[string]any{{"status_key": "todo"}, {"status_key": "in_review"}}})).Want(201).JSON(&wf)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue_workflow WHERE id=$1`, wf.ID) })
	p := fx.Project(t, "HANDOFF_DIAGNOSTIC access project")
	testutil.Call(t, h.SetProjectWorkflow, req(testUserID, "POST", "/api/projects/"+p+"/workflow", map[string]any{"workflow_id": wf.ID}, "id", p)).Want(200)
	testutil.Call(t, h.SetIssueWorkflowHandoff, req(testUserID, "PUT", "/api/issue-workflows/"+wf.ID+"/handoffs/in_review", map[string]any{"handler": map[string]any{"type": "agent", "id": target}}, "id", wf.ID, "status", "in_review")).Want(200)
	id := fx.Issue(t, "HANDOFF_DIAGNOSTIC private gate", testutil.Cols{"status": "todo", "project_id": p})
	// Preview is advisory: revoking access before the write must keep the
	// original boundary refusal, with no diagnostic disclosure or enqueue.
	fx.Exec(t, `UPDATE agent SET permission_mode='public_to' WHERE id=$1`, target)
	fx.Insert(t, "agent_invocation_target", testutil.Cols{"agent_id": target, "target_type": "member", "target_id": outsider, "created_by": testUserID})
	var allowed handler.IssueTriggerPreviewResponse
	testutil.Call(t, h.PreviewIssueTrigger, req(outsider, "POST", "/api/issues/preview-trigger", map[string]any{"issue_ids": []string{id}, "status": "in_review"})).Want(200).JSON(&allowed)
	if allowed.TotalCount != 1 {
		t.Fatal("public preview did not allow target")
	}
	fx.Exec(t, `UPDATE agent SET permission_mode='private' WHERE id=$1`, target)
	var preview handler.IssueTriggerPreviewResponse
	pr := testutil.Call(t, h.PreviewIssueTrigger, req(outsider, "POST", "/api/issues/preview-trigger", map[string]any{"issue_ids": []string{id}, "status": "in_review"})).Want(200).JSON(&preview)
	if preview.TotalCount != 0 {
		t.Fatal("unauthorized target readiness exposed")
	}
	wr := testutil.Call(t, h.UpdateIssue, req(outsider, "PUT", "/api/issues/"+id, map[string]any{"status": "in_review"}, "id", id)).Want(403)
	batchDenied := testutil.Call(t, h.BatchUpdateIssues, req(outsider, "PUT", "/api/issues/batch", map[string]any{"issue_ids": []string{id}, "updates": map[string]any{"status": "in_review"}})).Want(200)
	var batchCount struct {
		Updated int `json:"updated"`
	}
	batchDenied.JSON(&batchCount)
	if batchCount.Updated != 0 {
		t.Fatal("batch enqueued or updated a denied target")
	}
	foreignWS := fx.Workspace(t, "Foreign diagnostic workspace", "foreign-diagnostic")
	foreignRuntime := fx.Insert(t, "agent_runtime", testutil.Cols{"workspace_id": foreignWS, "owner_id": testUserID, "name": "foreign secret runtime", "runtime_mode": "cloud", "provider": "diagnostic_never_execute", "status": "online"})
	foreignTarget := fx.Agent(t, "foreign secret target", foreignRuntime, testutil.Cols{"workspace_id": foreignWS})
	foreignDenied := testutil.Call(t, h.UpdateIssue, req(outsider, "PUT", "/api/issues/"+id, map[string]any{"status": "in_review", "assignee_type": "agent", "assignee_id": foreignTarget}, "id", id)).Want(400)
	ti := testutil.Call(t, h.ListTimeline, req(outsider, "GET", "/api/issues/"+id+"/timeline", nil, "id", id)).Want(200)
	issue, e := q.GetIssue(ctx, util.MustParseUUID(id))
	if e != nil || issue.Status != "todo" {
		t.Fatalf("rejected update persisted: %v", e)
	}
	for _, s := range []string{pr.Body.String(), wr.Body.String(), batchDenied.Body.String(), foreignDenied.Body.String(), ti.Body.String()} {
		for _, secret := range []string{target, rt, foreignTarget, foreignRuntime, "foreign secret target", "foreign secret runtime", "HANDOFF_DIAGNOSTIC private target", "HANDOFF_DIAGNOSTIC private runtime", "SQLSTATE"} {
			if strings.Contains(s, secret) {
				t.Fatalf("boundary leaked %s", secret)
			}
		}
	}
	t.Logf("HANDOFF_DIAGNOSTIC_ACCESS preview=%s write=%s timeline=%s", pr.Body.String(), wr.Body.String(), ti.Body.String())
}
