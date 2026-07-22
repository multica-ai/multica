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
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
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
}

var oidcProviders sync.Map

func loadOIDCRuntimeConfig() (oidcRuntimeConfig, error) {
	cfg := oidcRuntimeConfig{
		IssuerURL:            strings.TrimSpace(os.Getenv("OIDC_ISSUER_URL")),
		ClientID:             strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID")),
		ClientSecret:         strings.TrimSpace(os.Getenv("OIDC_CLIENT_SECRET")),
		RedirectURI:          strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URI")),
		ProviderName:         strings.TrimSpace(os.Getenv("OIDC_PROVIDER_NAME")),
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
	cfg.Scopes = strings.Fields(os.Getenv("OIDC_SCOPES"))
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	if !contains(cfg.Scopes, oidc.ScopeOpenID) {
		cfg.Scopes = append([]string{oidc.ScopeOpenID}, cfg.Scopes...)
	}
	return cfg, nil
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
	if err := idToken.Claims(&claims); err != nil {
		writeError(w, http.StatusBadGateway, "failed to parse OIDC claims")
		return
	}
	userInfo, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err == nil {
		if userInfo.Subject != idToken.Subject {
			writeError(w, http.StatusUnauthorized, "OIDC userinfo subject does not match ID token")
			return
		}
		if err := userInfo.Claims(&claims); err != nil {
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
	login, ok := h.completeFederatedLogin(w, r, claims.Email, claims.Name, claims.Picture, "oidc")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, oidcLoginResponse{LoginResponse: login, AppState: flow.AppState})
}

func oidcConfigForPublicResponse() (string, bool) {
	cfg, err := loadOIDCRuntimeConfig()
	if err != nil {
		return "", false
	}
	return cfg.ProviderName, true
}
