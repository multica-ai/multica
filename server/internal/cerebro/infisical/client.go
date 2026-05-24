package infisical

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

type SecretRef struct {
	SecretName  string
	Environment string
	SecretPath  string
}

func (c Client) FetchRawSecret(ctx context.Context, ref SecretRef) (string, error) {
	secretName := strings.TrimSpace(ref.SecretName)
	if secretName == "" {
		return "", fmt.Errorf("infisical: secret_name is required")
	}
	environment := strings.TrimSpace(ref.Environment)
	if environment == "" {
		return "", fmt.Errorf("infisical: environment is required")
	}
	secretPath := strings.TrimSpace(ref.SecretPath)
	if secretPath == "" {
		secretPath = "/"
	}
	projectID := strings.TrimSpace(c.ProjectID)
	if projectID == "" {
		return "", fmt.Errorf("infisical: INFISICAL_PROJECT_ID not configured")
	}
	token := strings.TrimSpace(c.Token)
	if token == "" {
		return "", fmt.Errorf("infisical: INFISICAL_TOKEN not configured")
	}
	siteURL := strings.TrimRight(strings.TrimSpace(c.SiteURL), "/")
	if siteURL == "" {
		siteURL = DefaultSiteURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, siteURL+"/api/v3/secrets/raw/"+url.PathEscape(secretName), nil)
	if err != nil {
		return "", fmt.Errorf("infisical: build request: %w", err)
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
		return "", fmt.Errorf("infisical: request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("infisical: read response: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return "", fmt.Errorf("infisical: unauthorized, check INFISICAL_TOKEN")
	case http.StatusForbidden:
		return "", fmt.Errorf("infisical: access denied to secret %q in %q", secretName, environment)
	case http.StatusNotFound:
		return "", fmt.Errorf("infisical: secret %q not found in environment %q path %q", secretName, environment, secretPath)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("infisical: HTTP %d: %s", resp.StatusCode, truncate(string(body), 256))
	}

	var result struct {
		Secret struct {
			SecretValue string `json:"secretValue"`
		} `json:"secret"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return strings.TrimSpace(string(body)), nil
	}
	return result.Secret.SecretValue, nil
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
