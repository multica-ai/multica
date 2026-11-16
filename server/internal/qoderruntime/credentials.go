package qoderruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

// repositoryKey binds each credential to an exact GitHub repository.
func repositoryKey(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return "", fmt.Errorf("repository must be an HTTPS github.com URL without credentials")
	}
	path := strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), ".git")
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return "", fmt.Errorf("repository URL must contain an owner and repository")
	}
	return "https://github.com/" + strings.ToLower(strings.Join(parts, "/")), nil
}

// ErrGitHubAppNotCovered reports that the workspace's GitHub App integration
// cannot authorize a repository; callers fall back to other credentials or
// reject the run.
var ErrGitHubAppNotCovered = errors.New("GitHub App installation does not cover the repository")

// ErrGitHubAppPermissions reports that the workspace's GitHub App is
// installed without the repository write permissions automatic authorization
// needs. Unlike ErrGitHubAppNotCovered this is operator-actionable: approve
// the added permissions on GitHub or fall back to a manual repository token.
var ErrGitHubAppPermissions = errors.New("workspace GitHub App has not granted repository write permissions; approve Contents and Pull requests write access on GitHub, or add a manual repository token")

// sessionResources creates an ephemeral request copy; never put secrets in the
// journal. A repository resolves its credential in priority order: an explicit
// manual token binding first, then a short-lived single-repository token from
// the workspace's GitHub App integration. GitHub App tokens rotate on every
// request, so the fingerprint hashes the stable authorization identity
// (installation + repository + branch) instead of the token, letting
// follow-ups reuse an idle session while the pre-run re-resolution still
// blocks a revoked installation.
func (b *Bridge) sessionResources(ctx context.Context, resources []map[string]any) ([]map[string]any, string, bool, error) {
	out := make([]map[string]any, 0, len(resources))
	entries := make([]fingerprintEntry, 0, len(resources))
	rotating := false
	for _, resource := range resources {
		raw, _ := resource["url"].(string)
		key, err := repositoryKey(raw)
		if err != nil {
			return nil, "", false, err
		}
		token := b.cfg.GitHubTokens[key]
		path := ""
		for configured, file := range b.cfg.GitHubTokenFiles {
			candidate, err := repositoryKey(configured)
			if err != nil {
				return nil, "", false, fmt.Errorf("invalid GitHub credential repository binding")
			}
			if candidate == key {
				if path != "" {
					return nil, "", false, fmt.Errorf("duplicate GitHub credential repository binding")
				}
				path = file
			}
		}
		credential := token
		if credential == "" && path != "" {
			if credential, err = readRepositoryToken(path); err != nil {
				return nil, "", false, err
			}
		}
		identity := credential
		if credential == "" {
			if b.cfg.GitHubAppToken == nil {
				return nil, "", false, fmt.Errorf("no GitHub credential configured for project repository")
			}
			owner, repo, ok := repositoryParts(key)
			if !ok {
				return nil, "", false, fmt.Errorf("no GitHub credential configured for project repository")
			}
			issued, installation, err := b.cfg.GitHubAppToken(ctx, owner, repo)
			if errors.Is(err, ErrGitHubAppNotCovered) {
				return nil, "", false, fmt.Errorf("no GitHub credential configured for project repository")
			}
			if err != nil {
				return nil, "", false, err
			}
			if issued == "" || installation == 0 {
				return nil, "", false, fmt.Errorf("GitHub App token issuer returned no credential")
			}
			credential = issued
			identity = fmt.Sprintf("github_app:%d", installation)
			rotating = true
		}
		copy := make(map[string]any, len(resource)+1)
		for k, v := range resource {
			copy[k] = v
		}
		copy["authorization_token"] = credential
		out = append(out, copy)
		entries = append(entries, fingerprintEntry{key: key, branch: checkoutBranch(resource["checkout"]), credential: identity})
	}
	return out, resourceFingerprint(entries), rotating, nil
}

// repositoryParts splits a canonical repository key into owner and name.
func repositoryParts(key string) (owner, repo string, ok bool) {
	parts := strings.Split(strings.TrimPrefix(key, "https://github.com/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func readRepositoryToken(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cannot open GitHub credential file")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("GitHub credential file must be a private regular file (0600)")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(raw) > 16384 {
		return "", fmt.Errorf("cannot read GitHub credential file or file too large")
	}
	token := strings.TrimSpace(string(raw))
	if token == "" || strings.ContainsAny(token, "\r\n\t ") {
		return "", fmt.Errorf("GitHub credential file must contain one non-empty token")
	}
	return token, nil
}

// RepositoryKey validates and canonicalizes a repository credential binding.
func RepositoryKey(raw string) (string, error) { return repositoryKey(raw) }
