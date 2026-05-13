package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	enterpriseLark "github.com/multica-ai/multica/server/internal/enterprise/lark"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type LarkLoginRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
}

func (h *Handler) LarkLogin(w http.ResponseWriter, r *http.Request) {
	var req LarkLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	profile, err := enterpriseLark.NewOAuthClientFromEnv().ExchangeCode(r.Context(), req.Code, req.RedirectURI)
	if err != nil {
		status := http.StatusBadGateway
		switch err.Error() {
		case "Lark login is not enabled":
			status = http.StatusNotFound
		case "Lark login is not configured":
			status = http.StatusServiceUnavailable
		case "Lark tenant is not allowed":
			status = http.StatusForbidden
		}
		slog.Warn("lark login failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, status, err.Error())
		return
	}

	user, isNew, err := h.findOrCreateLarkUser(r, profile)
	if err != nil {
		var signupErr SignupError
		if errors.As(err, &signupErr) {
			writeError(w, http.StatusForbidden, signupErr.Error())
			return
		}
		slog.Error("failed to bind lark user", append(logger.RequestAttrs(r), "error", err, "open_id", profile.OpenID)...)
		writeError(w, http.StatusInternalServerError, "failed to log in with Lark")
		return
	}
	if isNew {
		evt := analytics.Signup(uuidToString(user.ID), user.Email, signupSourceFromRequest(r))
		evt.Properties["auth_method"] = "lark"
		h.Analytics.Capture(evt)
	}

	tokenString, err := h.issueJWT(user)
	if err != nil {
		slog.Warn("lark login failed", append(logger.RequestAttrs(r), "error", err, "user_id", uuidToString(user.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	if err := auth.SetAuthCookies(w, tokenString); err != nil {
		slog.Warn("failed to set auth cookies", "error", err)
	}
	if h.CFSigner != nil {
		for _, cookie := range h.CFSigner.SignedCookies(time.Now().Add(30 * 24 * time.Hour)) {
			http.SetCookie(w, cookie)
		}
	}

	slog.Info("user logged in via lark", append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "open_id", profile.OpenID)...)
	writeJSON(w, http.StatusOK, LoginResponse{
		Token: tokenString,
		User:  userToResponse(user),
	})
}

func (h *Handler) findOrCreateLarkUser(r *http.Request, profile enterpriseLark.Profile) (db.User, bool, error) {
	user, err := h.Queries.GetUserByExternalIdentity(r.Context(), db.GetUserByExternalIdentityParams{
		Provider:  enterpriseLark.ProviderName,
		TenantKey: profile.TenantKey,
		UnionID:   profile.UnionID,
		OpenID:    profile.OpenID,
	})
	isNew := false
	if err != nil {
		if !isNotFound(err) {
			return db.User{}, false, err
		}
		email := larkEmail(profile)
		user, isNew, err = h.findOrCreateUser(r.Context(), email)
		if err != nil {
			return db.User{}, false, err
		}
	}

	user, err = h.updateUserFromLarkProfile(r, user, profile)
	if err != nil {
		return db.User{}, false, err
	}

	raw := profile.Raw
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if _, err := h.Queries.UpsertUserExternalIdentityByOpenID(r.Context(), db.UpsertUserExternalIdentityByOpenIDParams{
		UserID:         user.ID,
		Provider:       enterpriseLark.ProviderName,
		TenantKey:      profile.TenantKey,
		ExternalUserID: strToText(profile.ExternalUserID),
		OpenID:         strToText(profile.OpenID),
		UnionID:        strToText(profile.UnionID),
		Email:          strToText(profile.Email),
		Name:           strToText(profile.Name),
		AvatarUrl:      strToText(profile.AvatarURL),
		RawProfile:     raw,
	}); err != nil {
		return db.User{}, false, err
	}

	return user, isNew, nil
}

func (h *Handler) updateUserFromLarkProfile(r *http.Request, user db.User, profile enterpriseLark.Profile) (db.User, error) {
	name := strings.TrimSpace(profile.Name)
	avatarURL := strings.TrimSpace(profile.AvatarURL)
	if name == "" && avatarURL == "" {
		return user, nil
	}

	newName := user.Name
	newAvatar := user.AvatarUrl
	needsUpdate := false
	if name != "" && name != user.Name {
		newName = name
		needsUpdate = true
	}
	if avatarURL != "" && (!user.AvatarUrl.Valid || user.AvatarUrl.String != avatarURL) {
		newAvatar = pgtype.Text{String: avatarURL, Valid: true}
		needsUpdate = true
	}
	if !needsUpdate {
		return user, nil
	}
	updated, err := h.Queries.UpdateUser(r.Context(), db.UpdateUserParams{
		ID:        user.ID,
		Name:      newName,
		AvatarUrl: newAvatar,
	})
	if err != nil {
		return user, err
	}
	return updated, nil
}

func larkEmail(profile enterpriseLark.Profile) string {
	if profile.Email != "" {
		return strings.ToLower(strings.TrimSpace(profile.Email))
	}
	id := profile.UnionID
	if id == "" {
		id = profile.OpenID
	}
	replacer := strings.NewReplacer("@", "-", ":", "-", "/", "-", "\\", "-", " ", "-")
	return strings.ToLower(replacer.Replace(id)) + "@lark.local"
}
