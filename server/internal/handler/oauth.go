package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type oauthLoginRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
}

// OAuthLogin is the single entry point for all OAuth providers, dispatched
// by the {provider} URL param against h.OAuthProviders. Adding a new provider
// is one ProviderSpec entry in the registry — no new route or handler.
func (h *Handler) OAuthLogin(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "provider")
	provider, ok := h.OAuthProviders[providerID]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown provider")
		return
	}
	if !provider.Configured() {
		writeError(w, http.StatusServiceUnavailable, providerID+" login is not configured")
		return
	}

	var req oauthLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	redirectURI := req.RedirectURI
	if redirectURI == "" {
		redirectURI = provider.RedirectURI()
	}

	ctx := r.Context()
	accessToken, err := provider.Exchange(ctx, req.Code, redirectURI)
	if err != nil {
		slog.Error(providerID+" code exchange failed", "error", err)
		writeError(w, http.StatusBadRequest, "failed to exchange code with "+providerID)
		return
	}

	profile, err := provider.FetchProfile(ctx, accessToken)
	if err != nil {
		slog.Warn(providerID+" user fetch failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to fetch user from "+providerID)
		return
	}

	profile.Email = strings.ToLower(strings.TrimSpace(profile.Email))
	if profile.Email == "" {
		writeError(w, http.StatusForbidden, providerID+" account has no email")
		return
	}

	h.completeOAuthLogin(w, r, profile, providerID)
}

func (h *Handler) completeOAuthLogin(w http.ResponseWriter, r *http.Request, p auth.OAuthProfile, providerID string) {
	ctx := r.Context()
	user, isNew, err := h.findOrCreateUser(ctx, p.Email)
	if err != nil {
		var signupErr SignupError
		if errors.As(err, &signupErr) {
			writeError(w, http.StatusForbidden, signupErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	if isNew {
		evt := analytics.Signup(uuidToString(user.ID), user.Email, signupSourceFromRequest(r))
		evt.Properties["auth_method"] = providerID
		h.Analytics.Capture(evt)
	}

	needsUpdate := false
	newName := user.Name
	newAvatar := user.AvatarUrl
	if p.Name != "" {
		// Only backfill the name when the stored name still matches the
		// email-prefix default that findOrCreateUser assigns to new rows;
		// avoid overwriting a name the user later customised.
		defaultName := p.Email
		if at := strings.Index(p.Email, "@"); at > 0 {
			defaultName = p.Email[:at]
		}
		if user.Name == defaultName {
			newName = p.Name
			needsUpdate = true
		}
	}
	if p.Picture != "" && !user.AvatarUrl.Valid {
		newAvatar = pgtype.Text{String: p.Picture, Valid: true}
		needsUpdate = true
	}
	if needsUpdate {
		if updated, uErr := h.Queries.UpdateUser(ctx, db.UpdateUserParams{
			ID:        user.ID,
			Name:      newName,
			AvatarUrl: newAvatar,
		}); uErr == nil {
			user = updated
		}
	}

	tokenString, err := h.issueJWT(user)
	if err != nil {
		slog.Warn(providerID+" login failed", append(logger.RequestAttrs(r), "error", err, "email", p.Email)...)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	if err := auth.SetAuthCookies(w, tokenString); err != nil {
		slog.Warn("failed to set auth cookies", "error", err)
	}
	if h.CFSigner != nil {
		for _, cookie := range h.CFSigner.SignedCookies(time.Now().Add(72 * time.Hour)) {
			http.SetCookie(w, cookie)
		}
	}

	slog.Info("user logged in via "+providerID, append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "email", user.Email)...)
	writeJSON(w, http.StatusOK, LoginResponse{
		Token: tokenString,
		User:  userToResponse(user),
	})
}
