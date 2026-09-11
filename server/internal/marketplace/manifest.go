package marketplace

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"
)

type SourceKind string

const (
	SourceRelative  SourceKind = "relative"
	SourceGitHub    SourceKind = "github"
	SourceGitURL    SourceKind = "git"
	SourceURL       SourceKind = "url"
	SourceGitSubdir SourceKind = "git-subdir"
	SourceNPM       SourceKind = "npm"
)

type Marketplace struct {
	Name        string   `json:"name"`
	Owner       Owner    `json:"owner"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version,omitempty"`
	Plugins     []Plugin `json:"plugins"`
}
type Owner struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}
type Plugin struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version,omitempty"`
	Source      Source   `json:"source"`
	Strict      *bool    `json:"strict,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}
type Source struct {
	Kind       SourceKind
	Repository string
	URL        string
	Ref        string
	SHA        string
	Subdir     string
	Package    string
	Version    string
}

func (s *Source) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("source: invalid JSON: %w", err)
	}
	switch value := raw.(type) {
	case string:
		if value == "" {
			return fmt.Errorf("source: empty")
		}
		if strings.HasPrefix(value, "./") {
			s.Kind, s.URL = SourceRelative, path.Clean(value)
			return nil
		}
		return fmt.Errorf("source: unsupported string %q", value)
	case map[string]any:
		kind, _ := value["source"].(string)
		switch SourceKind(kind) {
		case SourceGitHub:
			repo, ok := value["repo"].(string)
			if !ok || !validRepo(repo) {
				return fmt.Errorf("source: invalid github repo")
			}
			s.Kind, s.Repository = SourceGitHub, repo
		case SourceGitURL, SourceURL:
			rawURL, ok := value["url"].(string)
			if !ok || !safeURL(rawURL) {
				return fmt.Errorf("source: invalid URL")
			}
			s.Kind, s.URL = SourceKind(kind), rawURL
		case SourceGitSubdir:
			rawURL, ok := value["url"].(string)
			if !ok || !safeURL(rawURL) {
				return fmt.Errorf("source: invalid git-subdir URL")
			}
			s.Kind, s.URL = SourceGitSubdir, rawURL
			s.Subdir, _ = value["path"].(string)
			if s.Subdir == "" || path.IsAbs(s.Subdir) || strings.HasPrefix(path.Clean(s.Subdir), "..") {
				return fmt.Errorf("source: invalid subdirectory")
			}
		case SourceNPM:
			pkg, ok := value["package"].(string)
			if !ok || pkg == "" {
				return fmt.Errorf("source: invalid npm package")
			}
			s.Kind, s.Package = SourceNPM, pkg
			s.Version, _ = value["version"].(string)
		default:
			return fmt.Errorf("source: unsupported kind %q", kind)
		}
		s.Ref, _ = value["ref"].(string)
		s.SHA, _ = value["sha"].(string)
		return nil
	default:
		return fmt.Errorf("source: expected string or object")
	}
}

func ParseMarketplace(data []byte) (Marketplace, error) {
	var manifest Marketplace
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Marketplace{}, fmt.Errorf("marketplace.json: %w", err)
	}
	if manifest.Name == "" || manifest.Owner.Name == "" {
		return Marketplace{}, fmt.Errorf("marketplace.json: name and owner.name are required")
	}
	if reservedMarketplaceNames[manifest.Name] {
		return Marketplace{}, fmt.Errorf("marketplace.json: reserved marketplace name %q", manifest.Name)
	}
	if len(manifest.Plugins) == 0 {
		return Marketplace{}, fmt.Errorf("marketplace.json: plugins must not be empty")
	}
	seen := map[string]bool{}
	for i, plugin := range manifest.Plugins {
		if plugin.Name == "" {
			return Marketplace{}, fmt.Errorf("plugins[%d].name is required", i)
		}
		if seen[plugin.Name] {
			return Marketplace{}, fmt.Errorf("plugins[%d]: duplicate name %q", i, plugin.Name)
		}
		seen[plugin.Name] = true
	}
	return manifest, nil
}

var reservedMarketplaceNames = map[string]bool{"claude-code-marketplace": true, "claude-code-plugins": true, "claude-plugins-official": true, "anthropic-marketplace": true, "anthropic-plugins": true, "agent-skills": true, "knowledge-work-plugins": true, "life-sciences": true}

func validRepo(repo string) bool {
	parts := strings.Split(repo, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.ContainsAny(repo, "\\ \t\r\n")
}
func safeURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}
