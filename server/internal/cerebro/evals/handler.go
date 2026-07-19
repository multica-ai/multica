package evals

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type Handler struct {
	store         *Store
	runNowEnabled bool
	// blockingGate authorizes *blocking* eval bindings (FIR-3496). Nil keeps the
	// gate open (advisory-only enforcement), which is the pre-flag behaviour;
	// wired in router.go over grouppermissions.
	blockingGate BlockingGateAuthorizer
}

// BlockingGateAuthorizer decides whether a member may create a blocking eval
// binding in a workspace. A workspace admin always may; otherwise the member
// must belong to a group granted the set_blocking_gate capability. Implemented
// by an adapter over grouppermissions.Service (router.go).
type BlockingGateAuthorizer interface {
	CanSetBlockingGate(ctx context.Context, workspaceID, userID uuid.UUID, isAdmin bool) (bool, error)
}

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{store: NewStore(pool)} }

func (h *Handler) WithRunExecutor(executor RunExecutor) *Handler {
	h.store.WithRunExecutor(executor)
	h.runNowEnabled = executor != nil
	return h
}

// WithBlockingGateAuthorizer enables the "who may set a blocking gate" check on
// CreateBinding. With no authorizer wired, blocking bindings are unrestricted
// (the pre-FIR-3496 behaviour). Returns the handler for chained wiring.
func (h *Handler) WithBlockingGateAuthorizer(a BlockingGateAuthorizer) *Handler {
	h.blockingGate = a
	return h
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Get("/bindings", h.ListBindings)
	r.Post("/bindings", h.CreateBinding)
	r.Delete("/bindings/{bindingId}", h.DeleteBinding)
	r.Get("/{id}", h.Get)
	r.Put("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	r.Get("/{id}/runs", h.ListRuns)
	r.Post("/{id}/runs", h.CreateRun)
	r.Post("/{id}/run", h.RunNow)
	return r
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, _, ok := requestContext(w, r)
	if !ok {
		return
	}
	items, err := h.store.List(r.Context(), workspaceID)
	if err != nil {
		h.internalError(w, r, "failed to list evals", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"evals": items})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, _, ok := requestContext(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	item, err := h.store.Get(r.Context(), workspaceID, id)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actorID, workspaceID, actorType, ok := requestContext(w, r)
	if !ok {
		return
	}
	var input EvalInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := validateEvalInput(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := h.store.Create(r.Context(), workspaceID, actorID, actorType, input)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	actorID, workspaceID, _, ok := requestContext(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var input EvalInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := validateEvalInput(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Only the eval's owner (its creator) or a workspace admin may edit an eval
	// (FIR-3496). Load it first so the ownership check has the creator to compare.
	existing, err := h.store.Get(r.Context(), workspaceID, id)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	member, memberOK := middleware.MemberFromContext(r.Context())
	if !canEditEval(member, memberOK, actorID, existing) {
		writeError(w, http.StatusForbidden, "only the eval owner or a workspace admin may edit this eval")
		return
	}
	item, err := h.store.Update(r.Context(), workspaceID, id, input)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// canEditEval reports whether the actor may edit the eval: a workspace admin
// (owner/admin role) always may; otherwise only the eval's creator may.
func canEditEval(member db.Member, memberOK bool, actorID uuid.UUID, eval Eval) bool {
	if memberOK && (member.Role == "owner" || member.Role == "admin") {
		return true
	}
	return eval.CreatedByID == actorID
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, _, ok := requestContext(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := h.store.Delete(r.Context(), workspaceID, id); err != nil {
		h.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, _, ok := requestContext(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	runs, err := h.store.ListRuns(r.Context(), workspaceID, id)
	if err != nil {
		h.internalError(w, r, "failed to list eval runs", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (h *Handler) CreateRun(w http.ResponseWriter, r *http.Request) {
	actorID, workspaceID, actorType, ok := requestContext(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var input EvalRunInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := validateRunInput(input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	run, err := h.store.CreateRun(r.Context(), workspaceID, actorID, id, actorType, input)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (h *Handler) ListBindings(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, _, ok := requestContext(w, r)
	if !ok {
		return
	}
	var workflowID *uuid.UUID
	if raw := r.URL.Query().Get("workflow_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow_id")
			return
		}
		workflowID = &parsed
	}
	items, err := h.store.ListBindings(r.Context(), workspaceID, workflowID)
	if err != nil {
		h.internalError(w, r, "failed to list eval bindings", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bindings": items})
}

func (h *Handler) CreateBinding(w http.ResponseWriter, r *http.Request) {
	actorID, workspaceID, _, ok := requestContext(w, r)
	if !ok {
		return
	}
	var input BindingInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := validateBindingInput(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// A blocking binding needs the set_blocking_gate right (FIR-3496): a
	// workspace admin, or a member of a group granted it. Advisory bindings are
	// unrestricted. Only enforced when an authorizer is wired.
	if input.Blocking && h.blockingGate != nil {
		member, memberOK := middleware.MemberFromContext(r.Context())
		if !memberOK || !member.UserID.Valid {
			writeError(w, http.StatusForbidden, "only admins or granted members may set a blocking eval gate")
			return
		}
		userID, err := uuid.FromBytes(member.UserID.Bytes[:])
		if err != nil {
			writeError(w, http.StatusForbidden, "invalid user identity")
			return
		}
		isAdmin := member.Role == "owner" || member.Role == "admin"
		allowed, err := h.blockingGate.CanSetBlockingGate(r.Context(), workspaceID, userID, isAdmin)
		if err != nil {
			h.internalError(w, r, "failed to check blocking-gate permission", err)
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "only admins or granted members may set a blocking eval gate")
			return
		}
	}
	item, err := h.store.CreateBinding(r.Context(), workspaceID, actorID, input)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) DeleteBinding(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, _, ok := requestContext(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "bindingId")
	if !ok {
		return
	}
	if err := h.store.DeleteBinding(r.Context(), workspaceID, id); err != nil {
		h.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func requestContext(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, string, bool) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok || !member.UserID.Valid {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return uuid.Nil, uuid.Nil, "", false
	}
	actorType := "member"
	actorRaw := member.UserID.Bytes[:]
	if agentID := r.Header.Get("X-Agent-ID"); agentID != "" {
		parsed, err := uuid.Parse(agentID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid agent identity")
			return uuid.Nil, uuid.Nil, "", false
		}
		actorType = "agent"
		actorRaw = parsed[:]
	}
	actorID, err := uuid.FromBytes(actorRaw)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid user identity")
		return uuid.Nil, uuid.Nil, "", false
	}
	workspaceID, err := uuid.Parse(middleware.WorkspaceIDFromContext(r.Context()))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
		return uuid.Nil, uuid.Nil, "", false
	}
	return actorID, workspaceID, actorType, true
}

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return uuid.Nil, false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func (h *Handler) storeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "eval resource not found")
		return
	}
	if errors.Is(err, ErrImmutable) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			writeError(w, http.StatusConflict, "eval version or binding already exists")
			return
		case "23503", "23514":
			writeError(w, http.StatusBadRequest, "invalid eval reference or value")
			return
		}
	}
	h.internalError(w, r, "eval request failed", err)
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, message string, err error) {
	slog.Error(message, append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, message)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
