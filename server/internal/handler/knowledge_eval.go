package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
)

// Retrieval eval (spec 3.3, plan 10.10): the cases a search should answer, and
// the runs that measure how often it does. Anyone who writes may add a case;
// running the eval and deleting a case are for owners and admins.

// CreateKnowledgeEvalCaseRequest is the body of POST /api/knowledge/eval-cases.
type CreateKnowledgeEvalCaseRequest struct {
	Question           string  `json:"question"`
	ExpectedKind       string  `json:"expected_kind"`
	ExpectedDocumentID string  `json:"expected_document_id"`
	OwnerSquadID       *string `json:"owner_squad_id"`
}

// ListKnowledgeEvalCases (GET /api/knowledge/eval-cases).
func (h *Handler) ListKnowledgeEvalCases(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	cases, err := h.Knowledge.ListEvalCases(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cases": cases})
}

// CreateKnowledgeEvalCase (POST /api/knowledge/eval-cases) adds a case: a question
// and either the kind a good answer has or the document it should return.
func (h *Handler) CreateKnowledgeEvalCase(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req CreateKnowledgeEvalCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var squadID pgtype.UUID
	if req.OwnerSquadID != nil && *req.OwnerSquadID != "" {
		if squadID, ok = parseUUIDOrBadRequest(w, *req.OwnerSquadID, "owner_squad_id"); !ok {
			return
		}
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	view, err := h.Knowledge.CreateEvalCase(r.Context(), parseUUID(workspaceID),
		service.KnowledgeActor{Type: actorType, ID: actorUUID},
		service.EvalCaseInput{Question: req.Question, ExpectedKind: req.ExpectedKind, ExpectedDocumentID: req.ExpectedDocumentID, OwnerSquadID: squadID})
	if writeTaxonomyError(w, err) {
		return
	}
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

// DeleteKnowledgeEvalCase (DELETE /api/knowledge/eval-cases/{id}). Owners and
// admins only.
func (h *Handler) DeleteKnowledgeEvalCase(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if !h.requireKnowledgeAdmin(w, r, workspaceID) {
		return
	}
	err := h.Knowledge.DeleteEvalCase(r.Context(), parseUUID(workspaceID), chi.URLParam(r, "id"))
	if writeTaxonomyError(w, err) {
		return
	}
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListKnowledgeEvalRuns (GET /api/knowledge/eval/runs?limit=) lists the newest runs.
func (h *Handler) ListKnowledgeEvalRuns(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := h.Knowledge.ListEvalRuns(r.Context(), parseUUID(workspaceID), limit)
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

// RunKnowledgeEval (POST /api/knowledge/eval/runs) runs every case now. Owners
// and admins only. A run needs WeKnora; offline returns state "offline" and
// records nothing.
func (h *Handler) RunKnowledgeEval(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if !h.requireKnowledgeAdmin(w, r, workspaceID) {
		return
	}
	run, err := h.Knowledge.RunEval(r.Context(), parseUUID(workspaceID), time.Now())
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}
