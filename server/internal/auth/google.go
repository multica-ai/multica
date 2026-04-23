package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// GoogleSpec describes the Google OAuth provider. The handler wraps this
// with NewHTTPOAuthProvider to produce a runtime OAuthProvider.
var GoogleSpec = &ProviderSpec{
	ID:              "google",
	AuthorizeURL:    "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL:        "https://oauth2.googleapis.com/token",
	UserinfoURL:     "https://www.googleapis.com/oauth2/v2/userinfo",
	Scope:           "openid email profile",
	ClientIDEnv:     "GOOGLE_CLIENT_ID",
	ClientSecretEnv: "GOOGLE_CLIENT_SECRET",
	RedirectURIEnv:  "GOOGLE_REDIRECT_URI",

	ExtraAuthParams: map[string]string{
		"access_type": "offline",
		"prompt":      "select_account",
	},
	ExtraTokenParams: map[string]string{
		"grant_type": "authorization_code",
	},

	ParseProfile: parseGoogleProfile,
}

func parseGoogleProfile(_ context.Context, _ *http.Client, _ string, body []byte) (OAuthProfile, error) {
	var u struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return OAuthProfile{}, err
	}

	email := strings.TrimSpace(u.Email)
	// Google's OIDC userinfo only returns the email when it is verified,
	// so presence implies verification.
	return OAuthProfile{
		Email:         email,
		Name:          u.Name,
		Picture:       u.Picture,
		EmailVerified: email != "",
	}, nil
}
