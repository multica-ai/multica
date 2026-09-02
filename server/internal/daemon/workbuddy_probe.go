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
// Two platform shapes matter:
//
//   - macOS (and any unix): the bundled CLI is a shebang script with the
//     exec bit set and is launched directly. The shebang resolves `node`
//     through the process PATH, so discovery still verifies a Node runtime is
//     reachable and prefers the version WorkBuddy stages under
//     ~/.workbuddy/binaries/node/versions/<v>/bin/node (semver-maxed).
//
//   - Windows: the bundled CLI is a shebang script WITHOUT an executable
//     extension. exec.LookPath resolves only PATHEXT suffixes and
//     CreateProcess cannot execute a bare script (ERROR_BAD_EXE_FORMAT), so
//     the runtime cannot launch the file directly. Instead discovery pairs
//     the CLI with a Node executable and the launch prefix carries the script
//     path: Path=<node.exe>, LaunchPrefix=[<cli script>], which the
//     codebuddy family then spawns as `<node.exe> <script> -p ...`.
//
// The staged Node runtime under ~/.workbuddy is preferred over PATH node for
// two reasons: it is versioned alongside the bundled CLI that WorkBuddy
// ships, and a GUI-launched daemon frequently does not inherit the
// interactive shell's PATH (nvm/fnm multishells), which is exactly when the
// WorkBuddy bundle is the only codebuddy-family CLI present. PATH node is the
// fallback so an operator with a working node install is not forced to let
// WorkBuddy stage one. The choice never mutates the daemon process
// environment — the resolved Node path is carried per-entry in the launch
// prefix, so unrelated agent launches cannot observe it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// workbuddyBundleRelativeCLI is the path of the bundled CLI inside the
// WorkBuddy install / app bundle, shared by every platform.
var workbuddyBundleRelativeCLI = filepath.Join(
	"resources", "app.asar.unpacked", "cli", "bin", "codebuddy",
)

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
		cli := filepath.Join(root, workbuddyBundleRelativeCLI)
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
// runtime. On unix the CLI is a shebang script that must carry the exec bit
// to launch at all. On Windows the exec bit is meaningless (nothing is
// executable by mode); the CLI launches through the staged Node interpreter
// via the launch prefix, so existence is the only gate — the same bar the
// Windows codex bundle probe applies to its bundled binary.
func workbuddyBundleCLIUsable(cliPath string) bool {
	if runtime.GOOS == "windows" {
		info, err := os.Stat(cliPath)
		return err == nil && !info.IsDir()
	}
	return isExecutableFile(cliPath)
}

// workbuddyEntryWithRuntime builds the launchable AgentEntry for a resolved
// bundle CLI path. On unix the CLI launches directly (its shebang needs node
// on PATH, verified below); on Windows the CLI is a shebang script that
// cannot be spawned bare, so the entry launches the Node runtime with the
// script as its launch prefix.
func workbuddyEntryWithRuntime(cliPath, model string) (AgentEntry, bool) {
	node, ok := resolveWorkBuddyNode()
	if !ok {
		return AgentEntry{}, false
	}
	if runtime.GOOS == "windows" {
		// Command is deliberately empty: the field is the daemon's handle for
		// re-resolving a vanished pinned Path (MUL-4486), and a Path that is
		// the staged Node runtime must never be re-resolved — node is an
		// interpreter shared by every npm-installed CLI, and "resolving" it
		// would return an unrelated binary (or the WorkBuddy CLI script
		// itself, since Command doubles as the launch prefix token here).
		// Empty Command makes resolveAgentEntryWithHeal treat a vanished node
		// as an unrecoverable launch failure with a clear error, which is the
		// honest state: WorkBuddy stages node at install time and an operator
		// repairs a missing one by reinstalling / reconfiguring.
		return AgentEntry{
			Path:         node,
			Model:        model,
			LaunchPrefix: []string{cliPath},
		}, true
	}
	return workbuddyEntry(cliPath, model), true
}

// workbuddyEntry builds a plain AgentEntry launching cliPath directly (the
// unix shape of the WorkBuddy bundle). Command is left empty: the daemon's
// self-heal re-resolves a vanished pinned Path through the command name it
// was resolved from, and a WorkBuddy bundle path is not a command that can be
// re-resolved — the app owns it. A bundle path that vanishes (app updated or
// moved) is an unrecoverable launch failure with a clear error, not something
// a PATH re-resolution could repair.
func workbuddyEntry(cliPath, model string) AgentEntry {
	return AgentEntry{
		Path:  cliPath,
		Model: model,
	}
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

// workbuddyNodeUsable mirrors workbuddyBundleCLIUsable: exec-bit semantics on
// unix, existence on Windows (where node.exe is a PE binary and the mode bits
// carry no exec meaning).
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

// semverFromStagedNodePath extracts the version segment of a staged node path
// of the form …/versions/<semver>/bin/node[.exe]. Empty when the path does
// not match.
func semverFromStagedNodePath(path string) string {
	dir := filepath.Dir(filepath.Dir(path)) // …/versions/<semver>
	ver := filepath.Base(dir)
	if strings.HasPrefix(filepath.Base(filepath.Dir(dir)), "versions") {
		return ver
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
