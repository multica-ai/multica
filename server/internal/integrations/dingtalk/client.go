package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// errUnauthorized is returned by postJSON on an HTTP 401 so the outbound sender
// can drop the cached access_token and retry once with a fresh one.
var errUnauthorized = errors.New("dingtalk: unauthorized (access token expired or invalid)")

// This file is the thin DingTalk Open-API REST seam the install + outbound
// paths share: minting an access_token from AppKey/AppSecret. It is deliberately
// hand-rolled over net/http (not the Stream SDK) because the only REST call the
// server makes outside the Stream connection is the token mint plus the message
// send; keeping it here makes both trivially testable against an httptest
// server via the apiBase override.

// defaultAPIBase is the DingTalk Open-API host. The mainland cloud is the only
// region DingTalk exposes for these endpoints, so unlike Feishu there is no
// per-installation region split.
const defaultAPIBase = "https://api.dingtalk.com"

// accessTokenPath mints an enterprise-internal-app access_token from the app's
// AppKey/AppSecret. The response carries the token and its lifetime in seconds.
const accessTokenPath = "/v1.0/oauth2/accessToken"

// accessTokenResponse is the success shape of accessTokenPath.
type accessTokenResponse struct {
	AccessToken string `json:"accessToken"`
	ExpireIn    int64  `json:"expireIn"`
}

// apiError is the DingTalk Open-API error envelope (non-2xx responses).
type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestid"`
}

// fetchAccessToken mints an access_token for (appKey, appSecret). baseURL
// defaults to the DingTalk Open-API host; tests point it at an httptest server.
// httpClient defaults to http.DefaultClient. It returns the token and its
// lifetime in seconds. A non-2xx response or a missing token is an error — the
// install path uses a failure here as "these credentials are wrong".
func fetchAccessToken(ctx context.Context, httpClient *http.Client, baseURL, appKey, appSecret string) (string, int64, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	base := baseURL
	if base == "" {
		base = defaultAPIBase
	}
	base = strings.TrimRight(base, "/")

	reqBody, err := json.Marshal(map[string]string{"appKey": appKey, "appSecret": appSecret})
	if err != nil {
		return "", 0, fmt.Errorf("dingtalk: marshal access token request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+accessTokenPath, bytes.NewReader(reqBody))
	if err != nil {
		return "", 0, fmt.Errorf("dingtalk: build access token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("dingtalk: access token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, fmt.Errorf("dingtalk: read access token response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr apiError
		_ = json.Unmarshal(body, &apiErr)
		if apiErr.Message != "" {
			return "", 0, fmt.Errorf("dingtalk: access token: code=%q message=%q", apiErr.Code, apiErr.Message)
		}
		return "", 0, fmt.Errorf("dingtalk: access token: http %d", resp.StatusCode)
	}

	var tok accessTokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", 0, fmt.Errorf("dingtalk: decode access token response: %w", err)
	}
	if tok.AccessToken == "" {
		return "", 0, fmt.Errorf("dingtalk: access token response missing accessToken")
	}
	return tok.AccessToken, tok.ExpireIn, nil
}
