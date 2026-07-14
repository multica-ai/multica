package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/pkg/redact"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Gitea has no GitHub-App "installation" concept, so a connection is a plain
// workspace ↔ (instance base URL, PAT) binding. This file owns the backend
// connection management — connect (validate the PAT + persist encrypted),
// list, and disconnect — plus the neutral Gitea REST client used to validate
// the token. The PAT never leaves the server: list/broadcast responses carry
// only display metadata, and the stored token is sealed with the Gitea
// secretbox (MUL-2671 §4.4).

// GiteaConnectionResponse is the JSON shape returned by the list endpoint and
// broadcast on connection events. The PAT is deliberately absent — it is
// write-only from the API's perspective.
type GiteaConnectionResponse struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	BaseURL          string  `json:"base_url"`
	AccountLogin     string  `json:"account_login"`
	AccountAvatarURL *string `json:"account_avatar_url"`
	CreatedAt        string  `json:"created_at"`
}

func giteaConnectionToResponse(c db.GiteaConnection) GiteaConnectionResponse {
	return GiteaConnectionResponse{
		ID:               uuidToString(c.ID),
		WorkspaceID:      uuidToString(c.WorkspaceID),
		BaseURL:          c.BaseUrl,
		AccountLogin:     c.AccountLogin,
		AccountAvatarURL: textToPtr(c.AccountAvatarUrl),
		CreatedAt:        timestampToString(c.CreatedAt),
	}
}

// giteaConnectRequest is the connect payload: the instance web base URL plus a
// personal access token to authenticate against it.
type giteaConnectRequest struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
}

// normalizeGiteaBaseURL trims and validates the instance URL, returning a
// canonical form (scheme + host + optional path, no trailing slash) so that
// `https://gitea.example.com/` and `https://gitea.example.com` map to the same
// UNIQUE(workspace_id, base_url) row.
func normalizeGiteaBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("base_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("base_url is not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("base_url must start with http:// or https://")
	}
	if u.Host == "" {
		return "", errors.New("base_url must include a host")
	}
	// Drop any query/fragment and the trailing slash on the path.
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// giteaAccount is the subset of Gitea's `GET /api/v1/user` response we keep.
type giteaAccount struct {
	Login     string
	AvatarURL string
}

// verifyGiteaPAT calls the instance's `GET /api/v1/user` with the PAT to
// confirm both that the base URL is reachable and that the token is valid,
// returning the authenticated account for display. Any failure is surfaced as
// a user-actionable error; the token is never included in the error text.
func verifyGiteaPAT(ctx context.Context, baseURL, token string) (giteaAccount, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/api/v1/user"
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return giteaAccount{}, fmt.Errorf("could not reach the Gitea instance: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "token "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return giteaAccount{}, errors.New("could not reach the Gitea instance at that base URL")
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return giteaAccount{}, errors.New("the personal access token was rejected by the Gitea instance")
	case resp.StatusCode != http.StatusOK:
		return giteaAccount{}, fmt.Errorf("the Gitea instance returned an unexpected status %d", resp.StatusCode)
	}

	var body struct {
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	}
	// Cap the read so a non-Gitea endpoint at that URL can't stream an
	// unbounded body into the decoder.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return giteaAccount{}, errors.New("the base URL did not respond with a Gitea API payload")
	}
	if strings.TrimSpace(body.Login) == "" {
		return giteaAccount{}, errors.New("the base URL did not respond with a Gitea API payload")
	}
	return giteaAccount{Login: body.Login, AvatarURL: body.AvatarURL}, nil
}

// ── Connect ─────────────────────────────────────────────────────────────────

// GiteaConnect (POST /api/workspaces/{id}/gitea/connections) validates the PAT
// against the instance, then persists an encrypted connection row. Re-posting
// the same base_url refreshes the token in place.
func (h *Handler) GiteaConnect(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if h.GiteaSecretBox == nil {
		writeError(w, http.StatusServiceUnavailable, "gitea integration is not configured on this server")
		return
	}

	var req giteaConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	baseURL, err := normalizeGiteaBaseURL(req.BaseURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	account, err := verifyGiteaPAT(r.Context(), baseURL, token)
	if err != nil {
		// redact.Text guards against the (unexpected) case of a secret echoed
		// back in the failure string before it reaches the client / logs.
		writeError(w, http.StatusBadRequest, redact.Text(err.Error()))
		return
	}

	sealed, err := h.GiteaSecretBox.Seal([]byte(token))
	if err != nil {
		slog.Error("gitea: failed to encrypt token", "err", err, "base_url", baseURL)
		writeError(w, http.StatusInternalServerError, "failed to store connection")
		return
	}

	connectedBy := pgtype.UUID{}
	if userID := requestUserID(r); userID != "" {
		if u, err := parseStrictUUID(userID); err == nil {
			connectedBy = u
		}
	}
	conn, err := h.Queries.CreateGiteaConnection(r.Context(), db.CreateGiteaConnectionParams{
		WorkspaceID:      wsUUID,
		BaseUrl:          baseURL,
		TokenEncrypted:   sealed,
		AccountLogin:     account.Login,
		AccountAvatarUrl: ptrToText(strPtrOrNil(account.AvatarURL)),
		ConnectedByID:    connectedBy,
	})
	if err != nil {
		slog.Error("gitea: failed to persist connection", "err", err, "base_url", baseURL)
		writeError(w, http.StatusInternalServerError, "failed to store connection")
		return
	}

	resp := giteaConnectionToResponse(conn)
	h.publish(protocol.EventGiteaConnectionCreated, workspaceID, "system", "", map[string]any{
		"connection": resp,
	})
	writeJSON(w, http.StatusCreated, resp)
}

// ── Listing / disconnect ────────────────────────────────────────────────────

// ListGiteaConnections returns the workspace's Gitea connections to any member.
// Connect/disconnect are admin-only at the router; the response carries a
// can_manage hint so the UI can gate those affordances. No secret is ever
// included, so unlike GitHub there is nothing to strip per role.
func (h *Handler) ListGiteaConnections(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, _ := middleware.MemberFromContext(r.Context())
	canManage := roleAllowed(member.Role, "owner", "admin")

	rows, err := h.Queries.ListGiteaConnectionsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list connections")
		return
	}
	out := make([]GiteaConnectionResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, giteaConnectionToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connections": out,
		"configured":  h.GiteaSecretBox != nil,
		"can_manage":  canManage,
	})
}

func (h *Handler) DeleteGiteaConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	id := chi.URLParam(r, "connectionId")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "connection id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteGiteaConnection(r.Context(), db.DeleteGiteaConnectionParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove connection")
		return
	}
	h.publish(protocol.EventGiteaConnectionDeleted, workspaceID, "system", "", map[string]any{
		"id": id,
	})
	w.WriteHeader(http.StatusNoContent)
}
