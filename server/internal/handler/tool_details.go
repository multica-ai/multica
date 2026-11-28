package handler

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// An agent's tool_details_visibility: who can read the inputs and outputs of
// the tool calls in its run logs.
const (
	toolDetailsEveryone   = "everyone"
	toolDetailsRestricted = "restricted"
)

func validToolDetailsVisibility(v string) bool {
	return v == toolDetailsEveryone || v == toolDetailsRestricted
}

// toolDetailsAudience is who may read an agent's tool inputs and outputs.
type toolDetailsAudience struct {
	restricted bool
	// readers are the agent's owner and the workspace's owners and admins,
	// filled only when restricted.
	readers map[string]bool
}

// closedToolDetailsAudience is the answer when the audience cannot be read:
// restricted, with nobody allowed.
var closedToolDetailsAudience = toolDetailsAudience{restricted: true}

func (a toolDetailsAudience) allows(userID string) bool {
	return !a.restricted || (userID != "" && a.readers[userID])
}

// payloadFor returns the message as userID may read it.
func (a toolDetailsAudience) payloadFor(userID string, p protocol.TaskMessagePayload) protocol.TaskMessagePayload {
	if a.allows(userID) {
		return p
	}
	return p.WithoutToolDetails()
}

func (h *Handler) loadToolDetailsAudience(ctx context.Context, agentID pgtype.UUID) (toolDetailsAudience, error) {
	row, err := h.Queries.GetAgentToolDetailsReaders(ctx, agentID)
	if err != nil {
		return closedToolDetailsAudience, err
	}
	if row.ToolDetailsVisibility != toolDetailsRestricted {
		return toolDetailsAudience{}, nil
	}
	readers := make(map[string]bool, len(row.Readers))
	for _, id := range row.Readers {
		if id.Valid {
			readers[uuidToString(id)] = true
		}
	}
	return toolDetailsAudience{restricted: true, readers: readers}, nil
}

// toolDetailsReader is the user a run-log read is for, or "" when the caller
// is not a person allowed to be a reader: a machine credential, or an agent
// acting in the workspace. Tool details are for the people accountable for the
// agent, so a script or another agent reads the restricted view.
func (h *Handler) toolDetailsReader(r *http.Request, workspaceID string) string {
	if isMachineCredentialActor(r) {
		return ""
	}
	userID := requestUserID(r)
	if actorType, _ := h.resolveActor(r, userID, workspaceID); actorType != "member" {
		return ""
	}
	return userID
}
