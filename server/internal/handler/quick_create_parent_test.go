package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// TestQuickCreateIssueParentTrustBoundary locks the server-side trust boundary
// for the optional parent_issue_id field on POST /api/issues/quick-create.
//
// The frontend seeds parent_issue_id from the "Add sub issue" entry point and
// otherwise leaves it empty. The handler is the trust boundary: a forged
// request must not be able to smuggle a foreign parent UUID through to the
// quick-create task context, and the same-workspace happy path must thread
// the resolved UUID into QuickCreateContext.ParentIssueID so the daemon claim
// step can resolve the identifier and emit `--parent <uuid>` in the prompt.
//
// Three branches are covered:
//
//  1. Same-workspace parent → 202 Accepted, task enqueued with
//     QuickCreateContext.ParentIssueID populated.
//  2. Foreign-workspace parent → 400 Bad Request, no task enqueued.
//  3. Bogus UUID parent → 400 Bad Request, no task enqueued.
func TestQuickCreateIssueParentTrustBoundary(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Resolve the agent this request targets, then bump the CLI version on the
	// runtime BOUND TO that agent — that is the runtime the version gate checks
	// (the handler uses agent.RuntimeID). Picking an arbitrary `LIMIT 1`
	// agent_runtime is wrong when the shared test workspace holds more than one
	// runtime (other handler tests register their own): the LIMIT 1 row need
	// not be the agent's runtime, so the agent's real runtime stays on the
	// seed's empty '{}' metadata and trips the daemon-version gate before we
	// ever reach the parent_issue_id check.
	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id FROM agent WHERE id = $1`,
		agentID,
	).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch agent runtime: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_runtime SET metadata = jsonb_build_object('cli_version', $1::text) WHERE id = $2`,
		agent.MinQuickCreateCLIVersion, runtimeID,
	); err != nil {
		t.Fatalf("bump runtime cli_version: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`UPDATE agent_runtime SET metadata = '{}'::jsonb WHERE id = $1`, runtimeID)
	})

	// Same-workspace parent — must be accepted and threaded through.
	var localParentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'quick-create parent (local)', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&localParentID); err != nil {
		t.Fatalf("create local parent issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, localParentID)
	})

	// Foreign-workspace parent — must be rejected.
	var foreignWorkspaceID, foreignUserID, foreignParentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "QuickCreate Foreign", "quickcreate-foreign@multica.ai").Scan(&foreignUserID); err != nil {
		t.Fatalf("create foreign user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, foreignUserID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, "QuickCreate Foreign WS", "quickcreate-foreign-ws", "", "QCF").Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'quick-create parent (foreign)', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, foreignWorkspaceID, foreignUserID).Scan(&foreignParentID); err != nil {
		t.Fatalf("create foreign parent issue: %v", err)
	}
	// The foreign workspace cleanup above cascades, but the issue row also
	// needs a direct cleanup in case workspace deletion ordering changes.
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, foreignParentID)
	})

	// Helper for the "must not enqueue" assertions. Each rejection subtest
	// snapshots the count immediately before the request and re-checks after
	// so sibling subtests (and their t.Cleanup deletions) can't false-positive
	// or false-negative this assertion.
	countQuickCreateTasks := func(t *testing.T) int {
		t.Helper()
		var count int
		if err := testPool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM agent_task_queue WHERE agent_id = $1 AND context->>'type' = 'quick_create'`,
			agentID,
		).Scan(&count); err != nil {
			t.Fatalf("count quick-create tasks: %v", err)
		}
		return count
	}

	t.Run("same workspace parent enqueues with context", func(t *testing.T) {
		attachmentID := "019ec09d-6222-722b-bdfa-427b105d80be"
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/quick-create", map[string]any{
			"agent_id":        agentID,
			"prompt":          "Create a follow-up issue for the local parent",
			"parent_issue_id": localParentID,
			"attachment_ids":  []string{attachmentID},
		})
		testHandler.QuickCreateIssue(w, req)
		if w.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
		}
		var resp QuickCreateIssueResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, resp.TaskID)
		})

		// QuickCreateContext.ParentIssueID must contain the resolved UUID —
		// the daemon claim step reads this field to attach the parent
		// identifier and to inject `--parent <uuid>` into the prompt.
		var contextJSON []byte
		if err := testPool.QueryRow(context.Background(),
			`SELECT context FROM agent_task_queue WHERE id = $1`, resp.TaskID,
		).Scan(&contextJSON); err != nil {
			t.Fatalf("load task context: %v", err)
		}
		var qc service.QuickCreateContext
		if err := json.Unmarshal(contextJSON, &qc); err != nil {
			t.Fatalf("unmarshal context: %v", err)
		}
		if qc.Type != service.QuickCreateContextType {
			t.Fatalf("expected type=%q, got %q", service.QuickCreateContextType, qc.Type)
		}
		if qc.ParentIssueID != localParentID {
			t.Fatalf("expected parent_issue_id=%q in context, got %q", localParentID, qc.ParentIssueID)
		}
		if len(qc.AttachmentIDs) != 1 || qc.AttachmentIDs[0] != attachmentID {
			t.Fatalf("expected attachment_ids=[%q] in context, got %#v", attachmentID, qc.AttachmentIDs)
		}
	})

	t.Run("foreign workspace parent is rejected", func(t *testing.T) {
		before := countQuickCreateTasks(t)
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/quick-create", map[string]any{
			"agent_id":        agentID,
			"prompt":          "Try to smuggle a foreign parent",
			"parent_issue_id": foreignParentID,
		})
		testHandler.QuickCreateIssue(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for foreign parent, got %d: %s", w.Code, w.Body.String())
		}
		if got := countQuickCreateTasks(t); got != before {
			// Any increase means the foreign-parent request enqueued a
			// task despite the 400 — the trust boundary leaked.
			t.Fatalf("foreign parent must not enqueue a task: expected %d quick-create tasks, got %d", before, got)
		}
	})

	t.Run("bogus uuid parent is rejected", func(t *testing.T) {
		before := countQuickCreateTasks(t)
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/quick-create", map[string]any{
			"agent_id":        agentID,
			"prompt":          "Try a malformed parent UUID",
			"parent_issue_id": "not-a-uuid",
		})
		testHandler.QuickCreateIssue(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for bogus parent, got %d: %s", w.Code, w.Body.String())
		}
		if got := countQuickCreateTasks(t); got != before {
			t.Fatalf("bogus parent must not enqueue a task: expected %d quick-create tasks, got %d", before, got)
		}
	})

	t.Run("bogus uuid attachment id is rejected", func(t *testing.T) {
		before := countQuickCreateTasks(t)
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/quick-create", map[string]any{
			"agent_id":       agentID,
			"prompt":         "Try a malformed attachment UUID",
			"attachment_ids": []string{"not-a-uuid"},
		})
		testHandler.QuickCreateIssue(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for bogus attachment id, got %d: %s", w.Code, w.Body.String())
		}
		if got := countQuickCreateTasks(t); got != before {
			t.Fatalf("bogus attachment id must not enqueue a task: expected %d quick-create tasks, got %d", before, got)
		}
	})
}
