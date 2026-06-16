// Package forkdist holds the Firtal fork's self-update distribution
// coordinates.
//
// The fork (firtal-group/firtal-cerebro) ships its own CLI binaries and
// Homebrew tap, separate from upstream multica-ai. The daemon/CLI self-update
// path must therefore target the fork's release channel, not upstream — a
// daemon that upgraded from multica-ai would silently lose every cerebro
// patch (Cloudflare Access client, agent-trace upload, ...).
//
// Values are overridable via environment variables so an upstream build, a
// staging channel, or a test can repoint the updater without a code change.
package forkdist

import (
	"net"
	"net/url"
	"os"
	"strings"
)

const (
	// defaultRepo is the "owner/name" GitHub repo whose Releases carry the
	// fork's version metadata + binary assets (checksums.txt + per-os/arch
	// archives). It MUST be a PUBLIC repo: the daemon downloads release assets
	// unauthenticated (cli.fetchURLBytes does a plain GET, and the daemon holds
	// no GitHub token), so a private repo's assets would 404. The code repo
	// firtal-group/firtal-cerebro is private; firtal-group/homebrew-tap is
	// public and is where the release binaries + checksums.txt are published.
	defaultRepo = "firtal-group/homebrew-tap"
	// defaultBrewTap is the tap reference passed to `brew upgrade`. The tap
	// repo is firtal-group/homebrew-tap; Homebrew addresses it as
	// "<owner>/<tap>/<formula>".
	defaultBrewTap = "firtal-group/tap/multica"
)

// UpdateRepo is the GitHub "owner/name" the self-updater queries for release
// metadata and binary assets. Override with MULTICA_UPDATE_REPO.
func UpdateRepo() string { return envOr("MULTICA_UPDATE_REPO", defaultRepo) }

// BrewTap is the Homebrew tap reference handed to `brew upgrade`. Override
// with MULTICA_BREW_TAP.
func BrewTap() string { return envOr("MULTICA_BREW_TAP", defaultBrewTap) }

// AutoUpdateDefaultOn reports the fork's default for the daemon auto-update
// poller, given the server base URL the daemon is pointed at.
//
// Upstream defaults self-hosted daemons to OFF (MUL-2381): a self-host operator
// often runs a fork, and auto-upgrading to an UPSTREAM GitHub release would
// silently clobber it. That footgun is inverted for us — forkdist points the
// updater at the fork's OWN channel (firtal-group/homebrew-tap), whose binaries
// already carry every cerebro patch, so a self-update keeps (not loses) our
// work. We therefore default auto-update ON for any real server the fleet
// connects to, while leaving LOCAL dev daemons (localhost / 127.0.0.1 / ::1)
// OFF so a developer's machine is never surprised by a self-update. A tagged
// release is still required for the poller to act (the loop skips source
// builds), and any machine can opt out with MULTICA_DAEMON_AUTO_UPDATE=false.
func AutoUpdateDefaultOn(serverBaseURL string) bool {
	u, err := url.Parse(strings.TrimSpace(serverBaseURL))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" || isLoopbackHost(host) {
		return false
	}
	return true
}

// isLoopbackHost reports whether host names the local machine — "localhost"
// (and *.localhost), or any loopback IP literal (127.0.0.0/8, ::1).
func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
