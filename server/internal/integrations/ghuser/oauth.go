package ghuser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const oauthResponseLimit = 1 << 20

// tokenResponse is GitHub's answer to both the code exchange and a refresh.
// GitHub reports OAuth failures with HTTP 200 and an `error` field.
type tokenResponse struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	Error                 string `json:"error"`
}

// oauthError is an OAuth error code returned by GitHub's token endpoint. Only
// the code is kept: the description adds nothing an operator needs.
type oauthError struct {
	Code string
}

func (e *oauthError) Error() string { return "ghuser: github oauth error: " + e.Code }

// revokesConnection reports whether the error means the stored authorization
// is dead (bad_refresh_token and similar). A deployment credential problem is
// the operator's to fix and must not disconnect every user.
func (e *oauthError) revokesConnection() bool {
	return e.Code != "incorrect_client_credentials"
}

// AuthorizeURL is GitHub's login/oauth/authorize address for this App. An
// empty redirectURI lets GitHub use the App's registered callback URL.
func (s *Service) AuthorizeURL(state, redirectURI string) string {
	q := url.Values{}
	q.Set("client_id", s.clientID)
	q.Set("state", state)
	if redirectURI != "" {
		q.Set("redirect_uri", redirectURI)
	}
	return s.oauthBase + "/login/oauth/authorize?" + q.Encode()
}

func (s *Service) exchangeCode(ctx context.Context, code, redirectURI string) (tokenResponse, error) {
	form := url.Values{}
	form.Set("code", code)
	if redirectURI != "" {
		form.Set("redirect_uri", redirectURI)
	}
	return s.postToken(ctx, form)
}

func (s *Service) refreshTokens(ctx context.Context, refreshToken string) (tokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	return s.postToken(ctx, form)
}

func (s *Service) postToken(ctx context.Context, form url.Values) (tokenResponse, error) {
	form.Set("client_id", s.clientID)
	form.Set("client_secret", s.clientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.oauthBase+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return tokenResponse{}, errors.New("ghuser: github token request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, oauthResponseLimit))
	if err != nil {
		return tokenResponse{}, errors.New("ghuser: read github token response failed")
	}
	if resp.StatusCode != http.StatusOK {
		// The body is never echoed: it may carry the submitted credentials.
		return tokenResponse{}, fmt.Errorf("ghuser: github token endpoint status %d", resp.StatusCode)
	}
	var parsed tokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return tokenResponse{}, errors.New("ghuser: malformed github token response")
	}
	if parsed.Error != "" {
		return tokenResponse{}, &oauthError{Code: parsed.Error}
	}
	if parsed.AccessToken == "" {
		return tokenResponse{}, errors.New("ghuser: github returned no access token")
	}
	return parsed, nil
}

func (s *Service) fetchViewer(ctx context.Context, accessToken string) (Account, error) {
	var viewer struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := s.REST(ctx, accessToken, http.MethodGet, "/user", nil, &viewer); err != nil {
		return Account{}, err
	}
	if viewer.ID == 0 || viewer.Login == "" {
		return Account{}, errors.New("ghuser: malformed github user response")
	}
	return Account{GitHubUserID: viewer.ID, Login: viewer.Login, AvatarURL: viewer.AvatarURL}, nil
}

// revokeGrant deletes the user's authorization of the App on GitHub, which
// invalidates every token issued under it. It authenticates as the App with
// HTTP basic auth (client ID and secret).
func (s *Service) revokeGrant(ctx context.Context, accessToken string) error {
	payload, err := json.Marshal(map[string]string{"access_token": accessToken})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/applications/%s/grant", s.apiBase, url.PathEscape(s.clientID))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.clientID, s.clientSecret)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return errors.New("ghuser: revoke grant request failed")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, oauthResponseLimit))
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("ghuser: revoke grant status %d", resp.StatusCode)
	}
	return nil
}
