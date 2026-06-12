package agentvault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client talks to the Agent Vault management API over the internal path. It is
// constructed from Config and is safe for reuse; each call performs an admin
// login to obtain a short-lived bearer token (Agent Vault sessions are
// short-lived by design — see /v1/auth/login).
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient builds a Client. The caller is responsible for only constructing it
// when LoadConfig reported ok (admin credentials present).
func NewClient(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second}}
}

// VaultAccess is one row of an agent's scoped access: a box (vault) and the
// role the agent holds in it. Mirrors cerebro_agentvault_agent_access.
type VaultAccess struct {
	Vault string `json:"vault"`
	Role  string `json:"role"`
}

// login authenticates as the instance owner and returns a bearer token.
// Verified against the live instance: POST /v1/auth/login -> {token, expires_at}.
func (c *Client) login(ctx context.Context) (string, error) {
	body, _ := json.Marshal(map[string]string{"email": c.cfg.AdminEmail, "password": c.cfg.AdminPassword})
	var out struct {
		Token string `json:"token"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/auth/login", "", body, &out); err != nil {
		return "", fmt.Errorf("agentvault login: %w", err)
	}
	if out.Token == "" {
		return "", fmt.Errorf("agentvault login: empty token")
	}
	return out.Token, nil
}

// MintAgentToken creates (or refreshes) the Agent Vault identity for a Multica
// agent and returns a long-lived agent token scoped to the given vaults. The
// agent's instance role is always "no-access" — it may only proxy traffic for
// the vaults it was granted, and can never read or reveal a credential.
//
// Token shape (av_agt_...) is verified; the request body is the documented
// agent-vault v0.31.0 create-agent contract and is validated end-to-end before
// this path is wired into spawn (TECH-3196 Slice 2).
func (c *Client) MintAgentToken(ctx context.Context, agentName string, access []VaultAccess) (string, error) {
	adminTok, err := c.login(ctx)
	if err != nil {
		return "", err
	}
	vaults := make([]string, 0, len(access))
	for _, a := range access {
		role := a.Role
		if role == "" {
			role = "read-only"
		}
		vaults = append(vaults, fmt.Sprintf("%s:%s", a.Vault, role))
	}
	reqBody, _ := json.Marshal(map[string]any{
		"name":       agentName,
		"role":       "no-access",
		"vaults":     vaults,
		"token_only": true,
	})
	var out struct {
		Token string `json:"token"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/agents", adminTok, reqBody, &out); err != nil {
		return "", fmt.Errorf("agentvault mint token for %q: %w", agentName, err)
	}
	if out.Token == "" {
		return "", fmt.Errorf("agentvault mint token for %q: empty token", agentName)
	}
	return out.Token, nil
}

// do performs one JSON request against the internal Agent Vault API.
func (c *Client) do(ctx context.Context, method, path, bearer string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.InternalURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(payload))
	}
	if out != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
