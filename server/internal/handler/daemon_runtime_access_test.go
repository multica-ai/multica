package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestClaimTaskByRuntime_OwnerlessAgentOnPrivateRuntimeFailsExplicitly(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Ownerless agent private runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Ownerless private agent")
	dbfx.Exec(t, `UPDATE agent SET owner_id = NULL WHERE id = $1`, agentID)
	taskID := seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "ownerless-agent-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != `{"task":null}` {
		t.Fatalf("ClaimTaskByRuntime body = %q, want empty successful poll", w.Body.String())
	}

	var status, errorMessage, failureReason string
	dbfx.QueryRow(t, `
		SELECT status, error, failure_reason
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&status, &errorMessage, &failureReason)
	if status != "failed" {
		t.Fatalf("task status = %q, want failed", status)
	}
	if !strings.Contains(errorMessage, "agent has no owner") {
		t.Fatalf("task error = %q, want actionable missing-owner error", errorMessage)
	}
	if failureReason != taskfailure.ReasonRuntimeAccessDenied.String() {
		t.Fatalf("failure_reason = %q, want %q", failureReason, taskfailure.ReasonRuntimeAccessDenied)
	}
}

func TestClaimTaskByRuntime_SettlesPrivateOwnerMismatch(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	foreignOwnerID := dbfx.User(t, "Daemon mismatch owner", "daemon-mismatch-owner-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, foreignOwnerID, "member")
	runtimeID := createClaimReclaimRuntime(t, ctx, "Mismatch issue runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Mismatch issue agent")
	dbfx.Exec(t, `UPDATE agent SET owner_id = $1 WHERE id = $2`, foreignOwnerID, agentID)
	taskID := seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "mismatch-issue-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != `{"task":null}` {
		t.Fatalf("ClaimTaskByRuntime body = %q, want empty successful poll", w.Body.String())
	}

	var status, failureReason string
	dbfx.QueryRow(t, `SELECT status, failure_reason FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &failureReason)
	// Regression A (PUCK-132): a queued issue task whose agent lost access to
	// its private runtime settles as runtime_access_denied — matching the
	// admission-time dispatch reason — not invalid_task_identity, so clients
	// reach the dedicated recovery copy.
	if status != "failed" || failureReason != taskfailure.ReasonRuntimeAccessDenied.String() {
		t.Fatalf("task state = %q/%q, want failed/%s", status, failureReason, taskfailure.ReasonRuntimeAccessDenied)
	}
}

func TestClaimTaskByRuntime_SettlesPrivateOwnerMismatchChatFailure(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	foreignOwnerID := dbfx.User(t, "Daemon chat mismatch owner", "daemon-chat-mismatch-owner-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, foreignOwnerID, "member")
	runtimeID := createClaimReclaimRuntime(t, ctx, "Mismatch chat runtime")
	agentID, _ := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Mismatch chat agent")
	dbfx.Exec(t, `UPDATE agent SET owner_id = $1 WHERE id = $2`, foreignOwnerID, agentID)
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID, "issue_id": nil})

	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "mismatch-chat-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != `{"task":null}` {
		t.Fatalf("ClaimTaskByRuntime body = %q, want empty successful poll", w.Body.String())
	}

	var status, failureReason, assistantContent string
	dbfx.QueryRow(t, `SELECT status, failure_reason FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &failureReason)
	dbfx.QueryRow(t, `SELECT content FROM chat_message WHERE task_id = $1 AND role = 'assistant'`, taskID).Scan(&assistantContent)
	// Regression B (PUCK-132): queued chat task settlement carries
	// runtime_access_denied AND still produces the assistant failure message
	// through the existing FailTask path.
	if status != "failed" || failureReason != taskfailure.ReasonRuntimeAccessDenied.String() {
		t.Fatalf("task state = %q/%q, want failed/%s", status, failureReason, taskfailure.ReasonRuntimeAccessDenied)
	}
	if !strings.Contains(assistantContent, "agent and runtime have different owners") {
		t.Fatalf("assistant failure = %q, want owner-mismatch message", assistantContent)
	}
}

func TestClaimTasksByRuntime_SettlesMismatchAndReturnsValidTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	foreignOwnerID := dbfx.User(t, "Daemon batch mismatch owner", "daemon-batch-mismatch-owner-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, foreignOwnerID, "member")
	runtimeID := createClaimReclaimRuntime(t, ctx, "Mismatch batch runtime")
	mismatchAgentID, mismatchIssueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Mismatch batch agent")
	dbfx.Exec(t, `UPDATE agent SET owner_id = $1 WHERE id = $2`, foreignOwnerID, mismatchAgentID)
	validAgentID, validIssueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Valid batch agent")
	mismatchTaskID := seedQueuedIssueTask(t, ctx, mismatchAgentID, runtimeID, mismatchIssueID)
	validTaskID := seedQueuedIssueTask(t, ctx, validAgentID, runtimeID, validIssueID)

	w := postBatchClaim(t, testWorkspaceID, []string{runtimeID}, 2)
	if w.Code != http.StatusOK {
		t.Fatalf("batch claim status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp batchClaimResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode batch claim: %v", err)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].ID != validTaskID {
		t.Fatalf("batch tasks = %+v, want valid task %s", resp.Tasks, validTaskID)
	}

	var mismatchStatus, mismatchReason, validStatus string
	dbfx.QueryRow(t, `SELECT status, failure_reason FROM agent_task_queue WHERE id = $1`, mismatchTaskID).Scan(&mismatchStatus, &mismatchReason)
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, validTaskID).Scan(&validStatus)
	if mismatchStatus != "failed" || mismatchReason != taskfailure.ReasonRuntimeAccessDenied.String() {
		t.Fatalf("mismatch task state = %q/%q, want failed/%s", mismatchStatus, mismatchReason, taskfailure.ReasonRuntimeAccessDenied)
	}
	if validStatus != "dispatched" {
		t.Fatalf("valid task status = %q, want dispatched", validStatus)
	}
}

func TestBuildClaimedTaskResponseRejectsAgentOwnerChangedAfterClaim(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	newOwnerID := dbfx.User(t, "Claim owner mismatch", "claim-owner-mismatch-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, newOwnerID, "member")
	runtimeID := createClaimReclaimRuntime(t, ctx, "Claim then change owner runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Claim then change owner agent")
	taskID := seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

	task, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if task == nil || uuidToString(task.ID) != taskID {
		t.Fatalf("claimed task = %+v, want %s", task, taskID)
	}
	dbfx.Exec(t, `UPDATE agent SET owner_id = $1 WHERE id = $2`, newOwnerID, agentID)

	runtime, err := testHandler.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
		ID:          parseUUID(runtimeID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "claim-then-owner-change")
	_, _, _, _, failure := testHandler.buildClaimedTaskResponse(
		req, task, runtime, runtimeID, testWorkspaceID,
	)
	if failure == nil || failure.status != http.StatusForbidden || failure.outcome != "error_runtime_access_denied" {
		t.Fatalf("failure = %+v, want runtime-access forbidden", failure)
	}

	var status, errorMessage, failureReason string
	dbfx.QueryRow(t, `
		SELECT status, error, failure_reason
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&status, &errorMessage, &failureReason)
	if status != "failed" {
		t.Fatalf("task status = %q, want failed", status)
	}
	if !strings.Contains(errorMessage, "agent and runtime have different owners") {
		t.Fatalf("task error = %q, want actionable owner-mismatch error", errorMessage)
	}
	if strings.Contains(errorMessage, "agent has no owner") {
		t.Fatalf("task error = %q, must not describe a non-null owner as missing", errorMessage)
	}
	if failureReason != taskfailure.ReasonRuntimeAccessDenied.String() {
		t.Fatalf("failure_reason = %q, want %q", failureReason, taskfailure.ReasonRuntimeAccessDenied)
	}
}

func TestBuildClaimedTaskResponseRejectsAgentReboundAfterClaim(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	oldRuntimeID := createClaimReclaimRuntime(t, ctx, "Claim then rebind old runtime")
	newRuntimeID := createClaimReclaimRuntime(t, ctx, "Claim then rebind new runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, oldRuntimeID, "Claim then rebind agent")
	taskID := seedQueuedIssueTask(t, ctx, agentID, oldRuntimeID, issueID)

	task, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(oldRuntimeID))
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if task == nil || uuidToString(task.ID) != taskID {
		t.Fatalf("claimed task = %+v, want %s", task, taskID)
	}
	dbfx.Exec(t, `UPDATE agent SET runtime_id = $1 WHERE id = $2`, newRuntimeID, agentID)

	runtime, err := testHandler.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
		ID:          parseUUID(oldRuntimeID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load old runtime: %v", err)
	}
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+oldRuntimeID+"/tasks/claim", nil,
		testWorkspaceID, "claim-then-rebind")
	_, _, _, _, failure := testHandler.buildClaimedTaskResponse(
		req, task, runtime, oldRuntimeID, testWorkspaceID,
	)
	if failure == nil || failure.status != http.StatusConflict || failure.outcome != "error_agent_runtime_changed" {
		t.Fatalf("failure = %+v, want runtime-changed conflict", failure)
	}

	var status, errorMessage, failureReason string
	dbfx.QueryRow(t, `
		SELECT status, error, failure_reason
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&status, &errorMessage, &failureReason)
	if status != "failed" {
		t.Fatalf("task status = %q, want failed", status)
	}
	if !strings.Contains(errorMessage, "moved to another runtime") {
		t.Fatalf("task error = %q, want actionable rebind error", errorMessage)
	}
	// Regression C (PUCK-132): a genuine identity violation — the agent
	// rebound to another runtime — must keep invalid_task_identity and must
	// never be conflated with the runtime_access_denied ownership reason.
	if failureReason != taskfailure.ReasonInvalidTaskIdentity.String() {
		t.Fatalf("failure_reason = %q, want %q", failureReason, taskfailure.ReasonInvalidTaskIdentity)
	}
}

// PUCK-89 blocker 1: the claim path reads the runtime BEFORE claiming, so a
// concurrent re-registration can flip runtime.owner_id between that read and
// final delivery. The final delivery gate must re-authorize against the
// CURRENT owner inside the finalize transaction — the singular claim must not
// return the task to the daemon, must settle it through the existing failure
// path, and must keep the empty successful-poll response.
// finalizeClaimDeliveryForTest drives the singular finalize+delivery path
// (shared gate included) against a task already claimed out-of-band, so a test
// can mutate the runtime between claim and finalize the way a concurrent
// re-registration would in production.
func (h *Handler) finalizeClaimDeliveryForTest(
	r *http.Request, task *db.AgentTaskQueue, runtimeID, runtimeWorkspaceID string,
) (AgentTaskResponse, []pgtype.UUID, int, int, *claimBuildFailure, error) {
	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          parseUUID(runtimeID),
		WorkspaceID: parseUUID(runtimeWorkspaceID),
	})
	if err != nil {
		return AgentTaskResponse{}, nil, 0, 0, nil, fmt.Errorf("load runtime: %w", err)
	}
	return h.finalizeClaimDeliveryForTestWithRuntime(r, task, runtime, runtimeID, runtimeWorkspaceID)
}

func (h *Handler) finalizeClaimDeliveryForTestWithRuntime(
	r *http.Request, task *db.AgentTaskQueue, runtime db.AgentRuntime, runtimeID, runtimeWorkspaceID string,
) (AgentTaskResponse, []pgtype.UUID, int, int, *claimBuildFailure, error) {
	resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, buildFailure := h.buildClaimedTaskResponse(
		r, task, runtime, runtimeID, runtimeWorkspaceID,
	)
	if buildFailure != nil {
		return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, buildFailure, nil
	}
	if !runtime.OwnerID.Valid {
		return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount,
			&claimBuildFailure{outcome: "error_token", status: http.StatusInternalServerError, message: "runtime owner required"}, nil
	}
	tokenStr, terr := auth.GenerateAgentTaskToken()
	if terr != nil {
		return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, nil, fmt.Errorf("generate token: %w", terr)
	}
	remoteMCPToken, daemonTokens, derr := remoteMCPDaemonTokenForClaim(resp, runtime)
	if derr != nil {
		return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, nil, fmt.Errorf("remote mcp token: %w", derr)
	}
	commentBackedTask := task.TriggerCommentID.Valid || len(task.CoalescedCommentIds) > 0
	receipt, deliveryFailure, ferr := h.finalizeClaimDelivery(r.Context(), task, runtime, runtimeID, runtimeWorkspaceID, &resp, db.CreateTaskTokenParams{
		ID:          dbid.NewV7(),
		TokenHash:   auth.HashToken(tokenStr),
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		WorkspaceID: parseUUID(resp.WorkspaceID),
		UserID:      runtime.OwnerID,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}, deliveredCommentIDs, commentBackedTask, daemonTokens...)
	if ferr != nil {
		return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, nil, ferr
	}
	if deliveryFailure != nil {
		return AgentTaskResponse{}, deliveredCommentIDs, agentSkillCount, builtinSkillCount, deliveryFailure, nil
	}
	resp.AuthToken = tokenStr
	resp.RemoteMCPDaemonToken = remoteMCPToken
	resp.DeliveredCommentIDs = uuidStringsOrEmpty(receipt)
	return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, nil, nil
}

func TestClaimTaskByRuntime_RuntimeOwnerChangedAfterClaimNeverDelivered(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	newOwnerID := dbfx.User(t, "Delivery gate owner", "delivery-gate-owner-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, newOwnerID, "member")
	runtimeID := createClaimReclaimRuntime(t, ctx, "Delivery gate runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Delivery gate agent")
	taskID := seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

	// Initial runtime access read (owner = the workspace fixture user) happens
	// inside the handler; flip the runtime owner AFTER the task is claimed by
	// mutating the row directly between claim and finalize. To force that
	// interleaving deterministically, claim first, then change the owner, then
	// drive the finalize+delivery path through the handler's gate helper.
	task, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if task == nil || uuidToString(task.ID) != taskID {
		t.Fatalf("claimed task = %+v, want %s", task, taskID)
	}
	runtimeSnapshot, err := testHandler.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
		ID:          parseUUID(runtimeID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load claim-time runtime: %v", err)
	}
	if !runtimeSnapshot.OwnerID.Valid {
		t.Fatal("claim-time runtime owner unexpectedly missing")
	}
	dbfx.Exec(t, `UPDATE agent_runtime SET owner_id = $1 WHERE id = $2`, newOwnerID, runtimeID)

	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "delivery-gate-owner-change")
	resp, deliveredCommentIDs, _, _, settledFailure, finalizeErr := testHandler.finalizeClaimDeliveryForTestWithRuntime(
		req, task, runtimeSnapshot, runtimeID, testWorkspaceID,
	)
	if finalizeErr != nil {
		t.Fatalf("finalize delivery: %v", finalizeErr)
	}
	if settledFailure == nil || settledFailure.outcome != "error_runtime_access_denied" || !settledFailure.settled {
		t.Fatalf("settled failure = %+v, want settled runtime-access denial", settledFailure)
	}
	if len(deliveredCommentIDs) != 0 {
		t.Fatalf("delivered comment ids = %v, want none", deliveredCommentIDs)
	}
	if resp.AuthToken != "" || resp.RemoteMCPDaemonToken != "" {
		t.Fatalf("resp carries credentials %+v/%+v, want none — nothing may be delivered", resp.AuthToken, resp.RemoteMCPDaemonToken)
	}

	// No task token was minted and the task settled terminal.
	var tokenCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM task_token WHERE task_id = $1`, taskID).Scan(&tokenCount)
	if tokenCount != 0 {
		t.Fatalf("task token count = %d, want 0 — no credential may accompany a rejected delivery", tokenCount)
	}
	var status, errorMessage, failureReason string
	dbfx.QueryRow(t, `
		SELECT status, error, failure_reason
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&status, &errorMessage, &failureReason)
	if status != "failed" {
		t.Fatalf("task status = %q, want failed", status)
	}
	if !strings.Contains(errorMessage, "agent and runtime have different owners") {
		t.Fatalf("task error = %q, want actionable owner-mismatch error", errorMessage)
	}
	if failureReason != taskfailure.ReasonRuntimeAccessDenied.String() {
		t.Fatalf("failure_reason = %q, want %q", failureReason, taskfailure.ReasonRuntimeAccessDenied)
	}

	// And the full HTTP claim surface stays an empty successful poll.
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, func() *http.Request {
		r := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
			testWorkspaceID, "delivery-gate-poll")
		return testutil.WithURLParams(r, "runtimeId", runtimeID)
	}()).Want(http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != `{"task":null}` {
		t.Fatalf("post-settle poll body = %q, want empty successful poll", w.Body.String())
	}
}

// PUCK-89 blocker 1: a public runtime may keep delivering after its owner
// changes, but the task token must identify the owner from the locked runtime
// row rather than the claim-time snapshot.
func TestFinalizeClaimDelivery_UsesCurrentRuntimeOwnerForTaskToken(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	newOwnerID := dbfx.User(t, "Public delivery owner", "public-delivery-owner-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, newOwnerID, "member")
	runtimeID := createClaimReclaimRuntime(t, ctx, "Public delivery owner runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Public delivery owner agent")
	taskID := seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

	task, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(runtimeID))
	if err != nil || task == nil {
		t.Fatalf("claim task: task=%v err=%v", task, err)
	}
	runtimeSnapshot, err := testHandler.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
		ID:          parseUUID(runtimeID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load claim-time runtime: %v", err)
	}
	if !runtimeSnapshot.OwnerID.Valid {
		t.Fatal("claim-time runtime owner unexpectedly missing")
	}
	claimOwner, err := testHandler.Queries.GetUser(ctx, runtimeSnapshot.OwnerID)
	if err != nil {
		t.Fatalf("load claim-time runtime owner: %v", err)
	}
	const staleProfile = "stale claim-time owner profile"
	const currentProfile = "current delivery owner profile"
	dbfx.Exec(t, `UPDATE "user" SET profile_description = $1 WHERE id = $2`, staleProfile, runtimeSnapshot.OwnerID)
	dbfx.Exec(t, `UPDATE "user" SET profile_description = $1 WHERE id = $2`, currentProfile, newOwnerID)
	dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public', owner_id = $1 WHERE id = $2`, newOwnerID, runtimeID)

	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "public-delivery-owner-change")
	resp, _, _, _, failure, finalizeErr := testHandler.finalizeClaimDeliveryForTestWithRuntime(
		req, task, runtimeSnapshot, runtimeID, testWorkspaceID,
	)
	if finalizeErr != nil {
		t.Fatalf("finalize delivery: %v", finalizeErr)
	}
	if failure != nil {
		t.Fatalf("delivery failure = %+v, want successful public delivery", failure)
	}
	if resp.AuthToken == "" {
		t.Fatal("successful delivery did not return a task token")
	}

	var tokenOwnerID string
	dbfx.QueryRow(t, `SELECT user_id::text FROM task_token WHERE task_id = $1`, taskID).Scan(&tokenOwnerID)
	if tokenOwnerID != newOwnerID {
		t.Fatalf("task token user_id = %q, want current runtime owner %q", tokenOwnerID, newOwnerID)
	}
	currentOwner, err := testHandler.Queries.GetUser(ctx, parseUUID(newOwnerID))
	if err != nil {
		t.Fatalf("load current runtime owner: %v", err)
	}
	if resp.RequestingUserName != currentOwner.Name || resp.RequestingUserProfileDescription != currentProfile {
		t.Fatalf("requesting user = %q/%q, want current owner %q/%q", resp.RequestingUserName, resp.RequestingUserProfileDescription, currentOwner.Name, currentProfile)
	}
	if resp.RequestingUserName == claimOwner.Name || strings.Contains(resp.RequestingUserProfileDescription, staleProfile) {
		t.Fatalf("requesting user leaked stale claim-time owner %q/%q", claimOwner.Name, staleProfile)
	}
}

// PUCK-89 blocker 1: if the locked runtime loses its owner after claim, the
// stale claim-time owner must never be used to mint a task credential.
func TestFinalizeClaimDelivery_CurrentRuntimeOwnerMissingNeverMintsToken(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Ownerless delivery runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Ownerless delivery agent")
	taskID := seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

	task, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(runtimeID))
	if err != nil || task == nil {
		t.Fatalf("claim task: task=%v err=%v", task, err)
	}
	runtimeSnapshot, err := testHandler.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
		ID:          parseUUID(runtimeID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load claim-time runtime: %v", err)
	}
	if !runtimeSnapshot.OwnerID.Valid {
		t.Fatal("claim-time runtime owner unexpectedly missing")
	}
	dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public', owner_id = NULL WHERE id = $1`, runtimeID)

	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "ownerless-delivery")
	resp, _, _, _, failure, finalizeErr := testHandler.finalizeClaimDeliveryForTestWithRuntime(
		req, task, runtimeSnapshot, runtimeID, testWorkspaceID,
	)
	if finalizeErr != nil {
		t.Fatalf("finalize delivery: %v", finalizeErr)
	}
	if failure == nil || !failure.settled {
		t.Fatalf("delivery failure = %+v, want settled ownerless-runtime failure", failure)
	}
	if resp.AuthToken != "" || resp.RemoteMCPDaemonToken != "" {
		t.Fatalf("ownerless delivery returned credentials %q/%q", resp.AuthToken, resp.RemoteMCPDaemonToken)
	}
	var tokenCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM task_token WHERE task_id = $1`, taskID).Scan(&tokenCount)
	if tokenCount != 0 {
		t.Fatalf("task token count = %d, want 0", tokenCount)
	}
	var status, errorMessage string
	dbfx.QueryRow(t, `SELECT status, error FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &errorMessage)
	if status != "failed" || !strings.Contains(errorMessage, "runtime has no owner") {
		t.Fatalf("task state = %q/%q, want failed ownerless-runtime settlement", status, errorMessage)
	}
}

func injectFailTaskSettlementFailure(t *testing.T, ctx context.Context, taskID string) {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	functionName := "puck89_fail_task_" + suffix
	triggerName := "puck89_fail_task_trg_" + suffix
	t.Cleanup(func() {
		testPool.Exec(ctx, fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON agent_task_queue", triggerName))
		testPool.Exec(ctx, fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	})
	if _, err := testPool.Exec(ctx, fmt.Sprintf(`
CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
	IF NEW.status = 'failed' THEN
		RAISE EXCEPTION 'injected FailTask settlement failure';
	END IF;
	RETURN NEW;
END
$fn$;
`, functionName)); err != nil {
		t.Fatalf("create FailTask fault function: %v", err)
	}
	if _, err := testPool.Exec(ctx, fmt.Sprintf(`
CREATE TRIGGER %s
BEFORE UPDATE ON agent_task_queue
FOR EACH ROW WHEN (OLD.id = '%s'::uuid AND NEW.status = 'failed')
EXECUTE FUNCTION %s();
`, triggerName, taskID, functionName)); err != nil {
		t.Fatalf("create FailTask fault trigger: %v", err)
	}
}

func injectRuntimeOwnerFlipAfterClaim(t *testing.T, ctx context.Context, taskID, runtimeID, ownerID string) {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	functionName := "puck89_flip_runtime_owner_" + suffix
	triggerName := "puck89_flip_runtime_owner_trg_" + suffix
	t.Cleanup(func() {
		testPool.Exec(ctx, fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON agent_task_queue", triggerName))
		testPool.Exec(ctx, fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	})
	if _, err := testPool.Exec(ctx, fmt.Sprintf(`
CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
	UPDATE agent_runtime SET visibility = 'private', owner_id = '%s'::uuid WHERE id = '%s'::uuid;
	RETURN NEW;
END
$fn$;
`, functionName, ownerID, runtimeID)); err != nil {
		t.Fatalf("create runtime-owner flip function: %v", err)
	}
	if _, err := testPool.Exec(ctx, fmt.Sprintf(`
CREATE TRIGGER %s
AFTER UPDATE ON agent_task_queue
FOR EACH ROW WHEN (OLD.id = '%s'::uuid AND OLD.status = 'queued' AND NEW.status = 'dispatched')
EXECUTE FUNCTION %s();
`, triggerName, taskID, functionName)); err != nil {
		t.Fatalf("create runtime-owner flip trigger: %v", err)
	}
}

// PUCK-89 blocker 2: a final authorization rejection whose FailTask
// settlement fails is an HTTP error, not a successful empty poll. The exact
// claim is requeued and no token is inserted.
func TestFinalizeClaimDelivery_SettlementFailureIsUnsettled(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	foreignOwnerID := dbfx.User(t, "Settlement failure owner", "settlement-failure-owner-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, foreignOwnerID, "member")
	runtimeID := createClaimReclaimRuntime(t, ctx, "Settlement failure runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Settlement failure agent")
	taskID := seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)
	injectRuntimeOwnerFlipAfterClaim(t, ctx, taskID, runtimeID, foreignOwnerID)
	injectFailTaskSettlementFailure(t, ctx, taskID)

	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "settlement-failure")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusInternalServerError)
	if strings.TrimSpace(w.Text()) == `{"task":null}` {
		t.Fatal("settlement failure was hidden as a successful empty poll")
	}
	var tokenCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM task_token WHERE task_id = $1`, taskID).Scan(&tokenCount)
	if tokenCount != 0 {
		t.Fatalf("task token count = %d, want 0", tokenCount)
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if status != "queued" {
		t.Fatalf("task status = %q, want queued after settlement failure requeue", status)
	}
}

// PUCK-89 blocker 1 (batch): a task whose runtime owner changed after the
// claim-time snapshot must never appear in the batch response; the mismatch is
// settled and a valid task in the same batch still returns.
func TestClaimTasksByRuntime_OwnerChangedAfterSnapshotSettlesMismatchReturnsValid(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	newOwnerID := dbfx.User(t, "Batch delivery gate owner", "batch-delivery-gate-owner-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, newOwnerID, "member")
	runtimeID := createClaimReclaimRuntime(t, ctx, "Batch delivery gate runtime")
	mismatchAgentID, mismatchIssueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Batch delivery gate mismatch agent")
	validAgentID, validIssueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Batch delivery gate valid agent")
	mismatchTaskID := seedQueuedIssueTask(t, ctx, mismatchAgentID, runtimeID, mismatchIssueID)
	validTaskID := seedQueuedIssueTask(t, ctx, validAgentID, runtimeID, validIssueID)

	// The batch handler resolves runtime snapshots before claiming. Start with a
	// public runtime so both agents are claimable, then flip its visibility and
	// owner after the mismatch task is claimed. The valid agent already belongs
	// to the new owner, so only the stale-snapshot task is rejected at the gate.
	dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public' WHERE id = $1`, runtimeID)
	dbfx.Exec(t, `UPDATE agent SET owner_id = $1 WHERE id = $2`, newOwnerID, validAgentID)
	dbfx.Exec(t, `UPDATE agent_task_queue SET priority = 1 WHERE id = $1`, mismatchTaskID)
	injectRuntimeOwnerFlipAfterClaim(t, ctx, mismatchTaskID, runtimeID, newOwnerID)
	runtimeSnapshot, err := testHandler.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
		ID:          parseUUID(runtimeID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load batch claim-time runtime: %v", err)
	}
	if runtimeSnapshot.Visibility != "public" || !runtimeSnapshot.OwnerID.Valid {
		t.Fatalf("claim-time runtime = %+v, want public runtime with owner", runtimeSnapshot)
	}

	w := postBatchClaim(t, testWorkspaceID, []string{runtimeID}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("batch claim status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp batchClaimResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode batch claim: %v", err)
	}
	for _, got := range resp.Tasks {
		if got.ID == mismatchTaskID {
			t.Fatalf("batch returned the mismatched task %s", mismatchTaskID)
		}
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].ID != validTaskID {
		t.Fatalf("batch tasks = %+v, want only valid task %s", resp.Tasks, validTaskID)
	}

	var mismatchStatus, mismatchReason, validStatus string
	dbfx.QueryRow(t, `SELECT status, failure_reason FROM agent_task_queue WHERE id = $1`, mismatchTaskID).Scan(&mismatchStatus, &mismatchReason)
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, validTaskID).Scan(&validStatus)
	if mismatchStatus != "failed" || mismatchReason != taskfailure.ReasonRuntimeAccessDenied.String() {
		t.Fatalf("mismatch task state = %q/%q, want failed/%s", mismatchStatus, mismatchReason, taskfailure.ReasonRuntimeAccessDenied)
	}
	if validStatus != "dispatched" {
		t.Fatalf("valid task status = %q, want dispatched", validStatus)
	}
}
