package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
)

// RefreshSessionResponse is the body returned by RefreshSession.
//
// Token is present only for bearer-authenticated callers (Desktop, mobile),
// which hold the session as a string and have to persist the new one. A
// cookie-authenticated browser gets the renewed session as a Set-Cookie
// header and never sees the JWT — handing it back in a readable body would
// undo the point of the HttpOnly cookie.
//
// Renewed=false is a normal answer, not an error: it means the caller asked
// before the session entered its renewal window. Callers should read
// CheckAgainInSeconds rather than deciding a cadence for themselves.
type RefreshSessionResponse struct {
	Token               string `json:"token,omitempty"`
	ExpiresAt           string `json:"expires_at"`
	Renewed             bool   `json:"renewed"`
	CheckAgainInSeconds int    `json:"check_again_in_seconds"`
}

// RefreshSession extends the interactive session that authenticated this
// request, when that session is inside its renewal window.
//
// Browsers do not need this endpoint — the auth middleware re-issues their
// cookie inline on any authenticated safe request. It exists for the clients
// that carry the session as a string and therefore need the new string back:
// Desktop (localStorage) and mobile (Keychain). They call it at launch, when
// returning to the foreground, and on use; the server owns the "is it time
// yet" decision so no client ever has to read `exp` or trust its own clock.
//
// Only our own UI session JWTs are refreshable. A PAT (mul_), an agent task
// token (mat_) and a cloud-node PAT (mcn_) all authenticate perfectly well at
// the middleware, and every one of them must be refused here: none of them is
// an interactive session, and exchanging a machine credential for one would
// turn a scoped, revocable token into an unscoped, unrevocable one. An
// expired JWT cannot reach this handler at all (the middleware rejects it),
// and ShouldRenewSession refuses a non-positive remaining lifetime anyway —
// renewal extends a live session, it never resurrects a dead one.
func (h *Handler) RefreshSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// The middleware resolved the credential to a user but does not pass the
	// raw token forward, and renewal needs the claims themselves. Re-read it
	// from the same two places extractToken looks, in the same order.
	rawToken, fromCookie := sessionTokenFromRequest(r)
	if rawToken == "" {
		writeError(w, http.StatusBadRequest, "only interactive sessions can be refreshed")
		return
	}

	claims, err := auth.ParseSessionToken(rawToken)
	if err != nil {
		// Reached by PAT / task-token / cloud-PAT callers: authenticated, but
		// holding something that is not a session. 400 rather than 401 — the
		// credential is fine, the request is not.
		writeError(w, http.StatusBadRequest, "only interactive sessions can be refreshed")
		return
	}

	// Defense in depth, mirroring RenewCurrentPersonalAccessToken: the
	// middleware set X-User-ID from these very claims, so a mismatch means a
	// header was forged past it. Refuse to mint a session for someone else.
	if sub, _ := claims["sub"].(string); sub != userID {
		writeError(w, http.StatusUnauthorized, "token does not belong to caller")
		return
	}

	checkAgain := int(auth.SessionRenewCheckInterval().Seconds())
	expiresAt := auth.SessionExpiry(claims)

	if !auth.ShouldRenewSession(time.Now(), expiresAt) {
		writeJSON(w, http.StatusOK, RefreshSessionResponse{
			ExpiresAt:           formatSessionExpiry(expiresAt),
			Renewed:             false,
			CheckAgainInSeconds: checkAgain,
		})
		return
	}

	newToken, newExpiresAt, err := auth.RenewSessionToken(claims)
	if err != nil {
		if errors.Is(err, auth.ErrTemporarilyDisabledUser) {
			writeError(w, http.StatusForbidden, auth.TemporarilyDisabledUserError)
			return
		}
		slog.Warn("session refresh: failed to re-sign token",
			append(logger.RequestAttrs(r), "error", err, "user_id", userID)...)
		// The caller's current session is still valid — this failure costs it
		// nothing but a retry, so answer with the unrenewed state rather than
		// an error a client might mistake for a dead session.
		writeJSON(w, http.StatusOK, RefreshSessionResponse{
			ExpiresAt:           formatSessionExpiry(expiresAt),
			Renewed:             false,
			CheckAgainInSeconds: checkAgain,
		})
		return
	}

	resp := RefreshSessionResponse{
		ExpiresAt:           formatSessionExpiry(newExpiresAt),
		Renewed:             true,
		CheckAgainInSeconds: checkAgain,
	}

	if fromCookie {
		if err := auth.SetAuthCookies(w, newToken); err != nil {
			slog.Warn("session refresh: failed to set auth cookies",
				append(logger.RequestAttrs(r), "error", err)...)
			writeJSON(w, http.StatusOK, RefreshSessionResponse{
				ExpiresAt:           formatSessionExpiry(expiresAt),
				Renewed:             false,
				CheckAgainInSeconds: checkAgain,
			})
			return
		}
		if h.CFSigner != nil {
			for _, cookie := range h.CFSigner.SignedCookies(newExpiresAt) {
				http.SetCookie(w, cookie)
			}
		}
	} else {
		resp.Token = newToken
	}

	writeJSON(w, http.StatusOK, resp)
}

// sessionTokenFromRequest mirrors middleware.extractToken: Authorization
// header first, auth cookie second. The bool reports the cookie path, which
// is what decides whether the renewed token may be written into the response
// body.
func sessionTokenFromRequest(r *http.Request) (token string, fromCookie bool) {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		if tokenString := strings.TrimPrefix(authHeader, "Bearer "); tokenString != authHeader {
			return tokenString, false
		}
	}
	if cookie, err := r.Cookie(auth.AuthCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, true
	}
	return "", false
}

func formatSessionExpiry(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
