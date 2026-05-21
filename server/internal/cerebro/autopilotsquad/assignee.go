package autopilotsquad

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type Store interface {
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
	GetAgentInWorkspace(ctx context.Context, arg db.GetAgentInWorkspaceParams) (db.Agent, error)
	GetSquadInWorkspace(ctx context.Context, arg db.GetSquadInWorkspaceParams) (db.Squad, error)
}

type Error struct {
	StatusCode int
	Message    string
}

func (e *Error) Error() string {
	return e.Message
}

type UpdateInput struct {
	AssigneeTypeSent bool
	AssigneeIDSent   bool
	AssigneeType     *string
	AssigneeID       *string
}

func UpdateInputFromRaw(assigneeType, assigneeID *string, rawFields map[string]json.RawMessage) UpdateInput {
	_, typeSent := rawFields["assignee_type"]
	_, idSent := rawFields["assignee_id"]
	return UpdateInput{
		AssigneeTypeSent: typeSent,
		AssigneeIDSent:   idSent,
		AssigneeType:     assigneeType,
		AssigneeID:       assigneeID,
	}
}

func ApplyUpdate(ctx context.Context, store Store, prev db.Autopilot, input UpdateInput, params *db.UpdateAutopilotParams) *Error {
	if !input.AssigneeTypeSent && !input.AssigneeIDSent {
		return nil
	}

	nextType := prev.AssigneeType
	if input.AssigneeTypeSent && input.AssigneeType != nil && *input.AssigneeType != "" {
		nextType = *input.AssigneeType
	}

	nextID := prev.AssigneeID
	if input.AssigneeIDSent {
		if input.AssigneeID == nil {
			return badRequest("assignee_id cannot be null")
		}
		parsed, err := util.ParseUUID(*input.AssigneeID)
		if err != nil {
			return badRequest("invalid assignee_id")
		}
		nextID = parsed
	}

	if input.AssigneeTypeSent && !input.AssigneeIDSent && nextType != prev.AssigneeType {
		return badRequest("assignee_id is required when changing assignee_type")
	}
	if err := ValidateAssignee(ctx, store, nextType, nextID, prev.WorkspaceID); err != nil {
		return err
	}
	if input.AssigneeTypeSent {
		params.AssigneeType = pgtype.Text{String: nextType, Valid: true}
	}
	if input.AssigneeIDSent {
		params.AssigneeID = nextID
	}
	return nil
}

// ValidateAssignee checks that the autopilot assignee exists in the workspace.
// Squad validation also checks the leader, so users get feedback before a scheduled run fails later.
func ValidateAssignee(ctx context.Context, store Store, assigneeType string, assigneeID, workspaceID pgtype.UUID) *Error {
	switch assigneeType {
	case "agent":
		if _, err := store.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          assigneeID,
			WorkspaceID: workspaceID,
		}); err != nil {
			return badRequest("assignee must be a valid agent in this workspace")
		}
		return nil
	case "squad":
		squad, err := store.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          assigneeID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return badRequest("assignee must be a valid squad in this workspace")
		}
		if squad.ArchivedAt.Valid {
			return unprocessable("squad is archived; pick a different squad")
		}
		leader, err := store.GetAgent(ctx, squad.LeaderID)
		if err != nil {
			return badRequest("squad leader agent not found")
		}
		if leader.ArchivedAt.Valid {
			return unprocessable("squad leader is archived; pick a different squad or rotate the leader before assigning autopilot")
		}
		return nil
	default:
		return badRequest("assignee_type must be agent or squad")
	}
}

func badRequest(message string) *Error {
	return &Error{StatusCode: http.StatusBadRequest, Message: message}
}

func unprocessable(message string) *Error {
	return &Error{StatusCode: http.StatusUnprocessableEntity, Message: message}
}
