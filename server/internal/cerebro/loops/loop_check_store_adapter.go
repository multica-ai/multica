package loops

// loop_check_store_adapter.go implements workflows.LoopCheckStore over the
// existing Store, the same structural-implementation inversion check_gate.go
// uses for GateEvaluator: workflows declares the interface it needs
// (issue_loop_state.go), this file supplies it without workflows importing
// loops.

import (
	"context"
	"fmt"

	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
	"github.com/multica-ai/multica/server/internal/util"
)

// LoopCheckStoreAdapter implements workflows.LoopCheckStore over a Store.
type LoopCheckStoreAdapter struct {
	store *Store
}

// NewLoopCheckStoreAdapter builds a LoopCheckStoreAdapter over the given
// store.
func NewLoopCheckStoreAdapter(store *Store) *LoopCheckStoreAdapter {
	return &LoopCheckStoreAdapter{store: store}
}

func (a *LoopCheckStoreAdapter) GateState(ctx context.Context, issueID, gate string) (workflows.LoopGateState, error) {
	iid, err := util.ParseUUID(issueID)
	if err != nil {
		return workflows.LoopGateState{}, fmt.Errorf("loop check store: parse issue id: %w", err)
	}
	st, err := a.store.LoadGateState(ctx, iid, gate)
	if err != nil {
		return workflows.LoopGateState{}, err
	}
	return workflows.LoopGateState{Round: st.Round, Stopped: st.Stopped, StopReason: st.StopReason}, nil
}

func (a *LoopCheckStoreAdapter) PendingHumanChecks(ctx context.Context, issueID, gate string) ([]workflows.PendingHumanCheck, error) {
	iid, err := util.ParseUUID(issueID)
	if err != nil {
		return nil, fmt.Errorf("loop check store: parse issue id: %w", err)
	}
	st, err := a.store.LoadGateState(ctx, iid, gate)
	if err != nil {
		return nil, err
	}
	pending, err := a.store.PendingApprovals(ctx, iid, gate, st.Round)
	if err != nil {
		return nil, err
	}
	out := make([]workflows.PendingHumanCheck, 0, len(pending))
	for _, c := range pending {
		out = append(out, workflows.PendingHumanCheck{
			CheckID:      c.ID,
			Prompt:       c.Prompt,
			AssigneeType: c.AssigneeType,
			AssigneeID:   c.AssigneeID,
		})
	}
	return out, nil
}

func (a *LoopCheckStoreAdapter) ApproveHumanCheck(ctx context.Context, issueID, gate, checkID string, approved bool, note, approverID, approverType string) error {
	iid, err := util.ParseUUID(issueID)
	if err != nil {
		return fmt.Errorf("loop check store: parse issue id: %w", err)
	}
	st, err := a.store.LoadGateState(ctx, iid, gate)
	if err != nil {
		return err
	}
	approver, err := stringToUUID(approverID)
	if err != nil {
		return fmt.Errorf("loop check store: parse approver id: %w", err)
	}
	return a.store.ReportHuman(ctx, iid, gate, st.Round, checkID, approved, note, approver, approverType)
}
