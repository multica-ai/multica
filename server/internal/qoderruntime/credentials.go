package qoderruntime

import (
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

// sessionResources creates an ephemeral request copy; never put secrets in the journal.
func (b *Bridge) sessionResources(resources []map[string]any) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(resources))
	for _, resource := range resources {
		raw, _ := resource["url"].(string)
		key, err := repositoryKey(raw)
		if err != nil {
			return nil, err
		}
		token := b.cfg.GitHubTokens[key]
		path := ""
		for configured, file := range b.cfg.GitHubTokenFiles {
			candidate, err := repositoryKey(configured)
			if err != nil {
				return nil, fmt.Errorf("invalid GitHub credential repository binding")
			}
			if candidate == key {
				if path != "" {
					return nil, fmt.Errorf("duplicate GitHub credential repository binding")
				}
				path = file
			}
		}
		if path == "" && token == "" {
			return nil, fmt.Errorf("no GitHub credential configured for project repository")
		}
		if token == "" {
			token, err = readRepositoryToken(path)
			if err != nil {
				return nil, err
			}
		}
		copy := make(map[string]any, len(resource)+1)
		for k, v := range resource {
			copy[k] = v
		}
		copy["authorization_token"] = token
		out = append(out, copy)
	}
	return out, nil
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
