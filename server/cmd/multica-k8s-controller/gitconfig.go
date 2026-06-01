package main

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// gitconfigForTask returns the contents of a per-task `.gitconfig` file that
// rewrites every repo URL in `repos` to the local file:// path of its bare
// clone inside the mounted repo cache.
//
// The agent never sees this rewrite — when it runs `git clone <origin-url>`
// (or fetch / pull / push), git's url.<base>.insteadOf substitution kicks in
// before the protocol dial, turning the clone into a sub-second local
// `git clone --shared file://...`. The rewrite covers both http(s) and
// scp-style URLs, with and without the `.git` suffix, so any URL form an
// agent might construct from origin remotes hits the cache.
//
// Returns an empty string if the resulting config would have no rewrites
// (no repos, or every URL was malformed) so the caller can decide whether
// to skip creating the ConfigMap entirely.
func gitconfigForTask(workspaceID, mountPath string, repos []daemon.RepoData) string {
	if mountPath == "" {
		mountPath = "/repos"
	}
	cache := repocache.New("", nil)

	var blocks []string
	for _, r := range repos {
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		host, ownerRepo := splitGitURL(r.URL)
		if host == "" || ownerRepo == "" {
			continue
		}
		slug := cache.SlugFor(workspaceID, r.URL)
		base := fmt.Sprintf("file://%s/%s/%s", strings.TrimRight(mountPath, "/"), workspaceID, slug)

		var b strings.Builder
		fmt.Fprintf(&b, "[url \"%s\"]\n", base)
		fmt.Fprintf(&b, "\tinsteadOf = https://%s/%s\n", host, ownerRepo)
		fmt.Fprintf(&b, "\tinsteadOf = https://%s/%s.git\n", host, ownerRepo)
		fmt.Fprintf(&b, "\tinsteadOf = git@%s:%s\n", host, ownerRepo)
		fmt.Fprintf(&b, "\tinsteadOf = git@%s:%s.git\n", host, ownerRepo)
		blocks = append(blocks, b.String())
	}
	return strings.Join(blocks, "\n")
}

// splitGitURL extracts (host, "owner/repo") from one of:
//
//	https://github.com/owner/repo[.git]
//	http://...           (rare, supported)
//	ssh://git@host[:port]/owner/repo[.git]
//	git@host:owner/repo[.git]              (scp-style)
//
// Returns ("", "") if the URL isn't one of these forms.
func splitGitURL(rawURL string) (host, ownerRepo string) {
	s := strings.TrimRight(rawURL, "/")

	// Strip trailing .git on the path portion later, after the host is known.
	switch {
	case strings.HasPrefix(s, "https://"):
		host, ownerRepo = parseSchemeURL(strings.TrimPrefix(s, "https://"))
	case strings.HasPrefix(s, "http://"):
		host, ownerRepo = parseSchemeURL(strings.TrimPrefix(s, "http://"))
	case strings.HasPrefix(s, "ssh://"):
		// ssh://[user@]host[:port]/path
		rest := strings.TrimPrefix(s, "ssh://")
		if i := strings.Index(rest, "@"); i >= 0 {
			rest = rest[i+1:]
		}
		host, ownerRepo = parseSchemeURL(rest)
	default:
		// Try scp-style: [user@]host:path
		rest := s
		if i := strings.Index(rest, "@"); i >= 0 {
			rest = rest[i+1:]
		}
		i := strings.Index(rest, ":")
		if i <= 0 {
			return "", ""
		}
		host = rest[:i]
		ownerRepo = rest[i+1:]
	}

	host = strings.TrimSpace(host)
	ownerRepo = strings.TrimSuffix(strings.Trim(ownerRepo, "/"), ".git")
	return host, ownerRepo
}

// parseSchemeURL splits "host[:port]/path" into host and path (without leading slash).
func parseSchemeURL(s string) (host, path string) {
	i := strings.Index(s, "/")
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i+1:]
}
