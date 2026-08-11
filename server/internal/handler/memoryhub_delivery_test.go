package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// seedMemoryHubReadyTask seeds a queued MemoryHub task whose claim gate is
// ready (bound binding + active secret + docket + attachment ref) and returns
// its id plus the runtime id.
func seedMemoryHubReadyTask(t *testing.T, ctx context.Context) (taskID, runtimeID string) {
	t.Helper()
	runtimeID = createClaimReclaimRuntime(t, ctx, "memoryhub-ready")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "memoryhub-agent")

	execID := uuid.New().String()
	runID := "run-" + uuid.New().String()
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			execution_id, memoryhub_run_id, memory_policy, review_policy
		)
		VALUES ($1, $2, $3, 'queued', 0, $4, $5, 'required', 'none')
		RETURNING id
	`, agentID, runtimeID, issueID, execID, runID).Scan(&taskID); err != nil {
		t.Fatalf("seed ready task: %v", err)
	}

	// Gate-ready prerequisites.
	bindingID := uuid.New().String()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO memoryhub_binding (
			id, workspace_id, scope_kind, scope_id, subject_type, subject_id,
			status, version, idempotency_key, remote_name
		)
		VALUES ($1, $2, 'workspace', NULL, 'issue', $3, 'bound', 1, $4, 'remote')
	`, bindingID, testWorkspaceID, issueID, "idem-"+bindingID); err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM memoryhub_binding WHERE id = $1`, bindingID) })

	secretID := uuid.New().String()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO memoryhub_secret (
			id, workspace_id, credential_ref, kind, envelope_version, key_id,
			nonce, ciphertext, aad, user_key_hash, state, state_version
		)
		VALUES ($1, $2, $3, 'user_key', 1, 'k1', decode('00','hex'), decode('00','hex'), 'aad', NULL, 'active', 1)
	`, secretID, testWorkspaceID, testWorkspaceID); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM memoryhub_secret WHERE id = $1`, secretID) })

	docketID := uuid.New().String()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO memoryhub_memory_docket (
			id, workspace_id, scope_kind, scope_id, subject_type, subject_id, revision
		)
		VALUES ($1, $2, 'workspace', NULL, 'issue', $3, 1)
	`, docketID, testWorkspaceID, issueID); err != nil {
		t.Fatalf("seed docket: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM memoryhub_memory_docket WHERE id = $1`, docketID) })
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue SET memory_attachment_ref = 'attachment://' || $2 WHERE id = $1
	`, taskID, uuid.New().String()); err != nil {
		t.Fatalf("stamp attachment ref: %v", err)
	}

	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID, runtimeID
}

// TestClaimCarriesMemoryHubPreparation is the T10 delivery-chain test: a
// MemoryHub execution claimed through the daemon endpoint must carry the
// refs-only MemoryHubClaimPreparation (execution identity + attachment refs)
// in the claim response.
//
// Before the T10 wiring, buildClaimedTaskResponse did not attach the
// preparation and the field was absent (fail). After the wiring, the claimed
// response includes memoryhub.execution_identity.execution_id (pass).
func TestClaimCarriesMemoryHubPreparation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	taskID, runtimeID := seedMemoryHubReadyTask(t, ctx)

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/claim",
		map[string]any{"daemon_id": "t10-delivery", "runtime_ids": []string{runtimeID}, "max_tasks": 1},
		testWorkspaceID, "t10-delivery")
	rctx := chi.NewRouteContext()
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.ClaimTasksByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTasksByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var envelope struct {
		Tasks []struct {
			ID        string                                `json:"id"`
			MemoryHub *protocol.MemoryHubClaimPreparation   `json:"memoryhub"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal claim response: %v", err)
	}
	var found bool
	for _, tk := range envelope.Tasks {
		if tk.ID != taskID {
			continue
		}
		found = true
		if tk.MemoryHub == nil {
			t.Fatalf("task %s claim response carries no memoryhub preparation: T10 delivery chain did not attach it", taskID)
		}
		if tk.MemoryHub.ExecutionIdentity.ExecutionID == "" {
			t.Fatal("memoryhub.execution_identity.execution_id is empty")
		}
		if tk.MemoryHub.ExecutionIdentity.TaskID != taskID {
			t.Fatalf("execution_identity.task_id = %s, want %s", tk.MemoryHub.ExecutionIdentity.TaskID, taskID)
		}
	}
	if !found {
		t.Fatalf("claimed task %s not present in response (tasks=%d)", taskID, len(envelope.Tasks))
	}
}

var _ = util.UUIDToString
var _ = pgtype.UUID{}
