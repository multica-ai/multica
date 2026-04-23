package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// GithubSpec describes the GitHub OAuth provider. The handler wraps this
// with NewHTTPOAuthProvider to produce a runtime OAuthProvider.
var GithubSpec = &ProviderSpec{
	ID:              "github",
	AuthorizeURL:    "https://github.com/login/oauth/authorize",
	TokenURL:        "https://github.com/login/oauth/access_token",
	UserinfoURL:     "https://api.github.com/user",
	Scope:           "read:user user:email",
	ClientIDEnv:     "GITHUB_CLIENT_ID",
	ClientSecretEnv: "GITHUB_CLIENT_SECRET",
	RedirectURIEnv:  "GITHUB_REDIRECT_URI",

	TokenErrorFromBody: parseGithubTokenError,
	ParseProfile:       parseGithubProfile,
}

// GitHub returns {"error": "...", "error_description": "..."} with HTTP 200
// when the code is invalid, so the body must be inspected explicitly.
func parseGithubTokenError(body []byte) error {
	var payload struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	if payload.Error != "" {
		return fmt.Errorf("github token exchange: %s: %s", payload.Error, payload.ErrorDescription)
	}
	return nil
}

func parseGithubProfile(ctx context.Context, client *http.Client, accessToken string, body []byte) (OAuthProfile, error) {
	var u struct {
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return OAuthProfile{}, err
	}

	// Always go through /user/emails. /user.email is the public profile field
	// and is not guaranteed verified — trusting it would let an attacker set
	// someone else's address as their public email and log in as them.
	email, verified, err := fetchGithubPrimaryEmail(ctx, client, accessToken)
	if err != nil {
		return OAuthProfile{}, err
	}

	name := strings.TrimSpace(u.Name)
	if name == "" {
		// GitHub /user.name is sometimes null — fall back to login so the
		// user gets a reasonable display name on first signup.
		name = u.Login
	}

	return OAuthProfile{
		Email:         email,
		Name:          name,
		Picture:       u.AvatarURL,
		EmailVerified: verified,
	}, nil
}

// fetchGithubPrimaryEmail reads /user/emails and returns the primary verified
// entry. Separate from parseGithubProfile so tests can drive it via the
// fake HTTP client.
func fetchGithubPrimaryEmail(ctx context.Context, client *http.Client, accessToken string) (email string, verified bool, err error) {
	body, err := getBearerJSON(ctx, client, accessToken, "https://api.github.com/user/emails", map[string]string{
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	})
	if err != nil {
		return "", false, err
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal(body, &emails); err != nil {
		return "", false, err
	}
	for _, e := range emails {
		if e.Primary && e.Verified && e.Email != "" {
			return e.Email, true, nil
		}
	}
	// Fallback: any verified email, in list order.
	for _, e := range emails {
		if e.Verified && e.Email != "" {
			return e.Email, true, nil
		}
	}
	return "", false, nil
}
