package daemon

// WorkBuddy desktop app discovery.
//
// Tencent's WorkBuddy desktop app bundles the same @genie/agent-cli CodeBuddy
// Code ships, but never installs it onto PATH: on Windows it lives under
// <install>\resources\app.asar.unpacked\cli\bin\codebuddy, on macOS under
// WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy. The
// daemon treats it as its own runtime identity ("workbuddy", protocol family
// codebuddy) so a WorkBuddy install shows up as a usable agent runtime with
// no extra setup.
//
// One launch shape covers every platform:
//
//   - The bundled CLI is a Node shebang script (`#!/usr/bin/env node`) that
//     WorkBuddy never puts on PATH, so discovery can never treat it as a bare
//     command. Spawning the file directly is fragile everywhere: Windows
//     CreateProcess cannot run a bare extensionless script
//     (ERROR_BAD_EXE_FORMAT), and on unix the `env node` shebang resolves
//     `node` from the daemon process PATH, which a GUI-launched daemon
//     frequently does not inherit even though WorkBuddy staged a runtime.
//     Discovery therefore pairs the CLI with a Node executable and carries
//     the script path as the launch prefix: Path=<node>, LaunchPrefix=[<cli
//     script>], which the codebuddy family then spawns as
//     `<node> <script> -p ...` on every OS.
//
// The staged Node runtime under ~/.workbuddy is preferred over PATH node for
// two reasons: it is versioned alongside the bundled CLI that WorkBuddy
// ships, and a GUI-launched daemon frequently does not inherit the
// interactive shell's PATH (nvm/fnm multishells), which is exactly when the
// WorkBuddy bundle is the only codebuddy-family CLI present. PATH node is the
// fallback so an operator with a working node install is not forced to let
// WorkBuddy stage one. The choice never mutates the daemon process
// environment — the chosen Node path and the CLI script it runs travel only
// inside the entry's Path/LaunchPrefix, so unrelated agent launches cannot
// observe them.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// workbuddyBundleRelativeCLI is the path of the bundled CLI inside a WorkBuddy
// install root. It differs by platform because Electron packages resources
// differently: on Windows the root is the install directory and resources sit
// at its top level (<root>\resources\app.asar.unpacked\cli\bin\codebuddy),
// while on macOS the root is the .app bundle and resources are nested under
// Contents (<root>/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy).
// app.asar.unpacked/cli/bin/codebuddy is the common tail.
func workbuddyBundleRelativeCLI() string {
	return workbuddyBundleRelativeCLIForPlatform(runtime.GOOS)
}

// workbuddyBundleRelativeCLIForPlatform is the GOOS-parameterized form of
// workbuddyBundleRelativeCLI so the per-platform layouts can be asserted in
// tests without depending on the host platform.
func workbuddyBundleRelativeCLIForPlatform(goos string) string {
	if goos == "windows" {
		return filepath.Join("resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
	}
	return filepath.Join("Contents", "Resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
}

// workbuddyBundleRoots returns candidate install roots for the WorkBuddy
// desktop app, ordered most-likely-first. Windows installs are per-machine
// (any drive's Program Files) or per-user (LOCALAPPDATA\Programs); macOS app
// bundles are system (/Applications) or per-user (~/Applications), mirroring
// the codex bundle probe order. A var so tests can stub the locations.
var workbuddyBundleRoots = func() []string {
	switch runtime.GOOS {
	case "windows":
		roots := windowsProgramFilesRoots()
		if lpf := os.Getenv("LOCALAPPDATA"); lpf != "" {
			roots = append(roots, filepath.Join(lpf, "Programs", "WorkBuddy"))
		}
		return roots
	default: // darwin and any unix
		roots := []string{filepath.Join(string(filepath.Separator)+"Applications", "WorkBuddy.app")}
		if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots, filepath.Join(home, "Applications", "WorkBuddy.app"))
		}
		return roots
	}
}

// windowsProgramFilesRoots returns every "<drive>:\Program Files" root that
// exists on this machine, env-declared locations first (they are the most
// likely), then each existing fixed drive in A..Z order. Duplicates are
// dropped. The runtime switch in workbuddyBundleRoots calls it only when
// GOOS is windows, but it must still be declared in code that compiles for
// every platform — a _windows.go file would leave the identifier undefined
// on unix builds.
func windowsProgramFilesRoots() []string {
	var roots []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" {
			return
		}
		key := strings.ToLower(p)
		if seen[key] {
			return
		}
		seen[key] = true
		roots = append(roots, p)
	}

	for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if v := os.Getenv(env); v != "" {
			add(filepath.Join(v, "WorkBuddy"))
		}
	}
	for d := 'A'; d <= 'Z'; d++ {
		drive := string(d) + ":\\"
		if _, err := os.Stat(drive); err != nil {
			continue
		}
		add(filepath.Join(drive, "Program Files", "WorkBuddy"))
	}
	return roots
}

// workbuddyStagedNodeGlobs returns the glob patterns under which WorkBuddy
// stages the Node runtimes its CLI needs. Layouts differ by platform and
// WorkBuddy version: macOS stages the conventional
// ~/.workbuddy/binaries/node/versions/<semver>/bin/node, while Windows
// installs put node.exe directly in the version directory
// (…/versions/<semver>/node.exe — no bin/ layer, observed in WorkBuddy 5.x).
// Both Windows shapes are probed so an upstream layout change cannot silently
// drop the runtime back to PATH node.
var workbuddyStagedNodeGlobs = func() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	versions := filepath.Join(home, ".workbuddy", "binaries", "node", "versions")
	if runtime.GOOS == "windows" {
		return []string{
			filepath.Join(versions, "*", "node.exe"),
			filepath.Join(versions, "*", "bin", "node.exe"),
		}
	}
	return []string{filepath.Join(versions, "*", "bin", "node")}
}

// probeWorkBuddyAgent resolves the WorkBuddy-bundled CodeBuddy CLI to an
// AgentEntry, honoring the operator overrides MULTICA_WORKBUDDY_PATH /
// MULTICA_WORKBUDDY_MODEL first, then the desktop-app bundle locations.
// Explicitly configured but missing paths hard-miss (mirroring probe()), so a
// stale override never silently swaps in a bundled binary.
//
// Returns (AgentEntry, false) when no override or bundle resolves AND the
// launch prerequisites (a usable Node runtime where the platform requires
// one) cannot be satisfied.
func probeWorkBuddyAgent() (AgentEntry, bool) {
	model := strings.TrimSpace(os.Getenv("MULTICA_WORKBUDDY_MODEL"))

	// 1. Explicit override: an absolute or relative path (windows or unix).
	// It is resolved like any other pinned agent path, so a .cmd shim or a
	// real executable both work; a missing explicit path hard-misses.
	// Command is set so the daemon's self-heal can re-resolve the override
	// if the pinned file is later deleted (MUL-4486) — unlike a bundle path,
	// an operator override is a live, replaceable file the user owns.
	if raw := strings.TrimSpace(os.Getenv("MULTICA_WORKBUDDY_PATH")); raw != "" {
		if path, err := resolveAgentExecutablePath(raw); err == nil {
			return AgentEntry{
				Path:    path,
				Command: raw,
				Model:   model,
			}, true
		}
		return AgentEntry{}, false
	}

	// 2. Desktop-app bundle locations. Probe order matters: an explicit PATH
	// install of the default command ("codebuddy") is NOT consulted here —
	// that belongs to the codebuddy family probe and would make workbuddy
	// appear twice for one binary.
	for _, root := range workbuddyBundleRoots() {
		cli := filepath.Join(root, workbuddyBundleRelativeCLI())
		if !workbuddyBundleCLIUsable(cli) {
			continue
		}
		entry, ok := workbuddyEntryWithRuntime(cli, model)
		if !ok {
			// Bundle CLI exists but no Node runtime is usable; keep probing
			// other roots in case a later one pairs differently (test-only
			// distinction in practice — all roots stage under the same home).
			continue
		}
		return entry, true
	}
	return AgentEntry{}, false
}

// workbuddyBundleCLIUsable reports whether a bundled CLI file can back a
// runtime. The file is always run under an explicit Node interpreter (the
// launch prefix), never spawned directly, so an exec bit is not required on
// any platform — existence is the only gate, the same bar the codex bundle
// probe applies to its bundled binary.
func workbuddyBundleCLIUsable(cliPath string) bool {
	info, err := os.Stat(cliPath)
	return err == nil && !info.IsDir()
}

// workbuddyEntryWithRuntime builds the launchable AgentEntry for a resolved
// bundle CLI path: the entry runs the CLI under a pinned Node interpreter
// (`Path` = node, `LaunchPrefix` = [cli script]), so the `<node> <script>`
// launch shape is identical on every platform. Pinning the interpreter — and
// never mutating the daemon environment to make it reachable — matters for two
// reasons: Windows cannot spawn a bare extensionless shebang script at all,
// and on unix the script's `env node` shebang would resolve `node` from the
// daemon's process PATH, which a GUI-launched daemon may not have even though
// WorkBuddy staged a runtime under ~/.workbuddy.
//
// Command is deliberately empty: the field is the daemon's handle for
// re-resolving a vanished pinned Path (MUL-4486), and a Path that is the Node
// runtime must never be re-resolved — node is an interpreter shared by every
// npm-installed CLI, and "resolving" it would return an unrelated binary (or
// the WorkBuddy CLI script itself, since Command doubles as the launch prefix
// token here). Empty Command makes resolveAgentEntryWithHeal treat a vanished
// node as an unrecoverable launch failure with a clear error, which is the
// honest state: WorkBuddy stages node at install time and an operator repairs
// a missing one by reinstalling / reconfiguring.
func workbuddyEntryWithRuntime(cliPath, model string) (AgentEntry, bool) {
	node, ok := resolveWorkBuddyNode()
	if !ok {
		return AgentEntry{}, false
	}
	return AgentEntry{
		Path:         node,
		Model:        model,
		LaunchPrefix: []string{cliPath},
	}, true
}

// resolveWorkBuddyNode finds the Node runtime a bundled CodeBuddy CLI can run
// under. WorkBuddy's staged runtime (~/.workbuddy/binaries/node/versions/
// <semver>[/bin]/node[.exe]) wins by semantic version — newest staged version
// first, so 22.10.0 beats 22.9.0 (lexicographic order would get that wrong)
// — with PATH node as the fallback when nothing is staged. Returns ok=false
// when neither exists.
func resolveWorkBuddyNode() (string, bool) {
	var candidates []string
	for _, glob := range workbuddyStagedNodeGlobs() {
		paths, err := filepath.Glob(glob)
		if err == nil {
			candidates = append(candidates, paths...)
		}
	}
	if len(candidates) > 0 {
		sort.Slice(candidates, func(i, j int) bool {
			return workbuddyNodeVersionLess(candidates[i], candidates[j])
		})
		// candidates is now ascending by version; the last entry wins.
		for i := len(candidates) - 1; i >= 0; i-- {
			if p := candidates[i]; workbuddyNodeUsable(p) {
				return p, true
			}
		}
	}
	if node, err := exec.LookPath("node"); err == nil {
		return node, true
	}
	return "", false
}

// workbuddyNodeUsable reports whether a staged node binary can be exec'd as
// the entry's Path. Unlike the bundled CLI (always run through the launch
// prefix, existence only), node is spawned directly, so on unix it must carry
// an exec bit; on Windows the mode bits carry no exec meaning (node.exe is a
// PE binary), so existence is the gate.
func workbuddyNodeUsable(nodePath string) bool {
	if runtime.GOOS == "windows" {
		info, err := os.Stat(nodePath)
		return err == nil && !info.IsDir()
	}
	return isExecutableFile(nodePath)
}

// workbuddyNodeVersionLess orders two staged node binaries by the semantic
// version in their path (…/versions/<semver>/bin/…), ascending. Paths that
// do not parse as semver sort before any that do, so the highest parseable
// version always wins. A prerelease (22.10.0-rc.1) sorts below its release
// (22.10.0), per semver precedence.
func workbuddyNodeVersionLess(a, b string) bool {
	av := semverFromStagedNodePath(a)
	bv := semverFromStagedNodePath(b)
	ac, aprerelease, aok := parseSemver(av)
	bc, bprerelease, bok := parseSemver(bv)
	switch {
	case aok && !bok:
		return false
	case !aok && bok:
		return true
	case !aok && !bok:
		return av < bv
	}
	for i := 0; i < 3; i++ {
		if ac[i] != bc[i] {
			return ac[i] < bc[i]
		}
	}
	switch {
	case aprerelease == "" && bprerelease != "":
		return false // release > prerelease
	case aprerelease != "" && bprerelease == "":
		return true
	default:
		return aprerelease < bprerelease
	}
}

// semverFromStagedNodePath extracts the version segment of a staged node path.
// WorkBuddy stages Node in two shapes — macOS keeps a bin/ layer
// (…/versions/<semver>/bin/node) while the Windows layout puts node.exe
// directly in the version directory (…/versions/<semver>/node.exe) — so the
// semver is the path component immediately after the "versions" directory in
// both, not a fixed parent depth from the file. Walking up a fixed number of
// directories would parse only one shape (the macOS one) and leave every flat
// Windows candidate unparseable, silently degrading back to lexical ordering.
// Empty when the path has no "versions" segment.
func semverFromStagedNodePath(path string) string {
	segs := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "versions" {
			return segs[i+1]
		}
	}
	return ""
}

// parseSemver parses a dotted numeric version ("22.10.0", tolerating a
// leading "v") into three numeric components plus any prerelease suffix
// ("-rc.0"). ok=false when the leading components do not parse.
func parseSemver(ver string) (core [3]int, prerelease string, ok bool) {
	s := strings.TrimPrefix(strings.TrimSpace(ver), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s, prerelease = s[:i], s[i+1:]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 3 {
		return core, "", false
	}
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return core, "", false
		}
		core[i] = n
	}
	return core, prerelease, true
}
