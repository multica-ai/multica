package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/automation"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// hookExecutionListLimit bounds the execution-trace read for the debug endpoint.
const hookExecutionListLimit = 100

// HookResponse is the API view of a hook plus its active revision.
type HookResponse struct {
	ID             string               `json:"id"`
	WorkspaceID    string               `json:"workspace_id"`
	Name           string               `json:"name"`
	Enabled        bool                 `json:"enabled"`
	Scope          HookScopeResponse    `json:"scope"`
	Origin         string               `json:"origin"`
	DisabledReason string               `json:"disabled_reason,omitempty"`
	Revision       HookRevisionResponse `json:"revision"`
	CreatedAt      string               `json:"created_at"`
	UpdatedAt      string               `json:"updated_at"`
}

// HookScopeResponse is the hook lifecycle scope.
type HookScopeResponse struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

// HookRevisionResponse is the active immutable revision.
type HookRevisionResponse struct {
	ID         string          `json:"id"`
	Revision   int32           `json:"revision"`
	Event      string          `json:"event"`
	Match      json.RawMessage `json:"match"`
	Conditions json.RawMessage `json:"conditions"`
	FireMode   string          `json:"fire_mode"`
	Actions    json.RawMessage `json:"actions"`
	CreatedAt  string          `json:"created_at"`
}

// HookExecutionResponse is one row of the execution trace (all null in PR2 — no
// matcher runs yet — but shaped so the debug endpoint is stable for PR3).
type HookExecutionResponse struct {
	ID          string `json:"id"`
	HookID      string `json:"hook_id"`
	RevisionID  string `json:"hook_revision_id"`
	EventID     string `json:"event_id"`
	Correlation string `json:"correlation_id"`
	Status      string `json:"status"`
	SkipReason  string `json:"skip_reason,omitempty"`
	ErrorCode   string `json:"error_code,omitempty"`
	CreatedAt   string `json:"created_at"`
}

func hookToResponse(hr service.HookWithRevision) HookResponse {
	h, rev := hr.Hook, hr.Revision
	return HookResponse{
		ID:             uuidToString(h.ID),
		WorkspaceID:    uuidToString(h.WorkspaceID),
		Name:           h.Name,
		Enabled:        h.Enabled,
		Scope:          HookScopeResponse{Type: h.ScopeType, ID: uuidToString(h.ScopeID)},
		Origin:         h.Origin,
		DisabledReason: textValue(h.DisabledReason),
		Revision: HookRevisionResponse{
			ID:         uuidToString(rev.ID),
			Revision:   rev.Revision,
			Event:      rev.EventType,
			Match:      rawJSON(rev.Match),
			Conditions: rawJSON(rev.Conditions),
			FireMode:   rev.FireMode,
			Actions:    rawJSON(rev.Actions),
			CreatedAt:  timestampToString(rev.CreatedAt),
		},
		CreatedAt: timestampToString(h.CreatedAt),
		UpdatedAt: timestampToString(h.UpdatedAt),
	}
}

func hookExecutionToResponse(e db.HookExecution) HookExecutionResponse {
	return HookExecutionResponse{
		ID:          uuidToString(e.ID),
		HookID:      uuidToString(e.HookID),
		RevisionID:  uuidToString(e.HookRevisionID),
		EventID:     uuidToString(e.EventID),
		Correlation: uuidToString(e.CorrelationID),
		Status:      e.Status,
		SkipReason:  textValue(e.SkipReason),
		ErrorCode:   textValue(e.ErrorCode),
		CreatedAt:   timestampToString(e.CreatedAt),
	}
}

// hookEnabled gates every hook endpoint behind the automation_event_hooks flag.
// Store-only PR2 already writes rows, but the whole surface stays invisible until
// the flag is deliberately turned on, so nothing changes for existing workspaces.
func (h *Handler) hookEnabled(w http.ResponseWriter, r *http.Request) bool {
	if !featureflags.EventHooksEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusNotFound, "event hooks are not enabled")
		return false
	}
	return true
}

// CreateHook validates and persists a new hook + revision #1.
func (h *Handler) CreateHook(w http.ResponseWriter, r *http.Request) {
	if !h.hookEnabled(w, r) {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	spec, ok := decodeHookSpec(w, r)
	if !ok {
		return
	}

	author, ok := h.resolveHookWriter(w, r, userID, workspaceID)
	if !ok {
		return
	}

	result, err := h.HookService.CreateHook(r.Context(), workspaceUUID, spec, author)
	if err != nil {
		h.writeHookError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, hookToResponse(result))
}

// ListHooks returns every non-archived hook in the workspace.
func (h *Handler) ListHooks(w http.ResponseWriter, r *http.Request) {
	if !h.hookEnabled(w, r) {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	hooks, err := h.HookService.ListHooks(r.Context(), workspaceUUID)
	if err != nil {
		h.writeHookError(w, err)
		return
	}
	resp := make([]HookResponse, 0, len(hooks))
	for _, hook := range hooks {
		resp = append(resp, hookToResponse(hook))
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetHook returns one hook with its active revision.
func (h *Handler) GetHook(w http.ResponseWriter, r *http.Request) {
	if !h.hookEnabled(w, r) {
		return
	}
	workspaceUUID, hookUUID, ok := h.hookPathParams(w, r)
	if !ok {
		return
	}
	result, err := h.HookService.GetHook(r.Context(), workspaceUUID, hookUUID)
	if err != nil {
		h.writeHookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hookToResponse(result))
}

// UpdateHook appends a new revision and repoints the active pointer.
func (h *Handler) UpdateHook(w http.ResponseWriter, r *http.Request) {
	if !h.hookEnabled(w, r) {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, hookUUID, ok := h.hookPathParams(w, r)
	if !ok {
		return
	}
	spec, ok := decodeHookSpec(w, r)
	if !ok {
		return
	}
	author, ok := h.resolveHookWriter(w, r, userID, workspaceID)
	if !ok {
		return
	}
	result, err := h.HookService.UpdateHook(r.Context(), workspaceUUID, hookUUID, spec, author)
	if err != nil {
		h.writeHookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hookToResponse(result))
}

// EnableHook re-enables a disabled hook.
func (h *Handler) EnableHook(w http.ResponseWriter, r *http.Request) {
	h.setHookEnabled(w, r, true)
}

// DisableHook disables a hook with an optional reason.
func (h *Handler) DisableHook(w http.ResponseWriter, r *http.Request) {
	h.setHookEnabled(w, r, false)
}

func (h *Handler) setHookEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	if !h.hookEnabled(w, r) {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, hookUUID, ok := h.hookPathParams(w, r)
	if !ok {
		return
	}
	reason := ""
	if !enabled && r.ContentLength != 0 {
		var body struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		reason = body.Reason
	}
	author, ok := h.resolveHookWriter(w, r, userID, workspaceID)
	if !ok {
		return
	}
	result, err := h.HookService.SetEnabled(r.Context(), workspaceUUID, hookUUID, enabled, reason, author)
	if err != nil {
		h.writeHookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hookToResponse(result))
}

// DeleteHook soft-archives a hook.
func (h *Handler) DeleteHook(w http.ResponseWriter, r *http.Request) {
	if !h.hookEnabled(w, r) {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, hookUUID, ok := h.hookPathParams(w, r)
	if !ok {
		return
	}
	author, ok := h.resolveHookWriter(w, r, userID, workspaceID)
	if !ok {
		return
	}
	if err := h.HookService.ArchiveHook(r.Context(), workspaceUUID, hookUUID, author); err != nil {
		h.writeHookError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListHookExecutions returns the newest execution-trace rows for a hook.
func (h *Handler) ListHookExecutions(w http.ResponseWriter, r *http.Request) {
	if !h.hookEnabled(w, r) {
		return
	}
	workspaceUUID, hookUUID, ok := h.hookPathParams(w, r)
	if !ok {
		return
	}
	execs, err := h.HookService.ListExecutions(r.Context(), workspaceUUID, hookUUID, hookExecutionListLimit)
	if err != nil {
		h.writeHookError(w, err)
		return
	}
	resp := make([]HookExecutionResponse, 0, len(execs))
	for _, e := range execs {
		resp = append(resp, hookExecutionToResponse(e))
	}
	writeJSON(w, http.StatusOK, resp)
}

// hookPathParams resolves the workspace and the {id} hook UUID for a scoped route.
func (h *Handler) hookPathParams(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	hookUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return workspaceUUID, hookUUID, true
}

// decodeHookSpec strictly decodes the request body into a HookSpec, rejecting
// unknown fields so a stray top-level or nested key can never be silently
// accepted (MUL-4332 PR2 review point 3). Per-action disallowed fields are
// rejected by the automation validator.
func decodeHookSpec(w http.ResponseWriter, r *http.Request) (automation.HookSpec, bool) {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var spec automation.HookSpec
	if err := dec.Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return automation.HookSpec{}, false
	}
	// The body must be exactly one JSON value. A second Decode must hit io.EOF;
	// anything else means trailing data (a smuggled second document) that
	// DisallowUnknownFields alone does not catch (MUL-4332 review round 3, point 4).
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body: expected exactly one JSON document")
		return automation.HookSpec{}, false
	}
	return spec, true
}

// resolveHookWriter derives the audit creator actor, the accountable human
// principal (§8), and whether that principal is a workspace owner/admin, plus the
// agent-admission callback used for fail-closed trigger_agent validation. A
// member acts under their own authority; an agent must resolve to the human
// behind its current task. The resolved principal must still be a workspace
// member (review point 2) — an agent UUID is never a substitute for a human, and
// a departed principal can no longer author hooks.
func (h *Handler) resolveHookWriter(w http.ResponseWriter, r *http.Request, userID, workspaceID string) (service.HookAuthor, bool) {
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	actorUUID, err := util.ParseUUID(actorID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid actor id")
		return service.HookAuthor{}, false
	}
	principal := h.invokeOriginatorFromRequest(r, actorType, actorID)
	if principal == "" {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return service.HookAuthor{}, false
	}
	principalUUID, err := util.ParseUUID(principal)
	if err != nil {
		writeError(w, http.StatusForbidden, "no accountable authorization principal")
		return service.HookAuthor{}, false
	}
	// Membership, role, and every target admission are (re)checked by the service
	// inside the write transaction against this principal, so a stale snapshot can
	// never authorize the write.
	return service.HookAuthor{ActorType: actorType, ActorID: actorUUID, PrincipalUserID: principalUUID}, true
}

func (h *Handler) writeHookError(w http.ResponseWriter, err error) {
	if ve, ok := automation.AsValidationError(err); ok {
		writeError(w, http.StatusBadRequest, ve.Error())
		return
	}
	switch {
	case errors.Is(err, service.ErrHookNotFound):
		writeError(w, http.StatusNotFound, "hook not found")
	case errors.Is(err, service.ErrHookEventNotFound):
		writeError(w, http.StatusNotFound, "event not found")
	case errors.Is(err, service.ErrHookSystemManaged):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrHookForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrHookPrincipalDeparted):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrHookNoPrincipal):
		writeError(w, http.StatusForbidden, "no accountable authorization principal")
	default:
		writeError(w, http.StatusInternalServerError, "hook request failed")
	}
}

// rawJSON returns b as json.RawMessage, defaulting an empty column to a JSON null.
func rawJSON(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}

// textValue unwraps a nullable text column to a plain string ("" when NULL).
func textValue(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}
