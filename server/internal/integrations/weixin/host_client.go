// Package weixin talks to the Weixin host (apps/weixin-host), the Node
// service that runs Tencent's official openclaw-weixin plugin and relays
// Weixin chats to Multica agents through the Chat API. The backend owns
// access control; the host owns Weixin credentials and the relay loops.
package weixin

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
	"time"
)

// ErrNotFound is returned when the host does not know the login or
// installation.
var ErrNotFound = errors.New("weixin host: not found")

// ErrConflict is returned when the host refuses a state transition, e.g.
// completing a login that is not connected.
var ErrConflict = errors.New("weixin host: conflict")

// Installation is one Weixin bot account relayed to one agent, as reported
// by the host. The installer's access token never leaves the host.
type Installation struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	AgentID         string  `json:"agent_id"`
	AccountID       string  `json:"account_id"`
	InstallerUserID string  `json:"installer_user_id"`
	TokenID         string  `json:"token_id"`
	CreatedAt       string  `json:"created_at"`
	Running         bool    `json:"running"`
	LastError       *string `json:"last_error"`
}

// Login is the state of one QR login.
type Login struct {
	ID                string `json:"id"`
	State             string `json:"state"`
	QRContent         string `json:"qr_content"`
	Message           string `json:"message"`
	VerifyCodeInvalid bool   `json:"verify_code_invalid"`
	AccountID         string `json:"account_id,omitempty"`
	WorkspaceID       string `json:"workspace_id"`
	AgentID           string `json:"agent_id"`
	UserID            string `json:"user_id"`
	Consumed          bool   `json:"consumed"`
}

// LoginOwner ties a login to the member who started it, so only they can
// finish it.
type LoginOwner struct {
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	UserID      string `json:"user_id"`
}

// CreateInstallationRequest turns a connected login into a relay.
type CreateInstallationRequest struct {
	LoginID         string `json:"login_id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
	InstallerUserID string `json:"installer_user_id"`
	Token           string `json:"token"`
	TokenID         string `json:"token_id"`
}

// SupersededInstallation is an installation the host dropped because the
// new one replaces it; its token should be revoked.
type SupersededInstallation struct {
	ID              string `json:"id"`
	TokenID         string `json:"token_id"`
	InstallerUserID string `json:"installer_user_id"`
}

// CreateInstallationResponse is the host's answer to CreateInstallation.
type CreateInstallationResponse struct {
	Installation Installation             `json:"installation"`
	Superseded   []SupersededInstallation `json:"superseded"`
}

// HostClient calls the Weixin host's control API.
type HostClient struct {
	baseURL string
	secret  string
	http    *http.Client
}

// NewHostClient returns a client for the host at baseURL, authenticating
// with the shared secret.
func NewHostClient(baseURL, secret string) *HostClient {
	return &HostClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// ListInstallations returns the installations in a workspace.
func (c *HostClient) ListInstallations(ctx context.Context, workspaceID string) ([]Installation, error) {
	var out struct {
		Installations []Installation `json:"installations"`
	}
	path := "/v1/installations?workspace_id=" + url.QueryEscape(workspaceID)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Installations, nil
}

// StartLogin asks the host for a fresh Weixin QR login.
func (c *HostClient) StartLogin(ctx context.Context, owner LoginOwner) (Login, error) {
	var out Login
	err := c.do(ctx, http.MethodPost, "/v1/logins", owner, &out)
	return out, err
}

// GetLogin returns a login's current state.
func (c *HostClient) GetLogin(ctx context.Context, loginID string) (Login, error) {
	var out Login
	err := c.do(ctx, http.MethodGet, "/v1/logins/"+url.PathEscape(loginID), nil, &out)
	return out, err
}

// SubmitVerifyCode forwards the number shown on the phone.
func (c *HostClient) SubmitVerifyCode(ctx context.Context, loginID, code string) (Login, error) {
	var out Login
	err := c.do(ctx, http.MethodPost, "/v1/logins/"+url.PathEscape(loginID)+"/verify-code",
		map[string]string{"code": code}, &out)
	return out, err
}

// CreateInstallation starts relaying a connected login's account.
func (c *HostClient) CreateInstallation(ctx context.Context, req CreateInstallationRequest) (CreateInstallationResponse, error) {
	var out CreateInstallationResponse
	err := c.do(ctx, http.MethodPost, "/v1/installations", req, &out)
	return out, err
}

// DeleteInstallation stops a relay and forgets its Weixin credentials.
func (c *HostClient) DeleteInstallation(ctx context.Context, installationID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/installations/"+url.PathEscape(installationID), nil, nil)
}

func (c *HostClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("weixin host unreachable: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusConflict:
		return fmt.Errorf("%w: %s", ErrConflict, hostError(data))
	case resp.StatusCode >= 300:
		return fmt.Errorf("weixin host %s %s: %d %s", method, path, resp.StatusCode, hostError(data))
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func hostError(data []byte) string {
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &body) == nil && body.Error != "" {
		return body.Error
	}
	return strings.TrimSpace(string(data))
}
