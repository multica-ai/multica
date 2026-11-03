package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/mobilepush"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Mobile push registrations live no longer than the login that created them.
// Require a signed human session JWT, even though the surrounding auth group
// also accepts machine/PAT credentials for other endpoints.
func mobilePushSession(r *http.Request) (string, time.Time, bool) {
	raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	token, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) { return auth.JWTSecret(), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return "", time.Time{}, false
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", time.Time{}, false
	}
	sub, err := claims.GetSubject()
	if err != nil || sub == "" || sub != r.Header.Get("X-User-ID") {
		return "", time.Time{}, false
	}
	expiry, err := claims.GetExpirationTime()
	if err != nil || expiry == nil {
		return "", time.Time{}, false
	}
	return sub, expiry.Time, true
}

func (h *Handler) MobilePushSubscription(w http.ResponseWriter, r *http.Request) {
	user, expiry, ok := mobilePushSession(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "mobile push requires a user session")
		return
	}
	if r.Method != http.MethodDelete && !h.MobilePushEnabled {
		writeError(w, http.StatusServiceUnavailable, "mobile push is not configured")
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || !mobilepush.ValidToken(req.Token) {
		writeError(w, http.StatusBadRequest, "invalid push token")
		return
	}
	userID, ok := parseUUIDOrBadRequest(w, user, "user_id")
	if !ok {
		return
	}
	var err error
	if r.Method == http.MethodDelete {
		err = h.Queries.DeleteMobilePushSubscription(r.Context(), db.DeleteMobilePushSubscriptionParams{Token: req.Token, UserID: userID})
	} else {
		err = h.Queries.UpsertMobilePushSubscription(r.Context(), db.UpsertMobilePushSubscriptionParams{Token: req.Token, UserID: userID, ExpiresAt: pgtype.Timestamptz{Time: expiry, Valid: true}})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save push subscription")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
