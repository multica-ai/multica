package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Service accounts (MUL-9163) are workspace-scoped machine identities: a user
// row with kind='service' that is a member of exactly one workspace and
// authenticates with an ordinary mul_ PAT. This file implements the owner/admin
// surface that mints, lists, rotates, and revokes those credentials.
//
// Scoping is the workspace membership itself: ListDaemonWorkspaces returns only
// the workspaces the authenticated user belongs to, so a daemon enrolled with a
// service-account token discovers exactly its one workspace — the server-side,
// auditable control the maintainers asked for in #4375, with no daemon-side
// allowlist.

// ServiceAccountEmailSuffix marks generated service account emails. The local
// part embeds a random UUID, so emails are unique and — because nothing ever
// sends mail to them — are never a reachable mailbox. Login is refused for
// kind='service' rows regardless of the domain, so the domain is not the
// security boundary.
const ServiceAccountEmailSuffix = ".multica.service"

type ServiceAccountResponse struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Email        string  `json:"email"`
	CreatedAt    string  `json:"created_at"`
	ActiveTokens int64   `json:"active_tokens"`
	LastUsedAt   *string `json:"last_used_at"`
}

type CreateServiceAccountRequest struct {
	Name          string `json:"name"`
	ExpiresInDays *int   `json:"expires_in_days"`
}

// ServiceAccountIssuedResponse is returned once per credential mint. Token is
// the only time the raw credential appears; only workspace owner/admin mint
// tokens, and the service account itself cannot (RequireUserAccount blocks
// self-minting).
type ServiceAccountIssuedResponse struct {
	ServiceAccountResponse
	Token string `json:"token"`
}

// serviceAccountExpiresAt resolves the optional expiry for a minted token. A
// zero pgtype.Timestamptz (Valid=false) means "never expires", the sensible
// default for a credential living inside a Kubernetes secret.
func serviceAccountExpiresAt(days *int) pgtype.Timestamptz {
	if days == nil || *days <= 0 {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{
		Time:  time.Now().Add(time.Duration(*days) * 24 * time.Hour),
		Valid: true,
	}
}

func mintServiceAccountToken(ctx context.Context, qtx *db.Queries, userID pgtype.UUID, expiresAt pgtype.Timestamptz) (string, db.PersonalAccessToken, error) {
	rawToken, err := auth.GeneratePATToken()
	if err != nil {
		return "", db.PersonalAccessToken{}, err
	}
	prefix := rawToken
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	pat, err := qtx.CreatePersonalAccessToken(ctx, db.CreatePersonalAccessTokenParams{
		UserID:      userID,
		Name:        "service account credential",
		TokenHash:   auth.HashToken(rawToken),
		TokenPrefix: prefix,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		return "", db.PersonalAccessToken{}, err
	}
	return rawToken, pat, nil
}

func issuedServiceAccountResponse(user db.User, token string) ServiceAccountIssuedResponse {
	return ServiceAccountIssuedResponse{
		ServiceAccountResponse: ServiceAccountResponse{
			ID:        uuidToString(user.ID),
			Name:      user.Name,
			Email:     user.Email,
			CreatedAt: timestampToString(user.CreatedAt),
		},
		Token: token,
	}
}

// CreateServiceAccount mints a service account (user + membership + initial
// credential) in one transaction. Owner/admin only — the route group enforces
// it, and the service account itself cannot mint credentials.
func (h *Handler) CreateServiceAccount(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	var req CreateServiceAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	expiresAt := serviceAccountExpiresAt(req.ExpiresInDays)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create service account")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	user, err := qtx.CreateServiceAccountUser(r.Context(), db.CreateServiceAccountUserParams{
		Name:          req.Name,
		WorkspaceSlug: pgtype.Text{String: ws.Slug, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create service account")
		return
	}
	if _, err := qtx.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: wsUUID,
		UserID:      user.ID,
		Role:        "member",
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create service account")
		return
	}
	rawToken, _, err := mintServiceAccountToken(r.Context(), qtx, user.ID, expiresAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create service account")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create service account")
		return
	}

	writeJSON(w, http.StatusCreated, issuedServiceAccountResponse(user, rawToken))
}

// ListServiceAccounts shows the workspace's service accounts with token state.
// Owner/admin only, matching the mint surface.
func (h *Handler) ListServiceAccounts(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	rows, err := h.Queries.ListServiceAccountsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list service accounts")
		return
	}
	out := make([]ServiceAccountResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, serviceAccountToResponse(row))
	}
	writeJSON(w, http.StatusOK, out)
}

type CreateServiceAccountTokenRequest struct {
	ExpiresInDays *int `json:"expires_in_days"`
}

// CreateServiceAccountToken mints an additional credential for an existing
// service account (rotation without downtime: mint, deploy the replacement,
// then revoke the old one).
func (h *Handler) CreateServiceAccountToken(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req CreateServiceAccountTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if _, err := h.Queries.GetServiceAccountMember(r.Context(), db.GetServiceAccountMemberParams{
		UserID:      accountID,
		WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "service account not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to mint token")
		return
	}

	rawToken, _, err := mintServiceAccountToken(r.Context(), h.Queries, accountID, serviceAccountExpiresAt(req.ExpiresInDays))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mint token")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mint token")
		return
	}

	writeJSON(w, http.StatusCreated, issuedServiceAccountResponse(user, rawToken))
}

// RevokeServiceAccountToken revokes one of a workspace service account's
// credentials. The EXISTS clause in the query makes the revoke a no-op unless
// the token belongs to a service account still in the caller's workspace.
func (h *Handler) RevokeServiceAccountToken(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	tokenID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "tokenId"), "token id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	if _, err := h.Queries.RevokeServiceAccountToken(r.Context(), db.RevokeServiceAccountTokenParams{
		ID:          tokenID,
		WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func serviceAccountToResponse(row db.ListServiceAccountsByWorkspaceRow) ServiceAccountResponse {
	return ServiceAccountResponse{
		ID:           uuidToString(row.ID),
		Name:         row.Name,
		Email:        row.Email,
		CreatedAt:    timestampToString(row.CreatedAt),
		ActiveTokens: row.ActiveTokenCount,
		LastUsedAt:   timestampToPtr(row.LastUsedAt),
	}
}
