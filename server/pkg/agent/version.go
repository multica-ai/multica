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

// MinQuickCreateCLIVersion gates the agent-create (quick-create) flow against
// the multica CLI version reported by the daemon at registration time. The
// quick-create prompt that the agent runs depends on CLI behavior introduced
// after this version (attachment URL handling, no-retry semantics on
// `multica issue create` failure — see PR #1851); older daemons would either
// double-create issues or mishandle pasted screenshot URLs. Treated as a hard
// requirement: missing / unparsable / below this threshold all fail closed.
const MinQuickCreateCLIVersion = "0.2.20"

// Errors returned by CheckMinCLIVersion. Callers branch on these to surface
// "needs upgrade" vs "version not reported" with the right user message.
var (
	ErrCLIVersionMissing = errors.New("multica CLI version not reported by daemon")
	ErrCLIVersionTooOld  = errors.New("multica CLI version is below required minimum")
)

// devDescribeRe matches the `git describe --tags --always --dirty` output for
// a build past the latest tag, e.g. `v0.2.15-235-gdaf0e935` (optionally with a
// trailing `-dirty`). Daemons built from source (Makefile `make build` / `make
// daemon`) report this shape; tagged releases are bare semver. Treating dev-
// described daemons as OK keeps `make daemon` unblocked without weakening the
// gate for staging or production users running stale stable releases.
var devDescribeRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+-\d+-g[0-9a-fA-F]+`)

// CheckMinCLIVersion returns nil when `detected` parses as ≥ minimum. Returns
// ErrCLIVersionMissing for empty or unparsable input, and ErrCLIVersionTooOld
// when parsable but below the minimum. The caller can check for these
// sentinel errors with errors.Is to drive the response shape.
//
// Dev-built daemons (git-describe shape) always pass — the version string
// itself is the shared signal, so the modal pre-check and this server gate
// agree by construction without needing to compare separate env flags.
func CheckMinCLIVersion(detected string) error {
	d := strings.TrimSpace(detected)
	if d == "" {
		return ErrCLIVersionMissing
	}
	if devDescribeRe.MatchString(d) {
		return nil
	}
	parsed, err := parseSemver(d)
	if err != nil {
		return ErrCLIVersionMissing
	}
	min, err := parseSemver(MinQuickCreateCLIVersion)
	if err != nil {
		// Misconfiguration in the constant itself — fail closed as missing.
		return ErrCLIVersionMissing
	}
	if parsed.lessThan(min) {
		return ErrCLIVersionTooOld
	}
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
