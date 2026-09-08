package execenv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runWorkspaceFixture(t *testing.T) (string, string) {
	t.Helper()
	source := t.TempDir()
	git := func(args ...string) string {
		out, err := gitRunWorkspace(context.Background(), source, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	git("init", "--quiet")
	for name, contents := range map[string]string{"tracked.txt": "accepted base", ".gitignore": "secret.env\n"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "-m", "fixture")
	commit := git("rev-parse", "HEAD")
	for name, contents := range map[string]string{"tracked.txt": "owner dirty edits", "untracked.txt": "owner untracked", "secret.env": "ignored sentinel"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	real, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	return real, commit
}

func ownedParams(root, source, commit string, n int) PrepareParams {
	return PrepareParams{WorkspacesRoot: root, WorkspaceID: "owned-workspace", TaskID: fmt.Sprintf("00000000-0000-4000-8000-%012d", n),
		Task:         TaskContextForEnv{IssueID: fmt.Sprintf("issue-%d", n), AgentID: "same-specialist"},
		RunWorkspace: &RunWorkspaceParams{SourcePath: source, BaseCommit: commit, HostID: "test-host", RuntimeID: "same-runtime"}}
}

func TestRunWorkspaceCleanBasePreservesOwner(t *testing.T) {
	source, commit := runWorkspaceFixture(t)
	before, err := gitRunWorkspace(context.Background(), source, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		t.Fatal(err)
	}
	refs, err := gitRunWorkspace(context.Background(), source, "show-ref")
	if err != nil {
		t.Fatal(err)
	}
	params := ownedParams(t.TempDir(), source, commit, 1)
	env, err := Prepare(params, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer env.Cleanup(true)
	got, err := os.ReadFile(filepath.Join(env.WorkDir, "tracked.txt"))
	if err != nil || string(got) != "accepted base" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, name := range []string{"untracked.txt", "secret.env"} {
		if _, err := os.Stat(filepath.Join(env.WorkDir, name)); !os.IsNotExist(err) {
			t.Fatalf("owner file copied: %s", name)
		}
	}
	after, _ := gitRunWorkspace(context.Background(), source, "status", "--porcelain=v1", "--untracked-files=all")
	afterRefs, _ := gitRunWorkspace(context.Background(), source, "show-ref")
	if after != before || afterRefs != refs {
		t.Fatal("owner checkout or refs changed")
	}
	var receipt RunWorkspaceReceipt
	b, err := os.ReadFile(filepath.Join(env.RootDir, "run-workspace.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.RunID != params.TaskID || receipt.BaseCommit != commit || receipt.WorkDir != env.WorkDir {
		t.Fatalf("incorrect receipt: %+v", receipt)
	}
	t.Logf("owner content/ref preservation passed; run=%s branch=%s session_owner=%s", receipt.RunID, receipt.Branch, receipt.SessionOwner)
}

func TestRunWorkspaceScratchAndCrashPreservation(t *testing.T) {
	source, _ := runWorkspaceFixture(t)
	params := ownedParams(t.TempDir(), source, "", 2)
	env, err := Prepare(params, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(env.WorkDir, "tracked.txt")); !os.IsNotExist(err) {
		t.Fatal("scratch copied source")
	}
	partial := filepath.Join(env.WorkDir, "partial.txt")
	if err := os.WriteFile(partial, []byte("keep interrupted work"), 0600); err != nil {
		t.Fatal(err)
	}
	env.ReleaseLock() // simulate process exit, without destructive cleanup
	if _, err := Prepare(params, discardLogger()); err == nil || !strings.Contains(err.Error(), "reconcile") {
		t.Fatalf("redelivery should require reconciliation: %v", err)
	}
	b, err := os.ReadFile(partial)
	if err != nil || string(b) != "keep interrupted work" {
		t.Fatal("partial work erased")
	}
}

func TestRunWorkspaceRejectsMutableBaseAndEscapedRoom(t *testing.T) {
	source, _ := runWorkspaceFixture(t)
	for _, base := range []string{"main", "HEAD", strings.Repeat("f", 39), "--upload-pack=evil"} {
		params := ownedParams(t.TempDir(), source, base, 3)
		if _, err := Prepare(params, discardLogger()); err == nil {
			t.Fatalf("accepted base %q", base)
		}
	}
	root := t.TempDir()
	if err := prepareRunWorkspace(ownedParams(root, source, "", 4), root, t.TempDir()); err == nil {
		t.Fatal("accepted room outside root")
	}
}

func TestRunWorkspaceFailedAllocationKeepsReceipt(t *testing.T) {
	source, _ := runWorkspaceFixture(t)
	params := ownedParams(t.TempDir(), source, strings.Repeat("0", 40), 5)
	if _, err := Prepare(params, discardLogger()); err == nil {
		t.Fatal("accepted missing commit")
	}
	root, err := ResolveRootDir(RootDirParams{WorkspacesRoot: params.WorkspacesRoot, WorkspaceID: params.WorkspaceID, TaskID: params.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "run-workspace.json")); err != nil {
		t.Fatal("lost failed allocation receipt", err)
	}
}

// A test-created fake provider implemented by this test binary. It never
// resolves an ambient provider CLI or reads an account. The parent verifies
// actual simultaneous OS processes and cancellation in separate run rooms.
func TestRunWorkspaceProviderProcess(t *testing.T) {
	if os.Getenv("MULTICA_TEST_OWNED_CHILD") != "1" {
		return
	}
	if err := os.WriteFile("ready", []byte(os.Getenv("MULTICA_TEST_SESSION")), 0600); err != nil {
		os.Exit(2)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat("release"); err == nil {
			if err := os.WriteFile("result", []byte(os.Getenv("MULTICA_TEST_SESSION")), 0600); err != nil {
				os.Exit(3)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(4)
}

func TestRunWorkspaceSameSpecialistProcessesAndCancellation(t *testing.T) {
	source, _ := runWorkspaceFixture(t)
	root := t.TempDir()
	var envs []*Environment
	var commands []*exec.Cmd
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	for n := 0; n < 4; n++ {
		params := ownedParams(root, source, "", 10+n)
		env, err := Prepare(params, discardLogger())
		if err != nil {
			t.Fatal(err)
		}
		envs = append(envs, env)
		defer env.Cleanup(true)
		cmd := exec.Command(executable, "-test.run=^TestRunWorkspaceProviderProcess$")
		cmd.Dir = env.WorkDir
		cmd.Env = append(os.Environ(), "MULTICA_TEST_OWNED_CHILD=1", "MULTICA_TEST_SESSION="+params.TaskID)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Logf("run=%s profile=%s runtime=%s pid=%d workdir=%s", params.TaskID, params.Task.AgentID, params.RunWorkspace.RuntimeID, cmd.Process.Pid, env.WorkDir)
		commands = append(commands, cmd)
		t.Cleanup(func() { _ = cmd.Process.Kill() })
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, env := range envs {
		for {
			if _, err := os.Stat(filepath.Join(env.WorkDir, "ready")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("burst exceeded 5s")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err := commands[0].Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = commands[0].Wait()
	for n := 1; n < len(commands); n++ {
		if err := os.WriteFile(filepath.Join(envs[n].WorkDir, "release"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := commands[n].Wait(); err != nil {
			t.Fatal("sibling process failed", err)
		}
		got, err := os.ReadFile(filepath.Join(envs[n].WorkDir, "result"))
		if err != nil || string(got) != ownedParams(root, source, "", 10+n).TaskID {
			t.Fatal("result misrouted", err)
		}
	}
	t.Logf("4/4 real fake-provider processes overlapped in distinct rooms; 1 selected cancellation; 3/3 survivor receipts; elapsed=%s", time.Since(started))
}
