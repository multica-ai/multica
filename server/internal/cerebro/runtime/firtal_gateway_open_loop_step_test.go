package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebroloops "github.com/multica-ai/multica/server/internal/cerebro/loops"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestLoopStepCapabilityComesOnlyFromTrustedTaskContext(t *testing.T) {
	issueID := openStepTestUUID(t, "11111111-1111-1111-1111-111111111111")
	task := db.AgentTaskQueue{
		IssueID: issueID,
		Context: []byte(`{
			"type":"workflow_block",
			"loop_step":{
				"workflow_id":"22222222-2222-2222-2222-222222222222",
				"phase_id":"delivery",
				"block_id":"build",
				"step_number":2,
				"steps":{"allowed":true,"max":3},
				"phase_limits":{"max_steps":5,"max_rounds":2,"no_progress_stalls":2}
			}
		}`),
	}

	capability := loopStepCapabilityFromTask(task)
	if capability == nil {
		t.Fatal("steps-enabled task did not receive open-step capability")
	}
	if capability.Current.IssueID != issueID || capability.Current.PhaseID != "delivery" ||
		capability.Current.BlockID != "build" || capability.Current.Number != 2 {
		t.Fatalf("wrong pinned current step: %+v", capability.Current)
	}
	if capability.Steps.Max != 3 || capability.Limits.MaxSteps != 5 {
		t.Fatalf("trusted limits were not carried through: %+v", capability)
	}

	task.Context = []byte(`{"loop_step":{"steps":{"allowed":false,"max":3}}}`)
	if capability := loopStepCapabilityFromTask(task); capability != nil {
		t.Fatalf("non-steps task received capability: %+v", capability)
	}
}

func TestRunToolLoopFinalizesTaskMandateAfterAddingOpenLoopStep(t *testing.T) {
	offeredResult := make(chan []string, 1)
	requestErrors := make(chan error, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []GatewayToolDef `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			requestErrors <- err
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		offered := make([]string, 0, len(request.Tools))
		for _, tool := range request.Tools {
			offered = append(offered, tool.Function.Name)
		}
		offeredResult <- offered
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"done"}}]}`))
	}))
	defer srv.Close()

	executor, agentID := newToolPolicyGatedExecutor(t, &gateFakeApprovals{})
	setAgentToolPolicy(t, agentID, "get_issue", toolpolicy.SettingAllow)
	executor.gateway = NewGatewayClient(FirtalGatewayRuntimeConfig{
		BaseURL: srv.URL, APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096,
	}, srv.Client())
	executor.logger = testLogger()
	executor.registry = NewRegistry(runtimeAccountTestPool)
	executor.SetLoopStore(cerebroloops.NewStore(runtimeAccountTestPool))
	mandates := &captureTaskMandates{}
	executor.SetTaskMandates(mandates)

	ctx := context.Background()
	workspaceFlags, err := executor.cerebro.ListCerebroWorkspaceFeatureFlags(ctx, runtimeAccountTestWSID)
	if err != nil {
		t.Fatalf("list workspace flags: %v", err)
	}
	var previousMemoryFlag *cerebrodb.ListCerebroWorkspaceFeatureFlagsRow
	for i := range workspaceFlags {
		if workspaceFlags[i].FlagKey == cerebroMemoryFlagKey {
			previous := workspaceFlags[i]
			previousMemoryFlag = &previous
			break
		}
	}
	if err := executor.cerebro.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: runtimeAccountTestWSID,
		FlagKey:     cerebroMemoryFlagKey,
		Enabled:     true,
		Locked:      false,
	}); err != nil {
		t.Fatalf("enable memory tools: %v", err)
	}
	t.Cleanup(func() {
		if previousMemoryFlag != nil {
			_ = executor.cerebro.UpsertCerebroWorkspaceFeatureFlag(context.Background(), cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
				WorkspaceID: runtimeAccountTestWSID,
				FlagKey:     cerebroMemoryFlagKey,
				Enabled:     previousMemoryFlag.Enabled,
				Locked:      previousMemoryFlag.Locked,
			})
			return
		}
		_ = executor.cerebro.DeleteCerebroWorkspaceFeatureFlag(context.Background(), cerebrodb.DeleteCerebroWorkspaceFeatureFlagParams{
			WorkspaceID: runtimeAccountTestWSID,
			FlagKey:     cerebroMemoryFlagKey,
		})
	})

	var issueID pgtype.UUID
	if err := runtimeAccountTestPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'Gateway mandate finalization probe', 'member', $2)
		RETURNING id
	`, runtimeAccountTestWSID, runtimeAccountTestUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	workflowID := openStepTestUUID(t, "22222222-2222-2222-2222-222222222222")
	taskContext, err := json.Marshal(map[string]any{
		"type": "workflow_block",
		"loop_step": map[string]any{
			"workflow_id": util.UUIDToString(workflowID),
			"phase_id":    "delivery", "block_id": "build", "step_number": 1,
			"steps":        map[string]any{"allowed": true, "max": 2},
			"phase_limits": map[string]any{"max_steps": 3, "max_rounds": 1, "no_progress_stalls": 1},
		},
	})
	if err != nil {
		t.Fatalf("marshal task context: %v", err)
	}
	var taskID pgtype.UUID
	if err := runtimeAccountTestPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, context)
		VALUES ($1, $2, $3, 'queued', 0, $4)
		RETURNING id
	`, agentID, runtimeAccountTestRuntimeID, issueID, taskContext).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = runtimeAccountTestPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		_, _ = runtimeAccountTestPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	agent, err := executor.queries.GetAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if _, err := executor.runToolLoop(
		ctx,
		FirtalGatewayRuntimeConfig{BaseURL: srv.URL, APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096},
		agent,
		[]GatewayMessage{{Role: "user", Content: "continue"}},
		GatewayRequestMeta{TaskID: util.UUIDToString(taskID)},
		agentID,
		runtimeAccountTestWSID,
		runtimeAccountTestUserID,
		false,
	); err != nil {
		t.Fatalf("run tool loop: %v", err)
	}
	select {
	case err := <-requestErrors:
		t.Fatalf("decode gateway request: %v", err)
	default:
	}
	offered := <-offeredResult

	if !containsString(offered, "open_loop_step") {
		t.Fatalf("offered tools = %v, want open_loop_step", offered)
	}
	if !equalStrings(offered, mandates.issued) {
		t.Fatalf("Task Mandate = %v, want exact offered tools %v", mandates.issued, offered)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func openStepTestUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		t.Fatalf("parse UUID: %v", err)
	}
	return id
}
