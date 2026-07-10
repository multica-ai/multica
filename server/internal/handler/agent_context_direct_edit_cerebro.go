package handler

// Cerebro fork file (FIR-1775 Phase 2, "Agent Office"): a direct agent edit
// through the generic UpdateAgent endpoint must land in the agent context
// version history, so edits that bypass the propose/approve change-request
// flow are still tracked and rollbackable. The recording logic lives in
// server/internal/cerebro/agentoffice (Service.RecordDirectEdit); this file
// holds the upstream-side seam interface and the helper UpdateAgent calls.

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CerebroAgentContextDirectEditRecorder is the seam UpdateAgent uses to
// snapshot a direct edit into the agent context version history. Implemented
// by agentoffice.(*Service).RecordDirectEdit; nil on a server without Agent
// Office wired.
type CerebroAgentContextDirectEditRecorder interface {
	// RecordDirectEdit re-reads the agent's live composite context and, when it
	// differs from the latest recorded version, bumps the version pointer and
	// appends a snapshot. recorded=false means the edit changed no versioned
	// field and nothing was written.
	RecordDirectEdit(ctx context.Context, agentID, editorID pgtype.UUID, description string) (version string, recorded bool, err error)
}

// recordAgentContextDirectEdit is called by UpdateAgent after all its writes
// have succeeded. Best-effort by design: the direct edit has already been
// committed, so a failed version record must never fail the response — it is
// logged and the next successful record captures the live state anyway.
func (h *Handler) recordAgentContextDirectEdit(r *http.Request, agent db.Agent) {
	if h.AgentContextDirectEdit == nil {
		return
	}
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, uuidToString(agent.WorkspaceID))
	// created_by is polymorphic (user or agent id, no FK) like the change
	// request tables; an unparseable actor id degrades to NULL rather than
	// failing the record.
	editorID, _ := util.ParseUUID(actorID)
	description := "Direct edit via agent update API"
	if actorType == "agent" {
		description = "Direct edit via agent update API (agent actor)"
	}
	version, recorded, err := h.AgentContextDirectEdit.RecordDirectEdit(r.Context(), agent.ID, editorID, description)
	if err != nil {
		slog.Warn("agent context direct-edit version record failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		return
	}
	if recorded {
		slog.Info("agent context direct edit recorded in version history",
			append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "context_version", version)...)
	}
}
