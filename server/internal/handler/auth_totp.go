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
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Account-level attempt budget for codes checked against an enabled TOTP
// secret. authVerifyRL is per IP and disabled without Redis, so it cannot
// bound guesses against a six-digit code on its own.
const (
	totpMaxFailedAttempts = 5
	totpLockDuration      = 15 * time.Minute
)

type totpCheck int

const (
	totpAccepted totpCheck = iota
	totpRejected
	totpLocked
)

// checkEnabledTOTPCode verifies a code for an enabled TOTP secret under the
// account-level attempt budget and consumes its time step, so the same code
// cannot be accepted twice. Wrong and replayed codes count as failures.
func (h *Handler) checkEnabledTOTPCode(r *http.Request, userID pgtype.UUID, sealed []byte, lockedUntil pgtype.Timestamptz, code string) (totpCheck, error) {
	now := time.Now()
	if lockedUntil.Valid && lockedUntil.Time.After(now) {
		return totpLocked, nil
	}
	secret, err := h.TOTPService.OpenSecret(sealed)
	if err != nil {
		return totpRejected, err
	}
	if step, ok := h.TOTPService.MatchCode(secret, code, now); ok {
		consumed, err := h.Queries.ConsumeUserTOTPStep(r.Context(), db.ConsumeUserTOTPStepParams{
			ID:   userID,
			Step: step,
		})
		if err != nil {
			return totpRejected, err
		}
		if consumed == 1 {
			return totpAccepted, nil
		}
	}
	if err := h.Queries.RecordUserTOTPFailure(r.Context(), db.RecordUserTOTPFailureParams{
		ID:          userID,
		MaxAttempts: totpMaxFailedAttempts,
		LockUntil:   pgtype.Timestamptz{Time: now.Add(totpLockDuration), Valid: true},
	}); err != nil {
		return totpRejected, err
	}
	return totpRejected, nil
}

// POST /api/auth/totp/setup-init
// Authenticated. Generates a fresh TOTP secret for the current user,
// encrypts it, stores it with totp_enabled_at = NULL (pending verification),
// and returns the base32 secret + otpauth:// URI for the QR code.
func (h *Handler) TOTPSetupInit(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if h.TOTPService == nil {
		writeError(w, http.StatusServiceUnavailable, "totp not configured on this server")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	// P1-B guard: if TOTP is already enabled (enabled_at non-NULL), refuse to
	// overwrite the existing secret. Without this, a session-authenticated
	// user could effectively disable TOTP by calling setup-init without
	// presenting the current code — defeating the disable endpoint's
	// session-hijack defense (Task 5).
	// To rotate: disable first (requires current code) then set up again.
	if user.TotpEnabledAt.Valid {
		writeError(w, http.StatusConflict, "TOTP already enabled; disable it first (requires current code) before setting up a new authenticator")
		return
	}

	secret, otpAuthURL, err := h.TOTPService.GenerateSecret(user.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}
	sealed, err := h.TOTPService.SealSecret(secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt secret")
		return
	}
	stored, err := h.Queries.SetUserTOTPSecret(r.Context(), db.SetUserTOTPSecretParams{
		ID:                  user.ID,
		TotpSecretEncrypted: sealed,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist secret")
		return
	}
	if stored == 0 {
		// TOTP was enabled between the check above and this write.
		writeError(w, http.StatusConflict, "TOTP already enabled; disable it first (requires current code) before setting up a new authenticator")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"secret":      secret,
		"otpauth_url": otpAuthURL,
	})
}

// POST /api/auth/totp/setup-verify  { "code": "123456" }
// Authenticated. Verifies the first 6-digit code against the pending
// secret; on success flips totp_enabled_at to now().
func (h *Handler) TOTPSetupVerify(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if h.TOTPService == nil {
		writeError(w, http.StatusServiceUnavailable, "totp not configured on this server")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !isSixDigitCode(req.Code) {
		writeError(w, http.StatusBadRequest, "code must be 6 digits")
		return
	}

	row, err := h.Queries.GetUserTOTPSecret(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load totp state")
		return
	}
	if len(row.TotpSecretEncrypted) == 0 {
		writeError(w, http.StatusBadRequest, "no setup in progress")
		return
	}
	if row.TotpEnabledAt.Valid {
		writeError(w, http.StatusConflict, "TOTP already enabled")
		return
	}
	secret, err := h.TOTPService.OpenSecret(row.TotpSecretEncrypted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decrypt secret")
		return
	}
	step, ok := h.TOTPService.MatchCode(secret, req.Code, time.Now())
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid code")
		return
	}
	// Enable exactly the secret this code was checked against; a concurrent
	// setup-init that replaced it leaves 0 rows and the user must start over.
	enabled, err := h.Queries.EnableUserTOTP(r.Context(), db.EnableUserTOTPParams{
		ID:                  parseUUID(userID),
		TotpSecretEncrypted: row.TotpSecretEncrypted,
		Step:                step,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enable totp")
		return
	}
	if enabled == 0 {
		writeError(w, http.StatusConflict, "authenticator setup changed; start setup again")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true})
}

// POST /api/auth/totp/disable  { "code": "123456" }
// Authenticated. Requires a valid current TOTP code to prevent disable
// abuse by a session hijacker. On success clears secret and enabled_at.
func (h *Handler) TOTPDisable(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if h.TOTPService == nil {
		writeError(w, http.StatusServiceUnavailable, "totp not configured on this server")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !isSixDigitCode(req.Code) {
		writeError(w, http.StatusBadRequest, "code must be 6 digits")
		return
	}

	row, err := h.Queries.GetUserTOTPSecret(r.Context(), parseUUID(userID))
	if err != nil || len(row.TotpSecretEncrypted) == 0 {
		writeError(w, http.StatusBadRequest, "totp not enabled")
		return
	}
	if row.TotpEnabledAt.Valid {
		// The disable code is the guard against a hijacked session, so it
		// gets the same attempt budget and replay protection as login.
		result, err := h.checkEnabledTOTPCode(r, parseUUID(userID), row.TotpSecretEncrypted, row.TotpLockedUntil, req.Code)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to verify code")
			return
		}
		switch result {
		case totpLocked:
			writeError(w, http.StatusTooManyRequests, "too many invalid codes; try again later")
			return
		case totpRejected:
			writeError(w, http.StatusBadRequest, "invalid code")
			return
		}
	} else {
		// Pending setup: clearing an unverified secret needs only a valid code.
		secret, err := h.TOTPService.OpenSecret(row.TotpSecretEncrypted)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decrypt secret")
			return
		}
		if !h.TOTPService.ValidateCode(secret, req.Code) {
			writeError(w, http.StatusBadRequest, "invalid code")
			return
		}
	}
	if err := h.Queries.DisableUserTOTP(r.Context(), parseUUID(userID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable totp")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"disabled": true})
}

// POST /api/workspaces/{wsId}/members/{userId}/totp-reset
// Authenticated. Requires caller to be owner or admin of the workspace.
// Refuses self-reset (target == caller). Calls DisableUserTOTP on the
// target's user row — this clears the secret globally (across all
// workspaces the target is in). The frontend confirmation dialog (T12)
// makes the cross-workspace effect explicit to the admin.
//
// Audit: slog with workspace_id + actor_user_id + target_user_id +
// target_email.
func (h *Handler) AdminResetMemberTOTP(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "wsId")
	targetUserID := chi.URLParam(r, "userId")

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	targetUUID, ok := parseUUIDOrBadRequest(w, targetUserID, "user id")
	if !ok {
		return
	}

	// caller must be a workspace member with elevated role
	caller, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if caller.Role != "owner" && caller.Role != "admin" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	// D3: refuse self-reset. Compare canonical forms of both UUIDs (not the
	// raw URL string), otherwise an uppercase-hex UUID could pass
	// parseUUIDOrBadRequest yet bypass the string-equality guard.
	if uuidToString(caller.UserID) == uuidToString(targetUUID) {
		writeError(w, http.StatusForbidden, "use Settings → Security → Disable to reset your own TOTP")
		return
	}

	// target must be a member of THIS workspace
	target, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      targetUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "member not found in this workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load member")
		return
	}

	// Owner-management boundary: matches the existing rule used by
	// UpdateMemberRole / RemoveMember. An admin (non-owner) cannot reset
	// an owner's TOTP. TOTP is per-user account-wide, so without this
	// guard an admin in any shared workspace could globally clear an
	// owner's authenticator protection.
	if target.Role == "owner" && caller.Role != "owner" {
		writeError(w, http.StatusForbidden, "only an owner can reset another owner's TOTP")
		return
	}

	// for D5 audit
	targetUser, _ := h.Queries.GetUser(r.Context(), targetUUID)

	if err := h.Queries.DisableUserTOTP(r.Context(), targetUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reset totp")
		return
	}

	// D5: structured audit log
	slog.Info("admin reset member TOTP",
		"workspace_id", workspaceID,
		"actor_user_id", uuidToString(caller.UserID),
		"target_user_id", targetUserID,
		"target_email", targetUser.Email,
	)

	writeJSON(w, http.StatusOK, map[string]any{"reset": true})
}

// POST /api/auth/login-totp  { "email": "...", "code": "123456" }
// Unauthenticated. Alternative to /api/auth/verify-code: looks up the
// user by email + checks the code against their decrypted TOTP secret.
// On success: issues JWT + sets auth cookies (same as verify-code).
//
// All failure modes return 400 with the same generic message — never
// disambiguate "user not found" vs "TOTP not set up" vs "wrong code" vs
// "locked", otherwise the endpoint becomes an oracle for enumeration.
// 400 rather than 401: the web client treats any 401 as an expired session.
func (h *Handler) TOTPLogin(w http.ResponseWriter, r *http.Request) {
	if h.TOTPService == nil {
		writeError(w, http.StatusServiceUnavailable, "totp not configured on this server")
		return
	}
	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !isSixDigitCode(req.Code) {
		writeError(w, http.StatusBadRequest, "invalid credentials")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))

	row, err := h.Queries.GetUserTOTPSecretByEmail(r.Context(), email)
	if err != nil {
		// Includes ErrNoRows (no such enabled user). Generic 400, same as a
		// wrong code, so the response does not reveal whether TOTP is enabled.
		writeError(w, http.StatusBadRequest, "invalid credentials")
		return
	}
	result, err := h.checkEnabledTOTPCode(r, row.ID, row.TotpSecretEncrypted, row.TotpLockedUntil, req.Code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if result != totpAccepted {
		writeError(w, http.StatusBadRequest, "invalid credentials")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), row.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	tokenString, err := h.issueJWT(user)
	if err != nil {
		if errors.Is(err, auth.ErrTemporarilyDisabledUser) {
			writeError(w, http.StatusForbidden, auth.TemporarilyDisabledUserError)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}
	if err := auth.SetAuthCookies(w, tokenString); err != nil {
		slog.Warn("failed to set auth cookies", "error", err)
	}
	// Match VerifyCode/GoogleLogin: issue CloudFront signed cookies when
	// CFSigner is configured, otherwise TOTP-logged-in users cannot fetch
	// CDN-protected uploads until another login path refreshes the cookies.
	if h.CFSigner != nil {
		for _, cookie := range h.CFSigner.SignedCookies(time.Now().Add(auth.AuthTokenTTL())) {
			http.SetCookie(w, cookie)
		}
	}

	slog.Info("user logged in", append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "email", user.Email, "method", "totp")...)
	writeJSON(w, http.StatusOK, LoginResponse{
		Token: tokenString,
		User:  h.userToResponse(user),
	})
}

// GET /api/auth/totp-status?email=...
// Unauthenticated. Anti-enumeration: returns {configured: true} for ALL
// inputs. Real check is at /api/auth/login-totp time.
func (h *Handler) TOTPStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"configured": true})
}
