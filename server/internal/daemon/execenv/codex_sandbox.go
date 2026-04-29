package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// Background
//
// On macOS, Codex's Seatbelt sandbox in the `workspace-write` mode silently
// ignores `[sandbox_workspace_write] network_access = true`. DNS resolution is
// blocked at the syscall layer, so processes inside the sandbox see
// `no such host` errors when calling out (for example, `multica issue get`
// hitting the Multica API). See upstream issue openai/codex#10390.
//
// Until a fixed Codex release ships, the per-task Codex config on macOS needs
// to fall back to `sandbox_mode = "danger-full-access"` so the agent can
// actually reach the Multica API. On Linux (and on macOS once the upstream
// fix is released), the normal `workspace-write` + `network_access = true`
// combo is preferred because it keeps the filesystem sandbox intact.
//
// CodexDarwinNetworkAccessFixedVersion is the earliest Codex CLI version in
// which `network_access = true` is honored under Seatbelt on macOS. Bump this
// constant when the upstream fix ships. Empty string means "no known fixed
// release yet — always treat macOS Codex as broken for network access".
const CodexDarwinNetworkAccessFixedVersion = ""

// codexSandboxPolicy describes how the per-task Codex config.toml should
// configure the sandbox.
type codexSandboxPolicy struct {
	// Mode is the value written as `sandbox_mode = "..."`.
	Mode string
	// NetworkAccess controls `[sandbox_workspace_write] network_access`.
	// Only meaningful when Mode is "workspace-write".
	NetworkAccess bool
	// Reason is a short human-readable label used in warn-level logs.
	Reason string
}

// codexSandboxPolicyFor picks the right policy for the given platform and
// detected Codex CLI version.
//
// - Linux: workspace-write with network access. Landlock honors
//   network_access and Multica CLI can reach ~/.multica through regular
//   filesystem read/write of user home.
// - Windows: danger-full-access. Starting with codex 0.122+, workspace-write
//   on Windows is enforced by the experimental native Windows sandbox which
//   spawns each exec_command under CreateProcessWithLogonW. That sandbox
//   refuses access to user profile paths outside the workspace (including
//   ~/.multica where the CLI stores credentials), making `multica issue get`
//   and every other CLI call fail with "blocked by policy". We fall back to
//   danger-full-access so the daemon-injected Multica CLI works. Users who
//   need tighter Windows sandboxing should disable the experimental Windows
//   sandbox in their own ~/.codex/config.toml via
//   `[features] experimental_windows_sandbox = false`.
// - darwin with a version at or above CodexDarwinNetworkAccessFixedVersion:
//   workspace-write with network access (upstream bug fixed).
// - darwin otherwise (including when the version is unknown): fall back to
//   danger-full-access so the Multica CLI can reach the API.
func codexSandboxPolicyFor(goos, detectedVersion string) codexSandboxPolicy {
	if goos == "" {
		goos = runtime.GOOS
	}
	switch goos {
	case "linux":
		return codexSandboxPolicy{
			Mode:          "workspace-write",
			NetworkAccess: true,
			Reason:        "linux: Landlock honors workspace-write + network_access",
		}
	case "windows":
		return codexSandboxPolicy{
			Mode:   "danger-full-access",
			Reason: "windows: native sandbox blocks Multica CLI access to ~/.multica credentials under workspace-write",
		}
	}
	// darwin branch below
	if codexDarwinNetworkAccessFixed(detectedVersion) {
		return codexSandboxPolicy{
			Mode:          "workspace-write",
			NetworkAccess: true,
			Reason:        "codex version includes macOS network_access fix",
		}
	}
	reason := "codex on macOS: seatbelt ignores sandbox_workspace_write.network_access (openai/codex#10390)"
	if detectedVersion == "" {
		reason += " — version unknown, assuming broken"
	}
	return codexSandboxPolicy{
		Mode:          "danger-full-access",
		NetworkAccess: false,
		Reason:        reason,
	}
}

// codexDarwinNetworkAccessFixed returns true if the given detected version is
// known to honor `network_access = true` under Seatbelt on macOS.
func codexDarwinNetworkAccessFixed(detectedVersion string) bool {
	if CodexDarwinNetworkAccessFixedVersion == "" || detectedVersion == "" {
		return false
	}
	fixed, err := parseCodexSemver(CodexDarwinNetworkAccessFixedVersion)
	if err != nil {
		return false
	}
	got, err := parseCodexSemver(detectedVersion)
	if err != nil {
		return false
	}
	return !got.lessThan(fixed)
}

// codexUpgradeHint returns a short, actionable hint for users running a Codex
// version that suffers from the macOS network_access bug.
func codexUpgradeHint() string {
	return "upgrade Codex CLI (e.g. `brew upgrade codex` or `npm i -g @openai/codex`) once a release including openai/codex#10390 is available to restore workspace-write + network_access"
}

// multicaManagedBeginMarker / multicaManagedEndMarker delimit the block the
// daemon writes into the per-task config.toml. Everything between the markers
// is owned by the daemon and will be rewritten idempotently; anything outside
// the markers is preserved as-is.
const (
	multicaManagedBeginMarker = "# BEGIN multica-managed (do not edit; regenerated by daemon)"
	multicaManagedEndMarker   = "# END multica-managed"
)

// renderMulticaManagedBlock produces the managed block for the given policy.
//
// The block contains only top-level key=value assignments — no `[table]`
// headers — and uses TOML dotted-key syntax for nested values. This is
// important because the block is inserted into a user-owned config.toml:
//
//   - If the block opened a `[sandbox_workspace_write]` header, any user
//     content that happened to sit below it would be silently reparented into
//     that table.
//   - If the block were appended after a file that already ends inside some
//     other table (e.g. `[permissions.multica]`), a bare `sandbox_mode = ...`
//     key would be parsed as a child of that preceding table.
//
// Keeping the block as pure top-level dotted-key assignments, and placing it
// at the top of the file (see upsertMulticaManagedBlock), avoids both traps.
func renderMulticaManagedBlock(policy codexSandboxPolicy) string {
	var b strings.Builder
	b.WriteString(multicaManagedBeginMarker)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("sandbox_mode = %q\n", policy.Mode))
	if policy.Mode == "workspace-write" {
		b.WriteString(fmt.Sprintf("sandbox_workspace_write.network_access = %t\n", policy.NetworkAccess))
	}
	b.WriteString(multicaManagedEndMarker)
	b.WriteString("\n")
	return b.String()
}

// managedBlockRe captures the daemon-owned block (including the surrounding
// markers) so it can be replaced idempotently.
var managedBlockRe = regexp.MustCompile(
	`(?ms)^` + regexp.QuoteMeta(multicaManagedBeginMarker) +
		`.*?^` + regexp.QuoteMeta(multicaManagedEndMarker) + `\n?`)

// upsertMulticaManagedBlock returns the config content with the multica-managed
// block placed at the very top of the file. Any previously written managed
// block is removed in place; user content outside the markers is preserved.
//
// The block is always hoisted to the top (rather than replaced in place or
// appended to EOF) so that its top-level keys are parsed at the TOML root,
// regardless of whether the user's config ends inside a table like
// `[permissions.multica]` or `[profiles.foo]`. Combined with the dotted-key
// form used by renderMulticaManagedBlock, this means the managed block neither
// leaks into nor inherits from any surrounding table scope.
func upsertMulticaManagedBlock(content string, policy codexSandboxPolicy) string {
	// Drop any previously written managed block (wherever it sits).
	content = managedBlockRe.ReplaceAllString(content, "")
	block := renderMulticaManagedBlock(policy)
	// Trim leading blank lines left behind by the removal so we don't grow
	// the file on every idempotent rewrite.
	content = strings.TrimLeft(content, "\n")
	if content == "" {
		return block
	}
	return block + "\n" + content
}

// stripLegacySandboxDirectives removes top-level `sandbox_mode = ...` lines
// and any `[sandbox_workspace_write]` section that would otherwise conflict
// with the managed block. This lets the daemon migrate tasks whose config.toml
// was produced by an older daemon that wrote those values inline.
//
// Only top-level entries are stripped; anything under an unrelated section
// header (like `[permissions.foo]`) is preserved untouched.
func stripLegacySandboxDirectives(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inLegacyWorkspaceWrite := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			// Entering a new section. Exit legacy-tracking if we were in one.
			inLegacyWorkspaceWrite = trimmed == "[sandbox_workspace_write]"
			if inLegacyWorkspaceWrite {
				continue
			}
			out = append(out, line)
			continue
		}
		if inLegacyWorkspaceWrite {
			// Drop the legacy section body until the next section.
			continue
		}
		if strings.HasPrefix(trimmed, "sandbox_mode") {
			// Drop legacy top-level sandbox_mode declarations.
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// ensureCodexSandboxConfig writes the multica-managed sandbox block into the
// given config.toml according to the policy. It is idempotent: running it
// twice produces the same file contents. The file is created if it doesn't
// exist.
//
// The function logs (at warn level) when it falls back to danger-full-access
// on macOS so the incident is visible in daemon logs.
func ensureCodexSandboxConfig(configPath string, policy codexSandboxPolicy, detectedVersion string, logger *slog.Logger) error {
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config.toml: %w", err)
	}
	existing := string(data)

	// Drop inline sandbox_mode / [sandbox_workspace_write] from older daemon
	// versions so they don't collide with the managed block.
	if existing != "" && !managedBlockRe.MatchString(existing) {
		existing = stripLegacySandboxDirectives(existing)
	}

	updated := upsertMulticaManagedBlock(existing, policy)
	if updated == string(data) {
		return nil
	}

	if policy.Mode == "danger-full-access" && logger != nil {
		version := detectedVersion
		if version == "" {
			version = "unknown"
		}
		logger.Warn("codex sandbox: falling back to danger-full-access on macOS",
			"reason", policy.Reason,
			"codex_version", version,
			"hint", codexUpgradeHint(),
			"config_path", configPath,
		)
	}

	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	return nil
}

// --- small semver helper, scoped to this package to avoid an import cycle
// with server/pkg/agent. The agent package already has a similar parser; we
// duplicate the minimal bits here because execenv cannot depend on agent.

type codexSemver struct {
	Major, Minor, Patch int
}

var codexSemverRe = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

func parseCodexSemver(raw string) (codexSemver, error) {
	m := codexSemverRe.FindStringSubmatch(raw)
	if m == nil {
		return codexSemver{}, fmt.Errorf("cannot parse version %q", raw)
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	pat, _ := strconv.Atoi(m[3])
	return codexSemver{Major: maj, Minor: min, Patch: pat}, nil
}

func (v codexSemver) lessThan(o codexSemver) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}
