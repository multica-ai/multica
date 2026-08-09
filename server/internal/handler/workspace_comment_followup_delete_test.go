package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/pooltestdb"
)

type workspaceFollowupDeleteFixture struct {
	workspaceID string
	runtimeID   string
	agentID     string
	issueID     string
	commentID   string
}

func createWorkspaceFollowupDeleteFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	name string,
	slug string,
) workspaceFollowupDeleteFixture {
	t.Helper()
	ctx := context.Background()
	var fixture workspaceFollowupDeleteFixture
	if err := pool.QueryRow(ctx, `
INSERT INTO workspace (name, slug)
VALUES ($1, $2)
RETURNING id
`, name, slug).Scan(&fixture.workspaceID); err != nil {
		t.Fatalf("create %s workspace: %v", name, err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, fixture.workspaceID, testUserID); err != nil {
		t.Fatalf("create %s owner: %v", name, err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO agent_runtime (
	workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
)
VALUES ($1, $2, 'cloud', 'workspace-delete-followup-test', 'offline', '', '{}'::jsonb, $3)
RETURNING id
`, fixture.workspaceID, name+" runtime", testUserID).Scan(&fixture.runtimeID); err != nil {
		t.Fatalf("create %s runtime: %v", name, err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO agent (
	workspace_id, name, runtime_mode, runtime_config, runtime_id, owner_id
)
VALUES ($1, $2, 'cloud', '{}'::jsonb, $3, $4)
RETURNING id
`, fixture.workspaceID, name+" agent", fixture.runtimeID, testUserID).Scan(&fixture.agentID); err != nil {
		t.Fatalf("create %s agent: %v", name, err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO issue (workspace_id, title, creator_type, creator_id)
VALUES ($1, $2, 'member', $3)
RETURNING id
`, fixture.workspaceID, name+" issue", testUserID).Scan(&fixture.issueID); err != nil {
		t.Fatalf("create %s issue: %v", name, err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
VALUES ($1, $2, 'member', $3, $4)
RETURNING id
`, fixture.issueID, fixture.workspaceID, testUserID, name+" comment").Scan(&fixture.commentID); err != nil {
		t.Fatalf("create %s comment: %v", name, err)
	}
	return fixture
}

func TestDeleteWorkspaceRemovesCommentFollowupObligationsByEveryOwnershipPath(t *testing.T) {
	pool := pooltestdb.Open(t)
	ctx := context.Background()
	fixtureID := uuid.NewString()
	targetSlug := "handler-tests-delete-followup-target-" + fixtureID
	neighborSlug := "handler-tests-delete-followup-neighbor-" + fixtureID

	target := createWorkspaceFollowupDeleteFixture(t, pool, "Follow-up target", targetSlug)
	neighbor := createWorkspaceFollowupDeleteFixture(t, pool, "Follow-up neighbor", neighborSlug)
	var neighborSecondCommentID string
	if err := pool.QueryRow(ctx, `
INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
VALUES ($1, $2, 'member', $3, 'Follow-up neighbor control comment')
RETURNING id
`, neighbor.issueID, neighbor.workspaceID, testUserID).Scan(&neighborSecondCommentID); err != nil {
		t.Fatalf("create neighbor control comment: %v", err)
	}

	targetIssueHead := "workspace-delete-followup-target-issue-" + fixtureID
	targetAgentHead := "workspace-delete-followup-target-agent-" + fixtureID
	targetCommentHead := "workspace-delete-followup-target-comment-" + fixtureID
	neighborHead := "workspace-delete-followup-neighbor-" + fixtureID
	for _, obligation := range []struct {
		issueID, agentID, commentID, headSHA string
	}{
		{target.issueID, neighbor.agentID, neighbor.commentID, targetIssueHead},
		{neighbor.issueID, target.agentID, neighbor.commentID, targetAgentHead},
		{neighbor.issueID, neighbor.agentID, target.commentID, targetCommentHead},
		{neighbor.issueID, neighbor.agentID, neighborSecondCommentID, neighborHead},
	} {
		if _, err := pool.Exec(ctx, `
INSERT INTO agent_comment_followup_obligation (
	issue_id, agent_id, comment_id, comment_updated_at, head_sha
)
VALUES ($1, $2, $3, now(), $4)
`, obligation.issueID, obligation.agentID, obligation.commentID, obligation.headSHA); err != nil {
			t.Fatalf("create obligation %s: %v", obligation.headSHA, err)
		}
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
DELETE FROM agent_comment_followup_obligation
WHERE head_sha IN ($1, $2, $3, $4)
`, targetIssueHead, targetAgentHead, targetCommentHead, neighborHead)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id IN ($1, $2)`, target.workspaceID, neighbor.workspaceID)
	})

	recorder := httptest.NewRecorder()
	request := newRequest(http.MethodDelete, "/api/workspaces/"+target.workspaceID, nil)
	request = withURLParam(request, "id", target.workspaceID)
	testHandler.DeleteWorkspace(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace returned %d: %s", recorder.Code, recorder.Body.String())
	}

	var targetCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM agent_comment_followup_obligation
WHERE head_sha IN ($1, $2, $3)
`, targetIssueHead, targetAgentHead, targetCommentHead).Scan(&targetCount); err != nil {
		t.Fatalf("count target obligations: %v", err)
	}
	if targetCount != 0 {
		t.Fatalf("target workspace obligations survived deletion: got %d, want 0", targetCount)
	}

	var neighborCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM agent_comment_followup_obligation
WHERE head_sha = $1
`, neighborHead).Scan(&neighborCount); err != nil {
		t.Fatalf("count neighbor obligations: %v", err)
	}
	if neighborCount != 1 {
		t.Fatalf("neighbor workspace obligation count = %d, want 1", neighborCount)
	}
}

func TestDeleteWorkspaceRemovesCommentFollowupInsertedWhileCommentDeleteWaits(t *testing.T) {
	pool := pooltestdb.Open(t)
	ctx := context.Background()
	fixtureID := uuid.NewString()
	target := createWorkspaceFollowupDeleteFixture(
		t,
		pool,
		"Follow-up race target",
		"handler-tests-delete-followup-race-target-"+fixtureID,
	)
	neighbor := createWorkspaceFollowupDeleteFixture(
		t,
		pool,
		"Follow-up race neighbor",
		"handler-tests-delete-followup-race-neighbor-"+fixtureID,
	)
	targetHead := "workspace-delete-followup-race-target-" + fixtureID
	neighborHead := "workspace-delete-followup-race-neighbor-" + fixtureID
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
DELETE FROM agent_comment_followup_obligation WHERE head_sha IN ($1, $2)
`, targetHead, neighborHead)
		_, _ = pool.Exec(context.Background(), `
DELETE FROM workspace WHERE id IN ($1, $2)
`, target.workspaceID, neighbor.workspaceID)
	})

	if _, err := pool.Exec(ctx, `
INSERT INTO agent_comment_followup_obligation (
	issue_id, agent_id, comment_id, comment_updated_at, head_sha
)
VALUES ($1, $2, $3, now(), $4)
`, neighbor.issueID, neighbor.agentID, neighbor.commentID, neighborHead); err != nil {
		t.Fatalf("create neighbor obligation: %v", err)
	}

	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin obligation writer: %v", err)
	}
	defer writer.Rollback(ctx)
	if _, err := writer.Exec(ctx, `
SELECT id FROM comment WHERE id = $1 FOR UPDATE
`, target.commentID); err != nil {
		t.Fatalf("lock target comment: %v", err)
	}
	var writerPID int32
	if err := writer.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID); err != nil {
		t.Fatalf("load writer backend PID: %v", err)
	}

	type deleteResult struct {
		code int
		body string
	}
	deleteDone := make(chan deleteResult, 1)
	go func() {
		recorder := httptest.NewRecorder()
		request := newRequest(http.MethodDelete, "/api/workspaces/"+target.workspaceID, nil)
		request = withURLParam(request, "id", target.workspaceID)
		testHandler.DeleteWorkspace(recorder, request)
		deleteDone <- deleteResult{code: recorder.Code, body: recorder.Body.String()}
	}()

	waitDeadline := time.Now().Add(5 * time.Second)
	deleteIsWaiting := false
	for time.Now().Before(waitDeadline) {
		if err := pool.QueryRow(ctx, `
SELECT EXISTS (
	SELECT 1
	FROM pg_stat_activity activity
	WHERE $1 = ANY(pg_blocking_pids(activity.pid))
	  AND activity.wait_event_type = 'Lock'
	  AND activity.query LIKE '%-- name: DeleteWorkspaceComments :exec%'
)
`, writerPID).Scan(&deleteIsWaiting); err != nil {
			t.Fatalf("inspect workspace delete wait: %v", err)
		}
		if deleteIsWaiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !deleteIsWaiting {
		t.Fatal("DeleteWorkspace did not block in DeleteWorkspaceComments on the writer's Comment lock")
	}

	if _, err := writer.Exec(ctx, `
INSERT INTO agent_comment_followup_obligation (
	issue_id, agent_id, comment_id, comment_updated_at, head_sha
)
VALUES ($1, $2, $3, now(), $4)
`, target.issueID, target.agentID, target.commentID, targetHead); err != nil {
		t.Fatalf("insert target obligation while delete waits: %v", err)
	}
	if err := writer.Commit(ctx); err != nil {
		t.Fatalf("commit target obligation writer: %v", err)
	}

	select {
	case result := <-deleteDone:
		if result.code != http.StatusNoContent {
			t.Fatalf("DeleteWorkspace returned %d: %s", result.code, result.body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DeleteWorkspace did not finish after the Comment writer committed")
	}

	var targetCount, neighborCount int
	if err := pool.QueryRow(ctx, `
SELECT
	count(*) FILTER (WHERE head_sha = $1),
	count(*) FILTER (WHERE head_sha = $2)
FROM agent_comment_followup_obligation
`, targetHead, neighborHead).Scan(&targetCount, &neighborCount); err != nil {
		t.Fatalf("count obligations after concurrent workspace delete: %v", err)
	}
	if targetCount != 0 {
		t.Fatalf("concurrently inserted target obligation survived deletion: got %d, want 0", targetCount)
	}
	if neighborCount != 1 {
		t.Fatalf("neighbor obligation count = %d, want 1", neighborCount)
	}
}
