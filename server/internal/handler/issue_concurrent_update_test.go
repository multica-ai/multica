package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// A write that names only some fields must leave the others as a concurrent
// write left them. The update statement takes nullable fields as bare values,
// so a write that filled them in before waiting on the row would put the old
// assignee, dates, parent, project and stage back.
func TestConcurrentUpdateKeepsFieldsItDoesNotName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	batch := func(updates map[string]any, extra map[string]any) func(string) *httptest.ResponseRecorder {
		return func(issueID string) *httptest.ResponseRecorder {
			body := map[string]any{"issue_ids": []string{issueID}, "updates": updates}
			for k, v := range extra {
				body[k] = v
			}
			w := httptest.NewRecorder()
			testHandler.BatchUpdateIssues(w, newRequest("POST", "/api/issues/batch-update", body))
			return w
		}
	}
	label := dbfx.Insert(t, "issue_label", testutil.Cols{
		"workspace_id": testWorkspaceID, "name": "concurrent-update", "color": "#ff0000",
	})
	for name, write := range map[string]func(issueID string) *httptest.ResponseRecorder{
		"single": func(issueID string) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			testHandler.UpdateIssue(w, updateIssueRequest(issueID, map[string]any{"priority": "high"}))
			return w
		},
		"batch": batch(map[string]any{"priority": "high"}, nil),
		"batch with labels": batch(map[string]any{"priority": "high"}, map[string]any{
			"labels": map[string]any{"add": []string{label}},
		}),
		"public api content": func(issueID string) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
			if err == nil {
				title := "renamed"
				_, err = testHandler.IssueService.UpdateContent(context.Background(), issue, service.IssueContentPatch{Title: &title})
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return w
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			parent := dbfx.Issue(t, "concurrent-update-parent")
			issueID := dbfx.Issue(t, "concurrent-update")
			project := dbfx.Insert(t, "project", testutil.Cols{"workspace_id": testWorkspaceID, "title": "concurrent-update"})

			var mu sync.Mutex
			var payloads []map[string]any
			testHandler.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
				payload, ok := e.Payload.(map[string]any)
				if !ok {
					return
				}
				if resp, ok := payload["issue"].(IssueResponse); ok && resp.ID == issueID {
					mu.Lock()
					payloads = append(payloads, payload)
					mu.Unlock()
				}
			})

			// Hold the other write open so the one under test reads the old row
			// and then waits for this one.
			tx, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			var pid int
			if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `UPDATE issue SET assignee_type = 'member', assignee_id = $2,
				start_date = '2026-01-02', due_date = '2026-01-03', parent_issue_id = $3, project_id = $4, stage = 2
				WHERE id = $1`, issueID, testUserID, parent, project); err != nil {
				t.Fatal(err)
			}

			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- write(issueID) }()
			blocked := false
			for deadline := time.Now().Add(10 * time.Second); !blocked && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
				if err := testPool.QueryRow(ctx, `SELECT EXISTS (
					SELECT 1 FROM pg_stat_activity WHERE $1::int = ANY(pg_blocking_pids(pid)))`, pid).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case w := <-done:
				if w.Code != http.StatusOK {
					t.Fatalf("status %d: %s", w.Code, w.Body.String())
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if !blocked {
				t.Fatal("the update never waited on the row, so the interleaving under test did not happen")
			}

			var got string
			if err := testPool.QueryRow(ctx, `SELECT concat_ws(' ', assignee_type, assignee_id, start_date, due_date, parent_issue_id, project_id, stage)
				FROM issue WHERE id = $1`, issueID).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if want := "member " + testUserID + " 2026-01-02 2026-01-03 " + parent + " " + project + " 2"; got != want {
				t.Errorf("concurrent fields = %q, want %q", got, want)
			}

			// The other write announces its own changes.
			mu.Lock()
			defer mu.Unlock()
			for _, payload := range payloads {
				for _, key := range []string{"assignee_changed", "project_changed", "start_date_changed", "due_date_changed", "parent_changed", "stage_changed"} {
					if payload[key] == true {
						t.Errorf("%s = true for a write that did not touch it", key)
					}
				}
			}
		})
	}
}
