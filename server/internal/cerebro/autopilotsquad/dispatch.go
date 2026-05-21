package autopilotsquad

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type DispatchStore interface {
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
	GetSquad(ctx context.Context, id pgtype.UUID) (db.Squad, error)
}

type DispatchAssignee struct {
	IssueAssigneeType string
	IssueAssigneeID   pgtype.UUID
	Agent             db.Agent
	SquadID           pgtype.UUID
}

func ResolveDispatchAssignee(ctx context.Context, store DispatchStore, autopilot db.Autopilot) (DispatchAssignee, error) {
	if !autopilot.AssigneeID.Valid {
		return DispatchAssignee{}, fmt.Errorf("autopilot has no assignee")
	}

	switch autopilot.AssigneeType {
	case "agent":
		agent, err := store.GetAgent(ctx, autopilot.AssigneeID)
		if err != nil {
			return DispatchAssignee{}, fmt.Errorf("assignee agent not found: %w", err)
		}
		return DispatchAssignee{
			IssueAssigneeType: "agent",
			IssueAssigneeID:   autopilot.AssigneeID,
			Agent:             agent,
		}, nil
	case "squad":
		squad, err := store.GetSquad(ctx, autopilot.AssigneeID)
		if err != nil {
			return DispatchAssignee{}, fmt.Errorf("assignee squad not found: %w", err)
		}
		if squad.ArchivedAt.Valid {
			return DispatchAssignee{}, fmt.Errorf("assignee squad is archived")
		}
		leader, err := store.GetAgent(ctx, squad.LeaderID)
		if err != nil {
			return DispatchAssignee{}, fmt.Errorf("squad leader agent not found: %w", err)
		}
		return DispatchAssignee{
			IssueAssigneeType: "squad",
			IssueAssigneeID:   autopilot.AssigneeID,
			Agent:             leader,
			SquadID:           autopilot.AssigneeID,
		}, nil
	default:
		return DispatchAssignee{}, fmt.Errorf("unsupported autopilot assignee_type: %s", autopilot.AssigneeType)
	}
}
