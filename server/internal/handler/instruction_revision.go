package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Agent instruction revisions (spec 4.6): the history of an agent's
// instructions, and restoring an earlier one. Every write of the instructions
// records a revision in the same transaction (service/agent.go).

// instructionRevisionListLimit bounds the history list.
const instructionRevisionListLimit int32 = 100

// InstructionRevisionResponse is one revision of an agent's instructions.
type InstructionRevisionResponse struct {
	ID           string    `json:"id"`
	AgentID      string    `json:"agent_id"`
	Revision     int32     `json:"revision"`
	Instructions string    `json:"instructions"`
	Source       string    `json:"source"`
	ProposalID   *string   `json:"proposal_id"`
	ActorType    string    `json:"actor_type"`
	ActorID      *string   `json:"actor_id"`
	CreatedAt    time.Time `json:"created_at"`
}

func instructionRevisionResponse(rv db.AgentInstructionRevision) InstructionRevisionResponse {
	out := InstructionRevisionResponse{
		ID:           uuidToString(rv.ID),
		AgentID:      uuidToString(rv.AgentID),
		Revision:     rv.Revision,
		Instructions: rv.Instructions,
		Source:       rv.Source,
		ActorType:    rv.ActorType,
		CreatedAt:    rv.CreatedAt.Time,
	}
	if rv.ProposalID.Valid {
		id := uuidToString(rv.ProposalID)
		out.ProposalID = &id
	}
	if rv.ActorID.Valid {
		id := uuidToString(rv.ActorID)
		out.ActorID = &id
	}
	return out
}

// ListAgentInstructionRevisions returns an agent's revisions, newest first. A
// reader of the agent may read its history.
//
//	GET /api/agents/{id}/instruction-revisions
func (h *Handler) ListAgentInstructionRevisions(w http.ResponseWriter, r *http.Request) {
	existing, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListAgentInstructionRevisions(r.Context(), db.ListAgentInstructionRevisionsParams{
		WorkspaceID: existing.WorkspaceID, AgentID: existing.ID, RowLimit: instructionRevisionListLimit,
	})
	if err != nil {
		slog.Warn("list instruction revisions failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list instruction revisions")
		return
	}
	out := make([]InstructionRevisionResponse, 0, len(rows))
	for _, rv := range rows {
		out = append(out, instructionRevisionResponse(rv))
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": out})
}

// RestoreAgentInstructionRevision sets the agent's instructions back to an
// earlier revision. The restore is a new revision with source rollback. Only
// those who may manage the agent may restore it.
//
//	POST /api/agents/{id}/instruction-revisions/{revision}/restore
func (h *Handler) RestoreAgentInstructionRevision(w http.ResponseWriter, r *http.Request) {
	existing, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, existing) {
		return
	}
	n, err := strconv.ParseInt(chi.URLParam(r, "revision"), 10, 32)
	if err != nil || n < 1 {
		writeError(w, http.StatusBadRequest, "revision must be a number from 1")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start restore transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	agent, rev, err := service.RestoreAgentInstructionRevision(r.Context(), qtx, existing.WorkspaceID, existing.ID, int32(n), h.instructionChange(r, service.InstructionSourceRollback).Actor)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "instruction revision not found")
		return
	case errors.Is(err, service.ErrInstructionsUnchanged):
		writeError(w, http.StatusConflict, "the agent already has the instructions of this revision")
		return
	case err != nil:
		slog.Warn("restore instruction revision failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(existing.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to restore instruction revision")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit restore")
		return
	}
	resp := h.agentToResponse(agent)
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(existing.WorkspaceID))
	h.publish(protocol.EventAgentStatus, uuidToString(agent.WorkspaceID), actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
	writeJSON(w, http.StatusOK, map[string]any{"agent": resp, "revision": instructionRevisionResponse(rev)})
}
