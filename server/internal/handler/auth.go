package handler

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const devVerificationCodeEnv = "MULTICA_DEV_VERIFICATION_CODE"

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

func isDevVerificationCode(code string) bool {
	if isProductionEnv() {
		return false
	}

	devCode := strings.TrimSpace(os.Getenv(devVerificationCodeEnv))
	if !isSixDigitCode(devCode) {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(code), []byte(devCode)) == 1
}

func isProductionEnv() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production")
}

func isSixDigitCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
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
		h.EnsureLazyInviteMembership(ctx, user, false)
		return user, false, nil
	}

	name := email
	if at := strings.Index(email, "@"); at > 0 {
		name = email[:at]
	}
	created, err := h.Queries.CreateUser(ctx, db.CreateUserParams{
		Name:          name,
		Email:         email,
		EmailVerified: true, // magic-link flow proves the email
	})
	if err != nil {
		return db.User{}, false, err
	}
	h.EnsureLazyInviteMembership(ctx, created, true)
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

	// Lazy-invite domains are themselves an allowlist; let them through
	// regardless of ALLOW_SIGNUP / ALLOWED_EMAILS / ALLOWED_EMAIL_DOMAINS.
	if h.LazyInvite.IsAllowedDomain(email) {
		return nil
	}

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

	isDevCode := isDevVerificationCode(code)
	if !isDevCode && subtle.ConstantTimeCompare([]byte(code), []byte(dbCode.Code)) != 1 {
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

type GoogleLoginRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
}

type googleTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
}

// findOrCreateUserByGoogle resolves a verified Google identity to a user row.
// Precedence:
//  1. Match by google_id (already linked) -> log in.
//  2. Match by email (verified by Google) -> link the google_id.
//  3. New user -> create with google_id set.
//
// Step 2 is gated by ident.EmailVerified to defeat the unverified-email
// takeover attack: an attacker who points an unverified Google account at a
// victim's email would otherwise hijack the existing row. The caller MUST get
// ident from a verified ID token, never from userinfo.
func (h *Handler) findOrCreateUserByGoogle(ctx context.Context, ident auth.GoogleIdentity) (db.User, bool, error) {
	if ident.Sub == "" || ident.Email == "" {
		return db.User{}, false, errors.New("google identity missing sub or email")
	}
	if !ident.EmailVerified {
		return db.User{}, false, errors.New("google has not verified this email")
	}
	email := strings.ToLower(strings.TrimSpace(ident.Email))

	// 1. Match by google_id.
	if u, err := h.Queries.GetUserByGoogleID(ctx, pgtype.Text{String: ident.Sub, Valid: true}); err == nil {
		h.EnsureLazyInviteMembership(ctx, u, false)
		return u, false, nil
	} else if !isNotFound(err) {
		return db.User{}, false, err
	}

	// 2. Match by email — link.
	existing, err := h.Queries.GetUserByEmail(ctx, email)
	if err != nil && !isNotFound(err) {
		return db.User{}, false, err
	}
	if err == nil {
		linked, err := h.Queries.LinkGoogleIdentity(ctx, db.LinkGoogleIdentityParams{
			ID:       existing.ID,
			GoogleID: pgtype.Text{String: ident.Sub, Valid: true},
		})
		if err != nil {
			return db.User{}, false, err
		}
		h.EnsureLazyInviteMembership(ctx, linked, false)
		return linked, false, nil
	}

	// 3. New user.
	if err := h.checkSignupAllowed(email, true); err != nil {
		return db.User{}, false, err
	}
	name := ident.Name
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	avatar := pgtype.Text{}
	if ident.Picture != "" {
		avatar = pgtype.Text{String: ident.Picture, Valid: true}
	}
	created, err := h.Queries.CreateUser(ctx, db.CreateUserParams{
		Name:          name,
		Email:         email,
		AvatarUrl:     avatar,
		GoogleID:      pgtype.Text{String: ident.Sub, Valid: true},
		EmailVerified: true,
	})
	if err != nil {
		return db.User{}, false, err
	}
	h.EnsureLazyInviteMembership(ctx, created, true)
	return created, true, nil
}

func (h *Handler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	var req GoogleLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "Google login is not configured")
		return
	}
	if h.GoogleVerifier == nil {
		writeError(w, http.StatusServiceUnavailable, "Google login is not configured")
		return
	}

	redirectURI := req.RedirectURI
	if redirectURI == "" {
		redirectURI = os.Getenv("GOOGLE_REDIRECT_URI")
	}

	// Exchange authorization code for tokens. The token URL is overridable
	// for tests via h.googleTokenURL — production always hits Google.
	tokenURL := h.googleTokenURL
	if tokenURL == "" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}
	tokenResp, err := http.PostForm(tokenURL, url.Values{
		"code":          {req.Code},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		slog.Error("google oauth token exchange failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to exchange code with Google")
		return
	}
	defer tokenResp.Body.Close()

	tokenBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to read Google token response")
		return
	}

	if tokenResp.StatusCode != http.StatusOK {
		slog.Error("google oauth token exchange returned error", "status", tokenResp.StatusCode, "body", string(tokenBody))
		writeError(w, http.StatusBadRequest, "failed to exchange code with Google")
		return
	}

	var gToken googleTokenResponse
	if err := json.Unmarshal(tokenBody, &gToken); err != nil {
		writeError(w, http.StatusBadGateway, "failed to parse Google token response")
		return
	}
	if gToken.IDToken == "" {
		writeError(w, http.StatusBadGateway, "Google response missing id_token")
		return
	}

	// Verify the ID token: signature against Google's JWKs, iss=accounts.google.com,
	// aud=GOOGLE_CLIENT_ID, exp in the future. Identity is trusted only after this.
	ident, err := h.GoogleVerifier.Verify(r.Context(), gToken.IDToken)
	if err != nil {
		slog.Warn("google id_token verification failed", "error", err)
		writeError(w, http.StatusUnauthorized, "invalid Google identity")
		return
	}

	user, isNew, err := h.findOrCreateUserByGoogle(r.Context(), ident)
	if err != nil {
		var signupErr SignupError
		if errors.As(err, &signupErr) {
			writeError(w, http.StatusForbidden, signupErr.Error())
			return
		}
		slog.Warn("google login user resolution failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to sign in")
		return
	}
	if isNew {
		evt := analytics.Signup(uuidToString(user.ID), user.Email, signupSourceFromRequest(r))
		evt.Properties["auth_method"] = "google"
		h.Analytics.Capture(evt)
	}

	// Refresh display name / avatar from Google when the user has a default
	// placeholder. Same intent as the prior handler.
	needsUpdate := false
	newName := user.Name
	newAvatar := user.AvatarUrl
	emailLocal := strings.SplitN(strings.ToLower(user.Email), "@", 2)[0]
	if ident.Name != "" && user.Name == emailLocal {
		newName = ident.Name
		needsUpdate = true
	}
	if ident.Picture != "" && !user.AvatarUrl.Valid {
		newAvatar = pgtype.Text{String: ident.Picture, Valid: true}
		needsUpdate = true
	}
	if needsUpdate {
		if updated, err := h.Queries.UpdateUser(r.Context(), db.UpdateUserParams{
			ID:        user.ID,
			Name:      newName,
			AvatarUrl: newAvatar,
		}); err == nil {
			user = updated
		}
	}

	tokenString, err := h.issueJWT(user)
	if err != nil {
		slog.Warn("google login failed", append(logger.RequestAttrs(r), "error", err, "email", user.Email)...)
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

	slog.Info("user logged in via google", append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "email", user.Email)...)
	writeJSON(w, http.StatusOK, LoginResponse{
		Token: tokenString,
		User:  userToResponse(user),
	})
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
