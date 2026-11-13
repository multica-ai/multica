package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// MUL-8198: Remote MCP daemon tokens minted before migration 588 have no
// owner_id. Through v0.6.5 FinalizeTaskClaim inserted them in the same
// transaction as the task's task_token, so DaemonAuth recovers the issuing
// owner from that task_token. These tests pin that a running pre-upgrade task
// keeps working, and that every case without a single provable owner, or
// whose runtime changed hands, is still refused.

type legacyTokenFixture struct {
	daemonID  string
	runtimeID string
	taskID    string
}

func newLegacyTokenFixture(t *testing.T, label string) legacyTokenFixture {
	t.Helper()
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	f := legacyTokenFixture{daemonID: "legacy-" + label + "-" + suffix}
	f.runtimeID = dbfx.Runtime(t, label+" legacy runtime", testutil.Cols{
		"daemon_id": f.daemonID,
		"provider":  "claude",
	})
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, f.runtimeID, label+" legacy agent")
	f.taskID = dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": f.runtimeID,
		"issue_id":   issueID,
		"status":     "running",
		"started_at": testutil.Raw("now()"),
	})
	return f
}

// mintLegacyClaimTokens reproduces a v0.6.5 claim: one transaction inserts a
// task_token per (taskID, userID) pair and an unbound daemon token. It
// returns the raw daemon token.
func mintLegacyClaimTokens(t *testing.T, daemonID string, taskTokens ...[2]string) string {
	t.Helper()
	ctx := context.Background()
	raw, err := auth.GenerateDaemonToken()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, tt := range taskTokens {
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
			SELECT $1, q.id, q.agent_id, $2, $3, now() + interval '1 hour'
			FROM agent_task_queue q WHERE q.id = $4`,
			"legacy-tt-"+uuid.NewString(), testWorkspaceID, tt[1], tt[0]); err != nil {
			t.Fatal(err)
		}
	}
	var tokenID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO daemon_token (token_hash, workspace_id, daemon_id, expires_at)
		VALUES ($1, $2, $3, now() + interval '24 hours') RETURNING id`,
		auth.HashToken(raw), testWorkspaceID, daemonID).Scan(&tokenID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM daemon_token WHERE id = $1`, tokenID)
	})
	return raw
}

func legacyTaskStatus(t *testing.T, raw, taskID string) int {
	t.Helper()
	req := withURLParams(httptest.NewRequest("GET", "/api/daemon/tasks/"+taskID+"/status", nil), "taskId", taskID)
	req.Header.Set("Authorization", "Bearer "+raw)
	w := httptest.NewRecorder()
	middleware.DaemonAuth(testHandler.Queries, nil, nil, nil)(http.HandlerFunc(testHandler.GetTaskStatus)).ServeHTTP(w, req)
	return w.Code
}

func TestLegacyDaemonToken_RunningTaskKeepsAccess(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newLegacyTokenFixture(t, "ok")
	raw := mintLegacyClaimTokens(t, f.daemonID, [2]string{f.taskID, testUserID})
	if code := legacyTaskStatus(t, raw, f.taskID); code != http.StatusOK {
		t.Fatalf("legacy token on its own running task = %d, want 200", code)
	}
}

func TestLegacyDaemonToken_RefusedWithoutSingleProvableOwner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("no matching task_token", func(t *testing.T) {
		f := newLegacyTokenFixture(t, "none")
		raw := mintLegacyClaimTokens(t, f.daemonID)
		if code := legacyTaskStatus(t, raw, f.taskID); code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", code)
		}
	})

	t.Run("task finished and task_token deleted", func(t *testing.T) {
		f := newLegacyTokenFixture(t, "done")
		raw := mintLegacyClaimTokens(t, f.daemonID, [2]string{f.taskID, testUserID})
		dbfx.Exec(t, `DELETE FROM task_token WHERE task_id = $1`, f.taskID)
		if code := legacyTaskStatus(t, raw, f.taskID); code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", code)
		}
	})

	t.Run("runtime owner changed", func(t *testing.T) {
		f := newLegacyTokenFixture(t, "moved")
		raw := mintLegacyClaimTokens(t, f.daemonID, [2]string{f.taskID, testUserID})
		other := dbfx.User(t, "legacy new owner", "legacy-owner-"+uuid.NewString()+"@example.com")
		dbfx.Member(t, testWorkspaceID, other, "member")
		dbfx.Exec(t, `UPDATE agent_runtime SET owner_id = $1 WHERE id = $2`, other, f.runtimeID)
		if code := legacyTaskStatus(t, raw, f.taskID); code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", code)
		}
	})

	t.Run("ambiguous owners at the same timestamp", func(t *testing.T) {
		f := newLegacyTokenFixture(t, "ambig")
		g := newLegacyTokenFixture(t, "ambig2")
		dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id = $1, provider = 'codex' WHERE id = $2`, f.daemonID, g.runtimeID)
		other := dbfx.User(t, "legacy other", "legacy-other-"+uuid.NewString()+"@example.com")
		dbfx.Member(t, testWorkspaceID, other, "member")
		raw := mintLegacyClaimTokens(t, f.daemonID,
			[2]string{f.taskID, testUserID}, [2]string{g.taskID, other})
		if code := legacyTaskStatus(t, raw, f.taskID); code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", code)
		}
	})

	t.Run("task_token on another daemon", func(t *testing.T) {
		f := newLegacyTokenFixture(t, "otherd")
		raw := mintLegacyClaimTokens(t, "unrelated-"+f.daemonID, [2]string{f.taskID, testUserID})
		if code := legacyTaskStatus(t, raw, f.taskID); code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", code)
		}
	})
}
