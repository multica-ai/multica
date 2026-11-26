package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RetiredMemberAssets is what RetireMemberAssets changed, so the caller can
// publish events once the transaction commits.
type RetiredMemberAssets struct {
	ArchivedAgents    []db.Agent
	CancelledTasks    []db.AgentTaskQueue
	TransferredAgents []db.Agent
	PausedAutopilots  []db.Autopilot
	DisabledWakeups   int64
	EndedChatWakeups  int64
}

// IsEmpty reports whether nothing was changed.
func (r RetiredMemberAssets) IsEmpty() bool {
	return len(r.ArchivedAgents) == 0 && len(r.CancelledTasks) == 0 && len(r.TransferredAgents) == 0 &&
		len(r.PausedAutopilots) == 0 && r.DisabledWakeups == 0 && r.EndedChatWakeups == 0
}

// RetireMemberAssets stops everything a departing member left running in a
// workspace beyond their own runtimes (MUL-10284): the agents they own wherever
// they run, the tasks they started on any runtime, the autopilots they created,
// and their issue and chat wakeups. Their product agents (Mika) are handed to
// heir instead of archived; an invalid heir leaves them in place.
//
// It runs inside the caller's transaction: the member-removal flow, and the
// backfill for members removed before this existed. Archiving and pausing are
// reversible, so an admin can take over anything worth keeping.
func RetireMemberAssets(ctx context.Context, qtx *db.Queries, workspaceID, userID, archivedBy, heir pgtype.UUID) (RetiredMemberAssets, error) {
	var out RetiredMemberAssets
	var err error

	out.ArchivedAgents, err = qtx.ArchiveAgentsByOwner(ctx, db.ArchiveAgentsByOwnerParams{
		ArchivedBy:  archivedBy,
		WorkspaceID: workspaceID,
		OwnerID:     userID,
	})
	if err != nil {
		return out, fmt.Errorf("archive owned agents: %w", err)
	}
	if len(out.ArchivedAgents) > 0 {
		agentIDs := make([]pgtype.UUID, len(out.ArchivedAgents))
		for i, a := range out.ArchivedAgents {
			agentIDs[i] = a.ID
		}
		cancelled, err := qtx.CancelAgentTasksByRuntimeOrAgent(ctx, db.CancelAgentTasksByRuntimeOrAgentParams{
			RuntimeIds: []pgtype.UUID{},
			AgentIds:   agentIDs,
		})
		if err != nil {
			return out, fmt.Errorf("cancel owned agent tasks: %w", err)
		}
		out.CancelledTasks = append(out.CancelledTasks, cancelled...)
	}

	started, err := qtx.CancelAgentTasksStartedByMember(ctx, db.CancelAgentTasksStartedByMemberParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		return out, fmt.Errorf("cancel member-started tasks: %w", err)
	}
	out.CancelledTasks = append(out.CancelledTasks, started...)

	if len(out.CancelledTasks) > 0 {
		cancelledIDs := make([]pgtype.UUID, len(out.CancelledTasks))
		for i, task := range out.CancelledTasks {
			cancelledIDs[i] = task.ID
		}
		if err := qtx.WithdrawChatTaskSupplementsByTasks(ctx, cancelledIDs); err != nil {
			return out, fmt.Errorf("withdraw chat supplements: %w", err)
		}
		if err := SettleTerminalTaskState(ctx, qtx, out.CancelledTasks...); err != nil {
			return out, fmt.Errorf("settle cancelled tasks: %w", err)
		}
	}

	if heir.Valid && heir != userID {
		out.TransferredAgents, err = qtx.TransferProductAgentsOwner(ctx, db.TransferProductAgentsOwnerParams{
			NewOwnerID:  heir,
			WorkspaceID: workspaceID,
			OwnerID:     userID,
		})
		if err != nil {
			return out, fmt.Errorf("transfer product agents: %w", err)
		}
	}

	out.PausedAutopilots, err = qtx.PauseAutopilotsByCreator(ctx, db.PauseAutopilotsByCreatorParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		return out, fmt.Errorf("pause created autopilots: %w", err)
	}

	out.DisabledWakeups, err = qtx.DisableIssueWakeupsByCreator(ctx, db.DisableIssueWakeupsByCreatorParams{
		WorkspaceID: workspaceID,
		CreatedBy:   userID,
	})
	if err != nil {
		return out, fmt.Errorf("disable issue wakeups: %w", err)
	}
	out.EndedChatWakeups, err = qtx.EndActiveChatWakeupsForRequester(ctx, db.EndActiveChatWakeupsForRequesterParams{
		WorkspaceID: workspaceID,
		RequesterID: userID,
	})
	if err != nil {
		return out, fmt.Errorf("end chat wakeups: %w", err)
	}
	return out, nil
}

// MemberAssetHeir picks who inherits a departing member's product agents: the
// member who removed them, or, when they left on their own (or the actor is
// gone), the longest-standing other owner. Returns an invalid UUID when no
// such member exists.
func MemberAssetHeir(ctx context.Context, qtx *db.Queries, workspaceID, departing, actor pgtype.UUID) (pgtype.UUID, error) {
	members, err := qtx.ListMembers(ctx, workspaceID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("list members: %w", err)
	}
	if actor.Valid && actor != departing {
		for _, m := range members {
			if m.UserID == actor && (m.Role == "owner" || m.Role == "admin") {
				return actor, nil
			}
		}
	}
	for _, m := range members {
		if m.Role == "owner" && m.UserID != departing {
			return m.UserID, nil
		}
	}
	return pgtype.UUID{}, nil
}
