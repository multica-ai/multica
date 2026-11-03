package proposalkinds

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// decisionKind is the phase 1 proposal: a question for the boss. Approval with
// a handoff squad creates a project named after the proposal; the service then
// creates the kickoff issue after commit. Its payload is always empty.
type decisionKind struct{}

func (decisionKind) Key() string         { return KeyDecision }
func (decisionKind) FiledByAgents() bool { return true }

func (decisionKind) Validate(_ context.Context, _ Env, payload []byte) ([]byte, error) {
	var empty struct{}
	if err := decodeObject(payload, &empty); err != nil {
		return nil, err
	}
	return []byte("{}"), nil
}

func (decisionKind) Summarize([]byte) string { return "" }

func (decisionKind) Apply(ctx context.Context, q *db.Queries, d Decision) (Effect, error) {
	if !d.Proposal.HandoffSquadID.Valid {
		return Effect{}, nil
	}
	squad, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: d.Proposal.HandoffSquadID, WorkspaceID: d.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Effect{}, ErrSquadNotLive
		}
		return Effect{}, err
	}
	if squad.ArchivedAt.Valid {
		return Effect{}, ErrSquadNotLive
	}
	project, err := q.CreateProject(ctx, db.CreateProjectParams{
		WorkspaceID: d.WorkspaceID,
		Title:       d.Issue.Title,
		Status:      "planned",
		Priority:    "none",
	})
	if err != nil {
		return Effect{}, err
	}
	return Effect{Project: &project}, nil
}

func (decisionKind) Reject(context.Context, *db.Queries, Decision) error { return nil }
