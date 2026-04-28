package handler

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var emailRegexp = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

type StartEmailBindingRequest struct {
	Email string `json:"email"`
}

type StartEmailBindingResponse struct {
	Message string `json:"message"`
}

type VerifyEmailBindingRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type VerifyEmailBindingResponse struct {
	Binding NotificationBindingResponse `json:"binding"`
}

func (h *Handler) StartMyEmailBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req StartEmailBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !emailRegexp.MatchString(email) {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}

	// Check if email is already bound to another user.
	existing, err := h.Queries.GetExternalAccountBindingByProviderAndExternalID(r.Context(), db.GetExternalAccountBindingByProviderAndExternalIDParams{
		Provider:       "email",
		ExternalUserID: email,
	})
	if err == nil && uuidToString(existing.UserID) != userID {
		writeError(w, http.StatusConflict, "this email is already bound to another account")
		return
	}

	// Rate limit: max 1 code per 60 seconds per email.
	latest, err := h.Queries.GetLatestCodeByEmail(r.Context(), email)
	if err == nil && time.Since(latest.CreatedAt.Time) < 60*time.Second {
		writeError(w, http.StatusTooManyRequests, "please wait before requesting another code")
		return
	}

	code, err := generateCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate code")
		return
	}

	_, err = h.Queries.CreateVerificationCode(r.Context(), db.CreateVerificationCodeParams{
		Email:     email,
		Code:      code,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store verification code")
		return
	}

	if err := h.EmailService.SendVerificationCode(email, code); err != nil {
		slog.Error("failed to send email binding verification code", "email", email, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to send verification code")
		return
	}

	_ = h.Queries.DeleteExpiredVerificationCodes(r.Context())

	writeJSON(w, http.StatusOK, StartEmailBindingResponse{Message: "Verification code sent"})
}

func (h *Handler) VerifyMyEmailBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req VerifyEmailBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	code := strings.TrimSpace(req.Code)
	if email == "" || code == "" {
		writeError(w, http.StatusBadRequest, "email and code are required")
		return
	}

	dbCode, err := h.Queries.GetLatestVerificationCode(r.Context(), email)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired code")
		return
	}

	// No master code for email binding (unlike login).
	if subtle.ConstantTimeCompare([]byte(code), []byte(dbCode.Code)) != 1 {
		_ = h.Queries.IncrementVerificationCodeAttempts(r.Context(), dbCode.ID)
		writeError(w, http.StatusBadRequest, "invalid or expired code")
		return
	}

	if err := h.Queries.MarkVerificationCodeUsed(r.Context(), dbCode.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify code")
		return
	}

	binding, err := h.Queries.UpsertExternalAccountBinding(r.Context(), db.UpsertExternalAccountBindingParams{
		UserID:                parseUUID(userID),
		Provider:              "email",
		ExternalUserID:        email,
		DisplayName:           strToText(email),
		AccessTokenEncrypted:  pgtype.Text{},
		RefreshTokenEncrypted: pgtype.Text{},
		TokenExpiresAt:        pgtype.Timestamptz{},
		Status:                "active",
		Metadata:              []byte("{}"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create email binding")
		return
	}

	// If user has a placeholder @dingtalk.local email, update to the real one.
	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err == nil && strings.HasSuffix(user.Email, "@dingtalk.local") {
		_, _ = h.DB.Exec(r.Context(),
			`UPDATE "user" SET email = $1, updated_at = now() WHERE id = $2`,
			email, parseUUID(userID),
		)
	}

	resp := notificationBindingsToResponse([]db.ExternalAccountBinding{binding})
	writeJSON(w, http.StatusOK, VerifyEmailBindingResponse{Binding: resp[0]})
}
