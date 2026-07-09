package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// PATRenewThreshold is the remaining-lifetime window at which a PAT becomes
// eligible for an in-place renewal. The daemon polls every ~3 days, so a 7-day
// threshold guarantees at least one renewal attempt while the token still has
// ≥ 4 days of validity left — enough margin to absorb a transient network
// failure before the user actually has to re-run `multica login`.
const PATRenewThreshold = 7 * 24 * time.Hour

// PATRenewExtension is how far into the future a renewed PAT's expires_at is
// pushed. Matches the initial issuance window in CreatePersonalAccessToken
// (90 days) so renewed tokens converge on the same lifetime as freshly minted
// ones — no second-class renewed tokens.
const PATRenewExtension = 90 * 24 * time.Hour

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

// RenewPATResponse is the body returned by RenewCurrentPersonalAccessToken.
//
// Renewed=false is a no-op, not an error — it just means the caller polled
// before the token entered the renewal window. Callers should always read
// ExpiresAt for the authoritative expiry rather than assuming the old value
// is still current.
type RenewPATResponse struct {
	ExpiresAt string `json:"expires_at"`
	Renewed   bool   `json:"renewed"`
}

// RenewCurrentPersonalAccessToken extends the expires_at of the PAT used to
// authenticate this request, in-place, when it is inside the renewal window.
//
// The endpoint deliberately does NOT mint a new token — that would require
// either rotating the raw secret (breaks the CLI/daemon multi-process model,
// where a single PAT is shared by every process started from the same CLI
// config) or returning the raw token over the wire on every poll (a needless
// exposure since the daemon already holds it). Instead we extend the row's
// expires_at atomically; the cached PAT entry's TTL is short enough
// (auth.AuthCacheTTL ≤ 10m) that the cache catches up to the new expiry on
// the next cache miss without an explicit invalidation.
//
// Only mul_ PATs may be renewed: a cookie/JWT session has no PAT row to
// extend, and an mat_ task token is single-purpose and short-lived. mcn_
// cloud-node PATs are owned by Multica Cloud Fleet, not us — we don't even
// see the expiry locally.
func (h *Handler) RenewCurrentPersonalAccessToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Re-read the raw token from the Authorization header — the upstream Auth
	// middleware resolves it to a userID but doesn't pass the hash forward,
	// and we need the row, not just the user.
	authHeader := r.Header.Get("Authorization")
	rawToken := strings.TrimPrefix(authHeader, "Bearer ")
	if rawToken == "" || rawToken == authHeader || !strings.HasPrefix(rawToken, "mul_") {
		writeError(w, http.StatusBadRequest, "only personal access tokens can be renewed")
		return
	}

	hash := auth.HashToken(rawToken)
	pat, err := h.Queries.GetPersonalAccessTokenByHash(r.Context(), hash)
	if err != nil {
		// The Auth middleware already validated the token, so reaching here
		// with no row means the PAT was revoked or expired in the gap between
		// the middleware's cache hit and this DB read. Surface a 401 so the
		// daemon's 401 branch fires the same "please re-login" message it
		// would for any other auth failure, instead of a generic 500.
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "token is no longer valid")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up token")
		return
	}

	// Defense in depth: the middleware already set X-User-ID from the same
	// PAT row, so this mismatch should be impossible. If it ever fires, it
	// means a header was forged past the middleware and we MUST refuse to
	// renew on someone else's behalf — fail loudly.
	if uuidToString(pat.UserID) != userID {
		writeError(w, http.StatusUnauthorized, "token does not belong to caller")
		return
	}

	// PATs minted before this code existed may have a NULL expires_at (the
	// "never expires" case). There is nothing to extend — return the current
	// (absent) expiry and let the caller treat this as a permanent token.
	if !pat.ExpiresAt.Valid {
		writeJSON(w, http.StatusOK, RenewPATResponse{ExpiresAt: "", Renewed: false})
		return
	}

	now := time.Now()
	remaining := pat.ExpiresAt.Time.Sub(now)
	if remaining > PATRenewThreshold {
		writeJSON(w, http.StatusOK, RenewPATResponse{
			ExpiresAt: timestampToString(pat.ExpiresAt),
			Renewed:   false,
		})
		return
	}

	newExpiresAt := pgtype.Timestamptz{Time: now.Add(PATRenewExtension), Valid: true}
	// Pass the renewal threshold as the CAS predicate: only update if the
	// row's existing expires_at is still inside this window. After the
	// first writer succeeds the row sits at now+90d, which is well past
	// now+7d, so any concurrent renewer hits the WHERE and sees ErrNoRows.
	renewThreshold := pgtype.Timestamptz{Time: now.Add(PATRenewThreshold), Valid: true}
	updated, err := h.Queries.ExtendPersonalAccessTokenExpiry(r.Context(), db.ExtendPersonalAccessTokenExpiryParams{
		ID:               pat.ID,
		NewExpiresAt:     newExpiresAt,
		RenewThresholdAt: renewThreshold,
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, RenewPATResponse{
			ExpiresAt: timestampToString(updated),
			Renewed:   true,
		})
	case errors.Is(err, pgx.ErrNoRows):
		// A concurrent renew (or revoke) won the race. Re-read the current
		// row and report what's there now — the daemon's only correctness
		// guarantee is "after a successful call, expires_at is fresh enough
		// to last until the next poll", and a parallel writer already
		// satisfied that, so this is success from the caller's POV.
		current, getErr := h.Queries.GetPersonalAccessTokenByHash(r.Context(), hash)
		if getErr != nil {
			writeError(w, http.StatusUnauthorized, "token is no longer valid")
			return
		}
		writeJSON(w, http.StatusOK, RenewPATResponse{
			ExpiresAt: timestampToString(current.ExpiresAt),
			Renewed:   false,
		})
	default:
		writeError(w, http.StatusInternalServerError, "failed to renew token")
	}
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
	hash, err := h.Queries.RevokePersonalAccessToken(r.Context(), db.RevokePersonalAccessTokenParams{
		ID:     idUUID,
		UserID: parseUUID(userID),
	})
	switch {
	case err == nil:
		// Drop the cache entry immediately so the revocation takes effect
		// before the TTL would otherwise expire the cached lookup.
		h.PATCache.Invalidate(r.Context(), hash)
	case errors.Is(err, pgx.ErrNoRows):
		// Token doesn't exist or doesn't belong to this user. Preserve the
		// pre-existing idempotent 204 behavior — no cache entry to clear.
	default:
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
