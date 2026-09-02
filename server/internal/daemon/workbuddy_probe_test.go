package daemon

// Tests for the WorkBuddy desktop-app discovery probe (workbuddy_probe.go).
//
// The launch shape is the same on every platform:
//   Path=<staged node>, LaunchPrefix=[<bundle cli script>]
// i.e. the codebuddy family spawns the bundled Node shebang CLI as
// `<node> <cli script> -p ...`. No platform launches the CLI directly, so a
// daemon whose PATH has no `node` (a GUI launch) still works.
//
// Node selection must be semantic-version aware (22.10.0 > 22.9.0) and the
// probe must never mutate the daemon process environment — both are hard
// requirements carried over from upstream review of the macOS-only
// WorkBuddy-bundle proposal (multica-ai/multica#6624).

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// stageWorkBuddyInstall lays out a fake WorkBuddy install tree:
//
//	<dir>/WorkBuddy(.app)/resources/app.asar.unpacked/cli/bin/codebuddy
//
// and returns the WorkBuddy install root (<dir>/WorkBuddy(.app)) — the value
// the probe's bundle-roots list is expected to contain. The CLI is a Node
// shebang file and is always run under an explicit staged node (the launch
// prefix), never spawned directly, so it needs no exec bit on any platform.
func stageWorkBuddyInstall(t *testing.T, dir string) string {
	t.Helper()
	root := filepath.Join(dir, "WorkBuddy")
	if runtime.GOOS != "windows" {
		root += ".app"
	}
	cli := filepath.Join(root, "resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
	if err := os.MkdirAll(filepath.Dir(cli), 0o755); err != nil {
		t.Fatalf("mkdir bundle cli dir: %v", err)
	}
	content := []byte("#!/usr/bin/env node\n// fake workbuddy bundled cli\n")
	if runtime.GOOS == "windows" {
		content = []byte("// fake workbuddy bundled cli (no shebang needed under node)\n")
	}
	if err := os.WriteFile(cli, content, 0o755); err != nil {
		t.Fatalf("write bundle cli: %v", err)
	}
	return root
}

// stubWorkBuddyDiscovery points the probe at fake bundle roots and a fake
// staged-node root, and stubs the login-shell resolver so no shell is forked.
// Returns a restore func.
func stubWorkBuddyDiscovery(t *testing.T, bundleDirs []string, stagedVersions ...string) {
	t.Helper()

	origRoots := workbuddyBundleRoots
	workbuddyBundleRoots = func() []string { return bundleDirs }
	t.Cleanup(func() { workbuddyBundleRoots = origRoots })

	// staged node root: <tmp>/wbn/bin/node[.exe] staged per version under
	// versions/<ver>[/bin] — mirrors ~/.workbuddy/binaries/node/versions/*.
	nodeName := "node"
	if runtime.GOOS == "windows" {
		nodeName = "node.exe"
	}
	var stagedRoots []string
	if len(stagedVersions) > 0 {
		stagedRoot := t.TempDir()
		for _, ver := range stagedVersions {
			p := filepath.Join(stagedRoot, "versions", ver, "bin", nodeName)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatalf("mkdir staged node: %v", err)
			}
			if err := os.WriteFile(p, []byte("fake node"), 0o755); err != nil {
				t.Fatalf("write staged node: %v", err)
			}
			stagedRoots = append(stagedRoots, p)
		}
		origGlob := workbuddyStagedNodeGlobs
		workbuddyStagedNodeGlobs = func() []string {
			return []string{filepath.Join(stagedRoot, "versions", "*", "bin", nodeName)}
		}
		t.Cleanup(func() { workbuddyStagedNodeGlobs = origGlob })
	} else {
		origGlob := workbuddyStagedNodeGlobs
		workbuddyStagedNodeGlobs = func() []string { return nil }
		t.Cleanup(func() { workbuddyStagedNodeGlobs = origGlob })
	}

	origShell := resolveAgentsViaLoginShell
	resolveAgentsViaLoginShell = func([]string) map[string]string { return map[string]string{} }
	t.Cleanup(func() { resolveAgentsViaLoginShell = origShell })
	resetShellResolveCacheForTest(t)

	// probeAgentCLIs() probes every built-in agent CLI it can resolve, and the
	// dsh probe actually executes the resolved binary (probeDshMulticaProfile).
	// Point PATH at an empty directory so no ambient CLI on the test host — or
	// the CI agent-CLI guard — is ever resolved and executed, matching the
	// isolation the other probeAgentCLIs() tests use. WorkBuddy bundle and
	// staged-node discovery above do not depend on PATH.
	t.Setenv("PATH", t.TempDir())
}

// TestResolveWorkBuddyNode_WindowsFlatLayout guards the real-world Windows
// staged-node layout: node.exe sits directly in the version directory
// (~/.workbuddy/binaries/node/versions/<ver>/node.exe) with NO bin/ layer.
// A probe that only knows the macOS bin/ layout silently falls back to PATH
// node, which breaks GUI-launched daemons that lack node on PATH.
func TestResolveWorkBuddyNode_WindowsFlatLayout(t *testing.T) {
	origGlob := workbuddyStagedNodeGlobs
	defer func() { workbuddyStagedNodeGlobs = origGlob }()

	stagedRoot := t.TempDir()
	nodeExe := filepath.Join(stagedRoot, "versions", "22.22.2-2", "node.exe")
	if err := os.MkdirAll(filepath.Dir(nodeExe), 0o755); err != nil {
		t.Fatalf("mkdir flat staged node dir: %v", err)
	}
	if err := os.WriteFile(nodeExe, []byte("fake node.exe"), 0o755); err != nil {
		t.Fatalf("write flat staged node: %v", err)
	}
	// Probe with both the flat (windows) and bin/ (macOS) globs exactly like
	// production does, and confirm the flat layout is found.
	workbuddyStagedNodeGlobs = func() []string {
		return []string{
			filepath.Join(stagedRoot, "versions", "*", "node.exe"),
			filepath.Join(stagedRoot, "versions", "*", "bin", "node.exe"),
		}
	}
	// No PATH node: an empty PATH dir keeps exec.LookPath honest.
	t.Setenv("PATH", t.TempDir())

	node, ok := resolveWorkBuddyNode()
	if !ok {
		t.Fatal("resolveWorkBuddyNode found nothing; flat Windows layout must be discovered")
	}
	if filepath.Base(node) != "node.exe" {
		t.Errorf("resolveWorkBuddyNode = %q, want the flat-layout node.exe", node)
	}
}

func TestProbeAgentCLIs_DiscoversWorkBuddyBundle(t *testing.T) {
	bundle := stageWorkBuddyInstall(t, t.TempDir())
	stubWorkBuddyDiscovery(t, []string{bundle}, "22.22.2")

	t.Setenv("MULTICA_WORKBUDDY_PATH", "")
	t.Setenv("MULTICA_WORKBUDDY_MODEL", "")

	agents := probeAgentCLIs()
	entry, ok := agents["workbuddy"]
	if !ok {
		t.Fatal("workbuddy was not discovered from a staged bundle install")
	}
	if entry.Model != "" {
		t.Errorf("workbuddy model = %q, want empty", entry.Model)
	}

	cli := filepath.Join(bundle, "resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
	// The entry launches the CLI under the pinned staged node: Path is the node
	// runtime and LaunchPrefix is the CLI script — the same shape on every
	// platform, so a daemon whose PATH lacks node (a GUI launch) still works.
	if entry.Path == "" || !strings.Contains(strings.ToLower(filepath.Base(entry.Path)), "node") {
		t.Errorf("workbuddy Path = %q, want the staged node runtime", entry.Path)
	}
	if !strings.Contains(entry.Path, "22.22.2") {
		t.Errorf("workbuddy Path = %q, want the staged 22.22.2 node", entry.Path)
	}
	if len(entry.LaunchPrefix) != 1 || entry.LaunchPrefix[0] != cli {
		t.Errorf("workbuddy LaunchPrefix = %v, want [%s]", entry.LaunchPrefix, cli)
	}
}

func TestProbeAgentCLIs_WorkBuddyBundlePicksNewestStagedNodeSemver(t *testing.T) {
	bundle := stageWorkBuddyInstall(t, t.TempDir())
	// 22.9.0 sorts AFTER 22.10.0 lexicographically — the trap #6624's
	// lexicographic-reverse ordering fell into. The probe must pick 22.10.0.
	stubWorkBuddyDiscovery(t, []string{bundle}, "22.10.0", "22.9.0")
	t.Setenv("MULTICA_WORKBUDDY_PATH", "")

	agents := probeAgentCLIs()
	entry, ok := agents["workbuddy"]
	if !ok {
		t.Fatal("workbuddy was not discovered")
	}
	// Path carries the node runtime on every platform; assert the probe picked
	// the 22.10.0 staged node (semver max), not the lexicographic max 22.9.0.
	if !strings.Contains(entry.Path, "22.10.0") {
		t.Errorf("workbuddy Path = %q, want the 22.10.0 staged node (not lexicographic max)", entry.Path)
	}
	if filepath.Base(entry.Path) == "codebuddy" {
		t.Errorf("workbuddy Path = %q, want the staged node runtime, not the CLI script", entry.Path)
	}
}

func TestProbeAgentCLIs_ExplicitWorkBuddyPathPrecedesBundle(t *testing.T) {
	bundle := stageWorkBuddyInstall(t, t.TempDir())
	explicitDir := t.TempDir()
	explicit := filepath.Join(explicitDir, "workbuddy-cli")
	if runtime.GOOS == "windows" {
		explicit += ".cmd"
	}
	if err := os.WriteFile(explicit, []byte("@echo off\r\nfake\r\n"), 0o755); err != nil {
		t.Fatalf("write explicit workbuddy cli: %v", err)
	}

	stubWorkBuddyDiscovery(t, []string{bundle}, "22.22.2")
	t.Setenv("MULTICA_WORKBUDDY_PATH", explicit)
	t.Setenv("MULTICA_WORKBUDDY_MODEL", "glm-4.7")

	agents := probeAgentCLIs()
	entry, ok := agents["workbuddy"]
	if !ok {
		t.Fatal("workbuddy was not discovered via explicit MULTICA_WORKBUDDY_PATH")
	}
	if entry.Model != "glm-4.7" {
		t.Errorf("workbuddy model = %q, want glm-4.7", entry.Model)
	}
	if runtime.GOOS == "windows" {
		// resolveAgentExecutablePath canonicalizes; compare the base name.
		if !strings.Contains(strings.ToLower(entry.Path), "workbuddy-cli") {
			t.Errorf("workbuddy Path = %q, want the explicit override path", entry.Path)
		}
		if len(entry.LaunchPrefix) != 0 {
			t.Errorf("explicit-override LaunchPrefix = %v, want empty (override is self-launching)", entry.LaunchPrefix)
		}
	} else {
		if entry.Path != explicit {
			t.Errorf("workbuddy Path = %q, want %q", entry.Path, explicit)
		}
	}
}

func TestProbeAgentCLIs_MissingExplicitWorkBuddyPathHardMisses(t *testing.T) {
	bundle := stageWorkBuddyInstall(t, t.TempDir())
	stubWorkBuddyDiscovery(t, []string{bundle}, "22.22.2")

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if runtime.GOOS == "windows" {
		missing += ".cmd"
	}
	t.Setenv("MULTICA_WORKBUDDY_PATH", missing)

	agents := probeAgentCLIs()
	if _, ok := agents["workbuddy"]; ok {
		t.Fatal("workbuddy must hard-miss when MULTICA_WORKBUDDY_PATH points at a missing file; bundle fallback must not silently apply")
	}
}

func TestProbeAgentCLIs_WorkBuddyBundleRequiresNodeRuntime(t *testing.T) {
	bundle := stageWorkBuddyInstall(t, t.TempDir())
	stubWorkBuddyDiscovery(t, []string{bundle}) // no staged versions, no PATH node expected

	t.Setenv("MULTICA_WORKBUDDY_PATH", "")
	// Ensure PATH node cannot rescue the probe either.
	t.Setenv("PATH", t.TempDir())

	agents := probeAgentCLIs()
	if _, ok := agents["workbuddy"]; ok {
		t.Fatal("workbuddy must not be discovered when no Node runtime is usable")
	}
}

func TestProbeAgentCLIs_WorkBuddyDiscoveryDoesNotMutateProcessPath(t *testing.T) {
	bundle := stageWorkBuddyInstall(t, t.TempDir())
	stubWorkBuddyDiscovery(t, []string{bundle}, "22.22.2")

	t.Setenv("MULTICA_WORKBUDDY_PATH", "")
	before := os.Getenv("PATH")
	agents := probeAgentCLIs()
	if _, ok := agents["workbuddy"]; !ok {
		t.Fatal("workbuddy was not discovered; test premise broken")
	}
	after := os.Getenv("PATH")
	if before != after {
		t.Fatalf("probeAgentCLIs mutated the daemon process PATH:\nbefore=%q\nafter =%q", before, after)
	}
}

// TestProbeAgentCLIs_WorkBuddyVersionDetectionRunsNodeCli covers the actual
// Node-dependent launch path the #6624 review asked to exercise: the entry
// must run the bundled CLI under the pinned staged node (`<node> <cli script>
// --version`) and report the CLI's version — not Node's. A fake "node" shim
// answers with a Node version unless its first operand is the CLI script, so
// the reported version proves the argv shape; a record file captures exactly
// what node was asked to run. This is the same command the daemon's version
// probe builds from AgentEntry{Path, LaunchPrefix}.
func TestProbeAgentCLIs_WorkBuddyVersionDetectionRunsNodeCli(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("staged node shim is POSIX-only; the daemon suite's WorkBuddy paths run on unix CI")
	}
	bundle := stageWorkBuddyInstall(t, t.TempDir())
	stubWorkBuddyDiscovery(t, []string{bundle}, "22.22.2")
	t.Setenv("MULTICA_WORKBUDDY_PATH", "")

	agents := probeAgentCLIs()
	entry, ok := agents["workbuddy"]
	if !ok {
		t.Fatal("workbuddy was not discovered; test premise broken")
	}
	cli := filepath.Join(bundle, "resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
	if entry.Path == "" || len(entry.LaunchPrefix) != 1 || entry.LaunchPrefix[0] != cli {
		t.Fatalf("entry = {Path:%q LaunchPrefix:%v}, want the staged node + [cli script] launch shape", entry.Path, entry.LaunchPrefix)
	}

	// Overwrite the staged node (a plain discovery-time file) with an
	// executable shim that reports the bundled CLI's version only when invoked
	// as `<node> <cli> --version`; any other shape answers with a Node version.
	record := filepath.Join(t.TempDir(), "launch-argv.txt")
	shim := "#!/bin/sh\n" +
		`printf '%s\n' "$@" > "` + record + `"` + "\n" +
		`if [ "$1" = "` + cli + `" ]; then` + "\n" +
		"  echo 'codebuddy/2.137.1'\n" +
		"else\n" +
		"  echo 'node/v99.0.0'\n" +
		"fi\n"
	if err := os.WriteFile(entry.Path, []byte(shim), 0o755); err != nil {
		t.Fatalf("write staged node shim: %v", err)
	}

	version, err := agent.DetectVersion(context.Background(), agent.NewCommand(entry.Path, entry.LaunchPrefix))
	if err != nil {
		t.Fatalf("DetectVersion over the workbuddy entry: %v", err)
	}
	if !strings.Contains(version, "2.137.1") {
		t.Errorf("version = %q, want the bundled CLI's version (2.137.1) — a direct node --version would report v99.0.0", version)
	}
	argvBytes, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read recorded launch argv: %v", err)
	}
	if argv := string(argvBytes); !strings.Contains(argv, cli) || !strings.Contains(argv, "--version") {
		t.Errorf("staged node was invoked as %q, want it asked to run the CLI script with --version", argv)
	}
}

func TestWorkBuddyNodeSemverOrdering(t *testing.T) {
	// Regression guard for the lexicographic-vs-semver trap: 22.10.0 must
	// sort as newer than 22.9.0, and a staged path whose version does not
	// parse must never beat a parseable one.
	cases := []struct {
		a, b string
		want bool // want a < b
	}{
		{"…/versions/22.9.0/bin/node", "…/versions/22.10.0/bin/node", true},
		{"…/versions/22.10.0/bin/node", "…/versions/22.9.0/bin/node", false},
		{"…/versions/22.10.0/bin/node", "…/versions/22.10.0/bin/node", false},
		{"…/versions/v22.11.0/bin/node", "…/versions/22.10.0/bin/node", false}, // leading v tolerated
		{"…/versions/22.10.0-rc.1/bin/node", "…/versions/22.10.0/bin/node", true},
		{"…/versions/junk/bin/node", "…/versions/22.10.0/bin/node", true}, // unparseable sorts below
	}
	for _, c := range cases {
		if got := workbuddyNodeVersionLess(c.a, c.b); got != c.want {
			t.Errorf("workbuddyNodeVersionLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
