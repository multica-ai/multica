package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/oauth2"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// OIDC (OpenID Connect) login handler.
//
// Generic OIDC client. Compatible with any spec-conformant IDP (Authentik,
// Keycloak, Okta, Auth0, etc.) — the multica backend trusts ID tokens
// signed by the issuer's keys and uses the `sub` claim as the stable
// identifier across logins.
//
// Identity model: multica's `user` table gains two columns:
//   - external_user_id  TEXT  (the OIDC `sub` claim — opaque, stable per IDP)
//   - external_provider TEXT  (typically "oidc"; allows per-IDP namespacing
//                              if multiple IDPs are added later)
// The unique index is (external_provider, external_user_id) WHERE NOT NULL.
//
// Lookup precedence on login:
//   1. By (external_provider, external_user_id) — primary path for OIDC users
//   2. Fallback by email (case-insensitive) — for first-login of users who
//      previously authenticated via verify-code / Google. On match, the
//      external identity is backfilled.
//   3. Otherwise create a new user.
//
// This means an existing email-only user who switches to OIDC keeps their
// multica identity (issues, comments, memberships preserved) — the OIDC
// `sub` is just bound to the existing row.
//
// Refs: multica-ai/multica#1014.

const (
	defaultOIDCAuthMethod       = "oidc"
	envExternalProviderOverride = "OIDC_EXTERNAL_PROVIDER"
)

// oidcRuntime caches the *oidc.Provider, the corresponding verifier, and
// the oauth2 config so repeated logins don't re-fetch the discovery doc
// on every request. Concurrent-safe; rebuilt lazily on first login.
//
// We intentionally do NOT initialize at process start: missing OIDC
// config should not break the service — operators may run multica without
// OIDC and rely on verify-code or Google. The first OIDC login is the
// canonical "configured" signal.
type oidcRuntime struct {
	mu       sync.Mutex
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	cfg      oauth2.Config
	scopes   []string
	loaded   bool
	loadErr  error
}

var oidcRT = &oidcRuntime{}

// load initializes the OIDC provider once. Safe to call from multiple
// goroutines. Subsequent calls are no-ops unless a previous attempt failed,
// in which case it retries (so transient IDP outages at startup don't
// permanently disable login).
func (r *oidcRuntime) load(ctx context.Context) (*oidc.Provider, *oidc.IDTokenVerifier, oauth2.Config, []string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loaded && r.loadErr == nil {
		return r.provider, r.verifier, r.cfg, r.scopes, nil
	}
	r.loaded = true
	r.loadErr = nil

	issuer := os.Getenv("OIDC_ISSUER_URL")
	clientID := os.Getenv("OIDC_CLIENT_ID")
	clientSecret := os.Getenv("OIDC_CLIENT_SECRET")
	redirectURI := os.Getenv("OIDC_REDIRECT_URI")
	scopesEnv := os.Getenv("OIDC_SCOPES")

	if issuer == "" || clientID == "" || clientSecret == "" {
		r.loadErr = errors.New("OIDC not configured (OIDC_ISSUER_URL / OIDC_CLIENT_ID / OIDC_CLIENT_SECRET missing)")
		return nil, nil, oauth2.Config{}, nil, r.loadErr
	}

	// Discovery + JWKS fetch. Provider caches keys internally and rotates.
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		r.loadErr = err
		return nil, nil, oauth2.Config{}, nil, err
	}

	scopes := []string{oidc.ScopeOpenID, "email", "profile"}
	if scopesEnv != "" {
		scopes = strings.Fields(scopesEnv)
	}

	cfg := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURI,
		Scopes:       scopes,
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})

	r.provider = provider
	r.verifier = verifier
	r.cfg = cfg
	r.scopes = scopes
	return provider, verifier, cfg, scopes, nil
}

// OIDCLoginRequest is the JSON body the frontend posts after the IDP
// redirects the browser back with a code. The redirect_uri MUST exactly
// match what was used during /authorize and what was registered in the
// IDP's allowed redirect URIs — IDPs reject mismatches as a CSRF defense.
type OIDCLoginRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
}

// oidcClaims is the subset of standard OIDC claims multica relies on.
// We deliberately do not require optional claims like phone_number; only
// `sub` is structurally required (verifier enforces).
type oidcClaims struct {
	Sub               string `json:"sub"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Picture           string `json:"picture"`
}

func (h *Handler) OIDCLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req OIDCLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	if req.RedirectURI == "" {
		req.RedirectURI = os.Getenv("OIDC_REDIRECT_URI")
	}

	provider, verifier, cfg, _, err := oidcRT.load(ctx)
	if err != nil {
		slog.Error("oidc not configured", "error", err)
		writeError(w, http.StatusServiceUnavailable, "OIDC login is not configured")
		return
	}
	_ = provider // currently used only via verifier and cfg.Endpoint above

	// Exchange code -> tokens. The IDP returns id_token (a JWT signed
	// with the issuer's key), access_token, optional refresh_token.
	// We only consume id_token here; access_token would be required if
	// we needed to fetch /userinfo, but Authentik (and our config) emits
	// claims directly in id_token (`Include claims in id_token` provider
	// flag), so userinfo is not needed.
	exchangeCfg := cfg
	exchangeCfg.RedirectURL = req.RedirectURI

	tok, err := exchangeCfg.Exchange(ctx, req.Code)
	if err != nil {
		slog.Error("oidc token exchange failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to exchange code with IDP")
		return
	}

	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		slog.Error("oidc response missing id_token")
		writeError(w, http.StatusBadGateway, "IDP did not return id_token")
		return
	}

	idTok, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		slog.Error("oidc id_token verification failed", "error", err)
		writeError(w, http.StatusBadGateway, "IDP token signature invalid")
		return
	}

	var claims oidcClaims
	if err := idTok.Claims(&claims); err != nil {
		slog.Error("oidc claims parse failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to parse IDP claims")
		return
	}

	if claims.Sub == "" {
		writeError(w, http.StatusBadGateway, "IDP claims missing required `sub`")
		return
	}
	if claims.Email == "" {
		writeError(w, http.StatusBadRequest, "IDP account has no email; bind one with your tenant admin and try again")
		return
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))

	provName := os.Getenv(envExternalProviderOverride)
	if provName == "" {
		provName = defaultOIDCAuthMethod
	}

	user, isNew, err := h.findOrLinkUserByOIDC(ctx, claims.Sub, provName, email)
	if err != nil {
		var signupErr SignupError
		if errors.As(err, &signupErr) {
			writeError(w, http.StatusForbidden, signupErr.Error())
			return
		}
		slog.Error("oidc user upsert failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	if isNew {
		evt := analytics.Signup(uuidToString(user.ID), user.Email, signupSourceFromRequest(r))
		evt.Properties["auth_method"] = "oidc"
		evt.Properties["oidc_provider"] = provName
		h.Analytics.Capture(evt)
	}

	// Backfill name and avatar from IDP claims when they're missing or stale.
	displayName := claims.Name
	if displayName == "" {
		displayName = claims.PreferredUsername
	}

	needsUpdate := false
	newName := user.Name
	newAvatar := user.AvatarUrl

	// Always sync name/avatar from IDP — IDP is the source of truth for
	// OIDC-managed users. If the user manually edited their multica name
	// later, that edit will be overwritten on next login.
	if displayName != "" && displayName != user.Name {
		newName = displayName
		needsUpdate = true
	}
	if claims.Picture != "" && (!user.AvatarUrl.Valid || user.AvatarUrl.String != claims.Picture) {
		newAvatar = pgtype.Text{String: claims.Picture, Valid: true}
		needsUpdate = true
	}

	if needsUpdate {
		updated, err := h.Queries.UpdateUser(r.Context(), db.UpdateUserParams{
			ID:        user.ID,
			Name:      newName,
			AvatarUrl: newAvatar,
		})
		if err == nil {
			user = updated
		}
	}

	tokenString, err := h.issueJWT(user)
	if err != nil {
		slog.Warn("oidc login failed", append(logger.RequestAttrs(r), "error", err, "email", email)...)
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

	slog.Info("user logged in via oidc",
		append(logger.RequestAttrs(r),
			"user_id", uuidToString(user.ID),
			"email", user.Email,
			"oidc_provider", provName,
		)...)

	writeJSON(w, http.StatusOK, LoginResponse{
		Token: tokenString,
		User:  userToResponse(user),
	})
}

// findOrLinkUserByOIDC implements the lookup precedence described at the
// top of this file: external_id → email → create.
func (h *Handler) findOrLinkUserByOIDC(
	ctx context.Context,
	externalID, externalProvider, email string,
) (user db.User, isNew bool, err error) {
	// 1. Primary: existing OIDC-bound user.
	user, err = h.Queries.GetUserByExternalIdentity(ctx, db.GetUserByExternalIdentityParams{
		ExternalProvider: pgtype.Text{String: externalProvider, Valid: true},
		ExternalUserID:   pgtype.Text{String: externalID, Valid: true},
	})
	if err == nil {
		return user, false, nil
	}
	if !isNotFound(err) {
		return db.User{}, false, err
	}

	// 2. Fallback: legacy email-only user. On match, bind their
	//    external identity so the next login is fast-path #1.
	user, err = h.Queries.GetUserByEmail(ctx, email)
	if err == nil {
		linked, lerr := h.Queries.SetUserExternalIdentity(ctx, db.SetUserExternalIdentityParams{
			ID:               user.ID,
			ExternalProvider: pgtype.Text{String: externalProvider, Valid: true},
			ExternalUserID:   pgtype.Text{String: externalID, Valid: true},
		})
		if lerr == nil {
			user = linked
		}
		return user, false, nil
	}
	if !isNotFound(err) {
		return db.User{}, false, err
	}

	// 3. New user. Reuse signup-allowed gate from email path so admin
	//    signup restrictions apply uniformly across auth methods.
	if err := h.checkSignupAllowed(email, true); err != nil {
		return db.User{}, false, err
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
	if _, err := h.Queries.SetUserExternalIdentity(ctx, db.SetUserExternalIdentityParams{
		ID:               created.ID,
		ExternalProvider: pgtype.Text{String: externalProvider, Valid: true},
		ExternalUserID:   pgtype.Text{String: externalID, Valid: true},
	}); err != nil {
		// Identity bind failure is non-fatal at create time — the user
		// can still log in via email-link fallback next time. Log and
		// proceed.
		slog.Warn("oidc external identity bind failed (non-fatal)",
			"user_id", uuidToString(created.ID), "error", err)
	}
	return created, true, nil
}
