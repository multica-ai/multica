package middleware

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Scoped agents do not get issue search, arbitrary chat history, credential
// management, downstream task creation or broad attachment enumeration. The
// daemon persists replies via its own protocol; the model needs no API writes.
func aidoOwnerRequestAllowed(r *http.Request, queries *db.Queries, token db.TaskToken) bool {
	if r.Method != http.MethodGet {
		return false
	}
	binding, err := queries.GetAIDOwnerBinding(r.Context(), token.AgentID)
	if err != nil {
		return false
	}
	task, err := queries.GetAgentTask(r.Context(), token.TaskID)
	if err != nil || !task.ChatSessionID.Valid || task.AgentID != token.AgentID || task.RuntimeID != binding.RuntimeID {
		return false
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "tasks" && parts[2] == uuidToString(token.TaskID) && parts[3] == "messages" {
		return true
	}
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "attachments" || parts[3] != "download" {
		return false
	}
	var id pgtype.UUID
	if err := id.Scan(parts[2]); err != nil {
		return false
	}
	attachment, err := queries.GetAttachmentByIDOnly(r.Context(), id)
	return err == nil && attachment.WorkspaceID == token.WorkspaceID && attachment.ChatSessionID.Valid && attachment.ChatSessionID == task.ChatSessionID
}
