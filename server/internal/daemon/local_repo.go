package daemon

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// normalizeRepoURL canonicalizes daemon-local file repositories and validates
// them at the machine boundary. Network URLs pass through unchanged.
func normalizeRepoURL(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "file") {
		return raw, "", nil
	}
	if u.Host != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", fmt.Errorf("local repository file URL must not contain a host, credentials, query, or fragment")
	}
	if u.Path == "" || !filepath.IsAbs(u.Path) {
		return "", "", fmt.Errorf("local repository file URL must contain an absolute path")
	}
	sourcePath, err := filepath.EvalSymlinks(filepath.Clean(u.Path))
	if err != nil {
		return "", "", fmt.Errorf("resolve local repository path: %w", err)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", "", fmt.Errorf("stat local repository path: %w", err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("local repository path is not a directory: %s", sourcePath)
	}

	commonOut, err := exec.Command("git", "-C", sourcePath, "rev-parse", "--path-format=absolute", "--git-common-dir").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("local repository path is not a readable Git repository: %s", strings.TrimSpace(string(commonOut)))
	}
	commonPath := strings.TrimSpace(string(commonOut))
	if !filepath.IsAbs(commonPath) {
		commonPath = filepath.Join(sourcePath, commonPath)
	}
	commonPath, err = filepath.EvalSymlinks(filepath.Clean(commonPath))
	if err != nil {
		return "", "", fmt.Errorf("resolve local repository Git common directory: %w", err)
	}

	return (&url.URL{Scheme: "file", Path: sourcePath}).String(), commonPath, nil
}

func normalizeTaskRepos(repos []RepoData) ([]RepoData, error) {
	normalized := make([]RepoData, 0, len(repos))
	seenLocalRepos := make(map[string]struct{})
	for _, repo := range repos {
		canonicalURL, commonDir, err := normalizeRepoURL(repo.URL)
		if err != nil {
			return nil, err
		}
		if canonicalURL == "" {
			continue
		}
		if commonDir != "" {
			if _, exists := seenLocalRepos[commonDir]; exists {
				continue
			}
			seenLocalRepos[commonDir] = struct{}{}
		}
		repo.URL = canonicalURL
		normalized = append(normalized, repo)
	}
	return normalized, nil
}
