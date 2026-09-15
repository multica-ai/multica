package handler

import "os"

// Default endpoints of the historical, hard-coded provider. Keeping them here
// (rather than inline in GoogleLogin) means a deployment that sets none of the
// OIDC_* variables behaves exactly as it did before this file existed.
const (
	googleAuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL     = "https://oauth2.googleapis.com/token"
	googleUserInfoURL  = "https://www.googleapis.com/oauth2/v2/userinfo"

	defaultOIDCScopes = "openid email profile"
)

// oidcProvider is the resolved single sign-on provider for this deployment.
//
// The upstream sign-in flow was already provider-agnostic — an authorization
// code is swapped for an access token, and `/userinfo` is read for
// {email, name, picture}, all of which are standard OpenID Connect. Only the
// three endpoint URLs were pinned to Google. Making them configurable is
// therefore the whole of what generic OIDC support requires; no new protocol
// code is needed.
type oidcProvider struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	AuthorizeURL string
	TokenURL     string
	UserInfoURL  string
	Scopes       string
	// DisplayName labels the sign-in button and the operator-facing errors.
	DisplayName string
	// AuthMethod is the value reported to analytics for a signup via this
	// provider.
	AuthMethod string
}

// Configured reports whether sign-in through this provider can be attempted.
func (p oidcProvider) Configured() bool {
	return p.ClientID != "" && p.ClientSecret != ""
}

// IsGoogle reports whether the resolved provider is still the built-in one.
func (p oidcProvider) IsGoogle() bool {
	return p.TokenURL == googleTokenURL && p.UserInfoURL == googleUserInfoURL
}

func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// oidcProviderFromEnv resolves the provider on every call rather than caching
// it, matching GetConfig's existing "re-read from env on every request so
// operators can rotate keys via secret refresh without a server restart"
// contract.
//
// The OIDC_* names take precedence; the GOOGLE_* names remain accepted so an
// existing deployment keeps working untouched. Endpoints are given explicitly
// rather than discovered from an issuer: discovery would add a backchannel
// call to the identity provider on the sign-in path, and every endpoint of a
// Keycloak realm is derivable from its issuer by hand anyway.
func oidcProviderFromEnv() oidcProvider {
	p := oidcProvider{
		ClientID:     firstNonEmptyEnv("OIDC_CLIENT_ID", "GOOGLE_CLIENT_ID"),
		ClientSecret: firstNonEmptyEnv("OIDC_CLIENT_SECRET", "GOOGLE_CLIENT_SECRET"),
		RedirectURI:  firstNonEmptyEnv("OIDC_REDIRECT_URI", "GOOGLE_REDIRECT_URI"),
		AuthorizeURL: firstNonEmptyEnv("OIDC_AUTHORIZE_URL"),
		TokenURL:     firstNonEmptyEnv("OIDC_TOKEN_URL"),
		UserInfoURL:  firstNonEmptyEnv("OIDC_USERINFO_URL"),
		Scopes:       firstNonEmptyEnv("OIDC_SCOPES"),
		DisplayName:  firstNonEmptyEnv("OIDC_DISPLAY_NAME"),
	}

	if p.AuthorizeURL == "" {
		p.AuthorizeURL = googleAuthorizeURL
	}
	if p.TokenURL == "" {
		p.TokenURL = googleTokenURL
	}
	if p.UserInfoURL == "" {
		p.UserInfoURL = googleUserInfoURL
	}
	if p.Scopes == "" {
		p.Scopes = defaultOIDCScopes
	}

	if p.IsGoogle() {
		p.AuthMethod = "google"
		if p.DisplayName == "" {
			p.DisplayName = "Google"
		}
		return p
	}

	p.AuthMethod = "oidc"
	if p.DisplayName == "" {
		p.DisplayName = "SSO"
	}
	return p
}
