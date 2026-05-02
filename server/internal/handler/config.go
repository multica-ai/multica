package handler

import (
	"net/http"
	"os"
)

type AppConfig struct {
	CdnDomain string `json:"cdn_domain"`
	// Public auth config consumed by the web app at runtime so self-hosted
	// deployments do not need to rebuild the frontend image when operators
	// toggle signup or wire Google OAuth.
	AllowSignup    bool   `json:"allow_signup"`
	GoogleClientID string `json:"google_client_id,omitempty"`
	// OIDCIssuerURL is the public issuer URL of the OIDC IDP. Empty when
	// OIDC is not configured. The web frontend uses this to know whether
	// to render the "Continue with SSO" button and to compute the
	// authorize URL (issuer + /authorize, after spec-compliant
	// trailing-slash join).
	OIDCIssuerURL string `json:"oidc_issuer_url,omitempty"`
	// OIDCClientID exposed to the web frontend so the browser can build
	// the /authorize redirect with the correct client_id. Public per OIDC
	// spec (it is included in every authorize request URL anyway).
	OIDCClientID string `json:"oidc_client_id,omitempty"`
	// OIDCAuthorizationEndpoint is the spec-compliant authorize URL pulled
	// from the IDP's discovery document. We expose it (instead of letting
	// the frontend construct `<issuer>/authorize`) because IDPs do not
	// agree on URL layout: Authentik puts the authorize endpoint at
	// /application/o/authorize/ (shared across apps, distinguished by
	// client_id), while Auth0/Keycloak keep it under the issuer subpath.
	// Frontend reads this field and redirects directly — no URL
	// construction. Empty when OIDC is not configured or discovery hasn't
	// loaded yet.
	OIDCAuthorizationEndpoint string `json:"oidc_authorization_endpoint,omitempty"`

	// PostHog public config for the frontend. The key is the same Project
	// API Key the backend uses; returning it here (instead of baking it
	// into the frontend bundle via NEXT_PUBLIC_*) means self-hosted
	// instances — whose server returns an empty key — automatically
	// disable frontend event shipping too.
	PosthogKey  string `json:"posthog_key"`
	PosthogHost string `json:"posthog_host"`
}

// GetConfig is mounted on the public (unauthenticated) route group because
// the web app calls it before login to decide whether to render the Google
// sign-in button and signup UI. Only add fields here that are safe to expose
// to anonymous callers — never user- or tenant-scoped data.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	config := AppConfig{
		AllowSignup:    os.Getenv("ALLOW_SIGNUP") != "false",
		GoogleClientID: os.Getenv("GOOGLE_CLIENT_ID"),
		OIDCIssuerURL:  os.Getenv("OIDC_ISSUER_URL"),
		OIDCClientID:   os.Getenv("OIDC_CLIENT_ID"),
	}
	if h.Storage != nil {
		config.CdnDomain = h.Storage.CdnDomain()
	}

	// Resolve OIDC authorization_endpoint from the IDP's discovery doc.
	// First call pays the discovery fetch cost (~100-300ms); subsequent
	// calls hit the in-memory cache in oidcRT. We swallow load errors:
	// /api/config must remain healthy even when OIDC misconfigured.
	if config.OIDCIssuerURL != "" && config.OIDCClientID != "" {
		if provider, _, _, _, err := oidcRT.load(r.Context()); err == nil {
			config.OIDCAuthorizationEndpoint = provider.Endpoint().AuthURL
		}
	}

	// Re-read from env on every request so operators can rotate keys via
	// secret refresh without a server restart.
	if v := os.Getenv("ANALYTICS_DISABLED"); v != "true" && v != "1" {
		config.PosthogKey = os.Getenv("POSTHOG_API_KEY")
		config.PosthogHost = os.Getenv("POSTHOG_HOST")
		if config.PosthogHost == "" && config.PosthogKey != "" {
			config.PosthogHost = "https://us.i.posthog.com"
		}
	}

	writeJSON(w, http.StatusOK, config)
}
