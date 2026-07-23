package loops

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const busyWakeupPromptPrefix = "Resume the waiting Issue workflow chain after agents were busy.\n\nworkflow-chain:"

type chainTaskEnvelope struct {
	Type     string `json:"type"`
	Prompt   string `json:"prompt"`
	TargetID string `json:"workflow_target_issue_id"`
	AgentID  string `json:"agent_id"`
	Step     struct {
		WorkflowID string `json:"workflow_id"`
		PhaseID    string `json:"phase_id"`
		BlockID    string `json:"block_id"`
		Number     int    `json:"step_number"`
	} `json:"loop_step"`
}

// BusyWakeupPrompt carries the durable chain identity through the existing
// wakeup service so the fired wakeup can re-enter ChainDriver.
func BusyWakeupPrompt(d BlockDispatch) string {
	envelope := chainTaskEnvelope{Type: "workflow_chain_wakeup", TargetID: util.UUIDToString(d.Run.IssueID), AgentID: d.Run.AgentID}
	envelope.Step.WorkflowID = util.UUIDToString(d.Run.WorkflowID)
	envelope.Step.PhaseID = d.Phase.ID
	envelope.Step.BlockID = d.Block.ID
	envelope.Step.Number = int(d.Step.Number)
	raw, _ := json.Marshal(envelope)
	return busyWakeupPromptPrefix + string(raw)
}

func decodeChainTaskEnvelope(raw json.RawMessage) (chainTaskEnvelope, bool) {
	var envelope chainTaskEnvelope
	if json.Unmarshal(raw, &envelope) != nil {
		return chainTaskEnvelope{}, false
	}
	if envelope.Type == "wakeup" && strings.HasPrefix(envelope.Prompt, busyWakeupPromptPrefix) {
		if json.Unmarshal([]byte(strings.TrimPrefix(envelope.Prompt, busyWakeupPromptPrefix)), &envelope) != nil {
			return chainTaskEnvelope{}, false
		}
	}
	return envelope, envelope.Type == "workflow_block" || envelope.Type == "workflow_chain_wakeup"
}

type taskCompletionAdvancer interface {
	AdvanceOnComplete(context.Context, db.AgentTaskQueue)
}

type ChainTaskAdvancer struct {
	bridge   *IssueLoopBridge
	fallback taskCompletionAdvancer
}

func NewChainTaskAdvancer(bridge *IssueLoopBridge, fallback taskCompletionAdvancer) *ChainTaskAdvancer {
	return &ChainTaskAdvancer{bridge: bridge, fallback: fallback}
}

func (a *ChainTaskAdvancer) AdvanceOnComplete(ctx context.Context, task db.AgentTaskQueue) {
	envelope, ok := decodeChainTaskEnvelope(task.Context)
	if a == nil || a.bridge == nil || !ok {
		if a != nil && a.fallback != nil {
			a.fallback.AdvanceOnComplete(ctx, task)
		}
		return
	}
	workflowID, werr := parseChainUUID(envelope.Step.WorkflowID)
	issueID, ierr := parseChainUUID(envelope.TargetID)
	if werr != nil || ierr != nil {
		return
	}
	if envelope.Type == "workflow_block" {
		ref := StepRef{PhaseRunKey: PhaseRunKey{IssueID: issueID, WorkflowID: workflowID, PhaseID: envelope.Step.PhaseID}, BlockID: envelope.Step.BlockID, Number: int32(envelope.Step.Number)}
		outcome := json.RawMessage(`{"completed":true}`)
		if len(task.Result) > 0 && json.Valid(task.Result) {
			outcome = task.Result
		}
		if err := NewStore(a.bridge.pool).RecordStepOutcome(ctx, ref, StepCompleted, outcome); err != nil {
			return
		}
	}
	fields, err := a.bridge.columns.Get(ctx, workflowID)
	if err != nil {
		return
	}
	chain, err := decodeChain(fields.LoopSpec)
	if err != nil {
		return
	}
	_ = a.bridge.advanceChain(ctx, workflowID, issueID, chain)
}

func parseChainUUID(value string) (pgtype.UUID, error) {
	return util.ParseUUID(value)
}
