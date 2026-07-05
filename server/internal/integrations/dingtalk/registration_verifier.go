package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CredentialVerifier is the production AppCredentialVerifier: it
// exchanges the freshly minted (client_id, client_secret) pair for an
// app access token via POST /v1.0/oauth2/accessToken — the cheapest
// call that proves the device flow really created a working app before
// RegistrationService commits the credentials.
type CredentialVerifier struct {
	openAPIBase string
	httpClient  *http.Client
}

// NewCredentialVerifier constructs the verifier. base empty defaults to
// https://api.dingtalk.com; client nil defaults to a 30s-timeout client.
func NewCredentialVerifier(base string, client *http.Client) *CredentialVerifier {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = defaultOpenAPIBase
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &CredentialVerifier{openAPIBase: base, httpClient: client}
}

// VerifyAppCredentials implements AppCredentialVerifier.
func (v *CredentialVerifier) VerifyAppCredentials(ctx context.Context, clientID, clientSecret string) error {
	body, err := json.Marshal(map[string]string{
		"appKey":    clientID,
		"appSecret": clientSecret,
	})
	if err != nil {
		return fmt.Errorf("dingtalk credentials check: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.openAPIBase+"/v1.0/oauth2/accessToken", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("dingtalk credentials check: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk credentials check: http do: %w", err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Code: "credentials_check_failed", Message: strings.TrimSpace(truncate(string(payload), 256))}
	}
	var tokenResp struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(payload, &tokenResp); err != nil {
		return fmt.Errorf("dingtalk credentials check: decode: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return &APIError{Code: "empty_access_token", Message: "DingTalk returned no access token for the new app"}
	}
	return nil
}
