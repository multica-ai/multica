package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Agent instruction revisions (spec 4.6). Every write of an agent's
// instructions goes through this file, from the API, the CLI, the company
// template, the Secretary's setup, an applied org change and a rollback, so
// the history holds every change whoever made it. The writers call these inside
// the transaction that writes the instructions.

// Revision sources, as stored.
const (
	InstructionSourceHuman    = "human"
	InstructionSourceProposal = "proposal"
	InstructionSourceRollback = "rollback"
)

// InstructionChange says who changed the instructions and why.
type InstructionChange struct {
	// Source is human, proposal or rollback.
	Source string
	// ProposalID is the org_change proposal that made the change, when there is one.
	ProposalID pgtype.UUID
	// Actor is the member, agent or system that made the change.
	Actor GovernanceActor
}

// ChangeAgentInstructions is the hook every update of an agent's instructions
// calls before it writes the agent row, in the same transaction. It locks the
// agent row, so concurrent changes take turns, and compares newText with the
// instructions it finds there. A different text is recorded as the next
// revision and the function reports true; the caller then writes the agent row.
// Unchanged text records nothing and returns nil.
func ChangeAgentInstructions(ctx context.Context, q *db.Queries, agentID pgtype.UUID, newText string, change InstructionChange) (*db.AgentInstructionRevision, error) {
	locked, err := q.GetAgentForUpdate(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("lock agent for instructions: %w", err)
	}
	if locked.Instructions == newText {
		return nil, nil
	}
	rev, err := WriteAgentInstructionRevision(ctx, q, locked.WorkspaceID, agentID, newText, change)
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

// WriteAgentInstructionRevision appends a revision holding text. Creation calls
// it for a new agent; ChangeAgentInstructions calls it for an edit. The caller
// holds the agent row lock, which keeps the revision numbers in order.
func WriteAgentInstructionRevision(ctx context.Context, q *db.Queries, ws, agentID pgtype.UUID, text string, change InstructionChange) (db.AgentInstructionRevision, error) {
	source := change.Source
	switch source {
	case InstructionSourceHuman, InstructionSourceProposal, InstructionSourceRollback:
	default:
		return db.AgentInstructionRevision{}, fmt.Errorf("instruction revision source %q is not one of human, proposal, rollback", source)
	}
	next, err := q.GetLatestAgentInstructionRevisionNumber(ctx, db.GetLatestAgentInstructionRevisionNumberParams{
		WorkspaceID: ws, AgentID: agentID,
	})
	if err != nil {
		return db.AgentInstructionRevision{}, fmt.Errorf("read latest instruction revision: %w", err)
	}
	row, err := q.InsertAgentInstructionRevision(ctx, db.InsertAgentInstructionRevisionParams{
		ID:           dbid.NewV7(),
		WorkspaceID:  ws,
		AgentID:      agentID,
		Revision:     next + 1,
		Instructions: text,
		Source:       source,
		ProposalID:   change.ProposalID,
		ActorType:    change.Actor.Type,
		ActorID:      change.Actor.ID,
	})
	if err != nil {
		return db.AgentInstructionRevision{}, fmt.Errorf("record instruction revision: %w", err)
	}
	return row, nil
}

// SetAgentInstructions changes an agent's instructions through the hook and
// writes the row, for the paths that have no other update to make (an applied
// org change, a rollback). It returns the updated agent and the revision it
// recorded, which is nil when the text was already in place.
func SetAgentInstructions(ctx context.Context, q *db.Queries, agentID pgtype.UUID, text string, change InstructionChange) (db.Agent, *db.AgentInstructionRevision, error) {
	rev, err := ChangeAgentInstructions(ctx, q, agentID, text, change)
	if err != nil {
		return db.Agent{}, nil, err
	}
	agent, err := q.UpdateAgent(ctx, db.UpdateAgentParams{
		ID:           agentID,
		Instructions: pgtype.Text{String: text, Valid: true},
	})
	if err != nil {
		return db.Agent{}, nil, fmt.Errorf("write agent instructions: %w", err)
	}
	return agent, rev, nil
}

// ErrInstructionsUnchanged: a restore to the revision the agent already has.
var ErrInstructionsUnchanged = errors.New("the agent already has these instructions")

// RestoreAgentInstructionRevision sets an agent's instructions back to an
// earlier revision's text. The restore is a new revision with source rollback,
// so the history keeps what was replaced.
func RestoreAgentInstructionRevision(ctx context.Context, q *db.Queries, ws, agentID pgtype.UUID, revision int32, actor GovernanceActor) (db.Agent, db.AgentInstructionRevision, error) {
	target, err := q.GetAgentInstructionRevision(ctx, db.GetAgentInstructionRevisionParams{
		WorkspaceID: ws, AgentID: agentID, Revision: revision,
	})
	if err != nil {
		return db.Agent{}, db.AgentInstructionRevision{}, err
	}
	agent, rev, err := SetAgentInstructions(ctx, q, agentID, target.Instructions, InstructionChange{
		Source: InstructionSourceRollback, Actor: actor,
	})
	if err != nil {
		return db.Agent{}, db.AgentInstructionRevision{}, err
	}
	if rev == nil {
		return db.Agent{}, db.AgentInstructionRevision{}, ErrInstructionsUnchanged
	}
	return agent, *rev, nil
}
