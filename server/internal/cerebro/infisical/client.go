package infisical

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

// FolderRef identifies an Infisical folder (environment + path) whose secrets
// should be injected into an agent's environment.
type FolderRef struct {
	Environment string
	SecretPath  string
}

// Secret is a single key/value pair returned for a folder.
type Secret struct {
	Key   string
	Value string
}

// UserClient logs in as a per-user scoped machine identity (provisioned by
// AdminClient) and reads the secrets the identity is allowed to see. The
// caller resolves the credentials from cerebro_user_infisical_identity and
// decrypts the client secret with the credentials cipher before constructing
// the client.
//
// Tokens minted here are short-lived (TTL set by the admin attach call,
// 30 min by default). A UserClient instance is intended to be short-lived too
// — fresh per claim — so token caching is intentionally not done.
type UserClient struct {
	Domain       string
	ProjectID    string
	ClientID     string
	ClientSecret string
	HTTP         *http.Client
}

// FetchFolderSecrets logs in as the per-user identity and pulls every secret
// directly under each requested folder. The list is returned as a flat map
// keyed by the Infisical secret key, so the caller can splice it directly
// into the agent process env.
//
// Empty folders contribute no entries. A folder the identity is not allowed
// to read returns a 403 from Infisical — that surfaces as an error to the
// caller, who logs and skips it (the agent still runs without that folder's
// vars rather than failing the entire claim).
func (c *UserClient) FetchFolderSecrets(ctx context.Context, folders []FolderRef) (map[string]string, error) {
	if len(folders) == 0 {
		return map[string]string{}, nil
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	token, err := c.login(ctx)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	out := make(map[string]string, len(folders)*4)
	for _, folder := range folders {
		secrets, err := c.listFolder(ctx, token, folder)
		if err != nil {
			return nil, fmt.Errorf("list %s%s: %w", folder.Environment, folder.SecretPath, err)
		}
		for _, s := range secrets {
			if s.Key == "" {
				continue
			}
			out[s.Key] = s.Value
		}
	}
	return out, nil
}

func (c *UserClient) validate() error {
	if strings.TrimSpace(c.Domain) == "" {
		return fmt.Errorf("infisical user client: domain is not configured")
	}
	if strings.TrimSpace(c.ProjectID) == "" {
		return fmt.Errorf("infisical user client: project id is not configured")
	}
	if strings.TrimSpace(c.ClientID) == "" || strings.TrimSpace(c.ClientSecret) == "" {
		return fmt.Errorf("infisical user client: credentials are not configured")
	}
	return nil
}

func (c *UserClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (c *UserClient) baseURL() string {
	return strings.TrimRight(strings.TrimSpace(c.Domain), "/")
}

func (c *UserClient) login(ctx context.Context) (string, error) {
	body, err := json.Marshal(map[string]any{
		"clientId":     c.ClientID,
		"clientSecret": c.ClientSecret,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/api/v1/auth/universal-auth/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 256))
	}
	var out struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("empty access token")
	}
	return out.AccessToken, nil
}

func (c *UserClient) listFolder(ctx context.Context, token string, folder FolderRef) ([]Secret, error) {
	environment := strings.TrimSpace(folder.Environment)
	if environment == "" {
		return nil, fmt.Errorf("environment is required")
	}
	secretPath := strings.TrimSpace(folder.SecretPath)
	if secretPath == "" {
		secretPath = "/"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+"/api/v3/secrets/raw", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	q := req.URL.Query()
	q.Set("workspaceId", c.ProjectID)
	q.Set("environment", environment)
	q.Set("secretPath", secretPath)
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("unauthorized (token rejected)")
	case http.StatusForbidden:
		return nil, fmt.Errorf("access denied to folder %q in %q (identity scope mismatch)", secretPath, environment)
	case http.StatusNotFound:
		return nil, fmt.Errorf("folder %q not found in environment %q", secretPath, environment)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 256))
	}
	var out struct {
		Secrets []struct {
			SecretKey   string `json:"secretKey"`
			SecretValue string `json:"secretValue"`
		} `json:"secrets"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	result := make([]Secret, 0, len(out.Secrets))
	for _, s := range out.Secrets {
		key := strings.TrimSpace(s.SecretKey)
		if key == "" {
			continue
		}
		result = append(result, Secret{Key: key, Value: s.SecretValue})
	}
	return result, nil
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
