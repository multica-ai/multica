package handler

import (
	"context"
	"errors"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SignupError represents signup restriction errors
type SignupError struct {
	Message string
}

func (e SignupError) Error() string {
	return e.Message
}

var ErrSignupProhibited = SignupError{Message: "user registration is disabled on this self-hosted instance"}
var ErrEmailNotAllowed = SignupError{Message: "email address or domain not allowed on this instance"}

type UserResponse struct {
	ID                      string          `json:"id"`
	Name                    string          `json:"name"`
	Email                   string          `json:"email"`
	AvatarURL               *string         `json:"avatar_url"`
	OnboardedAt             *string         `json:"onboarded_at"`
	OnboardingQuestionnaire json.RawMessage `json:"onboarding_questionnaire"`
	StarterContentState     *string         `json:"starter_content_state"`
	CreatedAt               string          `json:"created_at"`
	UpdatedAt               string          `json:"updated_at"`
}

func userToResponse(u db.User) UserResponse {
	// JSONB column is []byte with DEFAULT '{}', so it's never nil at the DB
	// level. Defensive coalesce just in case a future ALTER makes the column
	// nullable and some row comes back with no default applied.
	q := u.OnboardingQuestionnaire
	if len(q) == 0 {
		q = []byte("{}")
	}
	return UserResponse{
		ID:                      uuidToString(u.ID),
		Name:                    u.Name,
		Email:                   u.Email,
		AvatarURL:               textToPtr(u.AvatarUrl),
		OnboardedAt:             timestampToPtr(u.OnboardedAt),
		OnboardingQuestionnaire: json.RawMessage(q),
		StarterContentState:     textToPtr(u.StarterContentState),
		CreatedAt:               timestampToString(u.CreatedAt),
		UpdatedAt:               timestampToString(u.UpdatedAt),
	}
}

type LoginResponse struct {
	Token string       `json:"token"`
	User  UserResponse `json:"user"`
}

type SendCodeRequest struct {
	Email string `json:"email"`
}

type VerifyCodeRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func generateCode() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	n := binary.BigEndian.Uint32(buf[:]) % 1000000
	return fmt.Sprintf("%06d", n), nil
}

func (h *Handler) issueJWT(user db.User) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   uuidToString(user.ID),
		"email": user.Email,
		"name":  user.Name,
		"exp":   time.Now().Add(30 * 24 * time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	})
	return token.SignedString(auth.JWTSecret())
}

// findOrCreateUser returns the existing user for an email, or creates one if
// none exists. isNew reports whether this call created the user — the signup
// event fires on that edge, covering both the verification-code and Google
// OAuth entry points.
func (h *Handler) findOrCreateUser(ctx context.Context, email string) (user db.User, isNew bool, err error) {
	user, err = h.Queries.GetUserByEmail(ctx, email)
	isNew = isNotFound(err)
	if err != nil && !isNew {
		return db.User{}, false, err
	}

	if err := h.checkSignupAllowed(email, isNew); err != nil {
		return db.User{}, false, err
	}

	if !isNew {
		return user, false, nil
	}

	name := email
	if at := strings.Index(email, "@"); at > 0 {
		name = email[:at]
	}
	created, err := h.Queries.CreateUser(ctx, db.CreateUserParams{
		Name:  name,
		Email: email,
	})
	if err != nil {
		return db.User{}, false, err
	}
	return created, true, nil
}

// signupSourceFromRequest reads the attribution cookie the web frontend
// sets on the first pageview (UTM + referrer bundle). The frontend writes
// a JSON string URL-encoded into the cookie value — Go does not
// auto-decode Cookie.Value, so we have to unescape here before the string
// lands in PostHog. Missing cookie / decode failures collapse to the
// empty string; that simply omits signup_source from the event rather
// than sending percent-encoded garbage. Never fall back to r.Referer() —
// the frontend has already sanitised attribution and a raw referer can
// leak OAuth code/state from the callback URL.
//
// The cap is the server-side defence against a client that manages to set
// an oversize cookie; it matches SIGNUP_SOURCE_MAX_LEN on the frontend.
const signupSourceMaxLen = 512

func signupSourceFromRequest(r *http.Request) string {
	c, err := r.Cookie("multica_signup_source")
	if err != nil || c == nil {
		return ""
	}
	decoded, err := url.QueryUnescape(c.Value)
	if err != nil {
		return ""
	}
	if len(decoded) > signupSourceMaxLen {
		return ""
	}
	return decoded
}

func (h *Handler) checkSignupAllowed(email string, isNewUser bool) error {
	if !isNewUser {
		return nil // existing users always allowed to log in
	}

	email = strings.ToLower(email)
	domain := ""
	if at := strings.Index(email, "@"); at > 0 {
		domain = email[at+1:]
	}

	// 1. explicit email whitelist always wins
	if len(h.cfg.AllowedEmails) > 0 && contains(h.cfg.AllowedEmails, email) {
		return nil
	}

	// 2. domain whitelist always wins
	if len(h.cfg.AllowedEmailDomains) > 0 && contains(h.cfg.AllowedEmailDomains, domain) {
		return nil
	}

	// 3. general signup flag
	if !h.cfg.AllowSignup {
		return ErrSignupProhibited
	}

	// 4. if allowlists are set but didn't match, block
	if len(h.cfg.AllowedEmailDomains) > 0 || len(h.cfg.AllowedEmails) > 0 {
		return ErrSignupProhibited
	}

	return nil
}

func contains(slice []string, s string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

func (h *Handler) SendCode(w http.ResponseWriter, r *http.Request) {
	var req SendCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	// Check signup restrictions before sending magic link
	_, err := h.Queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		if !isNotFound(err) {
			// Real database/query error → return 500
			writeError(w, http.StatusInternalServerError, "failed to lookup user")
			return
		}
		// User does not exist → treat as new user
		isNewUser := true
		if err := h.checkSignupAllowed(email, isNewUser); err != nil {
			var signupErr SignupError
			if errors.As(err, &signupErr) {
				writeError(w, http.StatusForbidden, signupErr.Error())
			} else {
				writeError(w, http.StatusForbidden, "user registration is disabled")
			}
			return
		}
	} else {
		// User already exists → always allowed to login
		isNewUser := false
		if err := h.checkSignupAllowed(email, isNewUser); err != nil {
			// This should rarely happen, but handle it anyway
			var signupErr SignupError
			if errors.As(err, &signupErr) {
				writeError(w, http.StatusForbidden, signupErr.Error())
			} else {
				writeError(w, http.StatusForbidden, "user registration is disabled")
			}
			return
		}
	}

	// Rate limit: max 1 code per 60 seconds per email
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
		slog.Error("failed to send verification code", "email", email, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to send verification code")
		return
	}

	// Best-effort cleanup of expired codes
	_ = h.Queries.DeleteExpiredVerificationCodes(r.Context())

	writeJSON(w, http.StatusOK, map[string]string{"message": "Verification code sent"})
}

func (h *Handler) VerifyCode(w http.ResponseWriter, r *http.Request) {
	var req VerifyCodeRequest
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

	isMasterCode := code == "888888" && os.Getenv("APP_ENV") != "production"
	if !isMasterCode && subtle.ConstantTimeCompare([]byte(code), []byte(dbCode.Code)) != 1 {
		_ = h.Queries.IncrementVerificationCodeAttempts(r.Context(), dbCode.ID)
		writeError(w, http.StatusBadRequest, "invalid or expired code")
		return
	}

	if err := h.Queries.MarkVerificationCodeUsed(r.Context(), dbCode.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify code")
		return
	}

	user, isNew, err := h.findOrCreateUser(r.Context(), email)
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
		h.Analytics.Capture(analytics.Signup(uuidToString(user.ID), user.Email, signupSourceFromRequest(r)))
	}

	tokenString, err := h.issueJWT(user)
	if err != nil {
		slog.Warn("login failed", append(logger.RequestAttrs(r), "error", err, "email", req.Email)...)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// Set HttpOnly auth cookie (browser clients) + CSRF cookie.
	if err := auth.SetAuthCookies(w, tokenString); err != nil {
		slog.Warn("failed to set auth cookies", "error", err)
	}

	// Set CloudFront signed cookies for CDN access.
	if h.CFSigner != nil {
		for _, cookie := range h.CFSigner.SignedCookies(time.Now().Add(30 * 24 * time.Hour)) {
			http.SetCookie(w, cookie)
		}
	}

	slog.Info("user logged in", append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "email", user.Email)...)
	writeJSON(w, http.StatusOK, LoginResponse{
		Token: tokenString,
		User:  userToResponse(user),
	})
}

func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, userToResponse(user))
}

type UpdateMeRequest struct {
	Name      *string `json:"name"`
	AvatarURL *string `json:"avatar_url"`
}

// IssueCliToken returns a fresh JWT for the authenticated user.
// This allows cookie-authenticated browser sessions to obtain a bearer token
// that can be handed off to the CLI via the cli_callback redirect.
func (h *Handler) IssueCliToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	tokenString, err := h.issueJWT(user)
	if err != nil {
		slog.Warn("cli-token: failed to issue JWT", append(logger.RequestAttrs(r), "error", err, "user_id", userID)...)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"token": tokenString})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	auth.ClearAuthCookies(w)
	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req UpdateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	currentUser, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	name := currentUser.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
	}

	params := db.UpdateUserParams{
		ID:   currentUser.ID,
		Name: name,
	}
	if req.AvatarURL != nil {
		params.AvatarUrl = pgtype.Text{String: strings.TrimSpace(*req.AvatarURL), Valid: true}
	}

	updatedUser, err := h.Queries.UpdateUser(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}

	writeJSON(w, http.StatusOK, userToResponse(updatedUser))
}
