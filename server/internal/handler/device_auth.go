package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/pkg/db/generated"
)

// Device authorization flow (RFC 8628 adapted): a CLI on a remote/headless
// machine POSTs /auth/device/start, shows the short user code, and polls
// /auth/device/token. The user approves the code on the web (/activate) in an
// already-authenticated browser session on any device; approval mints the
// same JWT the browser-login callback hands out, and the CLI exchanges it for
// a PAT exactly like a browser login. No localhost callback, no SSH tunnel.

const (
	deviceAuthTTL        = 10 * time.Minute
	deviceAuthPollSecs   = 5
	deviceUserCodeLength = 8
)

// deviceUserCodeAlphabet drops the ambiguous characters (0/O, 1/I/L) so a
// code read from a terminal can be retyped on a phone without guessing.
const deviceUserCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// deviceAuthError is the RFC 8628 error-code vocabulary returned by the
// polling endpoint. The CLI maps these to user-facing guidance.
const (
	deviceErrPending = "authorization_pending"
	deviceErrSlow    = "slow_down"
	deviceErrExpired = "expired_token"
	deviceErrDenied  = "access_denied"
	deviceErrInvalid = "invalid_device_code"
)

type StartDeviceAuthRequest struct {
	// ClientName is optional descriptive metadata for logs only.
	ClientName string `json:"client_name"`
}

type StartDeviceAuthResponse struct {
	DeviceCode       string `json:"device_code"`
	UserCode         string `json:"user_code"`
	VerificationURL  string `json:"verification_url"`
	ExpiresIn        int    `json:"expires_in"`
	Interval         int    `json:"interval"`
}

type DeviceTokenRequest struct {
	DeviceCode string `json:"device_code"`
}

type DeviceTokenResponse struct {
	Token string `json:"token,omitempty"`
	Error string `json:"error,omitempty"`
}

type ApproveDeviceAuthRequest struct {
	UserCode string `json:"user_code"`
}

func generateDeviceCode() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func generateDeviceUserCode() (string, error) {
	max := big.NewInt(int64(len(deviceUserCodeAlphabet)))
	out := make([]byte, deviceUserCodeLength)
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = deviceUserCodeAlphabet[n.Int64()]
	}
	return string(out), nil
}

// normalizeDeviceUserCode uppercases and strips separators so "abcd-1234",
// "abcd 1234" and "ABCD1234" all resolve to the same code.
func normalizeDeviceUserCode(input string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(input)) {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func formatDeviceUserCode(code string) string {
	if len(code) != deviceUserCodeLength {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// deviceVerificationURL points the user at the approval page. MULTICA_APP_URL
// is the deployed app origin; when it is unset (bare local dev) the CLI falls
// back to composing {app_url}/activate itself.
func deviceVerificationURL() string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("MULTICA_APP_URL")), "/")
	if base == "" {
		return ""
	}
	return base + "/activate"
}

// StartDeviceAuthorization begins a device flow: returns the device code
// (secret, held by the CLI) and the short user code (typed by the human).
func (h *Handler) StartDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	var req StartDeviceAuthRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body is optional metadata

	deviceCode, err := generateDeviceCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate device code")
		return
	}
	userCode, err := generateDeviceUserCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate user code")
		return
	}

	created, err := h.Queries.CreateDeviceAuthorization(r.Context(), db.CreateDeviceAuthorizationParams{
		DeviceCode:      deviceCode,
		UserCodeHash:    auth.HashToken(userCode),
		ExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(deviceAuthTTL), Valid: true},
		IntervalSeconds: deviceAuthPollSecs,
	})
	if err != nil {
		slog.Error("device-auth: failed to create", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to start device authorization")
		return
	}

	writeJSON(w, http.StatusOK, StartDeviceAuthResponse{
		DeviceCode:      created.DeviceCode,
		UserCode:        formatDeviceUserCode(userCode),
		VerificationURL: deviceVerificationURL(),
		ExpiresIn:       int(deviceAuthTTL.Seconds()),
		Interval:        int(created.IntervalSeconds),
	})
}

// ApproveDeviceAuthorization is called by the web app from an authenticated
// session: binds the approving user to the pending authorization and mints
// the token the CLI will collect.
func (h *Handler) ApproveDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req ApproveDeviceAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || normalizeDeviceUserCode(req.UserCode) == "" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	pending, err := h.Queries.GetPendingDeviceAuthorizationByUserCode(r.Context(), auth.HashToken(normalizeDeviceUserCode(req.UserCode)))
	if err != nil {
		// Unknown, already-approved, or expired — one message for all three
		// so the endpoint cannot be used to probe live codes beyond the
		// rate limiter.
		writeError(w, http.StatusBadRequest, "invalid or expired code")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	tokenString, err := h.issueJWT(user)
	if err != nil {
		if errors.Is(err, auth.ErrTemporarilyDisabledUser) {
			writeError(w, http.StatusForbidden, auth.TemporarilyDisabledUserError)
			return
		}
		slog.Warn("device-auth: failed to issue JWT", append(logger.RequestAttrs(r), "error", err, "user_id", userID)...)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// The status='pending' guard makes approval single-shot: two approvers
	// racing both get "invalid or expired code" except the first.
	if _, err := h.Queries.ApproveDeviceAuthorization(r.Context(), db.ApproveDeviceAuthorizationParams{
		ID:     pending.ID,
		UserID: user.ID,
		Token:  pgtype.Text{String: tokenString, Valid: true},
	}); err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired code")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

// DeviceToken is the CLI polling endpoint. Pending authorizations return the
// RFC 8628 error vocabulary; an approved one returns the token exactly once.
func (h *Handler) DeviceToken(w http.ResponseWriter, r *http.Request) {
	var req DeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.DeviceCode) == "" {
		writeJSON(w, http.StatusBadRequest, DeviceTokenResponse{Error: deviceErrInvalid})
		return
	}

	da, err := h.Queries.GetDeviceAuthorizationByDeviceCode(r.Context(), strings.TrimSpace(req.DeviceCode))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, DeviceTokenResponse{Error: deviceErrInvalid})
		return
	}

	if da.ExpiresAt.Time.Before(time.Now()) && da.Status == "pending" {
		writeJSON(w, http.StatusBadRequest, DeviceTokenResponse{Error: deviceErrExpired})
		return
	}

	switch da.Status {
	case "denied":
		writeJSON(w, http.StatusBadRequest, DeviceTokenResponse{Error: deviceErrDenied})
		return
	case "pending":
		// Enforce the polling interval: a client hammering faster than
		// `interval` gets slow_down and does not refresh last_polled_at.
		if da.LastPolledAt.Valid && time.Since(da.LastPolledAt.Time) < time.Duration(da.IntervalSeconds)*time.Second {
			writeJSON(w, http.StatusBadRequest, DeviceTokenResponse{Error: deviceErrSlow})
			return
		}
		if err := h.Queries.MarkDeviceAuthorizationPolled(r.Context(), da.ID); err != nil {
			slog.Warn("device-auth: failed to mark polled", append(logger.RequestAttrs(r), "error", err)...)
		}
		writeJSON(w, http.StatusBadRequest, DeviceTokenResponse{Error: deviceErrPending})
		return
	case "approved":
		if da.ConsumedAt.Valid {
			// The token was already collected; the device code is spent.
			writeJSON(w, http.StatusBadRequest, DeviceTokenResponse{Error: deviceErrInvalid})
			return
		}
		token, err := h.Queries.ConsumeDeviceAuthorizationToken(r.Context(), da.ID)
		if err != nil {
			// Lost the consume race — treat the code as spent.
			writeJSON(w, http.StatusBadRequest, DeviceTokenResponse{Error: deviceErrInvalid})
			return
		}
		writeJSON(w, http.StatusOK, DeviceTokenResponse{Token: token.String})
		return
	default:
		writeJSON(w, http.StatusBadRequest, DeviceTokenResponse{Error: deviceErrInvalid})
	}
}
