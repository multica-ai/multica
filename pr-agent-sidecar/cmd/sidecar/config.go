package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	GitHubAppID          int64
	GitHubAppPrivateKey  []byte
	GitHubWebhookSecret  string
	MulticaPAT           string
	MulticaBaseURL       string
	MulticaWorkspaceID   string
	PRReviewerAgentID    string
	RepoAllowlist        map[string]struct{}
	SidecarPublicURL     string
	Port                 string
}

func LoadConfig() (*Config, error) {
	c := &Config{
		Port: getenvDefault("PORT", "9000"),
	}

	var err error
	c.GitHubAppID, err = requireInt64("GITHUB_APP_ID")
	if err != nil {
		return nil, err
	}

	c.GitHubAppPrivateKey, err = loadPrivateKey(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	if err != nil {
		return nil, fmt.Errorf("GITHUB_APP_PRIVATE_KEY: %w", err)
	}

	c.GitHubWebhookSecret, err = requireString("GITHUB_WEBHOOK_SECRET")
	if err != nil {
		return nil, err
	}
	c.MulticaPAT, err = requireString("MULTICA_PAT")
	if err != nil {
		return nil, err
	}
	c.MulticaBaseURL, err = requireString("MULTICA_BASE_URL")
	if err != nil {
		return nil, err
	}
	c.MulticaBaseURL = strings.TrimRight(c.MulticaBaseURL, "/")

	c.MulticaWorkspaceID, err = requireString("MULTICA_WORKSPACE_ID")
	if err != nil {
		return nil, err
	}
	c.PRReviewerAgentID, err = requireString("PR_REVIEWER_AGENT_ID")
	if err != nil {
		return nil, err
	}
	c.SidecarPublicURL, err = requireString("SIDECAR_PUBLIC_URL")
	if err != nil {
		return nil, err
	}
	c.SidecarPublicURL = strings.TrimRight(c.SidecarPublicURL, "/")

	// REPO_ALLOWLIST is optional. Empty/unset = accept webhooks from any repo
	// where the GitHub App is installed. Set a comma-separated list of
	// owner/repo to restrict.
	c.RepoAllowlist = parseAllowlist(os.Getenv("REPO_ALLOWLIST"))

	return c, nil
}

// RepoAllowed returns true if fullName is in the allowlist, or if the
// allowlist is empty (allow-all mode).
func (c *Config) RepoAllowed(fullName string) bool {
	if len(c.RepoAllowlist) == 0 {
		return true
	}
	_, ok := c.RepoAllowlist[fullName]
	return ok
}

func requireString(key string) (string, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return "", fmt.Errorf("%s: required but unset", key)
	}
	return v, nil
}

func requireInt64(key string) (int64, error) {
	s, err := requireString(key)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: not an integer (%q)", key, s)
	}
	return n, nil
}

func getenvDefault(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

// loadPrivateKey accepts either a PEM string (starts with "-----BEGIN") or a
// filesystem path. Returns the raw PEM bytes.
func loadPrivateKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("required but unset")
	}
	if strings.HasPrefix(raw, "-----BEGIN") {
		return []byte(raw), nil
	}
	b, err := os.ReadFile(raw)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", raw, err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(b)), "-----BEGIN") {
		return nil, fmt.Errorf("file %q does not look like a PEM private key", raw)
	}
	return b, nil
}

func parseAllowlist(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out[item] = struct{}{}
	}
	return out
}
