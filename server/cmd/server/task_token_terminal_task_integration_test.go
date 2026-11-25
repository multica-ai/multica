package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestTaskTokenRejectedAfterOfflineSweepFailsTask is the GitHub #11378
// regression. The offline-runtime sweeper fails a running task without
// deleting its mat_ token, so the token row outlives the task. Auth must
// refuse it anyway: a token authenticates only while its task is live.
func TestTaskTokenRejectedAfterOfflineSweepFailsTask(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()

	issueID, agentID, taskID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	token := mintAgentTaskToken(t, agentID, taskID, testUserID)

	getIssue := func(t *testing.T) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, testServer.URL+"/api/issues/"+issueID, nil)
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("perform request: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := getIssue(t); got != http.StatusOK {
		t.Fatalf("running task: status = %d, want 200", got)
	}

	setAgentRuntimeOffline(t, agentID, defaultRuntimeReconnectGrace+time.Hour)
	failed, err := db.New(testPool).FailTasksForOfflineRuntimes(ctx, db.FailTasksForOfflineRuntimesParams{
		ReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
		MaxPerTick:         offlineTaskFailBatchSize,
	})
	if err != nil {
		t.Fatalf("FailTasksForOfflineRuntimes: %v", err)
	}
	swept := false
	for _, task := range failed {
		if task.ID.Bytes == parseUUIDBytes(taskID) {
			swept = true
		}
	}
	if !swept {
		t.Fatal("sweeper did not fail the task")
	}

	// The precondition the bug needs: the sweep left the token row in place.
	var rows int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM task_token WHERE token_hash = $1`, auth.HashToken(token),
	).Scan(&rows); err != nil {
		t.Fatalf("count task tokens: %v", err)
	}
	if rows != 1 {
		t.Fatalf("task_token rows after sweep = %d, want 1 (test no longer covers the undeleted-token case)", rows)
	}

	if got := getIssue(t); got != http.StatusUnauthorized {
		t.Fatalf("failed task: status = %d, want 401", got)
	}
}

// TestTaskTokenLiveOnlyWhileTaskIsLive pins the status rule in
// GetTaskTokenByHash: every live status authenticates, every terminal one
// does not, whatever path put the task there.
func TestTaskTokenLiveOnlyWhileTaskIsLive(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()

	issueID, agentID, taskID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	token := mintAgentTaskToken(t, agentID, taskID, testUserID)
	queries := db.New(testPool)

	for _, tc := range []struct {
		status string
		live   bool
	}{
		{"queued", true},
		{"dispatched", true},
		{"running", true},
		{"waiting_local_directory", true},
		{"deferred", true},
		{"completed", false},
		{"failed", false},
		{"cancelled", false},
	} {
		t.Run(tc.status, func(t *testing.T) {
			if _, err := testPool.Exec(ctx,
				`UPDATE agent_task_queue SET status = $2 WHERE id = $1`, taskID, tc.status,
			); err != nil {
				t.Fatalf("set task status: %v", err)
			}
			_, err := queries.GetTaskTokenByHash(ctx, auth.HashToken(token))
			if tc.live && err != nil {
				t.Fatalf("live task: GetTaskTokenByHash error = %v, want the token", err)
			}
			if !tc.live && err == nil {
				t.Fatal("terminal task: GetTaskTokenByHash returned the token, want no row")
			}
		})
	}
}
