package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// seedPriorRunWithSnapshot creates the terminal prior task that anchors this
// agent's issue-state comparison, carrying the snapshot that run was handed.
// Passing an empty snapshot models a run recorded before the column existed.
func seedPriorRunWithSnapshot(t *testing.T, agentID, runtimeID, issueID, snapshot string) {
	t.Helper()
	cols := testutil.Cols{
		"runtime_id":   runtimeID,
		"issue_id":     issueID,
		"status":       "completed",
		"started_at":   testutil.Raw("now() - interval '1 hour'"),
		"completed_at": testutil.Raw("now() - interval '50 minutes'"),
	}
	if snapshot != "" {
		cols["issue_snapshot"] = snapshot
	}
	dbfx.Task(t, agentID, cols)
}

// issueSnapshotJSON renders a stored snapshot for the issue fixture, letting a
// caller override individual fields to model what the previous run saw.
// createClaimReclaimAgentAndIssue always creates an unassigned issue in
// status in_progress with priority none, and titles it "<name> issue".
func issueSnapshotJSON(t *testing.T, version int, title, description, status, priority string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"v":                  version,
		"status":             status,
		"priority":           priority,
		"title_sha256":       sha256Hex(title),
		"description_sha256": sha256Hex(description),
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	return string(raw)
}

// TestClaimTaskByRuntime_IssueUnchangedReportsEmptyDelta: the previous run saw
// the issue exactly as it is now, so the claim reports "compared, nothing
// changed" — the one answer a daemon may use to skip workflow step 1's read.
func TestClaimTaskByRuntime_IssueUnchangedReportsEmptyDelta(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Issue snapshot unchanged runtime")
	const name = "Issue snapshot unchanged agent"
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name)

	seedPriorRunWithSnapshot(t, agentID, runtimeID, issueID,
		issueSnapshotJSON(t, 1, name+" issue", "", "in_progress", "none"))
	createCommentTriggeredClaimTask(t, ctx, agentID, runtimeID, issueID, nil)

	resp := claimCommentTask(t, runtimeID, "issue-snapshot-unchanged")
	if !resp.Task.IssueStateDeltaKnown {
		t.Fatalf("issue_state_delta_known must be true when the comparison ran")
	}
	if len(resp.Task.IssueChangedFields) != 0 {
		t.Errorf("issue_changed_fields = %v, want empty for an unchanged issue", resp.Task.IssueChangedFields)
	}
	if resp.Task.IssueStatus != "in_progress" {
		t.Errorf("issue_status = %q, want in_progress", resp.Task.IssueStatus)
	}
}

// TestClaimTaskByRuntime_IssueChangedNamesFields: the comparison reports which
// of the compared fields moved, in the fixed order, so the daemon can name them
// instead of telling the agent to diff the whole record.
func TestClaimTaskByRuntime_IssueChangedNamesFields(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Issue snapshot changed runtime")
	const name = "Issue snapshot changed agent"
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name)

	// The previous run saw a different description and a different status;
	// title and priority are unchanged and must NOT be reported.
	seedPriorRunWithSnapshot(t, agentID, runtimeID, issueID,
		issueSnapshotJSON(t, 1, name+" issue", "an older description", "todo", "none"))
	createCommentTriggeredClaimTask(t, ctx, agentID, runtimeID, issueID, nil)

	resp := claimCommentTask(t, runtimeID, "issue-snapshot-changed")
	if !resp.Task.IssueStateDeltaKnown {
		t.Fatalf("issue_state_delta_known must be true when the comparison ran")
	}
	if got := strings.Join(resp.Task.IssueChangedFields, ","); got != "description,status" {
		t.Errorf("issue_changed_fields = %q, want \"description,status\"", got)
	}
}

// TestClaimTaskByRuntime_NoPriorSnapshotIsNotUnchanged pins the ambiguity this
// flag exists to remove. A first run on an issue, and a prior run recorded
// before the column existed, both produce an empty changed-field list — the
// same bytes as a genuine "nothing changed". Only the flag separates them, and
// getting it wrong means an agent continues from stale context.
//
// The current status and assignee ship regardless: a run that must read the
// issue anyway loses nothing, and workflow step 3 needs them either way.
func TestClaimTaskByRuntime_NoPriorSnapshotIsNotUnchanged(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cases := []struct {
		label    string
		priorRun bool
	}{
		// First run on the issue: the anchor query finds no row at all.
		{label: "no prior run at all", priorRun: false},
		// A prior run exists but predates the column, so its snapshot is NULL.
		{label: "prior run, no snapshot", priorRun: true},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := createClaimReclaimRuntime(t, ctx, "Issue snapshot unknown runtime "+tc.label)
			agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Issue snapshot unknown agent "+tc.label)
			if tc.priorRun {
				seedPriorRunWithSnapshot(t, agentID, runtimeID, issueID, "")
			}
			createCommentTriggeredClaimTask(t, ctx, agentID, runtimeID, issueID, nil)

			resp := claimCommentTask(t, runtimeID, "issue-snapshot-unknown")
			if resp.Task.IssueStateDeltaKnown {
				t.Errorf("issue_state_delta_known must be false when nothing was compared")
			}
			if len(resp.Task.IssueChangedFields) != 0 {
				t.Errorf("issue_changed_fields = %v, want empty", resp.Task.IssueChangedFields)
			}
			if resp.Task.IssueStatus != "in_progress" {
				t.Errorf("issue_status = %q, want in_progress even when the delta is unknown", resp.Task.IssueStatus)
			}
		})
	}
}

// TestClaimTaskByRuntime_ForeignSnapshotVersionIsUnknown: a snapshot written by
// a different shape version compared a different set of fields, so neither
// "unchanged" nor a changed-field list would be a true statement about it.
func TestClaimTaskByRuntime_ForeignSnapshotVersionIsUnknown(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Issue snapshot version runtime")
	const name = "Issue snapshot version agent"
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name)

	// Byte-identical to the unchanged case apart from `v`.
	seedPriorRunWithSnapshot(t, agentID, runtimeID, issueID,
		issueSnapshotJSON(t, issueSnapshotVersion+1, name+" issue", "", "in_progress", "none"))
	createCommentTriggeredClaimTask(t, ctx, agentID, runtimeID, issueID, nil)

	resp := claimCommentTask(t, runtimeID, "issue-snapshot-version")
	if resp.Task.IssueStateDeltaKnown {
		t.Errorf("a snapshot from another shape version must read as not compared, not as unchanged")
	}
}

// TestClaimTaskByRuntime_RecordsItsOwnIssueSnapshot closes the loop: the
// comparison above is only ever true if each claim persists what it saw. This
// is the write half, and it must happen for an ASSIGNMENT claim too — a run
// that records nothing leaves the next one with no baseline.
func TestClaimTaskByRuntime_RecordsItsOwnIssueSnapshot(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Issue snapshot write runtime")
	const name = "Issue snapshot write agent"
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name)

	// No trigger comment: this is the assignment path.
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
	})

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "issue-snapshot-write")
	req = withURLParam(req, "runtimeId", runtimeID)
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var raw []byte
	if err := testPool.QueryRow(ctx, `SELECT issue_snapshot FROM agent_task_queue WHERE id = $1`, taskID).Scan(&raw); err != nil {
		t.Fatalf("read back issue_snapshot: %v", err)
	}
	stored, ok := decodeIssueStateSnapshot(raw)
	if !ok {
		t.Fatalf("claim did not persist a decodable issue snapshot, got %q", string(raw))
	}
	if stored.Status != "in_progress" || stored.Priority != "none" {
		t.Errorf("stored snapshot = %+v, want the issue's status/priority", stored)
	}
	if stored.TitleSHA256 != sha256Hex(name+" issue") {
		t.Errorf("stored title hash does not match the claimed issue's title")
	}
	if strings.Contains(string(raw), name+" issue") {
		t.Errorf("snapshot must store a hash, never the issue text: %s", string(raw))
	}
}

// TestClaimTaskByRuntime_BothDeltasShareOneAnchor pins the merge: the comment
// delta and the issue-state delta are resolved from the SAME prior-run row
// (GetLastRunAnchorForIssueAndAgent), so "since your last run" is one fact on a
// claim rather than two independently-resolved ones that could disagree — and a
// comment-triggered claim reads that row once, not twice.
func TestClaimTaskByRuntime_BothDeltasShareOneAnchor(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Shared anchor runtime")
	const name = "Shared anchor agent"
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name)

	// One prior run carries both halves of the anchor: its started_at dates the
	// comment delta, its snapshot dates the issue-state delta.
	seedPriorRunWithSnapshot(t, agentID, runtimeID, issueID,
		issueSnapshotJSON(t, 1, name+" issue", "", "in_progress", "none"))

	// A member comment after that anchor, so the comment delta is non-zero and
	// the two deltas cannot both be trivially empty.
	dbfx.Comment(t, issueID, "something happened while you were away")
	createCommentTriggeredClaimTask(t, ctx, agentID, runtimeID, issueID, nil)

	resp := claimCommentTask(t, runtimeID, "shared-anchor-claim")
	if !resp.Task.DeltaKnown {
		t.Errorf("new_comments_delta_known must be true — the shared anchor carries started_at")
	}
	if resp.Task.NewCommentCount != 1 {
		t.Errorf("new_comment_count = %d, want 1", resp.Task.NewCommentCount)
	}
	if !resp.Task.IssueStateDeltaKnown {
		t.Errorf("issue_state_delta_known must be true — the same anchor row carries the snapshot")
	}
	if len(resp.Task.IssueChangedFields) != 0 {
		t.Errorf("issue_changed_fields = %v, want empty", resp.Task.IssueChangedFields)
	}
	// Both anchors describe the same prior run, so the comment anchor must be
	// that run's started_at, not this claim's own timestamp.
	if resp.Task.NewCommentsSince == "" {
		t.Errorf("new_comments_since must carry the shared anchor")
	}
}
