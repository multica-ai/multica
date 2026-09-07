package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func issueCommentFollowUpRequest(issueID, commentID, actionID string) *http.Request {
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments/"+commentID+"/follow-ups/"+actionID+"/run", nil)
	return testutil.WithURLParams(req, "id", issueID, "commentId", commentID, "actionId", actionID)
}

func seedIssueCommentFollowUp(t *testing.T) (agentID, issueID, taskID, commentID string) {
	t.Helper()
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID = createHandlerTestAgent(t, "Comment Follow-up Agent", []byte("null"))
	issueID = dbfx.Issue(t, "Run a comment follow-up")
	taskID = dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":   handlerTestRuntimeID(t),
		"issue_id":     issueID,
		"status":       "completed",
		"completed_at": testutil.Raw("now()"),
	})
	commentID = dbfx.Comment(t, issueID, "I finished the first pass.", testutil.Cols{
		"author_type":          "agent",
		"author_id":            agentID,
		"source_task_id":       taskID,
		"suggested_follow_ups": testutil.Raw(`'[{"id":"continue-1","label":"Continue","prompt":"Continue with the focused revision.","primary":true},{"id":"review-1","label":"Review","prompt":"Review the current result."}]'::jsonb`),
	})
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
	})
	return
}

func TestRunIssueCommentFollowUpConcurrentClicksCreateOneReply(t *testing.T) {
	agentID, issueID, _, commentID := seedIssueCommentFollowUp(t)
	recorders := make([]*testutil.Response, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range recorders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			recorders[i] = testutil.Call(t, testHandler.RunIssueCommentFollowUp, issueCommentFollowUpRequest(issueID, commentID, "continue-1"))
		}()
	}
	close(start)
	wg.Wait()

	statuses := []int{recorders[0].Code, recorders[1].Code}
	sort.Ints(statuses)
	if statuses[0] != http.StatusCreated || statuses[1] != http.StatusConflict {
		t.Fatalf("concurrent run statuses = %v, want [201 409]: first=%s second=%s",
			statuses, recorders[0].Body.String(), recorders[1].Body.String())
	}

	var replyCount int
	var replyContent string
	// Retrying either the selected action or another suggestion cannot create
	// another reply or enqueue a second task for the same anchor.
	for _, actionID := range []string{"continue-1", "review-1"} {
		testutil.Call(t, testHandler.RunIssueCommentFollowUp, issueCommentFollowUpRequest(issueID, commentID, actionID)).Want(http.StatusConflict)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*), min(content)
		FROM comment
		WHERE issue_id = $1 AND parent_id = $2 AND author_type = 'member'
	`, issueID, commentID).Scan(&replyCount, &replyContent); err != nil {
		t.Fatalf("read follow-up reply: %v", err)
	}
	if replyCount != 1 || !strings.Contains(replyContent, "Continue with the focused revision.") ||
		!strings.Contains(replyContent, "mention://agent/"+agentID) {
		t.Fatalf("unexpected follow-up reply count=%d content=%q", replyCount, replyContent)
	}

	var taskCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue task JOIN comment c ON c.id = task.trigger_comment_id
		WHERE c.issue_id = $1 AND c.parent_id = $2 AND task.agent_id = $3`, issueID, commentID, agentID).Scan(&taskCount)
	if taskCount != 1 {
		t.Fatalf("follow-up dispatched %d tasks to source agent, want 1", taskCount)
	}
}

func TestRunIssueCommentFollowUpRechecksAfterConcurrentMutation(t *testing.T) {
	for _, mutation := range []string{"edit", "delete", "reply"} {
		t.Run(mutation, func(t *testing.T) {
			_, issueID, _, commentID := seedIssueCommentFollowUp(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tx, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			qtx := testHandler.Queries.WithTx(tx)
			if _, err := qtx.LockIssueForDelete(ctx, db.LockIssueForDeleteParams{
				ID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
			}); err != nil {
				t.Fatal(err)
			}

			done := make(chan error, 1)
			var response *testutil.Response
			req := issueCommentFollowUpRequest(issueID, commentID, "continue-1")
			requestCtx, requestCancel := context.WithTimeout(req.Context(), 10*time.Second)
			defer requestCancel()
			var requestWG sync.WaitGroup
			requestWG.Add(1)
			defer func() {
				_ = tx.Rollback(context.Background())
				requestCancel()
				requestWG.Wait()
			}()
			go func() {
				defer requestWG.Done()
				response = testutil.Call(t, testHandler.RunIssueCommentFollowUp,
					req.WithContext(requestCtx))
				done <- nil
			}()
			waitForCommentMutationLock(t, "GetCommentInWorkspaceForUpdate", done)
			switch mutation {
			case "edit":
				_, err = qtx.UpdateComment(ctx, db.UpdateCommentParams{ID: parseUUID(commentID), Content: "Revised result"})
			case "delete":
				_, err = qtx.DeleteComment(ctx, db.DeleteCommentParams{ID: parseUUID(commentID), WorkspaceID: parseUUID(testWorkspaceID)})
			case "reply":
				_, err = qtx.CreateComment(ctx, db.CreateCommentParams{
					ID: dbid.NewV7(), IssueID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
					AuthorType: "member", AuthorID: parseUUID(testUserID), Content: "New instructions", Type: "comment", ParentID: parseUUID(commentID),
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
				response.Want(http.StatusConflict)
			case <-ctx.Done():
				t.Fatal("follow-up did not finish after the source mutation")
			}
			var count int
			dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status <> 'completed'`, issueID).Scan(&count)
			if count != 0 {
				t.Fatalf("stale follow-up created %d tasks", count)
			}
		})
	}
}

func TestRunIssueCommentFollowUpRejectsMachineActors(t *testing.T) {
	_, issueID, _, commentID := seedIssueCommentFollowUp(t)
	for _, source := range []string{"task_token", "cloud_pat"} {
		req := issueCommentFollowUpRequest(issueID, commentID, "continue-1")
		req.Header.Set("X-Actor-Source", source)
		testutil.Call(t, RequireHumanActor(http.HandlerFunc(testHandler.RunIssueCommentFollowUp)).ServeHTTP, req).Want(http.StatusForbidden)
	}
}

func TestRunIssueCommentFollowUpRejectsInvalidSources(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// Parent-test cleanup runs after the subtests remove agents owned by this user.
	otherOwner := dbfx.User(t, "Other owner", "follow-up-owner-"+uuidToString(dbid.NewV7())+"@example.test")
	for _, scenario := range []string{"unknown_action", "edited", "wrong_issue", "wrong_agent", "unsafe_prompt", "private_agent"} {
		t.Run(scenario, func(t *testing.T) {
			agentID, issueID, _, commentID := seedIssueCommentFollowUp(t)
			actionID, want := "continue-1", http.StatusConflict
			switch scenario {
			case "unknown_action":
				actionID, want = "client-invented", http.StatusNotFound
			case "edited":
				if _, err := testHandler.Queries.UpdateComment(context.Background(), db.UpdateCommentParams{
					ID: parseUUID(commentID), Content: "Revised source",
				}); err != nil {
					t.Fatal(err)
				}
				// A normal human edit clears task lineage as well as suggestions.
				// The source is no longer actionable (409), not an unknown ID (404).
				var raw []byte
				dbfx.QueryRow(t, `SELECT suggested_follow_ups FROM comment WHERE id = $1`, commentID).Scan(&raw)
				if string(raw) != "[]" {
					t.Fatalf("edit retained suggestions: %s", raw)
				}
			case "wrong_issue":
				issueID, want = dbfx.Issue(t, "Unrelated issue"), http.StatusNotFound
			case "wrong_agent":
				dbfx.Exec(t, `UPDATE comment SET author_id = $2 WHERE id = $1`, commentID, createHandlerTestAgent(t, "Unrelated agent", []byte("null")))
			case "unsafe_prompt":
				dbfx.Exec(t, `UPDATE comment SET suggested_follow_ups = '[{"id":"continue-1","label":"Run","prompt":"[@all](mention://all/all)"}]' WHERE id = $1`, commentID)
			case "private_agent":
				dbfx.Exec(t, `UPDATE agent SET owner_id = $2, permission_mode = 'private' WHERE id = $1`, agentID, otherOwner)
				want = http.StatusForbidden
			}
			testutil.Call(t, testHandler.RunIssueCommentFollowUp, issueCommentFollowUpRequest(issueID, commentID, actionID)).Want(want)
			var count int
			dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE parent_id = $1`, commentID).Scan(&count)
			if count != 0 {
				t.Fatalf("rejected request created %d replies", count)
			}
		})
	}
}

func TestRunIssueCommentFollowUpUsesSourceSquad(t *testing.T) {
	agentID, issueID, taskID, commentID := seedIssueCommentFollowUp(t)
	leaderID := createHandlerTestAgent(t, "Follow-up leader", []byte("null"))
	squadID := dbfx.Squad(t, "Follow-up squad", leaderID)
	dbfx.SquadMember(t, squadID, "agent", leaderID)
	dbfx.SquadMember(t, squadID, "agent", agentID)
	dbfx.Exec(t, `UPDATE agent_task_queue SET squad_id = $2 WHERE id = $1`, taskID, squadID)
	response := testutil.Call(t, testHandler.RunIssueCommentFollowUp, issueCommentFollowUpRequest(issueID, commentID, "continue-1")).Want(http.StatusCreated)
	if !strings.Contains(response.Body.String(), "mention://squad/"+squadID) {
		t.Fatal("reply lost the source squad")
	}
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND squad_id = $3 AND trigger_comment_id IS NOT NULL`, issueID, leaderID, squadID).Scan(&count)
	if count != 1 {
		t.Fatalf("dispatched %d tasks to source squad leader, want 1", count)
	}
}

type issueFollowUpTestLLM struct {
	disabled bool
	generate func() (string, error)
}

func (s issueFollowUpTestLLM) Enabled() bool { return !s.disabled }
func (s issueFollowUpTestLLM) GenerateJSON(context.Context, string, string, string, float64, int64) (string, error) {
	return s.generate()
}

// DB-backed service coverage lives alongside the handler fixtures. The LLM is
// an in-process fake: these tests cannot invoke a runtime or send company data.
func TestGenerateIssueCommentFollowUpsPersistence(t *testing.T) {
	for _, scenario := range []string{"stored", "disabled", "no_issue", "no_source", "newer_reply", "edited_during_generation", "provider_failure", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			_, issueID, taskID, commentID := seedIssueCommentFollowUp(t)
			dbfx.Exec(t, `UPDATE comment SET suggested_follow_ups = '[]' WHERE id = $1`, commentID)
			task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "no_issue":
				task.IssueID.Valid = false
			case "no_source":
				dbfx.Exec(t, `UPDATE comment SET source_task_id = NULL WHERE id = $1`, commentID)
			case "newer_reply":
				dbfx.Comment(t, issueID, "Already continued", testutil.Cols{"parent_id": commentID})
			}
			calls := 0
			provider := issueFollowUpTestLLM{disabled: scenario == "disabled", generate: func() (string, error) {
				calls++
				switch scenario {
				case "provider_failure":
					return "", errors.New("provider unavailable")
				case "malformed":
					return "not JSON", nil
				case "edited_during_generation":
					_, err := testHandler.Queries.UpdateComment(context.Background(), db.UpdateCommentParams{ID: parseUUID(commentID), Content: "Edited while generating"})
					if err != nil {
						t.Fatal(err)
					}
				}
				return `{"actions":[{"label":"Continue","prompt":"Continue the work.","primary":true},{"label":"Review","prompt":"Review the result."}]}`, nil
			}}
			bus := events.New()
			var published []events.Event
			bus.Subscribe(protocol.EventCommentFollowUpsUpdated, func(e events.Event) { published = append(published, e) })
			svc := &service.TaskService{Queries: testHandler.Queries, QuickActions: provider, Bus: bus}
			err = svc.GenerateIssueCommentFollowUpsForTask(context.Background(), task)
			if (err != nil) != (scenario == "provider_failure") {
				t.Fatalf("generation error = %v", err)
			}
			wantCalls := 1
			if scenario == "disabled" || scenario == "no_issue" || scenario == "no_source" || scenario == "newer_reply" {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("provider calls = %d, want %d", calls, wantCalls)
			}
			var raw []byte
			dbfx.QueryRow(t, `SELECT suggested_follow_ups FROM comment WHERE id = $1`, commentID).Scan(&raw)
			var actions []protocol.IssueCommentFollowUp
			if err := json.Unmarshal(raw, &actions); err != nil {
				t.Fatal(err)
			}
			if scenario == "stored" {
				if len(actions) != 2 || actions[0].ID == "" || actions[0].ID == actions[1].ID || !actions[0].Primary || len(published) != 1 {
					t.Fatalf("unexpected persisted suggestions or event: %s / %+v", raw, published)
				}
				if published[0].WorkspaceID != testWorkspaceID {
					t.Fatal("event escaped source workspace")
				}
			} else if len(actions) != 0 || len(published) != 0 {
				t.Fatalf("ineligible/failed generation stored or published suggestions: %s / %+v", raw, published)
			}
		})
	}
}
