package agentvault

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
)

var browserLoginCredentialKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// connectionResolver resolves a workspace's enabled connections server-side. It
// is satisfied by *connections.Store — the SAME seam the MCP relay uses to
// resolve a connection's credential without going through an agent. Declared as
// an interface so the client can be unit-tested with a fake.
type connectionResolver interface {
	ListEnabled(ctx context.Context, workspaceID pgtype.UUID) ([]connections.Connection, error)
}

// Client talks to the Agent Vault management API over the internal path. It
// resolves the workspace's "Agent Vault" connection at call time and uses that
// connection's own Bearer credential — no admin login, no env secret.
type Client struct {
	cfg   Config
	conns connectionResolver
	http  *http.Client
}

// NewClient builds a Client. conns resolves the Agent Vault connection per
// workspace; a nil conns makes ListVaults degrade to an empty list (the same
// "not configured" contract the vaults endpoint has always used).
func NewClient(cfg Config, conns connectionResolver) *Client {
	return &Client{cfg: cfg, conns: conns, http: &http.Client{Timeout: 20 * time.Second}}
}

// Vault is one Agent Vault box as listed by GET /v1/vaults. Only the two fields
// the credential Permissions vault-picker needs are decoded — the Agent Vault
// response also carries role/membership/pending_proposals/credential_store,
// which the picker deliberately ignores. Verified against agent-vault
// handleVaultList -> {"vaults":[{id,name,...}]}.
type Vault struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListVaults returns the Agent Vault boxes visible to the "Agent Vault"
// connection's credential (an owner token, so every box on the instance). It
// resolves the connection for this workspace, then calls GET /v1/vaults with the
// connection's Bearer token. Returns an empty list (not an error) when no Agent
// Vault connection is configured for the workspace, so the picker degrades to
// "no vaults available" rather than breaking the Permissions screen. Used
// read-only to populate the vault picker on the credential Permissions row
// (FIR-1739 v1, vault-level).
func (c *Client) ListVaults(ctx context.Context, workspaceID pgtype.UUID) ([]Vault, error) {
	conn, ok, err := c.resolveConnection(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	bearer := strings.TrimSpace(conn.AuthConfig.BearerToken)
	if bearer == "" {
		return nil, fmt.Errorf("agentvault: %q connection has no bearer token", conn.Name)
	}
	base := strings.TrimRight(strings.TrimSpace(conn.URL), "/")
	var out struct {
		Vaults []Vault `json:"vaults"`
	}
	if err := c.get(ctx, base+"/v1/vaults", bearer, &out); err != nil {
		return nil, fmt.Errorf("agentvault list vaults: %w", err)
	}
	return out.Vaults, nil
}

// RevealCredential reads one value for the secure browser-fill bridge. The
// value is returned only to the trusted server caller; handlers must never
// serialize it to an agent-facing response or log it.
func (c *Client) RevealCredential(ctx context.Context, workspaceID pgtype.UUID, vault, key string) (string, error) {
	vault = strings.TrimSpace(vault)
	key = strings.TrimSpace(key)
	const prefix = "Shared/browser-login/"
	app := strings.TrimPrefix(vault, prefix)
	if app == vault || app == "" || strings.Contains(app, "/") {
		return "", fmt.Errorf("agentvault: vault must be Shared/browser-login/<app>")
	}
	if !browserLoginCredentialKey.MatchString(key) {
		return "", fmt.Errorf("agentvault: invalid credential key")
	}
	conn, ok, err := c.resolveConnection(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("agentvault: connection is not configured")
	}
	bearer := strings.TrimSpace(conn.AuthConfig.BearerToken)
	if bearer == "" {
		return "", fmt.Errorf("agentvault: %q connection has no bearer token", conn.Name)
	}
	base := strings.TrimRight(strings.TrimSpace(conn.URL), "/")
	params := url.Values{"vault": {vault}, "reveal": {"true"}, "key": {key}}
	var out struct {
		Credentials []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"credentials"`
	}
	if err := c.getSensitive(ctx, base+"/v1/credentials?"+params.Encode(), bearer, &out); err != nil {
		return "", fmt.Errorf("agentvault reveal credential: %w", err)
	}
	if len(out.Credentials) != 1 || out.Credentials[0].Key != key {
		return "", fmt.Errorf("agentvault: credential not found")
	}
	return out.Credentials[0].Value, nil
}

// getSensitive is intentionally separate from get: an upstream error body on
// a credential endpoint may itself contain sensitive material and must never
// be propagated into server logs or an agent-facing error.
func (c *Client) getSensitive(ctx context.Context, fullURL, bearer string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode response")
	}
	return nil
}

// resolveConnection finds the enabled "Agent Vault" connection for the workspace:
// the REST API connection whose URL points at the configured internal endpoint.
// The internal host is a fixed deployment fact (the connection wouldn't function
// with any other URL), so matching on it is credential-free and does not depend
// on the connection's display name or slug. Returns ok=false when none is
// configured (degrade to an empty vault list); a nil resolver behaves the same.
func (c *Client) resolveConnection(ctx context.Context, workspaceID pgtype.UUID) (connections.Connection, bool, error) {
	if c == nil || c.conns == nil {
		return connections.Connection{}, false, nil
	}
	list, err := c.conns.ListEnabled(ctx, workspaceID)
	if err != nil {
		return connections.Connection{}, false, fmt.Errorf("agentvault: list connections: %w", err)
	}
	want := hostOf(c.cfg.InternalURL)
	for _, conn := range list {
		if conn.Type == connections.TypeAPI && want != "" && hostOf(conn.URL) == want {
			return conn, true, nil
		}
	}
	return connections.Connection{}, false, nil
}

// hostOf returns the host:port of a URL, or "" when it does not parse. Used to
// match a connection's URL against the Agent Vault internal endpoint regardless
// of trailing slash or path. A scheme-less value (e.g. "agent-vault.internal:14321"
// from a mis-set override) is treated as a host:port so a missing scheme does not
// silently blank the match on one side only.
func hostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// get performs one authenticated JSON GET against the internal Agent Vault API.
func (c *Client) get(ctx context.Context, fullURL, bearer string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
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
