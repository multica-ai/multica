package credentials

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

type Handler struct {
	Service *Service
}

// New constructs the handler. The Cipher is optional at the type level
// (Service surfaces ErrCipherMissing if it isn't wired) so tests can stand
// up the package without a real key; production callers should always
// supply NewCipherFromEnv() and fail startup if it returned nil.
//
// JEH-1197: the returned handler ships with DenyAllChecker as the policy.
// Production callers chain .WithPolicy(...) to wire the owner+persona
// composite before mounting routes.
func New(cerebro *cerebrodb.Queries, cipher *Cipher, bus *events.Bus) *Handler {
	return &Handler{Service: NewService(cerebro, cipher, bus)}
}

// WithPolicy returns the handler with its Service rebound to use the
// supplied policy checker. Chained from the router so the wiring stays
// a single CEREBRO-PATCH line in cmd/server/router.go.
func (h *Handler) WithPolicy(p PolicyChecker) *Handler {
	if h == nil {
		return nil
	}
	clone := *h
	clone.Service = h.Service.WithPolicy(p)
	return &clone
}

// Mount registers every credential-registry route under the workspace-scoped
// chi.Router passed in. Defined as a method on Handler so the upstream
// router.go only needs a single CEREBRO-PATCH line to wire the entire
// feature — keeping the cerebro-patch surface inside the cerebro zone where
// new routes can be added without touching upstream code.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/credentials", h.List)
	r.Post("/credentials", h.Create)
	r.Get("/credentials/{credId}", h.Get)
	r.Patch("/credentials/{credId}", h.Update)
	r.Delete("/credentials/{credId}", h.Delete)
	r.Post("/credentials/{credId}/reveal", h.Reveal)
	r.Post("/credentials/{credId}/rotate", h.Rotate)
	r.Get("/credentials/{credId}/bindings", h.ListBindings)
	r.Post("/credentials/{credId}/bindings", h.CreateBinding)
	r.Delete("/credentials/{credId}/bindings/{bindingId}", h.DeleteBinding)
	r.Get("/credentials/{credId}/audit", h.ListAudit)
}

// createRequest is the POST body. Value is plaintext on the wire; TLS in
// transit, AES-256-GCM at rest. Metadata is raw JSON so type-specific
// fields (e.g. GCP service-account email, repo URL for a deploy key) can
// be attached without growing the column set.
type createRequest struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Value       string          `json:"value"`
	Metadata    json.RawMessage `json:"metadata"`
	ExpiresAt   *string         `json:"expires_at"`
}

type updateRequest struct {
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	Metadata    json.RawMessage `json:"metadata"`
	// pointer-to-pointer-to-string: nil  → leave untouched; non-nil + nil
	// inner → clear; non-nil + non-nil inner → set.
	ExpiresAt **string `json:"expires_at,omitempty"`
}

type rotateRequest struct {
	Value string `json:"value"`
}

type bindingRequest struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

// List handles GET /api/workspaces/{id}/credentials.
//
// JEH-1197: read_redacted policy is enforced per-credential. Rows the
// actor cannot read are filtered out — no error, just an empty list
// when the workspace has nothing the caller is allowed to see. We do
// NOT write an allow audit row for each list-row (volume), but a deny
// row IS written by AuthorizeRead so a probing actor leaves a trail.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	wsID, ok := workspaceIDFromRequest(w, r)
	if !ok {
		return
	}
	actorType, actorID, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	rows, err := h.Service.List(r.Context(), wsID)
	if err != nil {
		slog.Error("credentials list failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list credentials")
		return
	}
	out := make([]CredentialResponse, 0, len(rows))
	for _, row := range rows {
		if err := h.Service.AuthorizeRead(r.Context(), row.WorkspaceID, row.ID, Type(row.Type), actorType, actorID); err != nil {
			if errors.Is(err, ErrPolicyDenied) {
				continue
			}
			h.writeServiceError(w, r, err)
			return
		}
		out = append(out, credentialResponseFromModel(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

// Get handles GET /api/workspaces/{id}/credentials/{credId}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	wsID, credID, ok := h.credentialIDs(w, r)
	if !ok {
		return
	}
	actorType, actorID, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	row, err := h.Service.Get(r.Context(), wsID, credID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if err := h.Service.AuthorizeRead(r.Context(), row.WorkspaceID, row.ID, Type(row.Type), actorType, actorID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, credentialResponseFromModel(row))
}

// Create handles POST /api/workspaces/{id}/credentials.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	wsID, ok := workspaceIDFromRequest(w, r)
	if !ok {
		return
	}
	actorType, actorID, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	expiresAt, ok := parseOptionalTime(w, req.ExpiresAt, "expires_at")
	if !ok {
		return
	}
	row, err := h.Service.Create(r.Context(), CreateInput{
		WorkspaceID: wsID,
		Type:        Type(req.Type),
		Name:        req.Name,
		Description: req.Description,
		Value:       req.Value,
		Metadata:    req.Metadata,
		ExpiresAt:   expiresAt,
		ActorType:   actorType,
		ActorID:     actorID,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, credentialResponseFromModel(row))
}

// Update handles PATCH /api/workspaces/{id}/credentials/{credId}. Mutable
// fields only — the encrypted value is never touched here; use /rotate to
// replace the secret.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	wsID, credID, ok := h.credentialIDs(w, r)
	if !ok {
		return
	}
	actorType, actorID, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in := UpdateInput{
		Name:        req.Name,
		Description: req.Description,
		Metadata:    req.Metadata,
	}
	if req.ExpiresAt != nil {
		// PATCH semantics: explicit null clears the value, explicit string sets it.
		if *req.ExpiresAt == nil {
			var t *time.Time
			in.ExpiresAt = &t
		} else {
			parsed, err := time.Parse(time.RFC3339, **req.ExpiresAt)
			if err != nil {
				writeError(w, http.StatusBadRequest, "expires_at must be RFC3339")
				return
			}
			t := parsed
			pt := &t
			in.ExpiresAt = &pt
		}
	}
	row, err := h.Service.Update(r.Context(), wsID, credID, actorType, actorID, in)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, credentialResponseFromModel(row))
}

// Rotate handles POST /api/workspaces/{id}/credentials/{credId}/rotate.
// Separate endpoint from Update because rotating bumps last_rotated_at and
// re-encrypts; the rotation policy module keys off these fields.
func (h *Handler) Rotate(w http.ResponseWriter, r *http.Request) {
	wsID, credID, ok := h.credentialIDs(w, r)
	if !ok {
		return
	}
	actorType, actorID, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	var req rotateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	row, err := h.Service.Rotate(r.Context(), wsID, credID, actorType, actorID, req.Value)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, credentialResponseFromModel(row))
}

// Delete handles DELETE /api/workspaces/{id}/credentials/{credId}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	wsID, credID, ok := h.credentialIDs(w, r)
	if !ok {
		return
	}
	actorType, actorID, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	if _, err := h.Service.Delete(r.Context(), wsID, credID, actorType, actorID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Reveal handles POST /api/workspaces/{id}/credentials/{credId}/reveal. The
// plaintext is returned exclusively from this endpoint and the call is
// recorded in cerebro_credential_audit before the response is written.
func (h *Handler) Reveal(w http.ResponseWriter, r *http.Request) {
	wsID, credID, ok := h.credentialIDs(w, r)
	if !ok {
		return
	}
	actorType, actorID, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	row, plaintext, err := h.Service.Reveal(r.Context(), wsID, credID, actorType, actorID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, RevealResponse{
		ID:    util.UUIDToString(row.ID),
		Type:  row.Type,
		Name:  row.Name,
		Value: plaintext,
	})
}

// ListBindings handles GET /api/workspaces/{id}/credentials/{credId}/bindings.
func (h *Handler) ListBindings(w http.ResponseWriter, r *http.Request) {
	wsID, credID, ok := h.credentialIDs(w, r)
	if !ok {
		return
	}
	actorType, actorID, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	cred, err := h.Service.Get(r.Context(), wsID, credID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if err := h.Service.AuthorizeRead(r.Context(), cred.WorkspaceID, cred.ID, Type(cred.Type), actorType, actorID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	rows, err := h.Service.ListBindings(r.Context(), wsID, credID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	out := make([]BindingResponse, len(rows))
	for i, row := range rows {
		out[i] = bindingResponseFromModel(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"bindings": out})
}

// CreateBinding handles POST /api/workspaces/{id}/credentials/{credId}/bindings.
func (h *Handler) CreateBinding(w http.ResponseWriter, r *http.Request) {
	wsID, credID, ok := h.credentialIDs(w, r)
	if !ok {
		return
	}
	actorType, actorID, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	var req bindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	resourceID, err := util.ParseUUID(req.ResourceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid resource_id")
		return
	}
	row, err := h.Service.CreateBinding(r.Context(), wsID, credID, req.ResourceType, resourceID, actorType, actorID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, bindingResponseFromModel(row))
}

// DeleteBinding handles DELETE /api/workspaces/{id}/credentials/{credId}/bindings/{bindingId}.
func (h *Handler) DeleteBinding(w http.ResponseWriter, r *http.Request) {
	wsID, credID, ok := h.credentialIDs(w, r)
	if !ok {
		return
	}
	bindingID, err := util.ParseUUID(chi.URLParam(r, "bindingId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid binding id")
		return
	}
	actorType, actorID, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	if _, err := h.Service.DeleteBinding(r.Context(), wsID, credID, bindingID, actorType, actorID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListAudit handles GET /api/workspaces/{id}/credentials/{credId}/audit.
//
// JEH-1197: gated by read_redacted because the audit trail names every
// member/agent that has touched the credential. The same actor that may
// see the redacted credential is the one allowed to see its audit
// trail.
func (h *Handler) ListAudit(w http.ResponseWriter, r *http.Request) {
	wsID, credID, ok := h.credentialIDs(w, r)
	if !ok {
		return
	}
	actorType, actorID, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	cred, err := h.Service.Get(r.Context(), wsID, credID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if err := h.Service.AuthorizeRead(r.Context(), cred.WorkspaceID, cred.ID, Type(cred.Type), actorType, actorID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	limit := int32(100)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = int32(parsed)
		}
	}
	rows, err := h.Service.ListAudit(r.Context(), wsID, credID, limit)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	out := make([]auditResponse, len(rows))
	for i, row := range rows {
		out[i] = auditResponseFromModel(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": out})
}

type auditResponse struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	CredentialID string          `json:"credential_id"`
	Action       string          `json:"action"`
	ActorType    string          `json:"actor_type"`
	ActorID      string          `json:"actor_id"`
	Metadata     json.RawMessage `json:"metadata"`
	Result       string          `json:"result"`
	Reason       string          `json:"reason"`
	CreatedAt    string          `json:"created_at"`
}

func auditResponseFromModel(a cerebrodb.CerebroCredentialAudit) auditResponse {
	meta := json.RawMessage(a.Metadata)
	if len(meta) == 0 {
		meta = json.RawMessage("{}")
	}
	return auditResponse{
		ID:           util.UUIDToString(a.ID),
		WorkspaceID:  util.UUIDToString(a.WorkspaceID),
		CredentialID: util.UUIDToString(a.CredentialID),
		Action:       a.Action,
		ActorType:    a.ActorType,
		ActorID:      util.UUIDToString(a.ActorID),
		Metadata:     meta,
		Result:       a.Result,
		Reason:       a.Reason,
		CreatedAt:    util.TimestampToString(a.CreatedAt),
	}
}

func (h *Handler) credentialIDs(w http.ResponseWriter, r *http.Request) (wsID, credID pgtype.UUID, ok bool) {
	wsID, ok = workspaceIDFromRequest(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	credID, err := util.ParseUUID(chi.URLParam(r, "credId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid credential id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return wsID, credID, true
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrCredentialNotFound):
		writeError(w, http.StatusNotFound, "credential not found")
	case errors.Is(err, ErrBindingNotFound), errors.Is(err, ErrCredentialBindingMismatch):
		writeError(w, http.StatusNotFound, "binding not found")
	case errors.Is(err, ErrInvalidType):
		writeError(w, http.StatusBadRequest, "invalid type")
	case errors.Is(err, ErrInvalidName):
		writeError(w, http.StatusBadRequest, "name is required")
	case errors.Is(err, ErrInvalidValue):
		writeError(w, http.StatusBadRequest, "value is required")
	case errors.Is(err, ErrInvalidResourceType):
		writeError(w, http.StatusBadRequest, "resource_type is required")
	case errors.Is(err, ErrInvalidResourceID):
		writeError(w, http.StatusBadRequest, "resource_id is required")
	case errors.Is(err, ErrInvalidMetadata):
		writeError(w, http.StatusBadRequest, "metadata must be a JSON object")
	case errors.Is(err, ErrCredentialExists):
		writeError(w, http.StatusConflict, "a credential with this name and type already exists in this workspace")
	case errors.Is(err, ErrCipherMissing):
		writeError(w, http.StatusServiceUnavailable, "credentials encryption is not configured")
	case errors.Is(err, ErrInvalidCiphertext):
		writeError(w, http.StatusInternalServerError, "credential ciphertext is invalid")
	case errors.Is(err, ErrPolicyDenied):
		// JEH-1197: 403 carries the policy reason so a UI can surface
		// why a member or agent is blocked. The audit row already has
		// the same reason recorded server-side.
		reason := "policy denied this action"
		var pe *PolicyDeniedError
		if errors.As(err, &pe) && pe != nil {
			reason = pe.Error()
		}
		writeError(w, http.StatusForbidden, reason)
	default:
		slog.Error("credentials request failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "credentials request failed")
	}
}

func workspaceIDFromRequest(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	id := middleware.WorkspaceIDFromContext(r.Context())
	if id == "" {
		id = chi.URLParam(r, "id")
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return pgtype.UUID{}, false
	}
	parsed, err := util.ParseUUID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
		return pgtype.UUID{}, false
	}
	return parsed, true
}

// actorFromRequest mirrors references.requireCreator: task tokens carry an
// agent identity, user tokens carry an X-User-ID header. Reveal/Delete in
// particular MUST have a known actor — the audit row needs it.
func actorFromRequest(w http.ResponseWriter, r *http.Request) (string, pgtype.UUID, bool) {
	if scope := middleware.AuthScopeFromContext(r.Context()); scope == middleware.ScopeTask {
		ts := middleware.TaskScopeFromContext(r.Context())
		agentUUID, err := util.ParseUUID(ts.AgentID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid agent id on task token")
			return "", pgtype.UUID{}, false
		}
		return "agent", agentUUID, true
	}
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", pgtype.UUID{}, false
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", pgtype.UUID{}, false
	}
	return "member", userUUID, true
}

func parseOptionalTime(w http.ResponseWriter, raw *string, field string) (*time.Time, bool) {
	if raw == nil || *raw == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, field+" must be RFC3339")
		return nil, false
	}
	return &t, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
