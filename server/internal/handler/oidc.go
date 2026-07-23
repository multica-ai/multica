package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"golang.org/x/oauth2"
)

const (
	maxOIDCAppStateLength = 2048
	oidcRequestTimeout    = 15 * time.Second
)

type oidcRuntimeConfig struct {
	IssuerURL            string
	ClientID             string
	ClientSecret         string
	RedirectURI          string
	ProviderName         string
	Scopes               []string
	AllowedGroups        []string
	GroupsClaim          string
	RequireVerifiedEmail bool
}

type oidcStartRequest struct {
	AppState string `json:"app_state"`
}

type oidcStartResponse struct {
	AuthorizationURL string `json:"authorization_url"`
}

type oidcLoginRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

type oidcLoginResponse struct {
	LoginResponse
	AppState string `json:"app_state,omitempty"`
}

type oidcClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	// Groups is populated exclusively from the raw-claims pass in
	// readOIDCClaims, which accepts both a string and an array of strings.
	// Excluded from the struct unmarshal (`json:"-"`) so a scalar `groups`
	// claim can't fail the whole decode before that pass runs.
	Groups []string `json:"-"`
}

var oidcProviders sync.Map

func loadOIDCRuntimeConfig() (oidcRuntimeConfig, error) {
	cfg := oidcRuntimeConfig{
		IssuerURL:            strings.TrimSpace(os.Getenv("OIDC_ISSUER_URL")),
		ClientID:             strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID")),
		ClientSecret:         strings.TrimSpace(os.Getenv("OIDC_CLIENT_SECRET")),
		RedirectURI:          strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URI")),
		ProviderName:         strings.TrimSpace(os.Getenv("OIDC_PROVIDER_NAME")),
		AllowedGroups:        splitCommaSeparated(os.Getenv("OIDC_ALLOWED_GROUPS")),
		GroupsClaim:          strings.TrimSpace(os.Getenv("OIDC_GROUPS_CLAIM")),
		RequireVerifiedEmail: os.Getenv("OIDC_REQUIRE_VERIFIED_EMAIL") != "false",
	}
	if cfg.IssuerURL == "" || cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURI == "" {
		return oidcRuntimeConfig{}, errors.New("OIDC is not configured")
	}
	issuer, err := url.Parse(cfg.IssuerURL)
	if err != nil || !validOIDCURL(issuer) {
		return oidcRuntimeConfig{}, errors.New("OIDC_ISSUER_URL must be HTTPS or a loopback URL")
	}
	redirect, err := url.Parse(cfg.RedirectURI)
	if err != nil || !validOIDCURL(redirect) {
		return oidcRuntimeConfig{}, errors.New("OIDC_REDIRECT_URI must be HTTPS or a loopback URL")
	}
	if cfg.ProviderName == "" {
		cfg.ProviderName = "OpenID Connect"
	}
	if cfg.GroupsClaim == "" {
		cfg.GroupsClaim = "groups"
	}
	cfg.Scopes = strings.Fields(os.Getenv("OIDC_SCOPES"))
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	if !contains(cfg.Scopes, oidc.ScopeOpenID) {
		cfg.Scopes = append([]string{oidc.ScopeOpenID}, cfg.Scopes...)
	}
	return cfg, nil
}

func splitCommaSeparated(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" && !contains(result, item) {
			result = append(result, item)
		}
	}
	return result
}

func readOIDCClaims(read func(any) error, claims *oidcClaims, groupsClaim string) error {
	if err := read(claims); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := read(&raw); err != nil {
		return err
	}
	value, ok := raw[groupsClaim]
	if !ok {
		return nil
	}
	var groups []string
	if err := json.Unmarshal(value, &groups); err != nil {
		var group string
		if stringErr := json.Unmarshal(value, &group); stringErr != nil {
			return errors.New("OIDC groups claim must be a string or an array of strings")
		}
		groups = []string{group}
	}
	claims.Groups = groups
	return nil
}

func oidcGroupAllowed(userGroups, allowedGroups []string) bool {
	if len(allowedGroups) == 0 {
		return true
	}
	for _, allowed := range allowedGroups {
		if contains(userGroups, allowed) {
			return true
		}
	}
	return false
}

func validOIDCURL(value *url.URL) bool {
	if value == nil || value.Host == "" {
		return false
	}
	if value.Scheme == "https" {
		return true
	}
	return value.Scheme == "http" && (value.Hostname() == "localhost" || value.Hostname() == "127.0.0.1" || value.Hostname() == "::1")
}

func discoverOIDCProvider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	if cached, ok := oidcProviders.Load(issuer); ok {
		return cached.(*oidc.Provider), nil
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	actual, _ := oidcProviders.LoadOrStore(issuer, provider)
	return actual.(*oidc.Provider), nil
}

func oidcOAuthConfig(cfg oidcRuntimeConfig, provider *oidc.Provider) oauth2.Config {
	return oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  cfg.RedirectURI,
		Scopes:       cfg.Scopes,
	}
}

func (h *Handler) StartOIDCLogin(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadOIDCRuntimeConfig()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var req oidcStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.AppState) > maxOIDCAppStateLength {
		writeError(w, http.StatusBadRequest, "app_state is too long")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), oidcRequestTimeout)
	defer cancel()
	provider, err := discoverOIDCProvider(ctx, cfg.IssuerURL)
	if err != nil {
		slog.Error("OIDC discovery failed", "issuer", cfg.IssuerURL, "error", err)
		writeError(w, http.StatusBadGateway, "failed to discover OIDC provider")
		return
	}
	verifier := oauth2.GenerateVerifier()
	flow, err := auth.NewOIDCFlow(req.AppState, verifier)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize OIDC login")
		return
	}
	if err := auth.SetOIDCFlowCookie(w, flow); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize OIDC login")
		return
	}
	config := oidcOAuthConfig(cfg, provider)
	authorizationURL := config.AuthCodeURL(
		flow.State,
		oidc.Nonce(flow.Nonce),
		oauth2.S256ChallengeOption(flow.CodeVerifier),
	)
	writeJSON(w, http.StatusOK, oidcStartResponse{AuthorizationURL: authorizationURL})
}

func (h *Handler) OIDCLogin(w http.ResponseWriter, r *http.Request) {
	defer auth.ClearOIDCFlowCookie(w)
	cfg, err := loadOIDCRuntimeConfig()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var req oidcLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	flow, err := auth.ReadOIDCFlowCookie(r)
	if err != nil || req.State == "" || req.State != flow.State {
		writeError(w, http.StatusBadRequest, "invalid or expired OIDC state")
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), oidcRequestTimeout)
	defer cancel()
	provider, err := discoverOIDCProvider(ctx, cfg.IssuerURL)
	if err != nil {
		slog.Error("OIDC discovery failed", "issuer", cfg.IssuerURL, "error", err)
		writeError(w, http.StatusBadGateway, "failed to discover OIDC provider")
		return
	}
	config := oidcOAuthConfig(cfg, provider)
	token, err := config.Exchange(ctx, req.Code, oauth2.VerifierOption(flow.CodeVerifier))
	if err != nil {
		slog.Warn("OIDC token exchange failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusBadRequest, "failed to exchange OIDC authorization code")
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		writeError(w, http.StatusBadGateway, "OIDC provider did not return an ID token")
		return
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		slog.Warn("OIDC ID token verification failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusUnauthorized, "failed to verify OIDC ID token")
		return
	}
	if idToken.Nonce != flow.Nonce {
		writeError(w, http.StatusUnauthorized, "invalid OIDC nonce")
		return
	}
	claims := oidcClaims{}
	if err := readOIDCClaims(idToken.Claims, &claims, cfg.GroupsClaim); err != nil {
		writeError(w, http.StatusBadGateway, "failed to parse OIDC claims")
		return
	}
	userInfo, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err == nil {
		if userInfo.Subject != idToken.Subject {
			writeError(w, http.StatusUnauthorized, "OIDC userinfo subject does not match ID token")
			return
		}
		if err := readOIDCClaims(userInfo.Claims, &claims, cfg.GroupsClaim); err != nil {
			slog.Warn("failed to parse OIDC userinfo claims", append(logger.RequestAttrs(r), "error", err)...)
		}
	}
	claims.Email = strings.ToLower(strings.TrimSpace(claims.Email))
	if claims.Email == "" {
		writeError(w, http.StatusBadRequest, "OIDC account has no email")
		return
	}
	if cfg.RequireVerifiedEmail && !claims.EmailVerified {
		writeError(w, http.StatusForbidden, "OIDC account email is not verified")
		return
	}
	if !oidcGroupAllowed(claims.Groups, cfg.AllowedGroups) {
		writeError(w, http.StatusForbidden, "OIDC account is not in an allowed group")
		return
	}
	user, isNew, err := h.resolveOIDCUser(ctx, idToken.Issuer, idToken.Subject, claims.Email)
	if err != nil {
		var signupErr SignupError
		if errors.As(err, &signupErr) {
			writeError(w, http.StatusForbidden, signupErr.Error())
			return
		}
		slog.Error("failed to resolve OIDC identity", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to resolve OIDC account")
		return
	}
	login, ok := h.completeFederatedLoginForUser(w, r, user, isNew, claims.Name, claims.Picture, "oidc")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, oidcLoginResponse{LoginResponse: login, AppState: flow.AppState})
}

func (h *Handler) resolveOIDCUser(ctx context.Context, issuer, subject, email string) (db.User, bool, error) {
	if h.TxStarter == nil {
		return db.User{}, false, errors.New("OIDC identity storage is unavailable")
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.User{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	identityKey := issuer + "\x00" + subject
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", identityKey); err != nil {
		return db.User{}, false, err
	}
	queries := h.Queries.WithTx(tx)
	user, err := queries.GetUserByOIDCIdentity(ctx, db.GetUserByOIDCIdentityParams{Issuer: issuer, Subject: subject})
	if err == nil {
		if err := queries.UpdateOIDCIdentityEmail(ctx, db.UpdateOIDCIdentityEmailParams{Issuer: issuer, Subject: subject, Email: email}); err != nil {
			return db.User{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return db.User{}, false, err
		}
		return user, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, false, err
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "oidc-email\x00"+email); err != nil {
		return db.User{}, false, err
	}
	user, isNew, err := h.findOrCreateUserWithQueries(ctx, queries, email)
	if err != nil {
		return db.User{}, false, err
	}
	if err := queries.CreateOIDCIdentity(ctx, db.CreateOIDCIdentityParams{
		Issuer: issuer, Subject: subject, UserID: user.ID, Email: email,
	}); err != nil {
		return db.User{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.User{}, false, err
	}
	return user, isNew, nil
}

func oidcConfigForPublicResponse() (string, bool) {
	cfg, err := loadOIDCRuntimeConfig()
	if err != nil {
		return "", false
	}
	return cfg.ProviderName, true
}
