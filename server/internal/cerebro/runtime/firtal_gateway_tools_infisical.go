package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// FirtalGetSecretTool implements Tool for fetching a secret directly from
// Infisical. Requires INFISICAL_TOKEN and INFISICAL_PROJECT_ID env vars.
// INFISICAL_SITE_URL is optional (defaults to https://app.infisical.com).
type FirtalGetSecretTool struct{}

func (t *FirtalGetSecretTool) Name() string { return "infisical_get_secret" }
func (t *FirtalGetSecretTool) Description() string {
	return "Fetch a secret from Infisical. secret_name is the key (e.g. DATABASE_URL), environment is the Infisical environment slug (e.g. production)."
}
func (t *FirtalGetSecretTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"secret_name", "environment"},
		"properties": map[string]any{
			"secret_name": map[string]any{
				"type":        "string",
				"description": "The secret key name, e.g. DATABASE_URL",
			},
			"environment": map[string]any{
				"type":        "string",
				"description": "Infisical environment slug, e.g. production or development",
			},
			"secret_path": map[string]any{
				"type":        "string",
				"description": "Folder path within the environment, e.g. /backend. Defaults to /.",
			},
			"project_id": map[string]any{
				"type":        "string",
				"description": "Infisical project ID. Overrides INFISICAL_PROJECT_ID env var.",
			},
		},
	}
}

func (t *FirtalGetSecretTool) Call(ctx context.Context, args map[string]any) (string, error) {
	secretName, _ := args["secret_name"].(string)
	secretName = strings.TrimSpace(secretName)
	if secretName == "" {
		return "", fmt.Errorf("infisical_get_secret: secret_name is required")
	}

	environment, _ := args["environment"].(string)
	environment = strings.TrimSpace(environment)
	if environment == "" {
		return "", fmt.Errorf("infisical_get_secret: environment is required")
	}

	secretPath := "/"
	if sp, ok := args["secret_path"].(string); ok && strings.TrimSpace(sp) != "" {
		secretPath = strings.TrimSpace(sp)
	}

	projectID, _ := args["project_id"].(string)
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		projectID = strings.TrimSpace(os.Getenv("INFISICAL_PROJECT_ID"))
	}
	if projectID == "" {
		return "", fmt.Errorf("infisical_get_secret: INFISICAL_PROJECT_ID not configured")
	}

	token := strings.TrimSpace(os.Getenv("INFISICAL_TOKEN"))
	if token == "" {
		return "", fmt.Errorf("infisical_get_secret: INFISICAL_TOKEN not configured")
	}

	siteURL := strings.TrimRight(strings.TrimSpace(os.Getenv("INFISICAL_SITE_URL")), "/")
	if siteURL == "" {
		siteURL = "https://app.infisical.com"
	}

	endpoint := siteURL + "/api/v3/secrets/raw/" + url.PathEscape(secretName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("infisical_get_secret: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	q := req.URL.Query()
	q.Set("workspaceId", projectID)
	q.Set("environment", environment)
	q.Set("secretPath", secretPath)
	req.URL.RawQuery = q.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("infisical_get_secret: request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("infisical_get_secret: read response: %w", err)
	}

	switch resp.StatusCode {
	case 401:
		return "", fmt.Errorf("infisical_get_secret: unauthorized — check INFISICAL_TOKEN")
	case 403:
		return "", fmt.Errorf("infisical_get_secret: access denied to secret %q in %q", secretName, environment)
	case 404:
		return "", fmt.Errorf("infisical_get_secret: secret %q not found in environment %q path %q", secretName, environment, secretPath)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("infisical_get_secret: HTTP %d: %s", resp.StatusCode, truncateGatewayError(string(body), 256))
	}

	// Infisical returns {"secret": {"secretValue": "...", ...}}
	var result struct {
		Secret struct {
			SecretValue string `json:"secretValue"`
		} `json:"secret"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return strings.TrimSpace(string(body)), nil
	}
	out, err := json.Marshal(map[string]any{"value": result.Secret.SecretValue})
	if err != nil {
		return "", fmt.Errorf("infisical_get_secret: marshal result: %w", err)
	}
	return string(out), nil
}
