// Package servicetokenapi is the HTTP surface for service tokens. It lives in
// a sibling package so the core servicetoken package (which the upstream auth
// middleware imports for the msv_ branch) stays free of any internal/middleware
// dependency — that dependency lives only here, and only here needs it.
package servicetokenapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/servicetoken"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// serviceReadLimit caps rows returned by the machine read surface so a single
// call cannot pull an unbounded workspace history.
const serviceReadLimit = 200

// Handler serves two distinct surfaces:
//
//   - Management (/api/service-tokens): owner/admin CRUD over the credentials,
//     authenticated as a human member (gated by RequireWorkspaceRole).
//   - Machine (/api/service/...): the scoped, read-only data surface a service
//     token itself calls, gated by servicetoken.RequireScope.
type Handler struct {
	tokens *servicetoken.TokenService
	q      *db.Queries
}

// NewHandler builds the HTTP handler over the token service and the data
// dependencies its read-only machine surface exposes.
func NewHandler(tokens *servicetoken.TokenService, q *db.Queries) *Handler {
	return &Handler{tokens: tokens, q: q}
}

// ---------------------------------------------------------------------------
// Management surface (human owner/admin)
// ---------------------------------------------------------------------------

type tokenResponse struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"token_prefix"`
	Scopes     []string `json:"scopes"`
	ExpiresAt  *string  `json:"expires_at"`
	LastUsedAt *string  `json:"last_used_at"`
	Revoked    bool     `json:"revoked"`
	CreatedAt  string   `json:"created_at"`
}

type createTokenResponse struct {
	tokenResponse
	// Token is the raw secret, returned exactly once at creation.
	Token string `json:"token"`
}

func toTokenResponse(t servicetoken.Token) tokenResponse {
	return tokenResponse{
		ID:         t.ID,
		Name:       t.Name,
		Prefix:     t.TokenPrefix,
		Scopes:     t.Scopes,
		ExpiresAt:  timePtrToString(t.ExpiresAt),
		LastUsedAt: timePtrToString(t.LastUsedAt),
		Revoked:    t.Revoked,
		CreatedAt:  t.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type createTokenRequest struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes"`
	ExpiresInDays *int     `json:"expires_in_days"`
}

// CreateToken mints a new service token for the caller's workspace. Gated by
// RequireWorkspaceRole(owner, admin) at the router, so MemberFromContext
// resolves to the acting human.
func (h *Handler) CreateToken(w http.ResponseWriter, r *http.Request) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	if !h.requireEnabled(w, r, workspaceID) {
		return
	}

	var req createTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if req.ExpiresInDays == nil || *req.ExpiresInDays < 1 || *req.ExpiresInDays > servicetoken.MaxExpiryDays {
		writeError(w, http.StatusBadRequest, "expires_in_days is required and must be between 1 and 365")
		return
	}
	expiresAt := time.Now().Add(time.Duration(*req.ExpiresInDays) * 24 * time.Hour)

	tok, raw, err := h.tokens.Mint(r.Context(), workspaceID, req.Name, req.Scopes, &expiresAt, util.UUIDToString(member.UserID))
	if err != nil {
		// NormalizeScopes rejects unknown/empty scope sets with a caller
		// error; surface those as 400 rather than 500.
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, createTokenResponse{
		tokenResponse: toTokenResponse(tok),
		Token:         raw,
	})
}

// ListTokens returns the workspace's service tokens (never the secret/hash).
func (h *Handler) ListTokens(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	if !h.requireEnabled(w, r, workspaceID) {
		return
	}
	tokens, err := h.tokens.List(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list service tokens")
		return
	}
	resp := make([]tokenResponse, len(tokens))
	for i, t := range tokens {
		resp[i] = toTokenResponse(t)
	}
	writeJSON(w, http.StatusOK, resp)
}

// RevokeToken revokes one token. Idempotent — an unknown or already-revoked id
// still returns 204 (matching the PAT revoke contract).
func (h *Handler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	if !h.requireEnabled(w, r, workspaceID) {
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := util.ParseUUID(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid token id")
		return
	}
	if _, err := h.tokens.Revoke(r.Context(), id, workspaceID, util.UUIDToString(member.UserID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke service token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) requireEnabled(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	enabled, err := h.tokens.Enabled(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "service token feature is unavailable")
		return false
	}
	if !enabled {
		writeError(w, http.StatusNotFound, "service token feature is disabled")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Machine surface (service token, scoped)
// ---------------------------------------------------------------------------

// serviceWorkspace returns the workspace the calling service token is bound
// to. AuthBranch stamped it into X-Workspace-ID; a service token can never
// reach here with any other workspace.
func serviceWorkspace(r *http.Request) (pgtype.UUID, bool) {
	ws := r.Header.Get("X-Workspace-ID")
	u, err := util.ParseUUID(ws)
	if err != nil {
		return pgtype.UUID{}, false
	}
	return u, true
}

type skillResponse struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	CurrentVersion string `json:"current_version"`
	UpdatedAt      string `json:"updated_at"`
}

// ListSkills serves the skills:read scope — the immediate Atlas use case.
func (h *Handler) ListSkills(w http.ResponseWriter, r *http.Request) {
	ws, ok := serviceWorkspace(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "workspace not resolved")
		return
	}
	skills, err := h.q.ListSkillsByWorkspace(r.Context(), ws)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skills")
		return
	}
	resp := make([]skillResponse, len(skills))
	for i, s := range skills {
		resp[i] = skillResponse{
			ID:             util.UUIDToString(s.ID),
			Name:           s.Name,
			Description:    s.Description,
			CurrentVersion: s.CurrentVersion,
			UpdatedAt:      util.TimestampToString(s.UpdatedAt),
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type agentResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	RuntimeMode string `json:"runtime_mode"`
}

// ListAgents serves the agents:read scope.
func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	ws, ok := serviceWorkspace(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "workspace not resolved")
		return
	}
	agents, err := h.q.ListAgents(r.Context(), ws)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	resp := make([]agentResponse, len(agents))
	for i, a := range agents {
		resp[i] = agentResponse{
			ID:          util.UUIDToString(a.ID),
			Name:        a.Name,
			Description: a.Description,
			Status:      a.Status,
			RuntimeMode: a.RuntimeMode,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type issueResponse struct {
	ID       string `json:"id"`
	Number   int32  `json:"number"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

// ListIssues serves the issues:read scope. A workspace-bound machine read is
// treated as workspace-admin for visibility (is_admin), scoped to the token's
// own workspace — it can never see another workspace's issues.
func (h *Handler) ListIssues(w http.ResponseWriter, r *http.Request) {
	ws, ok := serviceWorkspace(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "workspace not resolved")
		return
	}
	issues, err := h.q.ListIssues(r.Context(), db.ListIssuesParams{
		WorkspaceID: ws,
		Limit:       serviceReadLimit,
		Offset:      0,
		IsAdmin:     true,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issues")
		return
	}
	resp := make([]issueResponse, len(issues))
	for i, is := range issues {
		resp[i] = issueResponse{
			ID:       util.UUIDToString(is.ID),
			Number:   is.Number,
			Title:    is.Title,
			Status:   is.Status,
			Priority: is.Priority,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func timePtrToString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
