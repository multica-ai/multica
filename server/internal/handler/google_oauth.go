package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	defaultGoogleTokenURL    = "https://oauth2.googleapis.com/token"
	defaultGoogleUserInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"
)

type googleOAuthStatusError struct {
	Endpoint string
	Status   int
	Body     string
}

func (e *googleOAuthStatusError) Error() string {
	return fmt.Sprintf("google oauth endpoint %s returned status %d", e.Endpoint, e.Status)
}

func googleTokenURL() string {
	if raw := strings.TrimSpace(os.Getenv("GOOGLE_TOKEN_URL")); raw != "" {
		return raw
	}
	return defaultGoogleTokenURL
}

func googleUserInfoURL() string {
	if raw := strings.TrimSpace(os.Getenv("GOOGLE_USERINFO_URL")); raw != "" {
		return raw
	}
	return defaultGoogleUserInfoURL
}

func exchangeGoogleCode(ctx context.Context, code, clientID, clientSecret, redirectURI string) (googleTokenResponse, error) {
	values := url.Values{
		"code":          {strings.TrimSpace(code)},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}

	endpoint := googleTokenURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return googleTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return googleTokenResponse{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return googleTokenResponse{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return googleTokenResponse{}, &googleOAuthStatusError{
			Endpoint: endpoint,
			Status:   resp.StatusCode,
			Body:     string(body),
		}
	}

	var token googleTokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return googleTokenResponse{}, err
	}
	return token, nil
}

func fetchGoogleUserInfo(ctx context.Context, accessToken string) (googleUserInfo, error) {
	endpoint := googleUserInfoURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return googleUserInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return googleUserInfo{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return googleUserInfo{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return googleUserInfo{}, &googleOAuthStatusError{
			Endpoint: endpoint,
			Status:   resp.StatusCode,
			Body:     string(body),
		}
	}

	var user googleUserInfo
	if err := json.Unmarshal(body, &user); err != nil {
		return googleUserInfo{}, err
	}
	return user, nil
}
