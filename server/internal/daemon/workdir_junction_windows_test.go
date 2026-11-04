//go:build windows

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #8946: moving the workspaces root to another drive and leaving a directory
// junction in its place is the no-elevation way to free space on C:, and it
// put a junction in the middle of every task path. filepath.EvalSymlinks fails
// with ENOTDIR on such a path under this module's winsymlink semantics, so the
// repo checkout authorization refused every checkout ("not owned by the active
// task") and the workdir-reuse check silently declined every follow-up. These
// run on a real Windows filesystem in the windows-execenv CI job; createJunction
// lives in gc_junction_windows_test.go.

// junctionedWorkspacesRoot returns a workspaces root that is a junction to a
// real directory, plus that directory, the way the #8946 reporter set it up.
func junctionedWorkspacesRoot(t *testing.T) (root, target string) {
	t.Helper()
	target = t.TempDir()
	root = filepath.Join(t.TempDir(), "multica_workspaces")
	createJunction(t, target, root)
	return root, target
}

func TestAuthorizeRepoCheckoutWorkDirThroughJunctionedRoot(t *testing.T) {
	root, target := junctionedWorkspacesRoot(t)
	rel := filepath.Join("ws-1", "0123456789ab", "workdir")
	viaTarget := filepath.Join(target, rel)
	if err := os.MkdirAll(filepath.Join(viaTarget, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	viaRoot := filepath.Join(root, rel)
	wantWorkdir, err := filepath.EvalSymlinks(viaTarget)
	if err != nil {
		t.Fatalf("resolve target workdir: %v", err)
	}

	cases := []struct {
		name      string
		active    string
		requested string
		want      string
	}{
		{name: "both spelled through the junction", active: viaRoot, requested: viaRoot, want: wantWorkdir},
		{name: "request spelled through the junction target", active: viaRoot, requested: viaTarget, want: wantWorkdir},
		{name: "active workdir spelled through the junction target", active: viaTarget, requested: viaRoot, want: wantWorkdir},
		{name: "subdirectory through the junction", active: viaRoot, requested: filepath.Join(viaRoot, "sub"), want: filepath.Join(wantWorkdir, "sub")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := authorizeRepoCheckoutWorkDir(tc.active, tc.requested)
			if err != nil {
				t.Fatalf("authorizeRepoCheckoutWorkDir(%q, %q): %v", tc.active, tc.requested, err)
			}
			if !strings.EqualFold(got, tc.want) {
				t.Fatalf("authorized workdir = %q, want %q", got, tc.want)
			}
		})
	}
}

// A junction at the END of the requested path is the opposite failure: the old
// check resolved it to its own name, so a junction inside the workdir pointing
// elsewhere passed as inside. One pointing back inside the workdir (the pnpm
// shape) must keep passing.
func TestAuthorizeRepoCheckoutWorkDirFollowsJunctionsInsideTheWorkdir(t *testing.T) {
	workDir := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	escape := filepath.Join(workDir, "escape")
	createJunction(t, outside, escape)
	for _, requested := range []string{escape, filepath.Join(escape, "sub")} {
		got, err := authorizeRepoCheckoutWorkDir(workDir, requested)
		if err == nil {
			t.Fatalf("authorizeRepoCheckoutWorkDir accepted %q (resolved %q), a junction out of the workdir", requested, got)
		}
		if !strings.Contains(err.Error(), "outside the active task workdir") {
			t.Fatalf("rejection of %q should say it is outside the workdir, got: %v", requested, err)
		}
	}

	inside := filepath.Join(workDir, "real")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("mkdir inside: %v", err)
	}
	link := filepath.Join(workDir, "link")
	createJunction(t, inside, link)
	if _, err := authorizeRepoCheckoutWorkDir(workDir, link); err != nil {
		t.Fatalf("a junction that stays inside the workdir was refused: %v", err)
	}
}

// With the active workdir spelled in extended-length form (a workspaces_root
// configured as `\\?\C:\...`), both sides of the check are device-namespace
// strings. A resolver that hands those back unwalked compares them lexically,
// and a junction inside the workdir then escapes it; the old EvalSymlinks
// check refused that path, and so must this one.
func TestAuthorizeRepoCheckoutWorkDirExtendedLengthSpelling(t *testing.T) {
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	createJunction(t, outside, filepath.Join(workDir, "escape"))
	active := `\\?\` + workDir
	want, err := filepath.EvalSymlinks(filepath.Join(workDir, "sub"))
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}

	got, err := authorizeRepoCheckoutWorkDir(active, `\\?\`+filepath.Join(workDir, "sub"))
	if err != nil {
		t.Fatalf("an extended-length subdirectory of the workdir was refused: %v", err)
	}
	if !strings.EqualFold(got, want) {
		t.Fatalf("authorized workdir = %q, want %q", got, want)
	}

	for _, requested := range []string{
		`\\?\` + filepath.Join(workDir, "escape", "sub"),
		`\\?\` + filepath.Join(workDir, "missing"),
	} {
		if got, err := authorizeRepoCheckoutWorkDir(active, requested); err == nil {
			t.Fatalf("authorizeRepoCheckoutWorkDir(%q, %q) accepted %q", active, requested, got)
		}
	}
}

func TestShouldReusePriorWorkdirThroughJunctionedRoot(t *testing.T) {
	root, target := junctionedWorkspacesRoot(t)
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "agent-leader")
	want, err := filepath.EvalSymlinks(filepath.Join(target, "ws-leader", "12345678", "workdir"))
	if err != nil {
		t.Fatalf("resolve target workdir: %v", err)
	}

	task := leaderReuseTestTask("task-junction-root")
	task.PriorWorkDir = workDir
	got, ok := shouldReusePriorWorkdir(task, nil, root)
	if !ok {
		t.Fatalf("a fully provenanced workdir under a junctioned workspaces root was refused for reuse")
	}
	if !strings.EqualFold(got, want) {
		t.Fatalf("reused workdir = %q, want %q", got, want)
	}

	// The whole reuse path, lock included, has to hold up too: it opens the
	// root by its configured (junction) name and locks the resolved env root.
	d := &Daemon{logger: discardLogger()}
	d.cfg.WorkspacesRoot = root
	claim, canonical, _, ok, err := d.lockReusablePriorEnvRoot(context.Background(), task, nil, "")
	if err != nil || !ok {
		t.Fatalf("lockReusablePriorEnvRoot under a junctioned root: ok=%v err=%v", ok, err)
	}
	defer claim.Release()
	if !strings.EqualFold(canonical, want) {
		t.Fatalf("locked workdir = %q, want %q", canonical, want)
	}
}
