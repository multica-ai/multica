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
//
// Most entries below name a protocol or capability the backend speaks through,
// so an older CLI simply cannot serve a task. The opencode entry is the one
// exception and is explained at its line: that CLI works fine, it damages the
// host it runs on.
var MinVersions = map[string]string{
	"antigravity": "1.1.10", // stream-json usage plus reliable headless --model selection
	"claude":      "2.0.0",
	"codex":       "0.100.0", // app-server --listen stdio:// added in 0.100.0
	"copilot":     "1.0.0",   // --output-format json envelope stable from 1.0.x
	"grok":        "0.2.89",  // ACP + authenticate/session-load/set_model/MCP and --effort thinking flag
	"qwen":        "0.20.0",  // stream-json protocol captured and verified against Qwen Code 0.20.0
	"dim":         "0.3.10",  // cross-run session/load: per-process lock releases on graceful exit
	"mcode":       "0.1.2",   // ACP v1 session/new, prompt, MCP capability forwarding
	"zeroclaw":    "0.8.0",   // persistent ACP sessions and session/resume were added in 0.8.0
	// opencode: honors TMPDIR/TMP/TEMP from 1.1.54. Earlier builds ignore all
	// three when their embedded Bun runtime extracts a native module, writing
	// into the shared system temp dir whatever the daemon exports — one 4-8 MB
	// module per successful run, under a fresh non-content-addressed name, never
	// removed. The per-task temp dir cannot contain that, and deleting by
	// filename in a shared /tmp is not safe, so refusing the CLI is the only
	// place we can stop it. See #8392: ~2,960 files, 11.16 GiB, root at 99%.
	"opencode": "1.1.54",
	// The whole fail-closed rlmMaxDepth gate is argued from v0.7.1 internals:
	// getAgentDir's PRIME_AGENT_CODING_AGENT_DIR handling, _resolveRlmMaxDepth's
	// precedence chain, and the settings.json path the gate opens. A build that
	// resolved that path differently would make the gate read the wrong file and
	// pass silently, so the assumption is enforced rather than only documented.
	// 0.7.1 is also the only version smoke-tested against a live account.
	// getAgentDir, ENV_AGENT_DIR, CONFIG_DIR_NAME and _resolveRlmMaxDepth were
	// diffed through v0.9.5 and prime-agent main as of 2026-09-23, and the
	// only divergence is v0.9.4 expanding a
	// Windows `~\` value that earlier versions leave relative; primeAgentDirsFor
	// checks both readings, so the floor admits nothing the gate cannot see.
	"prime": "0.7.1",
}

// MinQuickCreateCLIVersion gates the agent-create (quick-create) flow against
// the multica CLI version reported by the daemon at registration time. The
// quick-create prompt that the agent runs depends on CLI behavior introduced
// after this version (attachment URL handling, quick-create attachment
// binding, no-retry semantics on `multica issue create` failure — see PR
// #1851); older daemons would either double-create issues or mishandle pasted
// screenshot URLs. Treated as a hard requirement: missing / unparsable / below
// this threshold all fail closed.
const MinQuickCreateCLIVersion = "0.2.21"

// MinQuickCreateFieldsCLIVersion is the first daemon release that carries
// explicit quick-create priority and due-date fields from the claim response
// into the generated issue-create prompt. Basic quick-create remains on the
// older floor above; only requests using these optional fields need this gate.
const MinQuickCreateFieldsCLIVersion = "0.4.3"

// MinLocalWorktreeCLIVersion is the release that first shipped
// execution_mode=worktree for local_directory resources (MUL-5707).
//
// NOTHING GATES ON THIS. It is a display value: the number shown in the 422
// payload and the UI hint so a user knows roughly which release to update to.
// The gates themselves read protocol.DaemonCapabilityLocalWorktreeV1, which
// the daemon advertises only when it actually implements the mode.
//
// It stopped being a gate because it could not be one. A daemon without the
// implementation does not lose a field — it runs the task IN PLACE, editing the
// working copy the user asked to isolate. Version strings cannot answer that:
// CheckMinCLIVersionFor exempts git-describe dev builds so `make daemon` stays
// unblocked, and a v0.4.23-era daemon reporting "v0.4.21-24-gcd3c0bb89" sailed
// through the floor and ran two tasks in the user's own directory.
const MinLocalWorktreeCLIVersion = "0.4.24"

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
	return CheckMinCLIVersionFor(detected, MinQuickCreateCLIVersion)
}

// CheckMinCLIVersionFor applies the quick-create version policy against a
// caller-provided capability floor. It preserves the dev-build exemption so
// feature-specific server and frontend gates agree with the base gate.
func CheckMinCLIVersionFor(detected, minimum string) error {
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
	min, err := parseSemver(minimum)
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

// SupportsTaskSupplement gates the resolved executable, including custom
// commands. Unknown versions must not advertise a capability they may lack.
func SupportsTaskSupplement(provider, version string) bool {
	var minimum string
	switch provider {
	case "codex":
		// v0.100.0 exposes turn/steer with the expectedTurnId precondition.
		minimum = "0.100.0"
	case "claude":
		// 2.1.110 fixes PreToolUse additionalContext being lost on tool failure.
		// https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md
		minimum = "2.1.110"
	case "grok":
		// Grok Build 1.0.14 supports atomic delivery of in-turn interjections
		// through its x.ai/interject ACP extension.
		minimum = "1.0.14"
	default:
		return false
	}
	detected, err := parseSemver(version)
	floor, _ := parseSemver(minimum)
	return err == nil && !detected.lessThan(floor)
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

// BelowMinimumError reports a version that parsed successfully and is below
// the configured minimum. It is a distinct type so callers can tell a
// CONFIRMED too-old verdict apart from "could not parse the version": only
// the former is evidence strong enough to act on (taking a runtime offline),
// while an unreadable version must be treated like any other failed
// detection and leave working runtimes alone.
type BelowMinimumError struct {
	AgentType string
	Detected  string
	Minimum   string
}

func (e *BelowMinimumError) Error() string {
	return fmt.Sprintf("%s version %s is below minimum required %s — please upgrade", e.AgentType, e.Detected, e.Minimum)
}

// CheckMinVersion validates that detectedVersion meets the minimum for agentType.
// Returns nil if the version is acceptable or no minimum is defined, a
// *BelowMinimumError when the version parsed and is confirmed too old, and a
// plain error when the version could not be parsed at all.
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
		return &BelowMinimumError{AgentType: agentType, Detected: detectedVersion, Minimum: minRaw}
	}
	return nil
}
