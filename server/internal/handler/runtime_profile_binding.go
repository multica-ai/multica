package handler

import (
	"context"
	"errors"
	"fmt"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	errProfileMachineUnavailable = errors.New("select a registered local machine")
	errProfileMachineImmutable   = errors.New("this profile already belongs to another machine; create a new profile to use it there")
	errProfileMachineInUse       = errors.New("move or archive agents on other machines and finish their pending tasks before binding this profile")
)

type profileMachineBindingResult struct {
	profile   db.RuntimeProfile
	removed   []db.AgentRuntime
	teardowns []service.RuntimeTeardownResult
}

// Binding and retirement are atomic. The profile lock also prevents a stale
// daemon from recreating a removed instance before the binding commits.
func (h *Handler) bindRuntimeProfileMachine(ctx context.Context, params db.UpdateRuntimeProfileParams) (profileMachineBindingResult, error) {
	var result profileMachineBindingResult
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	profile, err := qtx.LockRuntimeProfileForDelete(ctx, db.LockRuntimeProfileForDeleteParams{ID: params.ID, WorkspaceID: params.WorkspaceID})
	if err != nil {
		return result, err
	}
	if profile.DaemonID.Valid && profile.DaemonID != params.DaemonID {
		return result, errProfileMachineImmutable
	}
	machines, err := qtx.ListRuntimeProfileMachines(ctx, db.ListRuntimeProfileMachinesParams{WorkspaceID: params.WorkspaceID, DaemonID: params.DaemonID})
	if err != nil {
		return result, err
	}
	if len(machines) != 1 {
		return result, errProfileMachineUnavailable
	}
	ids, err := qtx.ListAgentRuntimeIDsByProfile(ctx, db.ListAgentRuntimeIDsByProfileParams{ProfileID: params.ID, WorkspaceID: params.WorkspaceID})
	if err != nil {
		return result, err
	}
	for _, id := range ids {
		rt, err := qtx.GetAgentRuntime(ctx, id)
		if err != nil {
			return result, err
		}
		if rt.DaemonID == params.DaemonID {
			continue
		}
		agents, err := qtx.ListUserAgentsByRuntimeForUpdate(ctx, id)
		if err != nil {
			return result, err
		}
		for _, agent := range agents {
			if !agent.ArchivedAt.Valid {
				return result, fmt.Errorf("%w (%s)", errProfileMachineInUse, rt.Name)
			}
		}
		teardown, err := service.TeardownRuntime(ctx, qtx, id, service.RuntimeTeardownOptions{CancelNonTerminalTasks: false})
		if errors.Is(err, service.ErrRuntimeNotDrained) || errors.Is(err, service.ErrRuntimeWorkspaceMismatch) {
			return result, fmt.Errorf("%w (%s)", errProfileMachineInUse, rt.Name)
		}
		if err != nil {
			return result, err
		}
		if err := qtx.DeleteAgentRuntime(ctx, id); err != nil {
			return result, err
		}
		result.removed = append(result.removed, rt)
		result.teardowns = append(result.teardowns, teardown)
	}
	result.profile, err = qtx.UpdateRuntimeProfile(ctx, params)
	if err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	return result, nil
}
