package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type PersonalAccessTokenResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"token_prefix"`
	ExpiresAt  *string `json:"expires_at"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

type CreatePATResponse struct {
	PersonalAccessTokenResponse
	Token string `json:"token"`
}

func patToResponse(pat db.PersonalAccessToken) PersonalAccessTokenResponse {
	return PersonalAccessTokenResponse{
		ID:         uuidToString(pat.ID),
		Name:       pat.Name,
		Prefix:     pat.TokenPrefix,
		ExpiresAt:  timestampToPtr(pat.ExpiresAt),
		LastUsedAt: timestampToPtr(pat.LastUsedAt),
		CreatedAt:  timestampToString(pat.CreatedAt),
	}
}

type CreatePATRequest struct {
	Name          string `json:"name"`
	ExpiresInDays *int   `json:"expires_in_days"`
}

func (h *Handler) CreatePersonalAccessToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreatePATRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	rawToken, err := auth.GeneratePATToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	var expiresAt pgtype.Timestamptz
	if req.ExpiresInDays != nil && *req.ExpiresInDays > 0 {
		expiresAt = pgtype.Timestamptz{
			Time:  time.Now().Add(time.Duration(*req.ExpiresInDays) * 24 * time.Hour),
			Valid: true,
		}
	}

	prefix := rawToken
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}

	pat, err := h.Queries.CreatePersonalAccessToken(r.Context(), db.CreatePersonalAccessTokenParams{
		UserID:      parseUUID(userID),
		Name:        req.Name,
		TokenHash:   auth.HashToken(rawToken),
		TokenPrefix: prefix,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	writeJSON(w, http.StatusCreated, CreatePATResponse{
		PersonalAccessTokenResponse: patToResponse(pat),
		Token:                       rawToken,
	})
}

func (h *Handler) ListPersonalAccessTokens(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	pats, err := h.Queries.ListPersonalAccessTokensByUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}

	resp := make([]PersonalAccessTokenResponse, len(pats))
	for i, pat := range pats {
		resp[i] = patToResponse(pat)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RevokePersonalAccessToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "token id")
	if !ok {
		return
	}
	if err := h.Queries.RevokePersonalAccessToken(r.Context(), db.RevokePersonalAccessTokenParams{
		ID:     idUUID,
		UserID: parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
