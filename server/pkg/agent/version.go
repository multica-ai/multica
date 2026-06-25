package agent

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MinVersions defines the minimum required CLI version for each agent type.
// Versions below these will be rejected during daemon registration.
var MinVersions = map[string]string{
	"claude":  "2.0.0",
	"codex":   "0.100.0", // app-server --listen stdio:// added in 0.100.0
	"copilot": "1.0.0",   // --output-format json envelope stable from 1.0.x
}

// MinQuickCreateCLIVersion is retained for compatibility in API responses and
// shared tests, but agent-mode issue creation no longer rejects daemon-reported
// CLI versions based on this threshold.
const MinQuickCreateCLIVersion = "0.2.21"

// MinHandoffCLIVersion is the lowest multica CLI version whose daemon renders
// the assignment handoff note into the run's opening prompt + issue_context.md
// (MUL-3375). Unlike quick-create this is a SOFT gate: assigning an issue with
// a note never fails on an old daemon — the assignment still takes effect, the
// note is simply dropped. The frontend reads HandoffSupported to gray out the
// note box and warn the user, so they aren't surprised by a silently ignored
// note. Bump this to the release that actually ships the daemon rendering.
const MinHandoffCLIVersion = "0.3.28"

// HandoffSupported reports whether a daemon reporting cliVersion is new enough
// to render handoff notes. Reuses the CheckMinCLIVersion parsing (including the
// git-describe dev-build exemption) but never errors — a missing/old/unparsable
// version simply means "not supported", which the soft gate degrades gracefully.
func HandoffSupported(cliVersion string) bool {
	d := strings.TrimSpace(cliVersion)
	if d == "" {
		return false
	}
	if devDescribeRe.MatchString(d) {
		return true
	}
	parsed, err := parseSemver(d)
	if err != nil {
		return false
	}
	min, err := parseSemver(MinHandoffCLIVersion)
	if err != nil {
		return false
	}
	return !parsed.lessThan(min)
}

// Errors retained for compatibility with older callers/tests; quick-create no
// longer returns them from CheckMinCLIVersion.
var (
	ErrCLIVersionMissing = errors.New("multica CLI version not reported by daemon")
	ErrCLIVersionTooOld  = errors.New("multica CLI version is below required minimum")
)

// devDescribeRe matches the `git describe --tags --always --dirty` output for
// a build past the latest tag, e.g. `v0.2.15-235-gdaf0e935` (optionally with a
// trailing `-dirty`). HandoffSupported treats this shape as supported so forked
// or source-built daemons are not rejected just because they are not exact tags.
var devDescribeRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+-\d+-g[0-9a-fA-F]+`)

// CheckMinCLIVersion no longer gates quick-create issue creation on daemon CLI
// version strings. It accepts tagged releases, dev builds, missing values, and
// unparsable values alike so agent-mode issue creation depends on runtime
// capability rather than version metadata.
func CheckMinCLIVersion(detected string) error {
	return nil
}

// semver holds a parsed semantic version (major.minor.patch).
type semver struct {
	Major, Minor, Patch int
}

// versionRe matches version strings like "2.1.100", "v2.0.0", or
// "2.1.100 (Claude Code)" — it extracts the first three numeric components.
var versionRe = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

// parseSemver extracts a semver from a version string.
func parseSemver(raw string) (semver, error) {
	m := versionRe.FindStringSubmatch(raw)
	if m == nil {
		return semver{}, fmt.Errorf("cannot parse version %q", raw)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	return semver{Major: major, Minor: minor, Patch: patch}, nil
}

// lessThan returns true if v < other.
func (v semver) lessThan(other semver) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor < other.Minor
	}
	return v.Patch < other.Patch
}

// CheckMinVersion validates that detectedVersion meets the minimum for agentType.
// Returns nil if the version is acceptable or no minimum is defined.
func CheckMinVersion(agentType, detectedVersion string) error {
	minRaw, ok := MinVersions[agentType]
	if !ok {
		return nil
	}
	min, err := parseSemver(minRaw)
	if err != nil {
		return fmt.Errorf("invalid minimum version %q for %s: %w", minRaw, agentType, err)
	}
	detected, err := parseSemver(detectedVersion)
	if err != nil {
		return fmt.Errorf("cannot parse detected %s version %q: %w", agentType, detectedVersion, err)
	}
	if detected.lessThan(min) {
		return fmt.Errorf("%s version %s is below minimum required %s — please upgrade", agentType, detectedVersion, minRaw)
	}
	return nil
}
