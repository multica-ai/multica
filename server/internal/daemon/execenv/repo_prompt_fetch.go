// CEREBRO-PATCH(prompt-inspector): load repository CLAUDE.md / AGENTS.md for inspector and measurements (FIR-2765).
package execenv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxRepoPromptBytes = 512 * 1024

var repoPromptHTTPClient = &http.Client{Timeout: 15 * time.Second}

// LoadRepoPromptSnapshot loads repository instruction files for prompt inspection and
// context_duplication measurements. When workDir is set, checked-out files win over
// GitHub (same files the daemon merges at task prepare).
func LoadRepoPromptSnapshot(ctx context.Context, repoURL, workDir string) *RepoPromptSnapshot {
	if workDir != "" {
		if snap := loadRepoPromptFromWorkdir(workDir); snap.CLAUDE.Found || snap.AGENTS.Found {
			return snap
		}
	}
	if strings.TrimSpace(repoURL) != "" {
		return fetchRepoPromptFromGitHub(ctx, repoURL)
	}
	return &RepoPromptSnapshot{}
}

func loadRepoPromptFromWorkdir(workDir string) *RepoPromptSnapshot {
	snap := &RepoPromptSnapshot{}
	for _, item := range []struct {
		name string
		dest *RepoPromptFile
	}{
		{"CLAUDE.md", &snap.CLAUDE},
		{"AGENTS.md", &snap.AGENTS},
	} {
		path := filepath.Join(workDir, item.name)
		body, err := os.ReadFile(path)
		if err != nil || len(body) == 0 {
			continue
		}
		if len(body) > maxRepoPromptBytes {
			body = body[:maxRepoPromptBytes]
		}
		item.dest.Content = string(body)
		item.dest.Found = true
		item.dest.SourceURL = "file://" + path
	}
	return snap
}

func fetchRepoPromptFromGitHub(ctx context.Context, repoURL string) *RepoPromptSnapshot {
	snap := &RepoPromptSnapshot{}
	owner, repo, ok := parseGitHubRepoURL(repoURL)
	if !ok {
		return snap
	}
	ref := fetchGitHubDefaultBranch(ctx, owner, repo)
	for _, item := range []struct {
		name string
		dest *RepoPromptFile
	}{
		{"CLAUDE.md", &snap.CLAUDE},
		{"AGENTS.md", &snap.AGENTS},
	} {
		content, found, err := fetchGitHubMarkdownFile(ctx, owner, repo, ref, item.name)
		item.dest.SourceURL = githubBlobURL(owner, repo, ref, item.name)
		if err != nil || !found {
			continue
		}
		item.dest.Content = content
		item.dest.Found = true
	}
	return snap
}

func parseGitHubRepoURL(raw string) (owner, repo string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), true
}

func fetchGitHubDefaultBranch(ctx context.Context, owner, repo string) string {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s",
		url.PathEscape(owner), url.PathEscape(repo))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "main"
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	addRepoPromptGitHubAuthHeader(req)
	resp, err := repoPromptHTTPClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return "main"
	}
	defer resp.Body.Close()
	var info struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil || info.DefaultBranch == "" {
		return "main"
	}
	return info.DefaultBranch
}

func fetchGitHubMarkdownFile(ctx context.Context, owner, repo, ref, path string) (content string, found bool, err error) {
	rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
		url.PathEscape(owner), url.PathEscape(repo), escapeRefPath(ref), path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", false, err
	}
	addRepoPromptGitHubAuthHeader(req)
	resp, err := repoPromptHTTPClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, path)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRepoPromptBytes+1))
	if err != nil {
		return "", false, err
	}
	if len(body) > maxRepoPromptBytes {
		return "", false, fmt.Errorf("file %s exceeds size limit", path)
	}
	return string(body), true, nil
}

func addRepoPromptGitHubAuthHeader(req *http.Request) {
	if req == nil {
		return
	}
	token := strings.TrimSpace(os.Getenv("CEREBRO_SKILLS_SYNC_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func githubBlobURL(owner, repo, ref, path string) string {
	return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s",
		url.PathEscape(owner), url.PathEscape(repo), escapeRefPath(ref), path)
}

func escapeRefPath(ref string) string {
	parts := strings.Split(ref, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}
