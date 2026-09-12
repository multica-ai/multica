package handler

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func claimStageNotificationRun(t *testing.T, runtimeID string) *AgentTaskResponse {
	t.Helper()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "stage-notification-test")
	req = withURLParam(req, "runtimeId", runtimeID)
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityCoalescedCommentsV1)
	var response struct {
		Task *AgentTaskResponse `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&response)
	return response.Task
}

func finishStageNotificationRun(t *testing.T, runtimeID, signalID string, leader bool) {
	t.Helper()
	run := claimStageNotificationRun(t, runtimeID)
	if run == nil || run.TriggerCommentID == nil || *run.TriggerCommentID != signalID ||
		!slices.Contains(run.DeliveredCommentIDs, signalID) || run.IsLeaderTask != leader ||
		(!strings.Contains(run.TriggerCommentContent, "Stage 1 of this issue is complete") &&
			!strings.Contains(run.TriggerCommentContent, "All sub-issues are complete")) {
		t.Fatal("stage notification was not delivered with the expected comment receipt, body, and leader role")
	}
	if _, err := testHandler.TaskService.StartTask(context.Background(), parseUUID(run.ID)); err != nil {
		t.Fatal(err)
	}
	if extra := claimStageNotificationRun(t, runtimeID); extra != nil {
		t.Fatal("parent runs must execute serially")
	}
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+run.ID+"/complete", map[string]any{"output": "Processed the stage notification"}, testWorkspaceID, "stage-notification-test")
	testutil.Call(t, testHandler.CompleteTask, withURLParam(req, "taskId", run.ID)).Want(http.StatusOK)
	if extra := claimStageNotificationRun(t, runtimeID); extra != nil {
		t.Fatal("stage completion created an extra follow-up run")
	}
}

type stageNotificationRequestIndex struct{}

// Pause each request after its status writes commit, before it reads siblings.
// The insert gate additionally forces both publishers to contend for one key.
type stageNotificationBarrier struct {
	db.DBTX
	parentID      pgtype.UUID
	arrived       chan int
	release       [2]chan struct{}
	insertArrived chan struct{}
	insertRelease chan struct{}
}

func (b *stageNotificationBarrier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "-- name: ListChildIssues :many") && len(args) > 0 && args[0] == b.parentID {
		index := ctx.Value(stageNotificationRequestIndex{}).(int)
		b.arrived <- index
		select {
		case <-b.release[index]:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return b.DBTX.Query(ctx, sql, args...)
}

func (b *stageNotificationBarrier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if b.insertArrived != nil && strings.Contains(sql, "-- name: CreateComment :one") && slices.ContainsFunc(args, func(arg any) bool { id, ok := arg.(pgtype.UUID); return ok && id == b.parentID }) {
		b.insertArrived <- struct{}{}
		select {
		case <-b.insertRelease:
		case <-ctx.Done():
			return errRow{err: ctx.Err()}
		}
	}
	return b.DBTX.QueryRow(ctx, sql, args...)
}

func TestChildDoneConcurrentNotification(t *testing.T) {
	for _, owner := range []string{"agent", "squad"} {
		for _, staged := range []bool{false, true} {
			for _, batch := range []bool{false, true} {
				for _, schedule := range []string{"simultaneous", "after_parent_completed"} {
					t.Run(fmt.Sprintf("%s/staged=%t/batch=%t/%s", owner, staged, batch, schedule), func(t *testing.T) {
						ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
						defer cancel()
						runtimeID := dbfx.Runtime(t, "Concurrent stage runtime")
						agentID := dbfx.Agent(t, "Stage coordinator", runtimeID, testutil.Cols{"max_concurrent_tasks": 3})
						assigneeID := agentID
						if owner == "squad" {
							assigneeID = dbfx.Squad(t, "Stage squad", agentID)
						}
						parentID := dbfx.Issue(t, "One logical stage", testutil.Cols{"status": "in_progress", "assignee_type": owner, "assignee_id": assigneeID})
						count := 2
						if batch {
							count++
						}
						var children []string
						for range count {
							cols := testutil.Cols{"status": "in_progress", "parent_issue_id": parentID}
							if staged {
								cols["stage"] = 1
							}
							children = append(children, dbfx.Issue(t, "Concurrent stage child", cols))
						}
						barrier := &stageNotificationBarrier{DBTX: testPool, parentID: parseUUID(parentID), arrived: make(chan int, 2), release: [2]chan struct{}{make(chan struct{}), make(chan struct{})}}
						if schedule == "simultaneous" {
							barrier.insertArrived = make(chan struct{}, 2)
							barrier.insertRelease = make(chan struct{})
						}
						h := *testHandler
						h.Queries = db.New(barrier)
						h.Bus = events.New()
						var published atomic.Int32
						h.Bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) { published.Add(1) })
						requests := [2]*http.Request{
							withURLParam(newRequest(http.MethodPut, "/api/issues/"+children[0], map[string]any{"status": "done"}), "id", children[0]),
							withURLParam(newRequest(http.MethodPut, "/api/issues/"+children[1], map[string]any{"status": "done"}), "id", children[1]),
						}
						if batch {
							requests[0] = newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{"issue_ids": []string{children[0], children[2]}, "updates": map[string]any{"status": "done"}})
						}
						responses := [2]chan *testutil.Response{make(chan *testutil.Response, 1), make(chan *testutil.Response, 1)}
						done := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
						// Cancel and join before fixture cleanup even if a barrier assertion fails.
						defer func() {
							cancel()
							for _, ch := range done {
								<-ch
							}
						}()
						for i, req := range requests {
							req = req.WithContext(context.WithValue(ctx, stageNotificationRequestIndex{}, i))
							if !batch || i != 0 {
								req = withURLParam(req, "id", children[i])
							}
							go func() {
								defer close(done[i])
								if batch && i == 0 {
									responses[i] <- testutil.Call(t, h.BatchUpdateIssues, req)
								} else {
									responses[i] <- testutil.Call(t, h.UpdateIssue, req)
								}
							}()
						}
						for range requests {
							select {
							case <-barrier.arrived:
							case <-ctx.Done():
								t.Fatal("both requests must reach the post-commit sibling read")
							}
						}
						if n := dbfx.Count(t, `SELECT count(*) FROM issue WHERE parent_issue_id=$1 AND status='done'`, parentID); n != count {
							t.Fatalf("committed children=%d, want %d", n, count)
						}
						waitResponse := func(i int) {
							t.Helper()
							select {
							case r := <-responses[i]:
								r.Want(http.StatusOK)
							case <-ctx.Done():
								t.Fatal("released request did not finish")
							}
						}
						var signalID string
						wantRevision := int64(2)
						if schedule == "simultaneous" {
							for _, ch := range barrier.release {
								close(ch)
							}
							for range requests {
								select {
								case <-barrier.insertArrived:
								case <-ctx.Done():
									t.Fatal("both publishers must reach the comment insert")
								}
							}
							close(barrier.insertRelease)
							waitResponse(0)
							waitResponse(1)
						} else {
							close(barrier.release[0])
							waitResponse(0)
							dbfx.QueryRow(t, `SELECT id FROM comment WHERE issue_id=$1 AND type='system'`, parentID).Scan(&signalID)
							finishStageNotificationRun(t, runtimeID, signalID, owner == "squad")
							dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id=$1`, parentID).Scan(&wantRevision)
							close(barrier.release[1])
							waitResponse(1)
						}
						if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id=$1 AND type='system'`, parentID); n != 1 {
							t.Fatalf("one stage closure must publish exactly one notification, got %d", n)
						}
						if published.Load() != 1 {
							t.Fatalf("comment broadcasts=%d, want 1", published.Load())
						}
						if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, parentID); n != 1 {
							t.Fatalf("parent runs=%d, want 1", n)
						}
						// Losing the insert must also roll back CreateComment's issue touch.
						var revision int64
						dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id=$1`, parentID).Scan(&revision)
						if revision != wantRevision {
							t.Fatalf("duplicate publisher changed parent revision: %d", revision)
						}
						if schedule == "simultaneous" {
							dbfx.QueryRow(t, `SELECT id FROM comment WHERE issue_id=$1 AND type='system'`, parentID).Scan(&signalID)
							finishStageNotificationRun(t, runtimeID, signalID, owner == "squad")
						} else if extra := claimStageNotificationRun(t, runtimeID); extra != nil {
							t.Fatal("late callback must not wake an already-completed parent again")
						}
					})
				}
			}
		}
	}
}

func TestChildDoneNotificationLifecycle(t *testing.T) {
	for _, scenario := range []string{"ordinary_edits", "terminal_edit", "later_stage", "unstaged_sibling", "reopen", "custom_reopen", "new_child", "stage_roundtrip", "parent_roundtrip", "status_query_reopen"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := dbfx.Runtime(t, "Stage lifecycle runtime")
			agentID := dbfx.Agent(t, "Stage lifecycle coordinator", runtimeID)
			parentID := dbfx.Issue(t, "Stage lifecycle parent", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID})
			childID := dbfx.Issue(t, "Stage lifecycle child", testutil.Cols{"status": "in_progress", "parent_issue_id": parentID, "stage": 1})
			update := func(id string, fields map[string]any) {
				t.Helper()
				testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/api/issues/"+id, fields), "id", id)).Want(http.StatusOK)
			}
			load := func(id string) db.Issue {
				t.Helper()
				issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(id))
				if err != nil {
					t.Fatal(err)
				}
				return issue
			}
			signalIDs := func() []string {
				t.Helper()
				var ids []string
				dbfx.QueryRow(t, `SELECT COALESCE(array_agg(id::text ORDER BY created_at,id),'{}') FROM comment WHERE issue_id=$1 AND type='system'`, parentID).Scan(&ids)
				return ids
			}
			doneStatus := "done"
			openStatus := "in_progress"
			if scenario == "custom_reopen" {
				suffix := strings.ReplaceAll(childID, "-", "")[:8]
				doneStatus = "approved_" + suffix
				openStatus = "revising_" + suffix
				for key, category := range map[string]string{doneStatus: "done", openStatus: "in_progress"} {
					dbfx.Insert(t, "issue_status", testutil.Cols{"workspace_id": testWorkspaceID, "key": key, "name": key, "category": category, "color": "#123456"})
				}
			}
			before := load(childID)
			update(childID, map[string]any{"status": doneStatus})
			completed := load(childID)
			first := signalIDs()
			if len(first) != 1 {
				t.Fatalf("initial notifications=%v", first)
			}
			finishStageNotificationRun(t, runtimeID, first[0], false)
			want := 1
			switch scenario {
			case "ordinary_edits":
				update(childID, map[string]any{"title": "Edited result title", "priority": "high"})
				testutil.Call(t, testHandler.CreateComment, withURLParam(newRequest(http.MethodPost, "/api/issues/"+childID+"/comments", map[string]any{"content": "Additional result detail"}), "id", childID)).Want(http.StatusCreated)
				update(childID, map[string]any{"status": doneStatus})
			case "terminal_edit":
				update(childID, map[string]any{"status": "cancelled"})
				update(childID, map[string]any{"status": doneStatus})
			case "later_stage":
				next := dbfx.Issue(t, "Later work", testutil.Cols{"status": "backlog", "parent_issue_id": parentID, "stage": 2})
				update(next, map[string]any{"title": "Later stage was edited"})
			case "unstaged_sibling":
				dbfx.Issue(t, "Unstaged work", testutil.Cols{"status": "in_progress", "parent_issue_id": parentID})
			case "new_child":
				next := dbfx.Issue(t, "Added stage member", testutil.Cols{"status": "in_progress", "parent_issue_id": parentID, "stage": 1})
				testHandler.notifyParentOfChildDone(ctx, before, completed)
				if ids := signalIDs(); len(ids) != 1 {
					t.Fatal("new unfinished child must hold the stage open")
				}
				update(next, map[string]any{"status": doneStatus})
				want = 2
			default:
				switch scenario {
				case "stage_roundtrip":
					update(childID, map[string]any{"stage": 2})
					update(childID, map[string]any{"stage": 1})
				case "parent_roundtrip":
					other := dbfx.Issue(t, "Another parent", testutil.Cols{"status": "in_progress"})
					update(childID, map[string]any{"parent_issue_id": other})
					update(childID, map[string]any{"parent_issue_id": parentID})
				}
				if scenario == "status_query_reopen" {
					// Agent completion and VCS writers use this query directly.
					if _, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: parseUUID(childID), WorkspaceID: parseUUID(testWorkspaceID), Status: openStatus}); err != nil {
						t.Fatal(err)
					}
				} else {
					update(childID, map[string]any{"status": openStatus})
				}
				testHandler.notifyParentOfChildDone(ctx, before, completed)
				if ids := signalIDs(); len(ids) != 1 {
					t.Fatal("stale callback must not close a reopened or moved child")
				}
				update(childID, map[string]any{"status": doneStatus})
				want = 2
			}
			// Re-run both notification entrypoints with an old accepted transition.
			// Current state can differ in content, lifecycle, or stage layout.
			testHandler.notifyParentOfChildDone(ctx, before, completed)
			testHandler.notifyParentsOfBatchChildDone(ctx, []db.Issue{completed})
			ids := signalIDs()
			if len(ids) != want {
				t.Fatalf("notifications=%v, want %d", ids, want)
			}
			if want == 2 {
				if ids[0] == ids[1] {
					t.Fatal("a new closure must have a new event identity")
				}
				nextID := ids[0]
				if nextID == first[0] {
					nextID = ids[1]
				}
				finishStageNotificationRun(t, runtimeID, nextID, false)
			} else if extra := claimStageNotificationRun(t, runtimeID); extra != nil {
				t.Fatal("an edit duplicated the old parent wake")
			}
			current := load(childID)
			if scenario == "ordinary_edits" && current.Revision <= completed.Revision {
				t.Fatal("control edits must really advance the ordinary issue revision")
			}
			if want == 1 && current.StageGeneration != completed.StageGeneration {
				t.Fatal("ordinary/terminal edits must not advance the stage generation")
			}
		})
	}
}
