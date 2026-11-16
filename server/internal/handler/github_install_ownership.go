package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// GitHub's user OAuth endpoints live on the web host, separate from its API.
var githubOAuthBase = "https://github.com"

func githubAppClientID() string     { return strings.TrimSpace(os.Getenv("GITHUB_APP_CLIENT_ID")) }
func githubAppClientSecret() string { return strings.TrimSpace(os.Getenv("GITHUB_APP_CLIENT_SECRET")) }

var (
	// errGitHubUserAuthorizationMissing means the setup redirect carried no
	// user-authorization `code`. Almost always operator-actionable: the App
	// needs "Request user authorization (OAuth) during installation" enabled.
	errGitHubUserAuthorizationMissing  = errors.New("github: setup callback carried no user authorization code")
	errGitHubInstallationNotAuthorized = errors.New("github: installation is not accessible to the installing user")
)

const (
	githubUserInstallationsPageSize = 100
	// githubUserInstallationsMaxPages caps the walk at 2000 installations so a
	// hostile or pathological account cannot hold the callback open forever.
	githubUserInstallationsMaxPages = 20
)

// The signed state authorizes only the workspace. A new installation binding
// also requires GitHub to confirm the installing user's access; all failures
// abort the bind before any metadata fetch or database write.
func verifyGitHubInstallationOwnership(ctx context.Context, code string, installationID int64) error {
	clientID, clientSecret := githubAppClientID(), githubAppClientSecret()
	if clientID == "" || clientSecret == "" {
		return errors.New("github: user authorization credentials are not configured")
	}
	if strings.TrimSpace(code) == "" {
		return errGitHubUserAuthorizationMissing
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 15 * time.Second}
	userToken, err := exchangeGitHubUserCode(ctx, client, clientID, clientSecret, code)
	if err != nil {
		return err
	}
	// The token never leaves this function and is never stored; revoking it
	// mirrors how installation tokens are handled elsewhere in this file.
	defer revokeGitHubUserToken(client, clientID, clientSecret, userToken)
	return assertUserCanAccessInstallation(ctx, client, userToken, installationID)
}

// githubOwnershipErrorCode maps a verification failure to the `github_error`
// code carried back to Settings. The frontend renders one generic "connect
// failed" toast for all of them; the distinction is for operators reading
// logs and support tickets.
func githubOwnershipErrorCode(err error) string {
	switch {
	case errors.Is(err, errGitHubInstallationNotAuthorized):
		return "installation_not_authorized"
	case errors.Is(err, errGitHubUserAuthorizationMissing):
		return "missing_authorization"
	default:
		return "verification_failed"
	}
}

// exchangeGitHubUserCode trades the setup redirect's `code` for a
// user-to-server access token. GitHub answers 200 with an `error` field for a
// bad or replayed code, so the body is checked as well as the status.
func exchangeGitHubUserCode(
	ctx context.Context,
	client *http.Client,
	clientID, clientSecret, code string,
) (string, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("code", code)
	endpoint := strings.TrimRight(githubOAuthBase, "/") + "/login/oauth/access_token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange github user code: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, githubAPIResponseLimit))
		return "", fmt.Errorf("exchange github user code: github status %d", resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, githubAPIResponseLimit)).Decode(&body); err != nil {
		return "", fmt.Errorf("decode github user token: %w", err)
	}
	if body.Error != "" {
		return "", errors.New("github refused the user authorization code")
	}
	if body.AccessToken == "" {
		return "", errors.New("github returned an empty user access token")
	}
	return body.AccessToken, nil
}

// assertUserCanAccessInstallation returns nil only when GitHub lists
// installationID among the installations the token's user can reach.
func assertUserCanAccessInstallation(
	ctx context.Context,
	client *http.Client,
	userToken string,
	installationID int64,
) error {
	for page := 1; page <= githubUserInstallationsMaxPages; page++ {
		endpoint := fmt.Sprintf(
			"%s/user/installations?per_page=%d&page=%d",
			strings.TrimRight(githubAPIBase, "/"),
			githubUserInstallationsPageSize,
			page,
		)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		setGitHubAPIHeaders(req, userToken)
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("list user installations: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, githubAPIResponseLimit))
			resp.Body.Close()
			return fmt.Errorf("list user installations: github status %d", resp.StatusCode)
		}
		var body struct {
			Installations []struct {
				ID int64 `json:"id"`
			} `json:"installations"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, githubAPIResponseLimit)).Decode(&body)
		resp.Body.Close()
		if decodeErr != nil {
			return fmt.Errorf("decode user installations: %w", decodeErr)
		}
		for _, installation := range body.Installations {
			if installation.ID == installationID {
				return nil
			}
		}
		if len(body.Installations) < githubUserInstallationsPageSize {
			break
		}
	}
	return errGitHubInstallationNotAuthorized
}

// revokeGitHubUserToken drops the short-lived user token as soon as the
// ownership check is done, even if the caller cancelled. Revocation is best-effort.
func revokeGitHubUserToken(client *http.Client, clientID, clientSecret, token string) {
	if token == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint := fmt.Sprintf(
		"%s/applications/%s/token",
		strings.TrimRight(githubAPIBase, "/"),
		url.PathEscape(clientID),
	)
	payload, err := json.Marshal(map[string]string{"access_token": token})
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(clientID, clientSecret)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, githubAPIResponseLimit))
}
