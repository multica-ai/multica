package infisical

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultSiteURL = "https://app.infisical.com"

type Client struct {
	SiteURL   string
	Token     string
	ProjectID string
	HTTP      *http.Client
}

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

// ListFolderSecrets returns every secret directly under the given folder.
// Secrets are returned under their own Infisical key names so the caller can
// inject them as environment variables without any per-secret mapping.
func (c Client) ListFolderSecrets(ctx context.Context, ref FolderRef) ([]Secret, error) {
	environment := strings.TrimSpace(ref.Environment)
	if environment == "" {
		return nil, fmt.Errorf("infisical: environment is required")
	}
	secretPath := strings.TrimSpace(ref.SecretPath)
	if secretPath == "" {
		secretPath = "/"
	}
	projectID := strings.TrimSpace(c.ProjectID)
	if projectID == "" {
		return nil, fmt.Errorf("infisical: INFISICAL_PROJECT_ID not configured")
	}
	token := strings.TrimSpace(c.Token)
	if token == "" {
		return nil, fmt.Errorf("infisical: INFISICAL_TOKEN not configured")
	}
	siteURL := strings.TrimRight(strings.TrimSpace(c.SiteURL), "/")
	if siteURL == "" {
		siteURL = DefaultSiteURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, siteURL+"/api/v3/secrets/raw", nil)
	if err != nil {
		return nil, fmt.Errorf("infisical: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	q := req.URL.Query()
	q.Set("workspaceId", projectID)
	q.Set("environment", environment)
	q.Set("secretPath", secretPath)
	req.URL.RawQuery = q.Encode()

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("infisical: request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("infisical: read response: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("infisical: unauthorized, check INFISICAL_TOKEN")
	case http.StatusForbidden:
		return nil, fmt.Errorf("infisical: access denied to folder %q in %q", secretPath, environment)
	case http.StatusNotFound:
		return nil, fmt.Errorf("infisical: folder %q not found in environment %q", secretPath, environment)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("infisical: HTTP %d: %s", resp.StatusCode, truncate(string(body), 256))
	}

	var result struct {
		Secrets []struct {
			SecretKey   string `json:"secretKey"`
			SecretValue string `json:"secretValue"`
		} `json:"secrets"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("infisical: decode response: %w", err)
	}
	out := make([]Secret, 0, len(result.Secrets))
	for _, s := range result.Secrets {
		key := strings.TrimSpace(s.SecretKey)
		if key == "" {
			continue
		}
		out = append(out, Secret{Key: key, Value: s.SecretValue})
	}
	return out, nil
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
