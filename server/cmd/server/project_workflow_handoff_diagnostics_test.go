package main

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestWorkflowHandoffRunProjectEntry(t *testing.T) {
	for _, same := range []bool{false, true} {
		for _, mode := range []string{"pending", "idle", "suppress", "no_runtime", "pending_error", "agent_error", "enqueue_error", "notice_error", "no_entry", "batch_no_entry", "no_handoff"} {
			name := mode
			if same {
				name += "_same_handler"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				fx := testutil.New(testPool, testWorkspaceID, testUserID)
				rt := fx.Insert(t, "agent_runtime", testutil.Cols{"workspace_id": testWorkspaceID, "owner_id": testUserID, "name": "synthetic project entry runtime", "runtime_mode": "cloud", "provider": "never_execute", "status": "online"})
				a, b := fx.Agent(t, "synthetic A", rt), fx.Agent(t, "synthetic B", rt)
				if same {
					a = b
				}
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
				call := func(fn http.HandlerFunc, method, path string, body any, params ...string) *testutil.Response {
					r := testutil.WithURLParams(testutil.WithHeaders(testutil.JSONRequest(method, path, body), "X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID), params...)
					return testutil.Call(t, fn, r)
				}
				var wf handler.IssueWorkflowResponse
				call(h.CreateIssueWorkflow, "POST", "/api/issue-workflows", map[string]any{"name": "synthetic project entry", "steps": []map[string]any{{"status_key": "todo"}, {"status_key": "in_progress"}}}).Want(201).JSON(&wf)
				t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue_workflow WHERE id=$1`, wf.ID) })
				p := fx.Project(t, "synthetic destination")
				call(h.SetProjectWorkflow, "POST", "/api/projects/"+p+"/workflow", map[string]any{"workflow_id": wf.ID}, "id", p).Want(200)
				if mode != "no_handoff" {
					call(h.SetIssueWorkflowHandoff, "PUT", "/api/issue-workflows/"+wf.ID+"/handoffs/in_progress", map[string]any{"handler": map[string]any{"type": "agent", "id": b}}, "id", wf.ID, "status", "in_progress").Want(200)
				}
				id := fx.Issue(t, "synthetic same-status project entry", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": a})
				t.Cleanup(func() {
					cleanupActivities(t, id)
					testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id=$1`, id)
				})
				wantReason, wantRun, wantTasks, wantInsert := "enqueue_allowed", "true", 1, 1
				switch mode {
				case "pending":
					fx.Task(t, b, testutil.Cols{"issue_id": id, "runtime_id": rt})
					wantReason, wantRun, wantInsert = "pending_task", "false", 0
				case "suppress":
					wantReason, wantRun, wantTasks, wantInsert = "suppressed", "false", 0, 0
				case "no_runtime":
					fx.Exec(t, `UPDATE agent SET runtime_id=NULL WHERE id=$1`, b)
					wantReason, wantRun, wantTasks, wantInsert = "no_runtime", "false", 0, 0
				case "pending_error":
					tapped.fault = "HasPendingTaskForIssueAndAgent"
					wantReason, wantRun, wantTasks, wantInsert = "pending_check_failed", "false", 0, 0
				case "agent_error":
					tapped.fault = "GetAgent"
					wantReason, wantRun, wantTasks, wantInsert = "agent_check_failed", "false", 0, 0
				case "enqueue_error", "notice_error":
					tapped.fault = "CreateAgentTask"
					tapped.noticeFault = mode == "notice_error"
					wantTasks = 0
				case "no_entry", "batch_no_entry", "no_handoff":
					wantReason, wantTasks, wantInsert = "", 0, 0
				}
				evs = nil
				tapped.counts = map[string]int{}
				body := map[string]any{"project_id": p, "enter_workflow_step": mode != "no_entry"}
				if mode == "suppress" {
					body["suppress_run"] = true
				}
				var response *testutil.Response
				if mode == "batch_no_entry" {
					// Batch currently accepts project moves but does not opt into step entry.
					response = call(h.BatchUpdateIssues, "PUT", "/api/issues/batch", map[string]any{"issue_ids": []string{id}, "updates": body}).Want(200)
				} else {
					response = call(h.UpdateIssue, "PUT", "/api/issues/"+id, body, "id", id).Want(200)
				}
				writeCounts := maps.Clone(tapped.counts)
				tapped.fault, tapped.noticeFault = "", false
				after, err := q.GetIssue(ctx, util.MustParseUUID(id))
				if err != nil || after.Status != "in_progress" || util.UUIDToString(after.ProjectID) != p {
					t.Fatalf("project move failed: %+v %v", after, err)
				}
				owner := b
				if wantReason == "" {
					owner = a
				}
				if util.UUIDToString(after.AssigneeID) != owner {
					t.Fatal("unexpected owner")
				}
				acts := listActivitiesForIssue(t, db.New(testPool), id)
				wantActivities := 1
				if wantReason == "" {
					wantActivities = 0
				}
				if len(acts) != wantActivities {
					t.Fatalf("activities=%d want=%d", len(acts), wantActivities)
				}
				if wantReason != "" {
					var details map[string]string
					json.Unmarshal(acts[0].Details, &details)
					if acts[0].Action != "workflow_handoff" || acts[0].ActorType.String != "member" || util.UUIDToString(acts[0].ActorID) != testUserID || details["handoff_run_reason"] != wantReason || details["handoff_run"] != wantRun || details["step"] != "in_progress" || details["from"] != "" || details["to"] != "" {
						t.Fatalf("incorrect persisted handoff: %+v %v", acts[0], details)
					}
				}
				var n int
				if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, id).Scan(&n); err != nil || n != wantTasks {
					t.Fatalf("tasks=%d want=%d err=%v", n, wantTasks, err)
				}
				if writeCounts["CreateAgentTask"] != wantInsert || writeCounts["HasPendingTaskForIssueAndAgent"] > 1 || writeCounts["GetRuntime"] != 0 {
					t.Fatalf("extra dispatch/readiness work: %v", writeCounts)
				}
				// A new handler and event bus read persisted data, never the transient event.
				reader := handler.New(db.New(testPool), testPool, nil, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, handler.Config{})
				timeline := call(reader.ListTimeline, "GET", "/api/issues/"+id+"/timeline", nil, "id", id).Want(200)
				if wantReason != "" && !strings.Contains(timeline.Body.String(), `"handoff_run_reason":"`+wantReason+`"`) {
					t.Fatal("reloaded timeline lost the decision")
				}
				if strings.Contains(timeline.Body.String(), "secret-token") || strings.Contains(timeline.Body.String(), "mention://") {
					t.Fatal("diagnostic leaked private error or executable mention")
				}
				notices := 0
				var order []string
				for _, e := range evs {
					order = append(order, string(e.Type))
					if e.Type == protocol.EventCommentCreated {
						notices++
						if e.ActorType != "system" || e.ActorID != "" {
							t.Fatal("incorrect notice actor")
						}
					}
				}
				wantNotices := 0
				if mode == "enqueue_error" {
					wantNotices = 1
				}
				if notices != wantNotices {
					t.Fatalf("notices=%d want=%d", notices, wantNotices)
				}
				if wantReason != "" && (len(order) < 2 || order[0] != "issue:updated" || order[1] != "activity:created") {
					t.Fatalf("event order=%v", order)
				}
				if mode == "idle" && (len(order) != 3 || order[2] != "task:queued") {
					t.Fatalf("enqueue order=%v", order)
				}
				var entries any
				json.Unmarshal(timeline.Body.Bytes(), &entries)
				raw, _ := json.Marshal(map[string]any{"case": name, "timeline": entries, "response": json.RawMessage(response.Body.Bytes()), "events": evs, "write_counts": writeCounts})
				t.Logf("PROJECT_ENTRY_EVIDENCE %s", raw)
			})
		}
	}
}

func TestActivitySameStatusNonAgentHandoffKeepsAssignmentHistory(t *testing.T) {
	for _, targetType := range []string{"member", "squad"} {
		for _, changed := range []bool{false, true} {
			t.Run(targetType+"_changed_"+strconv.FormatBool(changed), func(t *testing.T) {
				id := createTestIssue(t, testWorkspaceID, testUserID)
				t.Cleanup(func() { cleanupActivities(t, id); cleanupTestIssue(t, id) })
				bus := events.New()
				q := db.New(testPool)
				registerActivityListeners(bus, q)
				targetID := testUserID
				bus.Publish(events.Event{
					Type: protocol.EventIssueUpdated, WorkspaceID: testWorkspaceID, ActorType: "member", ActorID: testUserID,
					Payload: map[string]any{
						"issue":          handler.IssueResponse{ID: id, WorkspaceID: testWorkspaceID, Status: "in_progress", AssigneeType: &targetType, AssigneeID: &targetID},
						"status_changed": false, "assignee_changed": changed,
						"workflow_handoff": map[string]string{"to_type": targetType, "to_id": targetID, "run": "false"},
					},
				})
				acts := listActivitiesForIssue(t, q, id)
				if !changed && len(acts) != 0 {
					t.Fatal("invented activity for unchanged non-agent handler")
				}
				if changed && (len(acts) != 1 || acts[0].Action != "assignee_changed" || strings.Contains(string(acts[0].Details), "handoff_")) {
					t.Fatalf("changed non-agent history semantics: %+v", acts)
				}
			})
		}
	}
}
