package infisical

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// AdminClient is the management surface for the scoped-per-user identity
// model. ONE admin machine identity (org-admin role) lives in the backend env;
// it provisions per-user machine identities whose Infisical-side privileges
// restrict reads to ONLY the folder paths an admin allow-listed for that user.
//
// Why this exists: with a single shared all-access token, a bug in our
// folder-filtering code can leak the whole project. With a scoped identity
// per user, Infisical itself denies any read outside the user's allow-list —
// the system fails closed.
//
// Concurrency: safe for concurrent use; the cached access token is guarded
// by a mutex and refreshed before expiry.
type AdminClient struct {
	Domain         string
	OrgID          string
	ProjectID      string
	ClientID       string
	ClientSecret   string
	HTTP           *http.Client
	IdentityPrefix string // optional, defaults to "multica"

	mu        sync.Mutex
	token     string
	tokenExp  time.Time
	tokenSafe time.Duration // skew before expiry to refresh; defaults to 30s
}

// UniversalAuthCreds is the (clientId, clientSecret) pair Multica stores for
// a user's scoped identity so it can log in later as that identity and fetch
// secrets. The plaintext ClientSecret leaves AdminClient only via this struct,
// and the caller is expected to encrypt before persistence.
type UniversalAuthCreds struct {
	IdentityID   string
	ClientID     string
	ClientSecret string
}

// ScopedPaths is the input to EnsureUserIdentity. Each entry expands into a
// read-only additional-privilege on the user's identity, restricting it to
// (environment, secretPath) — and only that path, no recursion across
// siblings.
type ScopedPaths struct {
	Folders []FolderRef
}

// Hash returns a deterministic fingerprint of the folder set so callers can
// detect "scope unchanged since last sync" and skip a no-op Infisical call.
// Order-independent.
func (s ScopedPaths) Hash() string {
	keys := make([]string, 0, len(s.Folders))
	for _, f := range s.Folders {
		keys = append(keys, strings.TrimSpace(f.Environment)+"\x00"+strings.TrimSpace(f.SecretPath))
	}
	sort.Strings(keys)
	h := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return hex.EncodeToString(h[:])
}

// EnsureUserIdentity creates or updates a per-user scoped identity.
//
// existingIdentityID may be empty (first-time provisioning) or the previously
// stored identity ID (re-scope path). On re-scope the existing identity is
// deleted and recreated — Infisical does not expose a "replace all
// privileges" atomic, and accumulating per-call privileges over time leaves
// stale grants behind.
//
// Returns the new credentials (always a fresh client secret the caller MUST
// encrypt and persist) along with the identity ID. The plaintext client
// secret is shown by Infisical exactly once, at create time.
func (c *AdminClient) EnsureUserIdentity(ctx context.Context, name string, existingIdentityID string, scope ScopedPaths) (UniversalAuthCreds, error) {
	if err := c.validateConfig(); err != nil {
		return UniversalAuthCreds{}, err
	}
	if strings.TrimSpace(name) == "" {
		return UniversalAuthCreds{}, fmt.Errorf("infisical admin: identity name is required")
	}

	if existingIdentityID != "" {
		// Idempotent delete: if the row in our DB points at an identity that
		// was already removed in Infisical (manual cleanup, test reset), don't
		// fail the whole sync — just continue and recreate.
		if err := c.DeleteIdentity(ctx, existingIdentityID); err != nil && !isInfisicalNotFound(err) {
			return UniversalAuthCreds{}, fmt.Errorf("delete stale identity %s: %w", existingIdentityID, err)
		}
	}

	identityID, err := c.createIdentity(ctx, name)
	if err != nil {
		return UniversalAuthCreds{}, fmt.Errorf("create identity: %w", err)
	}

	clientID, err := c.attachUniversalAuth(ctx, identityID)
	if err != nil {
		// Best-effort rollback so we don't leave an orphan identity in
		// Infisical with no auth attached.
		_ = c.DeleteIdentity(ctx, identityID)
		return UniversalAuthCreds{}, fmt.Errorf("attach universal-auth: %w", err)
	}

	clientSecret, err := c.createClientSecret(ctx, identityID)
	if err != nil {
		_ = c.DeleteIdentity(ctx, identityID)
		return UniversalAuthCreds{}, fmt.Errorf("create client secret: %w", err)
	}

	if err := c.addIdentityToProject(ctx, identityID); err != nil {
		_ = c.DeleteIdentity(ctx, identityID)
		return UniversalAuthCreds{}, fmt.Errorf("add identity to project: %w", err)
	}

	for i, folder := range scope.Folders {
		if err := c.addReadPrivilege(ctx, identityID, i, folder); err != nil {
			_ = c.DeleteIdentity(ctx, identityID)
			return UniversalAuthCreds{}, fmt.Errorf("grant read privilege %s%s: %w", folder.Environment, folder.SecretPath, err)
		}
	}

	return UniversalAuthCreds{
		IdentityID:   identityID,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}, nil
}

// DeleteIdentity removes a machine identity from the org. Used both for
// rollback inside EnsureUserIdentity and for the allow-list-cleared path
// where the user no longer has any folders granted.
func (c *AdminClient) DeleteIdentity(ctx context.Context, identityID string) error {
	if err := c.validateConfig(); err != nil {
		return err
	}
	if strings.TrimSpace(identityID) == "" {
		return fmt.Errorf("infisical admin: identity ID is required")
	}
	_, err := c.doRequest(ctx, http.MethodDelete, "/api/v1/identities/"+identityID, nil)
	return err
}

func (c *AdminClient) createIdentity(ctx context.Context, name string) (string, error) {
	body := map[string]any{
		"name":           name,
		"organizationId": c.OrgID,
		"role":           "no-access",
	}
	raw, err := c.doRequest(ctx, http.MethodPost, "/api/v1/identities", body)
	if err != nil {
		return "", err
	}
	var resp struct {
		Identity struct {
			ID string `json:"id"`
		} `json:"identity"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("decode identity response: %w", err)
	}
	if resp.Identity.ID == "" {
		return "", fmt.Errorf("infisical: create identity returned empty id")
	}
	return resp.Identity.ID, nil
}

func (c *AdminClient) attachUniversalAuth(ctx context.Context, identityID string) (string, error) {
	body := map[string]any{
		// 30-minute token TTL: long enough to fetch all of a user's folders in
		// one login burst, short enough that a leaked token is mostly stale.
		"accessTokenTTL":    1800,
		"accessTokenMaxTTL": 1800,
	}
	raw, err := c.doRequest(ctx, http.MethodPost, "/api/v1/auth/universal-auth/identities/"+identityID, body)
	if err != nil {
		return "", err
	}
	var resp struct {
		IdentityUniversalAuth struct {
			ClientID string `json:"clientId"`
		} `json:"identityUniversalAuth"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("decode attach response: %w", err)
	}
	if resp.IdentityUniversalAuth.ClientID == "" {
		return "", fmt.Errorf("infisical: attach universal-auth returned empty clientId")
	}
	return resp.IdentityUniversalAuth.ClientID, nil
}

func (c *AdminClient) createClientSecret(ctx context.Context, identityID string) (string, error) {
	body := map[string]any{
		"description":  "multica-scoped-user-identity",
		"numUsesLimit": 0, // unlimited
		"ttl":          0, // no expiration on the credential itself; the access token it mints is short-lived
	}
	raw, err := c.doRequest(ctx, http.MethodPost, "/api/v1/auth/universal-auth/identities/"+identityID+"/client-secrets", body)
	if err != nil {
		return "", err
	}
	var resp struct {
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("decode client-secret response: %w", err)
	}
	if resp.ClientSecret == "" {
		return "", fmt.Errorf("infisical: create client-secret returned empty value")
	}
	return resp.ClientSecret, nil
}

func (c *AdminClient) addIdentityToProject(ctx context.Context, identityID string) error {
	body := map[string]any{
		"role": "no-access",
	}
	_, err := c.doRequest(ctx, http.MethodPost, "/api/v1/projects/"+c.ProjectID+"/identity-memberships/"+identityID, body)
	return err
}

// addReadPrivilege restricts the identity to reading secrets only at one
// specific (environment, secretPath). One privilege per folder; the slug is
// the privilege index so re-grants are deterministic.
func (c *AdminClient) addReadPrivilege(ctx context.Context, identityID string, idx int, folder FolderRef) error {
	body := map[string]any{
		"identityId": identityID,
		"projectId":  c.ProjectID,
		"slug":       fmt.Sprintf("multica-folder-%d", idx),
		"permissions": []map[string]any{
			{
				"subject": "secrets",
				"action":  []string{"read"},
				"conditions": map[string]any{
					"environment": strings.TrimSpace(folder.Environment),
					"secretPath":  strings.TrimSpace(folder.SecretPath),
				},
			},
		},
	}
	_, err := c.doRequest(ctx, http.MethodPost, "/api/v2/identity-project-additional-privilege", body)
	return err
}

func (c *AdminClient) validateConfig() error {
	if strings.TrimSpace(c.Domain) == "" {
		return fmt.Errorf("infisical admin: domain is not configured")
	}
	if strings.TrimSpace(c.OrgID) == "" {
		return fmt.Errorf("infisical admin: organization id is not configured")
	}
	if strings.TrimSpace(c.ProjectID) == "" {
		return fmt.Errorf("infisical admin: project id is not configured")
	}
	if strings.TrimSpace(c.ClientID) == "" || strings.TrimSpace(c.ClientSecret) == "" {
		return fmt.Errorf("infisical admin: client credentials are not configured")
	}
	return nil
}

func (c *AdminClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *AdminClient) baseURL() string {
	return strings.TrimRight(strings.TrimSpace(c.Domain), "/")
}

// adminToken returns a cached admin access token, refreshing it via universal
// auth login when missing or near expiry.
func (c *AdminClient) adminToken(ctx context.Context) (string, error) {
	skew := c.tokenSafe
	if skew <= 0 {
		skew = 30 * time.Second
	}

	c.mu.Lock()
	if c.token != "" && time.Until(c.tokenExp) > skew {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	// Login outside the lock — concurrent callers will just login twice on a
	// cold cache, which is harmless.
	body := map[string]any{
		"clientId":     c.ClientID,
		"clientSecret": c.ClientSecret,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode login body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/api/v1/auth/universal-auth/login", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("infisical admin login: %w", err)
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("infisical admin login HTTP %d: %s", resp.StatusCode, truncate(string(rawBody), 256))
	}
	var out struct {
		AccessToken string `json:"accessToken"`
		ExpiresIn   int64  `json:"expiresIn"`
	}
	if err := json.Unmarshal(rawBody, &out); err != nil {
		return "", fmt.Errorf("decode login response: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("infisical admin login: empty access token")
	}

	c.mu.Lock()
	c.token = out.AccessToken
	if out.ExpiresIn > 0 {
		c.tokenExp = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	} else {
		c.tokenExp = time.Now().Add(30 * time.Minute)
	}
	c.mu.Unlock()
	return out.AccessToken, nil
}

// doRequest executes an authenticated admin request and returns the response
// body. body may be nil for DELETE / GET. Non-2xx responses are converted to
// errors carrying the HTTP status so callers can branch on 404 via
// isInfisicalNotFound.
func (c *AdminClient) doRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	token, err := c.adminToken(ctx)
	if err != nil {
		return nil, err
	}

	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL()+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("infisical admin %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &httpError{Status: resp.StatusCode, Body: truncate(string(rawBody), 512), Method: method, Path: path}
	}
	return rawBody, nil
}

type httpError struct {
	Status int
	Body   string
	Method string
	Path   string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("infisical %s %s: HTTP %d: %s", e.Method, e.Path, e.Status, e.Body)
}

func isInfisicalNotFound(err error) bool {
	if err == nil {
		return false
	}
	herr, ok := err.(*httpError)
	if !ok {
		return false
	}
	return herr.Status == http.StatusNotFound
}
