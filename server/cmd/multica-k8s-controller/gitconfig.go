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
// Two rewrite layers are emitted per repo:
//
//   - `insteadOf` (fetch path): every URL form an agent might use
//     (https/scp-style × with/without `.git`) rewrites to
//     `file:///<mountPath>/<workspaceID>/<slug>.git`. This makes `git clone`,
//     `git fetch`, and `git pull` sub-second local operations.
//
//   - `pushInsteadOf` (push path): the same URL forms rewrite to
//     `git@<host>:<owner>/<repo>.git` so pushes go to the origin via SSH.
//     Without this, pushes would route through `insteadOf` to the
//     read-only PVC mount and fail with "could not write to ref". The
//     runtime image bakes in github.com host keys and the worker pod
//     mounts the multica-git-ssh deploy key, so SSH push succeeds.
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
		sshPushBase := fmt.Sprintf("git@%s:%s.git", host, ownerRepo)

		var b strings.Builder
		fmt.Fprintf(&b, "[url \"%s\"]\n", base)
		fmt.Fprintf(&b, "\tinsteadOf = https://%s/%s\n", host, ownerRepo)
		fmt.Fprintf(&b, "\tinsteadOf = https://%s/%s.git\n", host, ownerRepo)
		fmt.Fprintf(&b, "\tinsteadOf = git@%s:%s\n", host, ownerRepo)
		fmt.Fprintf(&b, "\tinsteadOf = git@%s:%s.git\n", host, ownerRepo)
		fmt.Fprintf(&b, "[url \"%s\"]\n", sshPushBase)
		fmt.Fprintf(&b, "\tpushInsteadOf = https://%s/%s\n", host, ownerRepo)
		fmt.Fprintf(&b, "\tpushInsteadOf = https://%s/%s.git\n", host, ownerRepo)
		fmt.Fprintf(&b, "\tpushInsteadOf = git@%s:%s\n", host, ownerRepo)
		// We don't push-rewrite the cache file:// URL itself because git
		// applies pushInsteadOf to the ORIGINAL (pre-insteadOf) remote URL.
		// The agent's clone records the original URL, so push lookups
		// always start from one of the four entries above.
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
