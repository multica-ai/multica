package handler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCommentDeliveryObligationSurvivesWriterProcessExit(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("subprocess requires explicit fixture DATABASE_URL")
	}
	runtimeID := dbfx.Runtime(t, "restart obligation runtime")
	agentID := dbfx.Agent(t, "restart obligation target", runtimeID)
	issueID := dbfx.Issue(t, "restart obligation", testutil.Cols{
		"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID,
	})
	seedDupRacePR(t, issueID, 9995986)
	rootID := dbfx.Comment(t, issueID, "Old-head instruction")
	commentID := dbfx.Comment(t, issueID, "Persist beyond the accepting process", testutil.Cols{"parent_id": rootID})
	blockerID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": rootID,
		"status": "queued", "context": map[string]any{"head_sha": dupRaceHeadA},
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment_delivery_obligation WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
	})
	buildCtx, stopBuild := context.WithTimeout(t.Context(), 2*time.Minute)
	defer stopBuild()
	binary := filepath.Join(t.TempDir(), "comment-delivery-restart")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if out, err := exec.CommandContext(buildCtx, "go", "build", "-o", binary, "./testdata/comment_delivery_restart").CombinedOutput(); err != nil {
		t.Fatalf("build fixture helper: %v\n%s", err, out)
	}
	run := func(mode string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, mode, testWorkspaceID, commentID, agentID)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s process: %v\n%s", mode, err, out)
		}
		if mode == "admit" && !strings.Contains(string(out), "accepted_deferred") {
			t.Fatalf("writer did not confirm an accepted deferred HTTP response: %s", out)
		}
	}
	run("admit")
	// The writer has exited and closed its database connection. No handler or
	// in-memory queue survives from that process to the following reader.
	if _, err := testHandler.Queries.GetCommentDeliveryObligation(t.Context(), db.GetCommentDeliveryObligationParams{
		WorkspaceID: parseUUID(testWorkspaceID), CommentID: parseUUID(commentID), AgentID: parseUUID(agentID),
	}); err != nil {
		t.Fatalf("writer process lost accepted input: %v", err)
	}
	if commentCovered(t, issueID, agentID, commentID, "queued") {
		t.Fatal("writer folded current-head input into the old-head blocker")
	}
	// End the blocker without invoking completion reconciliation in this
	// process. Only the fresh reader may produce the follow-up.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed', completed_at=now(), delivered_comment_ids=ARRAY[trigger_comment_id] WHERE id=$1`, blockerID)
	// Prioritize this fixture's record without depending on other suites' rows.
	dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID)
	run("drain")
	if !commentCoveredByAnyActiveTask(t, issueID, agentID, commentID) {
		t.Fatal("fresh reader process did not recover accepted input")
	}
	if head := coveringTaskHead(t, issueID, agentID, commentID); head != dupRaceHeadB {
		t.Fatalf("restarted reader enqueued head %q, want %q", head, dupRaceHeadB)
	}
}
