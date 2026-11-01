package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
)

// The knowledge read and write endpoints. Members and agents read, search and
// write drafts; owners and admins publish, discard and delete (the router
// enforces the role, requireKnowledgeAdmin keeps agents out as a backstop).
// A sidecar that is down is the "offline" state: reads report it with empty
// results and writes answer 503 with state "offline", never 500.

// CreateKnowledgeDocumentRequest is the body of POST /api/knowledge/documents.
// Send content or url, not both. Publish is honored only for a human member.
type CreateKnowledgeDocumentRequest struct {
	KB             string  `json:"kb"`
	Title          string  `json:"title"`
	Content        string  `json:"content"`
	URL            string  `json:"url"`
	SourceType     string  `json:"source_type"`
	SourceID       *string `json:"source_id"`
	IdempotencyKey string  `json:"idempotency_key"`
	Publish        bool    `json:"publish"`
}

// knowledgeStateBody is a response that carries a knowledge state next to its
// message, so a client can show "offline" or "not enabled" instead of an error.
type knowledgeStateBody struct {
	Error string `json:"error"`
	State string `json:"state"`
}

func writeKnowledgeError(w http.ResponseWriter, r *http.Request, workspaceID string, err error) {
	var (
		notReady *service.KnowledgeNotReadyError
		invalid  *service.KnowledgeInvalidError
		conflict *service.KnowledgeConflictError
	)
	switch {
	case errors.As(err, &notReady):
		writeJSON(w, http.StatusConflict, knowledgeStateBody{Error: "knowledge is not ready for this workspace", State: notReady.State})
	case errors.Is(err, knowledge.ErrUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, knowledgeStateBody{Error: "the knowledge service is offline", State: service.KnowledgeStateOffline})
	case errors.As(err, &invalid):
		writeError(w, http.StatusBadRequest, invalid.Message)
	case errors.As(err, &conflict):
		writeError(w, http.StatusConflict, conflict.Message)
	case errors.Is(err, service.ErrKnowledgeBaseNotFound):
		writeError(w, http.StatusNotFound, "knowledge base not found")
	case errors.Is(err, service.ErrKnowledgeDocumentNotFound), errors.Is(err, knowledge.ErrNotFound):
		writeError(w, http.StatusNotFound, "document not found")
	case errors.Is(err, service.ErrKnowledgeForbidden):
		writeError(w, http.StatusForbidden, "agents cannot write to this knowledge base")
	case errors.Is(err, knowledge.ErrInvalid):
		writeError(w, http.StatusBadRequest, "the knowledge service rejected the request")
	case errors.Is(err, knowledge.ErrNotManual):
		writeError(w, http.StatusConflict, "only a written document can be published this way")
	case errors.Is(err, knowledge.ErrAuth), errors.Is(err, knowledge.ErrForbidden), errors.Is(err, knowledge.ErrQuota):
		slog.Warn("knowledge service refused a request", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusBadGateway, "the knowledge service refused the request")
	default:
		slog.Warn("knowledge request failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "knowledge request failed")
	}
}

// knowledgeNotReadyState extracts the state of a not-ready error, so a read
// can answer with that state and empty results instead of failing.
func knowledgeNotReadyState(err error) (string, bool) {
	var notReady *service.KnowledgeNotReadyError
	if errors.As(err, &notReady) {
		return notReady.State, true
	}
	return "", false
}

// ListKnowledgeBases (GET /api/knowledge/bases) lists the knowledge bases and
// the workspace's knowledge state. Any member may read it.
func (h *Handler) ListKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	view, err := h.Knowledge.Bases(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// ListKnowledgeDocuments (GET /api/knowledge/documents?kb=&state=).
func (h *Handler) ListKnowledgeDocuments(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	list, err := h.Knowledge.ListDocuments(r.Context(), parseUUID(workspaceID), r.URL.Query().Get("kb"), r.URL.Query().Get("state"))
	if state, ok := knowledgeNotReadyState(err); ok {
		list.State = state
		err = nil
	}
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// SearchKnowledge (GET /api/knowledge/search?q=&kb=&limit=) is retrieval: no
// LLM, and every call returns the best matches, however weak. The caller
// judges relevance from the score and the text.
func (h *Handler) SearchKnowledge(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	result, err := h.Knowledge.Search(r.Context(), parseUUID(workspaceID), q.Get("q"), q.Get("kb"), limit)
	if state, ok := knowledgeNotReadyState(err); ok {
		result.State = state
		err = nil
	}
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// CreateKnowledgeDocument (POST /api/knowledge/documents) writes a document.
// An agent always creates a draft. A member creates a draft too, unless the
// body says publish: true. A repeated idempotency_key returns the first
// write with 200 instead of 201.
func (h *Handler) CreateKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req CreateKnowledgeDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var sourceID pgtype.UUID
	if req.SourceID != nil && *req.SourceID != "" {
		if sourceID, ok = parseUUIDOrBadRequest(w, *req.SourceID, "source_id"); !ok {
			return
		}
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	// Publishing is a curator's decision: an agent's publish flag is ignored,
	// whatever it sends (the service enforces the same).
	view, created, err := h.Knowledge.CreateDocument(r.Context(), parseUUID(workspaceID),
		service.KnowledgeActor{Type: actorType, ID: actorUUID},
		service.KnowledgeDocumentInput{
			KB: req.KB, Title: req.Title, Content: req.Content, URL: req.URL,
			SourceType: req.SourceType, SourceID: sourceID, IdempotencyKey: req.IdempotencyKey,
			Publish: req.Publish && actorType == "member",
		})
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, view)
}

// knowledgeDocumentAdmin resolves the {id} of an admin action on a document.
func (h *Handler) knowledgeDocumentAdmin(w http.ResponseWriter, r *http.Request) (workspaceID string, id pgtype.UUID, ok bool) {
	workspaceID = h.resolveWorkspaceID(r)
	if !h.requireKnowledgeAdmin(w, r, workspaceID) {
		return "", pgtype.UUID{}, false
	}
	id, ok = parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	return workspaceID, id, ok
}

// PublishKnowledgeDocument (POST /api/knowledge/documents/{id}/publish)
// indexes a draft so it becomes searchable. Owners and admins only.
func (h *Handler) PublishKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID, id, ok := h.knowledgeDocumentAdmin(w, r)
	if !ok {
		return
	}
	view, err := h.Knowledge.PublishDocument(r.Context(), parseUUID(workspaceID), id)
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// DiscardKnowledgeDocument (POST /api/knowledge/documents/{id}/discard)
// deletes a draft. Owners and admins only.
func (h *Handler) DiscardKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID, id, ok := h.knowledgeDocumentAdmin(w, r)
	if !ok {
		return
	}
	if err := h.Knowledge.DiscardDocument(r.Context(), parseUUID(workspaceID), id); err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteKnowledgeDocument (DELETE /api/knowledge/documents/{id}) removes a
// document in any state. Owners and admins only.
func (h *Handler) DeleteKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID, id, ok := h.knowledgeDocumentAdmin(w, r)
	if !ok {
		return
	}
	if err := h.Knowledge.DeleteDocument(r.Context(), parseUUID(workspaceID), id); err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
