package execenv

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The guarantee here is behavioural, not textual: a sidecar Multica wrote into
// the user's own repository must be invisible to the git commands the AGENT
// runs, because that is what decides whether its `git add -A` sweeps them into
// the user's history (GitHub #7114). Asserting on the excludes file's bytes
// alone would pass for patterns git does not actually honour.
//
// Everything is scoped to the agent's environment, so every assertion runs git
// twice: once with that env (the agent's view) and once without (the user's).

// gitEnv runs git in dir with the task-scoped excludes environment applied.
func gitEnv(t *testing.T, dir string, env map[string]string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

func mkSidecarDir(t *testing.T, repo string, rel string) string {
	t.Helper()
	p := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	writeFile(t, filepath.Join(p, "SKILL.md"), "# skill\n")
	return p
}

func TestGitExcludesHideSidecarsFromTheAgent(t *testing.T) {
	repo := newTestRepo(t)
	envRoot := t.TempDir()
	skills := mkSidecarDir(t, repo, ".grok/skills/multica-working-on-issues")
	agentCtx := mkSidecarDir(t, repo, ".agent_context")
	multicaDir := mkSidecarDir(t, repo, ".multica")

	prot, err := PrepareGitExcludes(envRoot, repo, []string{skills, agentCtx, multicaDir})
	if err != nil {
		t.Fatalf("PrepareGitExcludes: %v", err)
	}
	if prot == nil || len(prot.Env) == 0 {
		t.Fatal("a git repository must produce protection env")
	}
	if err := prot.Verify(prot.Env); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	gitEnvVars := prot.Env

	if status := gitEnv(t, repo, gitEnvVars, "status", "--porcelain"); status != "" {
		t.Errorf("sidecars still visible to the agent's git:\n%s", status)
	}
	gitEnv(t, repo, gitEnvVars, "add", "-A")
	if staged := gitEnv(t, repo, gitEnvVars, "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("the agent's `git add -A` staged Multica runtime files:\n%s", staged)
	}
}

// The user's repository is theirs to see. Nothing we do may change what their
// own git reports, and nothing may be written into the repository or its git
// metadata.
func TestGitExcludesLeaveTheUsersOwnGitUntouched(t *testing.T) {
	repo := newTestRepo(t)
	envRoot := t.TempDir()
	sidecar := mkSidecarDir(t, repo, ".multica")

	excludeBefore, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))

	if _, err := PrepareGitExcludes(envRoot, repo, []string{sidecar}); err != nil {
		t.Fatalf("PrepareGitExcludes: %v", err)
	}

	if status := gitRun(t, repo, "status", "--porcelain"); !strings.Contains(status, ".multica") {
		t.Errorf("the user's own git should still show the sidecar; status:\n%s", status)
	}
	excludeAfter, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if string(excludeBefore) != string(excludeAfter) {
		t.Errorf(".git/info/exclude was modified:\n before: %q\n after: %q", excludeBefore, excludeAfter)
	}
	if _, err := os.Stat(filepath.Join(repo, gitExcludesFileName)); !os.IsNotExist(err) {
		t.Errorf("the excludes file must live in daemon scratch, not in the user's repo")
	}
}

// Elon review, must-fix 1. `.git/info/exclude` resolves to the repository's
// COMMON git dir, so an earlier revision of this leaked one task's patterns
// into every sibling worktree — hiding a parallel task's real output from its
// own `git add -A`. The task-scoped env must not reach the other worktree.
func TestGitExcludesDoNotLeakIntoSiblingWorktrees(t *testing.T) {
	repo := newTestRepo(t)
	envRoot := t.TempDir()
	sidecar := mkSidecarDir(t, repo, ".grok/skills/multica-a")

	sibling := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "sibling", sibling)
	t.Cleanup(func() { _, _ = gitTry(t, repo, "worktree", "remove", "--force", sibling) })

	// The other task's genuine output, at a path that happens to share our name.
	otherWork := filepath.Join(sibling, ".grok", "skills", "user-thing")
	if err := os.MkdirAll(otherWork, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(otherWork, "real-output.md"), "the other task's work\n")

	prot, err := PrepareGitExcludes(envRoot, repo, []string{sidecar})
	if err != nil {
		t.Fatalf("PrepareGitExcludes: %v", err)
	}

	if status := gitRun(t, sibling, "status", "--porcelain", "-uall"); !strings.Contains(status, "real-output.md") {
		t.Errorf("the sibling worktree lost sight of its own output; status:\n%s", status)
	}
	gitRun(t, sibling, "add", "-A")
	if staged := gitRun(t, sibling, "diff", "--cached", "--name-only"); !strings.Contains(staged, "real-output.md") {
		t.Errorf("the sibling worktree could not stage its own output; staged:\n%s", staged)
	}
	_ = prot
}

// Elon review, must-fix 3. A local_directory resource keeps the path the user
// typed; git reports the resolved one. Comparing them raw dropped every
// pattern and returned success, leaving the run silently unprotected.
func TestGitExcludesThroughASymlinkedResourcePath(t *testing.T) {
	repo := newTestRepo(t)
	envRoot := t.TempDir()
	link := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(repo, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// Sidecar paths carry the unresolved spelling, as Prepare would produce.
	sidecar := filepath.Join(link, ".multica")
	if err := os.MkdirAll(sidecar, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(sidecar, "marker.json"), "{}\n")

	prot, err := PrepareGitExcludes(envRoot, link, []string{sidecar})
	if err != nil {
		t.Fatalf("PrepareGitExcludes: %v", err)
	}
	gitEnvVars := prot.Env
	if status := gitEnv(t, link, gitEnvVars, "status", "--porcelain"); status != "" {
		t.Errorf("sidecar reached through a symlinked resource path is unprotected:\n%s", status)
	}
	gitEnv(t, link, gitEnvVars, "add", "-A")
	if staged := gitEnv(t, link, gitEnvVars, "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("`git add -A` staged Multica files through a symlinked path:\n%s", staged)
	}
}

// Elon review, must-fix 4. Dropping a pattern must never be silent: an
// unprotected run in a real repository is the reported bug.
func TestGitExcludesFailClosedWhenAPathCannotBeExpressed(t *testing.T) {
	repo := newTestRepo(t)
	envRoot := t.TempDir()
	outside := filepath.Join(t.TempDir(), "elsewhere")

	_, err := PrepareGitExcludes(envRoot, repo, []string{outside})
	if !errors.Is(err, ErrGitExcludesUnprotected) {
		t.Fatalf("expected ErrGitExcludesUnprotected, got %v", err)
	}
}

func TestGitExcludesFailClosedWithoutScratchDirectory(t *testing.T) {
	repo := newTestRepo(t)
	sidecar := mkSidecarDir(t, repo, ".multica")

	_, err := PrepareGitExcludes("", repo, []string{sidecar})
	if !errors.Is(err, ErrGitExcludesUnprotected) {
		t.Fatalf("expected ErrGitExcludesUnprotected, got %v", err)
	}
}

// A plain folder has nothing to protect, so it is a no-op rather than a
// failure — local_directory resources are allowed to point at one.
func TestGitExcludesNoOpOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	envRoot := t.TempDir()
	sidecar := mkSidecarDir(t, dir, ".multica")

	prot, err := PrepareGitExcludes(envRoot, dir, []string{sidecar})
	if err != nil {
		t.Fatalf("a plain folder must be a no-op, got: %v", err)
	}
	if prot != nil {
		t.Errorf("a plain folder must produce no protection, got %v", prot.Env)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Errorf("must not create a git repository in the user's plain folder")
	}
}

// Overriding core.excludesFile must not make the agent start seeing files the
// user globally ignores.
func TestGitExcludesPreserveTheUsersGlobalIgnores(t *testing.T) {
	repo := newTestRepo(t)
	envRoot := t.TempDir()
	// Set it where a user actually would — their GLOBAL config — not in the
	// repository. A repo-local core.excludesFile outranks anything a global
	// config can say, including ours; Verify is what turns that into a failed
	// task rather than a silently unprotected run.
	xdg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(xdg, "git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	globalIgnore := filepath.Join(xdg, "git", "ignore")
	writeFile(t, globalIgnore, "*.scratch\n")
	writeFile(t, filepath.Join(xdg, "git", "config"), "[core]\n\texcludesFile = "+globalIgnore+"\n")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	sidecar := mkSidecarDir(t, repo, ".multica")
	writeFile(t, filepath.Join(repo, "notes.scratch"), "personal\n")

	prot, err := PrepareGitExcludes(envRoot, repo, []string{sidecar})
	if err != nil {
		t.Fatalf("PrepareGitExcludes: %v", err)
	}
	gitEnvVars := prot.Env
	if status := gitEnv(t, repo, gitEnvVars, "status", "--porcelain"); status != "" {
		t.Errorf("the user's own global ignores stopped applying for the agent:\n%s", status)
	}
}

// Patterns are anchored at the repository root, so a directory the user names
// the same way deeper in the tree keeps showing up.
func TestGitExcludePatternsAnchorAtRepoRoot(t *testing.T) {
	repo := newTestRepo(t)
	envRoot := t.TempDir()
	sidecar := mkSidecarDir(t, repo, ".multica")
	mkSidecarDir(t, repo, "docs/.multica")

	prot, err := PrepareGitExcludes(envRoot, repo, []string{sidecar})
	if err != nil {
		t.Fatalf("PrepareGitExcludes: %v", err)
	}
	gitEnvVars := prot.Env
	// -uall so git lists files individually; the default collapses a wholly
	// untracked directory to "docs/" and would hide what is being asserted.
	status := gitEnv(t, repo, gitEnvVars, "status", "--porcelain", "-uall")
	if !strings.Contains(status, "docs/.multica") {
		t.Errorf("an unanchored pattern swallowed the user's docs/.multica; status:\n%s", status)
	}
}

// in_place resources may point at a subdirectory of a repository, so patterns
// have to be written relative to the repo root, not to the workdir.
func TestGitExcludesFromRepoSubdirectory(t *testing.T) {
	repo := newTestRepo(t)
	envRoot := t.TempDir()
	sub := filepath.Join(repo, "packages", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sidecar := mkSidecarDir(t, repo, "packages/api/.multica")

	prot, err := PrepareGitExcludes(envRoot, sub, []string{sidecar})
	if err != nil {
		t.Fatalf("PrepareGitExcludes: %v", err)
	}
	gitEnvVars := prot.Env
	if status := gitEnv(t, repo, gitEnvVars, "status", "--porcelain"); status != "" {
		t.Errorf("sidecar in a subdirectory still visible to the agent:\n%s", status)
	}
}

func TestGitExcludePatternsMarksDirectoriesAndSorts(t *testing.T) {
	repo := newTestRepo(t)
	dir := mkSidecarDir(t, repo, ".zzz-dir")
	file := filepath.Join(repo, ".aaa-file")
	writeFile(t, file, "x")

	got, dropped := gitExcludePatterns(canonicalPath(repo), []string{dir, file})
	if dropped != 0 {
		t.Fatalf("dropped %d in-repo paths", dropped)
	}
	want := []string{"/.aaa-file", "/.zzz-dir/"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pattern %d: got %q, want %q (dirs need a trailing slash, output must be sorted)", i, got[i], want[i])
		}
	}
}

// Elon review, must-fix 2. A repository already polluted by #7114 carries
// committed sidecars whose files were later deleted: absent from the working
// tree, still in the index. Writing there again modifies a TRACKED file, which
// no excludes file can hide.
func TestGitTrackedFilesUnderFindsCommittedThenDeletedSidecars(t *testing.T) {
	repo := newTestRepo(t)
	skillDir := mkSidecarDir(t, repo, ".grok/skills/multica-squads")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-m", "the bug: sidecars committed")
	// Cleanup removed them from the working tree, as the old flow did.
	if err := os.RemoveAll(filepath.Join(repo, ".grok")); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Fatalf("precondition: the directory should be gone from disk")
	}

	tracked, err := GitTrackedFilesUnder(repo, []string{filepath.Join(repo, ".grok", "skills")})
	if err != nil {
		t.Fatalf("GitTrackedFilesUnder: %v", err)
	}
	if len(tracked) == 0 {
		t.Fatal("a committed-then-deleted sidecar must still be reported as tracked")
	}

	m := &sidecarManifest{reserved: tracked}
	if !m.isReserved(skillDir) {
		t.Errorf("the tracked skill directory must be reserved, got reserved=%v", tracked)
	}
	if m.isReserved(filepath.Join(repo, ".grok", "skills", "something-else")) {
		t.Errorf("an untracked sibling must not be reserved")
	}
}

func TestGitTrackedFilesUnderIsEmptyForACleanRepo(t *testing.T) {
	repo := newTestRepo(t)
	tracked, err := GitTrackedFilesUnder(repo, []string{filepath.Join(repo, ".multica")})
	if err != nil || len(tracked) != 0 {
		t.Errorf("a repo that never carried sidecars must report none, got %v (err %v)", tracked, err)
	}
	tracked, err = GitTrackedFilesUnder(t.TempDir(), []string{"anything"})
	if err != nil || len(tracked) != 0 {
		t.Errorf("a non-git folder must report none, got %v (err %v)", tracked, err)
	}
}

// The manifest records every directory it created, including nested ones.
// Emitting all of them would write a dozen redundant lines describing one
// subtree; only the shallowest created path should survive.
func TestExcludablePathsReturnsMinimalCoveringSet(t *testing.T) {
	root := filepath.FromSlash("/repo")
	m := sidecarManifest{
		Dirs: []string{
			filepath.Join(root, ".grok"),
			filepath.Join(root, ".grok", "skills"),
			filepath.Join(root, ".grok", "skills", "multica-squads"),
			filepath.Join(root, ".multica"),
		},
		Files: []string{
			filepath.Join(root, ".grok", "skills", "multica-squads", "SKILL.md"),
			filepath.Join(root, ".multica", "project", "resources.json"),
			filepath.Join(root, "AGENTS.md"),
		},
	}

	got := m.excludablePaths()
	sort.Strings(got)
	want := []string{
		filepath.Join(root, ".grok"),
		filepath.Join(root, ".multica"),
		filepath.Join(root, "AGENTS.md"),
	}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// When the user already owns the parent directory (their own .grok/skills),
// only the specific directories we created may be covered, so their siblings
// stay visible.
func TestExcludablePathsKeepsUserOwnedParentsVisible(t *testing.T) {
	root := filepath.FromSlash("/repo")
	m := sidecarManifest{
		Dirs: []string{
			filepath.Join(root, ".grok", "skills", "multica-squads"),
			filepath.Join(root, ".grok", "skills", "multica-mentioning"),
		},
	}

	got := m.excludablePaths()
	if len(got) != 2 {
		t.Fatalf("got %v, want both Multica skill dirs listed individually", got)
	}
	for _, p := range got {
		if filepath.Base(filepath.Dir(p)) != "skills" {
			t.Errorf("path %q escaped up into a directory the user owns", p)
		}
	}
}

// Elon review round 2, must-fix 1. The protection rides in the agent's
// environment, which is assembled from several layers — an operator's
// custom_env among them. Reasoning about precedence is not enough, so Verify
// asks git directly, with the exact environment the child will get.
func TestVerifyRejectsAnEnvironmentThatDisarmsTheProtection(t *testing.T) {
	repo := newTestRepo(t)
	envRoot := t.TempDir()
	sidecar := mkSidecarDir(t, repo, ".multica")

	prot, err := PrepareGitExcludes(envRoot, repo, []string{sidecar})
	if err != nil {
		t.Fatalf("PrepareGitExcludes: %v", err)
	}
	if err := prot.Verify(prot.Env); err != nil {
		t.Fatalf("the untampered environment must verify: %v", err)
	}

	// git's own GIT_CONFIG_* entries outrank config files, so this wins over
	// our includeIf no matter which layer applied last.
	for _, tampered := range []map[string]string{
		{"GIT_CONFIG_GLOBAL": filepath.Join(t.TempDir(), "empty")},
		{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.excludesFile", "GIT_CONFIG_VALUE_0": filepath.Join(t.TempDir(), "nothing")},
	} {
		env := map[string]string{}
		for k, v := range prot.Env {
			env[k] = v
		}
		for k, v := range tampered {
			env[k] = v
		}
		if err := prot.Verify(env); !errors.Is(err, ErrGitExcludesUnprotected) {
			t.Errorf("Verify accepted a disarmed environment %v, got err=%v", tampered, err)
		}
	}
}

// Pre-existing GIT_CONFIG_* tuples set by the daemon or the user must survive:
// the protection no longer claims index 0, so nothing it does can displace them.
func TestGitExcludesPreserveExistingGitConfigTuples(t *testing.T) {
	repo := newTestRepo(t)
	envRoot := t.TempDir()
	sidecar := mkSidecarDir(t, repo, ".multica")

	prot, err := PrepareGitExcludes(envRoot, repo, []string{sidecar})
	if err != nil {
		t.Fatalf("PrepareGitExcludes: %v", err)
	}
	for k := range prot.Env {
		if strings.HasPrefix(k, "GIT_CONFIG_COUNT") || strings.HasPrefix(k, "GIT_CONFIG_KEY_") || strings.HasPrefix(k, "GIT_CONFIG_VALUE_") {
			t.Fatalf("the protection must not claim GIT_CONFIG_* tuple slots, got %q", k)
		}
	}

	env := map[string]string{
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "user.name",
		"GIT_CONFIG_VALUE_0": "Pre Existing",
	}
	for k, v := range prot.Env {
		env[k] = v
	}
	if err := prot.Verify(env); err != nil {
		t.Errorf("an unrelated pre-existing tuple must not break the protection: %v", err)
	}
	if got := gitEnv(t, repo, env, "config", "--get", "user.name"); got != "Pre Existing" {
		t.Errorf("pre-existing tuple lost: user.name = %q", got)
	}
}

// Elon review round 2, must-fix 2. core.excludesFile is process-global, so an
// agent that steps into a DIFFERENT repository mid-task would have had the
// same patterns applied there — hiding that repository's genuine files from
// its own `git add -A`. The conditional include confines them to this repo.
func TestGitExcludesDoNotReachAnotherRepositoryTheAgentVisits(t *testing.T) {
	repo := newTestRepo(t)
	envRoot := t.TempDir()
	sidecar := mkSidecarDir(t, repo, ".multica")

	other := newTestRepo(t)
	otherWork := mkSidecarDir(t, other, ".multica")
	writeFile(t, filepath.Join(otherWork, "genuine.json"), "the agent's real work\n")

	prot, err := PrepareGitExcludes(envRoot, repo, []string{sidecar})
	if err != nil {
		t.Fatalf("PrepareGitExcludes: %v", err)
	}

	// Same agent environment, second repository.
	if status := gitEnv(t, other, prot.Env, "status", "--porcelain", "-uall"); !strings.Contains(status, "genuine.json") {
		t.Errorf("the other repository's real files were hidden by this task's patterns; status:\n%s", status)
	}
	gitEnv(t, other, prot.Env, "add", "-A")
	if staged := gitEnv(t, other, prot.Env, "diff", "--cached", "--name-only"); !strings.Contains(staged, "genuine.json") {
		t.Errorf("the other repository could not stage its own files; staged:\n%s", staged)
	}
	// And the target repository is still protected.
	if status := gitEnv(t, repo, prot.Env, "status", "--porcelain"); status != "" {
		t.Errorf("target repository unprotected:\n%s", status)
	}
}

// Elon review round 2, must-fix 3. The tracked-path scan runs BEFORE any
// sidecar exists, so its inputs are paths EvalSymlinks cannot resolve. Falling
// back to the raw path there skipped the scan entirely for a symlinked
// resource, and isReserved then compared unresolved paths against git's
// resolved output.
func TestTrackedSidecarsAreFoundThroughASymlinkedResourcePath(t *testing.T) {
	repo := newTestRepo(t)
	mkSidecarDir(t, repo, ".grok/skills/multica-squads")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-m", "the bug: sidecars committed")
	if err := os.RemoveAll(filepath.Join(repo, ".grok")); err != nil {
		t.Fatalf("rm: %v", err)
	}

	link := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(repo, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// Roots as Prepare builds them: through the link, and not yet on disk.
	tracked, err := GitTrackedFilesUnder(link, SidecarScanRoots(link, "grok"))
	if err != nil {
		t.Fatalf("GitTrackedFilesUnder: %v", err)
	}
	if len(tracked) == 0 {
		t.Fatal("the scan missed committed sidecars reached through a symlinked resource path")
	}

	// And the reservation must match the unresolved path Prepare would write.
	m := &sidecarManifest{reserved: tracked}
	if !m.isReserved(filepath.Join(link, ".grok", "skills", "multica-squads")) {
		t.Errorf("isReserved failed to match the unresolved write path against git's resolved output")
	}
}

// SidecarScanRoots is the scan's coverage: a sidecar target missing from it is
// a path the scan cannot protect.
func TestSidecarScanRootsCoverEverySidecarTarget(t *testing.T) {
	workDir := t.TempDir()
	roots := SidecarScanRoots(workDir, "cursor")

	for _, want := range []string{".agent_context", ".multica", ".cursor", reasonixProjectConfigFile} {
		found := false
		for _, r := range roots {
			if filepath.Base(r) == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("scan roots miss %q; a sidecar written there could not be protected", want)
		}
	}
	// The provider's own skills tree has to be in there too.
	skills := canonicalPath(skillsDirPath(workDir, "cursor"))
	found := false
	for _, r := range roots {
		if r == skills {
			found = true
		}
	}
	if !found {
		t.Errorf("scan roots miss the runtime skills dir %q", skills)
	}
}
