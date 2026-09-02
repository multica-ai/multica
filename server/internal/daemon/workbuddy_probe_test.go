package daemon

// Tests for the WorkBuddy desktop-app discovery probe (workbuddy_probe.go).
//
// The launch shape under test differs by platform:
//   - unix:  Path=<bundle cli script>            (shebang, exec bit set)
//   - windows: Path=<staged node.exe>, LaunchPrefix=[<bundle cli script>]
//
// Node selection must be semantic-version aware (22.10.0 > 22.9.0) and the
// probe must never mutate the daemon process environment — both are hard
// requirements carried over from upstream review of the macOS-only
// WorkBuddy-bundle proposal (multica-ai/multica#6624).

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stageWorkBuddyInstall lays out a fake WorkBuddy install tree:
//
//	<dir>/WorkBuddy(.app)/resources/app.asar.unpacked/cli/bin/codebuddy
//
// and returns the WorkBuddy install root (<dir>/WorkBuddy(.app)) — the value
// the probe's bundle-roots list is expected to contain. On windows the CLI is
// created as a plain file (exec bits carry no meaning); on unix it gets the
// exec bit so the shebang-script path is exercised for real.
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
	if runtime.GOOS == "windows" {
		if entry.Path == "" || !strings.Contains(strings.ToLower(filepath.Base(entry.Path)), "node") {
			t.Errorf("windows workbuddy Path = %q, want the staged node runtime", entry.Path)
		}
		if len(entry.LaunchPrefix) != 1 || entry.LaunchPrefix[0] != cli {
			t.Errorf("windows workbuddy LaunchPrefix = %v, want [%s]", entry.LaunchPrefix, cli)
		}
	} else {
		if entry.Path != cli {
			t.Errorf("workbuddy Path = %q, want %q", entry.Path, cli)
		}
		if len(entry.LaunchPrefix) != 0 {
			t.Errorf("unix workbuddy LaunchPrefix = %v, want empty", entry.LaunchPrefix)
		}
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
	if runtime.GOOS == "windows" {
		if !strings.Contains(entry.Path, "22.10.0") {
			t.Errorf("windows workbuddy Path = %q, want the 22.10.0 staged node (not lexicographic max)", entry.Path)
		}
	} else {
		// unix shape does not carry the node path; verify via resolveWorkBuddyNode.
		node, ok := resolveWorkBuddyNode()
		if !ok {
			t.Fatal("resolveWorkBuddyNode found no staged node")
		}
		if !strings.Contains(node, "22.10.0") {
			t.Errorf("resolveWorkBuddyNode = %q, want 22.10.0 (semver max, not lexicographic)", node)
		}
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
