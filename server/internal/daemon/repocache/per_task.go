package repocache

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// PUL-94 helpers for per-task git worktree spawning.
//
// The daemon's per-task worktrees attach to globally-provisioned bare repos
// (e.g. /srv/pulse-bare.git, /srv/multica-bare.git) — not the workspace-scoped
// cache. This file holds the small pure functions the spawn handler needs to:
//
//   1. parse a project_resource.resource_ref JSONB blob into github "owner/name"
//   2. resolve that owner/name to a known bare repo path via a static map
//
// Both functions are pure and have no Cache state. The daemon passes in the
// map (loaded from config at startup) so this package stays deployment-agnostic.
//
// See: plans://Multica/2026-05-12-pul-94-agent-worktree-per-task.md (A1).

// GithubRepoRef is the parsed "owner/name" form extracted from a
// project_resource row's resource_ref JSONB. resource_ref shape for
// resource_type='github_repo' is {"url":"https://github.com/owner/name(.git)?"}
// or the SSH form. Other fields may be present and are ignored.
type GithubRepoRef struct {
	Owner string // e.g. "rabbeet"
	Name  string // e.g. "multica" (no .git suffix)
}

// String returns "owner/name", suitable for keying into a bare repo map.
func (r GithubRepoRef) String() string {
	return r.Owner + "/" + r.Name
}

// ParseGithubRepoRef decodes a project_resource.resource_ref JSON blob with a
// shape of {"url":"..."} and extracts the github owner+name. Supports HTTPS
// (https://github.com/owner/name(.git)?) and SSH (git@github.com:owner/name)
// forms; rejects non-github hosts and anything that can't be parsed into a
// clean two-segment path.
//
// Returns an error if the JSON is malformed, the url field is missing/empty,
// the host isn't github.com (case-insensitive), or the path doesn't have
// exactly owner+name.
func ParseGithubRepoRef(resourceRef json.RawMessage) (GithubRepoRef, error) {
	if len(resourceRef) == 0 {
		return GithubRepoRef{}, fmt.Errorf("empty resource_ref")
	}

	var payload struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(resourceRef, &payload); err != nil {
		return GithubRepoRef{}, fmt.Errorf("unmarshal resource_ref: %w", err)
	}
	raw := strings.TrimSpace(payload.URL)
	if raw == "" {
		return GithubRepoRef{}, fmt.Errorf("resource_ref has empty url")
	}

	owner, name, err := splitGithubURL(raw)
	if err != nil {
		return GithubRepoRef{}, err
	}
	return GithubRepoRef{Owner: owner, Name: name}, nil
}

// splitGithubURL accepts the URL forms we see in the wild for github_repo
// resources and returns (owner, name) stripped of any .git suffix.
//
//	https://github.com/owner/name           → owner, name
//	https://github.com/owner/name.git       → owner, name
//	https://github.com/owner/name/          → owner, name
//	git@github.com:owner/name.git           → owner, name
//	git@github.com:owner/name               → owner, name
//	ssh://git@github.com/owner/name.git     → owner, name
func splitGithubURL(raw string) (string, string, error) {
	// SSH "git@host:path" form — url.Parse can't handle it directly.
	if strings.HasPrefix(raw, "git@") {
		idx := strings.Index(raw, ":")
		if idx < 0 {
			return "", "", fmt.Errorf("malformed ssh github url: %s", raw)
		}
		host := raw[len("git@"):idx]
		if !strings.EqualFold(host, "github.com") {
			return "", "", fmt.Errorf("non-github host: %s", host)
		}
		return splitOwnerName(raw[idx+1:])
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("parse github url: %w", err)
	}
	if !strings.EqualFold(u.Host, "github.com") {
		return "", "", fmt.Errorf("non-github host: %s", u.Host)
	}
	return splitOwnerName(u.Path)
}

func splitOwnerName(p string) (string, string, error) {
	clean := strings.Trim(p, "/")
	clean = strings.TrimSuffix(clean, ".git")
	parts := strings.Split(clean, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("github url path must be owner/name: %q", p)
	}
	return parts[0], parts[1], nil
}

// ResolveBareFromGithubRef looks up the bare repo path for the given github
// owner/name in the supplied map and returns it. The map is case-insensitive
// on the "owner/name" key — callers can supply mixed-case keys in config
// without worrying about how github canonicalizes them.
//
// Returns (path, true) on hit, ("", false) on miss. The caller is responsible
// for surfacing the daemon.ErrBareMissing error when miss.
//
// Expected caller (PR-3): daemon config carries
//
//	BareRepoMap = {
//	    "rabbeet/Pulse":         "/srv/pulse-bare.git",
//	    "rabbeet/multica":       "/srv/multica-bare.git",
//	    "rabbeet/agent-context": "/srv/agent-context-bare.git",
//	}
//
// loaded from env or config file at daemon startup.
func ResolveBareFromGithubRef(bareRepoMap map[string]string, ref GithubRepoRef) (string, bool) {
	if len(bareRepoMap) == 0 {
		return "", false
	}
	// First try exact match (the common case).
	if p, ok := bareRepoMap[ref.String()]; ok {
		return p, true
	}
	// Fallback: case-insensitive scan. Github canonicalizes paths
	// case-insensitively, so "rabbeet/Pulse" and "rabbeet/pulse" should
	// resolve to the same bare. Keep the input map as written for ops
	// readability but allow case-insensitive lookups.
	want := strings.ToLower(ref.String())
	for k, v := range bareRepoMap {
		if strings.ToLower(k) == want {
			return v, true
		}
	}
	return "", false
}

// PerTaskWorktreePath returns the canonical filesystem location for a per-task
// worktree given the worktrees root, agent name, and the daemon's task ID
// short form. The path convention is <root>/<sanitized-agent>-<short-id>/.
//
// Used by both the daemon (when computing where to spawn) and the sweeper
// (when scanning for orphans). Pure function — no I/O.
func PerTaskWorktreePath(worktreesRoot, agentName, taskID string) string {
	return filepath.Join(worktreesRoot, sanitizeName(agentName)+"-"+shortID(taskID))
}
