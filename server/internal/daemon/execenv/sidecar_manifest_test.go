package execenv

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// walkRelative returns every relative path inside root (files and directories),
// sorted, with a `dir/` suffix on directories so dir-vs-file mismatches show up
// in the diff. The root itself is reported as "." so a fully-empty directory
// still surfaces a non-empty fingerprint and an empty-vs-missing comparison
// fails loudly instead of looking identical.
func walkRelative(t *testing.T, root string) []string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			entries = append(entries, ".")
			return nil
		}
		if d.IsDir() {
			entries = append(entries, rel+string(os.PathSeparator))
			return nil
		}
		entries = append(entries, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(entries)
	return entries
}

// snapshotWorkdir captures both the directory listing and the content of every
// regular file inside root, so a round-trip assertion can compare "exactly the
// same bytes everywhere" — not just "no orphan files survived". An empty map
// represents an empty workdir.
type workdirSnapshot struct {
	entries []string
	files   map[string]string
}

func snapshot(t *testing.T, root string) workdirSnapshot {
	t.Helper()
	snap := workdirSnapshot{files: map[string]string{}}
	snap.entries = walkRelative(t, root)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		snap.files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walk-for-content %s: %v", root, err)
	}
	return snap
}

func assertSnapshotEqual(t *testing.T, label string, want, got workdirSnapshot) {
	t.Helper()
	if !reflect.DeepEqual(want.entries, got.entries) {
		t.Errorf("[%s] directory listing differs\n want: %v\n  got: %v", label, want.entries, got.entries)
	}
	if !reflect.DeepEqual(want.files, got.files) {
		// Find a small diff first so the failure is actionable.
		for k, wv := range want.files {
			gv, ok := got.files[k]
			if !ok {
				t.Errorf("[%s] missing file %s after round-trip", label, k)
				continue
			}
			if wv != gv {
				t.Errorf("[%s] file %s differs\n want: %q\n  got: %q", label, k, wv, gv)
			}
		}
		for k := range got.files {
			if _, ok := want.files[k]; !ok {
				t.Errorf("[%s] orphan file %s after round-trip", label, k)
			}
		}
	}
}

// runPrepareLikeCycle replays the daemon's local_directory path against the
// supplied workDir and envRoot: writes context files (with manifest tracking),
// injects the runtime brief, then runs the matching cleanups. Tests use this
// to assert byte-exact reversibility without booting the full Prepare/Reuse
// pipeline (which would need a WorkspacesRoot, GC plumbing, etc.).
func runPrepareLikeCycle(t *testing.T, workDir, envRoot, provider string, ctx TaskContextForEnv) {
	t.Helper()
	manifest := &sidecarManifest{}
	if err := writeContextFiles(workDir, provider, ctx, manifest); err != nil {
		t.Fatalf("writeContextFiles(%s): %v", provider, err)
	}
	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatalf("writeSidecarManifest(%s): %v", provider, err)
	}
	if _, err := InjectRuntimeConfig(workDir, provider, ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig(%s): %v", provider, err)
	}
	// Mirror daemon.go ordering: runtime config first, sidecars second. The
	// order is incidental — neither cleanup touches the other's paths — but
	// pinning the same order in tests catches an accidental coupling.
	if err := CleanupRuntimeConfig(workDir, provider); err != nil {
		t.Fatalf("CleanupRuntimeConfig(%s): %v", provider, err)
	}
	if err := CleanupSidecars(envRoot); err != nil {
		t.Fatalf("CleanupSidecars(%s): %v", provider, err)
	}
}

// allFileBasedProviders lists every provider whose `writeContextFiles` /
// `InjectRuntimeConfig` writes into the user's workDir. Codex is included
// because it still writes AGENTS.md + .agent_context/ into the workdir (its
// skills live in codex-home, but that's not in workdir and is out of scope
// for this manifest). Adding a new provider that writes into workDir must
// also add it here so the round-trip invariant is enforced for it on day
// one — review the test diff before merging.
var allFileBasedProviders = []string{
	"claude",
	"codex",
	"copilot",
	"opencode",
	"openclaw",
	"hermes",
	"pi",
	"cursor",
	"kimi",
	"kiro",
	"antigravity",
	"gemini",
}

// TestPrepareThenCleanupSidecarsRoundTripEmptyWorkdir is the headline
// invariant the issue (MUL-2784) calls out: a user repo that contained
// nothing related to Multica before a task ran must contain nothing
// related to Multica after the task finishes — no .agent_context/,
// no .claude/skills/, no .multica/, no stub directories. The test
// runs the full Prepare → Inject → Cleanup cycle for every file-based
// provider against a fresh empty workdir and asserts the directory is
// byte-exactly empty again.
func TestPrepareThenCleanupSidecarsRoundTripEmptyWorkdir(t *testing.T) {
	t.Parallel()
	for _, provider := range allFileBasedProviders {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()
			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
				AgentSkills: []SkillContextForEnv{
					{
						Name:        "Issue Review",
						Description: "Review GH issues",
						Content:     "Steps to review",
						Files: []SkillFileContextForEnv{
							{Path: "templates/checklist.md", Content: "- [ ] check"},
						},
					},
					{
						Name:    "PR Review",
						Content: "Review PR diffs",
					},
				},
				ProjectID:    "proj-1",
				ProjectTitle: "Demo",
			}

			runPrepareLikeCycle(t, workDir, envRoot, provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, provider, before, after)
		})
	}
}

// TestPrepareThenCleanupSidecarsPreservesUserSkillSibling pins the
// non-destructive contract from the issue: if the user already keeps a
// hand-authored skill under the same parent directory we use, Cleanup
// must leave it bit-for-bit intact. The user-skill payload is laid down
// BEFORE Prepare runs and snapshotted; after Cleanup the user's skill
// must still exist and the Multica-written sibling must be gone.
func TestPrepareThenCleanupSidecarsPreservesUserSkillSibling(t *testing.T) {
	t.Parallel()
	// One representative case per provider that writes into a
	// provider-native skill directory. Gemini and Hermes don't have a
	// native discovery path; they fall back to .agent_context/skills/,
	// which is also covered (a user-created sibling under there should
	// also survive). Codex is intentionally excluded — its workspace
	// skills don't live in workdir, so the "user skill sibling"
	// scenario doesn't apply.
	cases := []struct {
		provider      string
		userSkillRel  string // path under workDir
		userSkillFile string // path under userSkillRel
	}{
		{"claude", filepath.Join(".claude", "skills", "my-own"), "SKILL.md"},
		{"copilot", filepath.Join(".github", "skills", "my-own"), "SKILL.md"},
		{"opencode", filepath.Join(".opencode", "skills", "my-own"), "SKILL.md"},
		{"openclaw", filepath.Join("skills", "my-own"), "SKILL.md"},
		{"pi", filepath.Join(".pi", "skills", "my-own"), "SKILL.md"},
		{"cursor", filepath.Join(".cursor", "skills", "my-own"), "SKILL.md"},
		{"kimi", filepath.Join(".kimi", "skills", "my-own"), "SKILL.md"},
		{"kiro", filepath.Join(".kiro", "skills", "my-own"), "SKILL.md"},
		{"antigravity", filepath.Join(".agents", "skills", "my-own"), "SKILL.md"},
		{"hermes", filepath.Join(".agent_context", "skills", "my-own"), "SKILL.md"},
		{"gemini", filepath.Join(".agent_context", "skills", "my-own"), "SKILL.md"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()

			userDir := filepath.Join(workDir, tc.userSkillRel)
			if err := os.MkdirAll(userDir, 0o755); err != nil {
				t.Fatalf("seed user skill dir: %v", err)
			}
			userBody := "---\nname: my-own\n---\n\nUser-authored.\n"
			if err := os.WriteFile(filepath.Join(userDir, tc.userSkillFile), []byte(userBody), 0o644); err != nil {
				t.Fatalf("seed user skill file: %v", err)
			}

			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
				AgentSkills: []SkillContextForEnv{
					{Name: "Issue Review", Content: "ours"},
				},
			}
			runPrepareLikeCycle(t, workDir, envRoot, tc.provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, tc.provider, before, after)

			// Defensive: independently re-read the user skill to make
			// sure no clever cleanup heuristic stripped its content.
			got, err := os.ReadFile(filepath.Join(userDir, tc.userSkillFile))
			if err != nil {
				t.Fatalf("user skill went missing after round-trip: %v", err)
			}
			if string(got) != userBody {
				t.Errorf("user skill content changed\n want: %q\n  got: %q", userBody, string(got))
			}
		})
	}
}

// TestPrepareThenCleanupSidecarsPreservesUnrelatedUserFiles covers the
// case where the user keeps a non-skill file under a parent we end up
// using — for example `.claude/config.json` next to where we drop
// `.claude/skills/`. Cleanup must rmdir only the directories it created;
// pre-existing siblings (and their parents) must survive.
func TestPrepareThenCleanupSidecarsPreservesUnrelatedUserFiles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider string
		userFile string // path under workDir
	}{
		{"claude", filepath.Join(".claude", "settings.json")},
		{"copilot", filepath.Join(".github", "CODEOWNERS")},
		{"opencode", filepath.Join(".opencode", "config.json")},
		{"pi", filepath.Join(".pi", "config.toml")},
		{"cursor", filepath.Join(".cursor", "settings.json")},
		{"kimi", filepath.Join(".kimi", "config.json")},
		{"kiro", filepath.Join(".kiro", "config.json")},
		{"antigravity", filepath.Join(".agents", "config.json")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()

			userPath := filepath.Join(workDir, tc.userFile)
			if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
				t.Fatalf("seed user dir: %v", err)
			}
			userBody := "user content " + tc.provider
			if err := os.WriteFile(userPath, []byte(userBody), 0o644); err != nil {
				t.Fatalf("seed user file: %v", err)
			}

			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
				AgentSkills: []SkillContextForEnv{
					{Name: "Issue Review", Content: "ours"},
				},
			}
			runPrepareLikeCycle(t, workDir, envRoot, tc.provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, tc.provider, before, after)
		})
	}
}

// TestPrepareThenCleanupSidecarsRepeatedCycles guards against the
// failure mode the issue describes most explicitly — every run
// accumulates one more directory layer than the last. Running the cycle
// twice in a row must leave the workdir in the same state as running it
// once (which is the seed state), with the manifest correctly
// regenerated each cycle.
func TestPrepareThenCleanupSidecarsRepeatedCycles(t *testing.T) {
	t.Parallel()
	for _, provider := range allFileBasedProviders {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()
			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
				AgentSkills: []SkillContextForEnv{
					{Name: "Issue Review", Content: "ours"},
				},
			}
			for i := 0; i < 3; i++ {
				runPrepareLikeCycle(t, workDir, envRoot, provider, ctx)
				after := snapshot(t, workDir)
				assertSnapshotEqual(t, provider, before, after)
			}
		})
	}
}

// TestPrepareThenCleanupSidecarsWithProjectResources extends the
// round-trip to the .multica/project/resources.json branch — a separate
// sidecar write that creates its own intermediate directory tree.
func TestPrepareThenCleanupSidecarsWithProjectResources(t *testing.T) {
	t.Parallel()
	for _, provider := range allFileBasedProviders {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()
			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID:      "11111111-2222-3333-4444-555555555555",
				ProjectID:    "proj-1",
				ProjectTitle: "Demo project",
				ProjectResources: []ProjectResourceForEnv{
					{
						ID:           "res-1",
						ResourceType: "github_repo",
						ResourceRef:  []byte(`{"url":"https://github.com/example/repo"}`),
					},
				},
			}
			runPrepareLikeCycle(t, workDir, envRoot, provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, provider, before, after)
		})
	}
}

// TestCleanupSidecarsNoOpWhenManifestMissing pins backward compatibility
// with envRoots that predate the manifest mechanism (older daemons, GC'd
// scratch dirs, fresh tempdirs). Cleanup must be a silent no-op rather
// than an error when there's nothing to clean.
func TestCleanupSidecarsNoOpWhenManifestMissing(t *testing.T) {
	t.Parallel()
	envRoot := t.TempDir()
	if err := CleanupSidecars(envRoot); err != nil {
		t.Errorf("CleanupSidecars on empty envRoot returned error: %v", err)
	}
	if err := CleanupSidecars(""); err != nil {
		t.Errorf("CleanupSidecars with empty envRoot returned error: %v", err)
	}
}

// TestCleanupSidecarsLeavesUserContentInTrackedDirIntact is the
// directed unit test for the "non-empty rmdir is silently skipped"
// branch. We build a manifest by hand that claims ownership of a
// directory the user later populated; Cleanup must rmdir-skip and
// leave the user's payload in place without surfacing an error.
func TestCleanupSidecarsLeavesUserContentInTrackedDirIntact(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	envRoot := t.TempDir()

	// Imagine Prepare wrote .multica/sidecar.txt and created
	// .multica/ + .multica/project/, then exited. Between Prepare
	// and Cleanup the user dropped their own file under .multica/.
	managedDir := filepath.Join(workDir, ".multica")
	managedProject := filepath.Join(managedDir, "project")
	managedFile := filepath.Join(managedProject, "resources.json")
	if err := os.MkdirAll(managedProject, 0o755); err != nil {
		t.Fatalf("seed dirs: %v", err)
	}
	if err := os.WriteFile(managedFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("seed managed file: %v", err)
	}
	userFile := filepath.Join(managedDir, "user-notes.txt")
	if err := os.WriteFile(userFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("seed user file: %v", err)
	}

	manifest := &sidecarManifest{
		Files: []string{managedFile},
		Dirs:  []string{managedDir, managedProject},
	}
	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := CleanupSidecars(envRoot); err != nil {
		t.Errorf("CleanupSidecars: %v", err)
	}

	if _, err := os.Stat(managedFile); !os.IsNotExist(err) {
		t.Errorf("managed file %s should be gone, stat err=%v", managedFile, err)
	}
	if _, err := os.Stat(managedProject); !os.IsNotExist(err) {
		t.Errorf("inner managed dir %s should be empty and removed, stat err=%v", managedProject, err)
	}
	// .multica still holds user-notes.txt, so rmdir must have been
	// skipped silently — the directory must survive.
	got, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatalf("user file went missing: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("user file content changed: %q", string(got))
	}
}

// TestCleanupSidecarsDoesNotRemovePreExistingDirs is the directed unit
// test for the "skip recording pre-existing ancestors" branch in
// recordMkdirAll. A directory the user owned before Prepare must NOT
// appear in the manifest, and therefore must NOT be eligible for rmdir
// during Cleanup — even if Cleanup runs after the user removed the
// last file from inside it.
func TestCleanupSidecarsDoesNotRemovePreExistingDirs(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	envRoot := t.TempDir()

	userDir := filepath.Join(workDir, ".claude")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("seed user dir: %v", err)
	}

	manifest := &sidecarManifest{}
	target := filepath.Join(userDir, "skills", "ours")
	if err := recordMkdirAll(target, 0o755, manifest); err != nil {
		t.Fatalf("recordMkdirAll: %v", err)
	}
	for _, d := range manifest.Dirs {
		if d == userDir {
			t.Fatalf("manifest must not record pre-existing user dir %s\nfull dirs: %v", userDir, manifest.Dirs)
		}
	}

	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := CleanupSidecars(envRoot); err != nil {
		t.Fatalf("CleanupSidecars: %v", err)
	}
	if _, err := os.Stat(userDir); err != nil {
		t.Errorf("pre-existing user dir %s removed by cleanup: %v", userDir, err)
	}
}

// TestRecordWriteFileRefusesToOverwritePreExistingFile pins the
// invariant the PR #3444 review identified as must-fix: a pre-existing
// path is user-owned (or stale state we can't safely distinguish from
// user-owned), and recordWriteFile MUST refuse to mutate it. The
// function returns errPathPreExists, the file's bytes survive
// untouched, and nothing is added to the manifest.
//
// The previous behaviour — overwrite at write time, then skip the
// manifest record so Cleanup wouldn't undo the damage — destroyed
// user data and called it preservation.
func TestRecordWriteFileRefusesToOverwritePreExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "user.md")
	if err := os.WriteFile(target, []byte("user bytes"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	m := &sidecarManifest{}
	err := recordWriteFile(target, []byte("ours"), 0o644, m)
	if !errors.Is(err, errPathPreExists) {
		t.Fatalf("recordWriteFile must return errPathPreExists for a pre-existing target, got: %v", err)
	}
	for _, f := range m.Files {
		if f == target {
			t.Errorf("manifest must not record pre-existing user file %s", target)
		}
	}
	// User bytes must survive the refused write — the whole point of
	// the new behaviour is that pre-existing paths are NEVER mutated.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "user bytes" {
		t.Errorf("user bytes must survive refused write\n want: %q\n  got: %q", "user bytes", string(got))
	}
}

// TestRecordWriteFileRefusesToOverwriteSymlinkOrDir is the directed
// edge-case companion: any kind of pre-existing entry at the target
// path — symlink or directory, not just a regular file — counts as
// user-owned. We use Lstat to detect symlinks (so a symlink to a
// regular file doesn't accidentally pass the existence check via
// Stat-follows-symlinks semantics) and bail out the same way.
func TestRecordWriteFileRefusesToOverwriteSymlinkOrDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Pre-existing symlink (dangling — points nowhere). Should still
	// count as "occupied" so we don't accidentally create a file the
	// symlink would then expose at an unexpected target.
	symlinkPath := filepath.Join(dir, "symlink.md")
	if err := os.Symlink(filepath.Join(dir, "does-not-exist"), symlinkPath); err != nil {
		t.Skipf("symlink not supported on this platform: %v", err)
	}
	if err := recordWriteFile(symlinkPath, []byte("ours"), 0o644, &sidecarManifest{}); !errors.Is(err, errPathPreExists) {
		t.Errorf("recordWriteFile on dangling symlink should refuse, got: %v", err)
	}

	// Pre-existing directory at the file path — should also refuse.
	dirPath := filepath.Join(dir, "subdir")
	if err := os.Mkdir(dirPath, 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := recordWriteFile(dirPath, []byte("ours"), 0o644, &sidecarManifest{}); !errors.Is(err, errPathPreExists) {
		t.Errorf("recordWriteFile on pre-existing directory should refuse, got: %v", err)
	}
}

// TestSidecarManifestRoundTripJSON pins the on-disk encoding so a
// future field rename or json-tag change doesn't silently break Cleanup
// for envRoots that carry an in-flight manifest at the moment of an
// upgrade.
func TestSidecarManifestRoundTripJSON(t *testing.T) {
	t.Parallel()
	envRoot := t.TempDir()
	original := &sidecarManifest{
		Files: []string{"/x/.agent_context/issue_context.md"},
		Dirs:  []string{"/x/.agent_context"},
	}
	if err := writeSidecarManifest(envRoot, original); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(envRoot, sidecarManifestFile))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{"files", "dirs", "issue_context.md", ".agent_context"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("manifest JSON missing %q\n got: %s", want, string(raw))
		}
	}
}

// sameSlugSkillProviderCases lists every file-based provider that
// writes per-skill subdirectories under workDir, paired with the
// natural skill path (relative to workDir) that a user-installed
// skill named "issue-review" would occupy. The same-slug collision
// matrix below replays the user-skill-already-present scenario per
// provider and asserts byte-exact round-trip — including that the
// user's SKILL.md bytes survive the task untouched and our
// collision-free sibling (which lives under .../issue-review-multica/)
// is fully cleaned up.
//
// Codex skills live under codex-home (not workdir), so the per-skill
// collision branch doesn't apply to it. Gemini falls back to
// .agent_context/skills/ same as the default; Hermes goes there too.
var sameSlugSkillProviderCases = []struct {
	provider string
	skillDir string // relative path under workDir for the colliding slug
}{
	{"claude", filepath.Join(".claude", "skills", "issue-review")},
	{"copilot", filepath.Join(".github", "skills", "issue-review")},
	{"opencode", filepath.Join(".opencode", "skills", "issue-review")},
	{"openclaw", filepath.Join("skills", "issue-review")},
	{"pi", filepath.Join(".pi", "skills", "issue-review")},
	{"cursor", filepath.Join(".cursor", "skills", "issue-review")},
	{"kimi", filepath.Join(".kimi", "skills", "issue-review")},
	{"kiro", filepath.Join(".kiro", "skills", "issue-review")},
	{"antigravity", filepath.Join(".agents", "skills", "issue-review")},
	{"hermes", filepath.Join(".agent_context", "skills", "issue-review")},
	{"gemini", filepath.Join(".agent_context", "skills", "issue-review")},
}

// TestPrepareThenCleanupSidecarsSameSlugCollisionPerProvider is the
// must-fix byte-exact matrix the PR #3444 review required: per
// provider, seed a user skill at the exact slug Multica would use
// (`.claude/skills/issue-review/SKILL.md` etc.), run the full
// Prepare → Inject → Cleanup cycle with a Multica skill of the same
// name, and assert the workdir snapshot is byte-identical to the
// seed. The user's SKILL.md must not be touched, and the Multica
// sibling (which lives at `<slug>-multica`) must be fully removed by
// CleanupSidecars.
func TestPrepareThenCleanupSidecarsSameSlugCollisionPerProvider(t *testing.T) {
	t.Parallel()
	for _, tc := range sameSlugSkillProviderCases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()

			userSkillDir := filepath.Join(workDir, tc.skillDir)
			if err := os.MkdirAll(userSkillDir, 0o755); err != nil {
				t.Fatalf("seed user skill dir: %v", err)
			}
			userBody := "---\nname: issue-review\ndescription: user-authored\n---\n\nUser owns this slug.\n"
			userSkillFile := filepath.Join(userSkillDir, "SKILL.md")
			if err := os.WriteFile(userSkillFile, []byte(userBody), 0o644); err != nil {
				t.Fatalf("seed user SKILL.md: %v", err)
			}
			// A second user-authored file under the same skill dir
			// — exercises the case where the user has more than
			// just SKILL.md in their skill (templates, scripts).
			userExtra := filepath.Join(userSkillDir, "notes.md")
			if err := os.WriteFile(userExtra, []byte("private notes"), 0o644); err != nil {
				t.Fatalf("seed user extra file: %v", err)
			}

			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
				AgentSkills: []SkillContextForEnv{
					{
						Name:        "Issue Review",
						Description: "Multica's version",
						Content:     "---\nname: issue-review\n---\n\nMultica skill content.\n",
						Files: []SkillFileContextForEnv{
							{Path: "templates/checklist.md", Content: "- [ ] check"},
						},
					},
				},
			}
			runPrepareLikeCycle(t, workDir, envRoot, tc.provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, tc.provider, before, after)

			// Defensive double-check: read the user's files
			// directly to make sure their content survived.
			gotBody, err := os.ReadFile(userSkillFile)
			if err != nil {
				t.Fatalf("user SKILL.md went missing: %v", err)
			}
			if string(gotBody) != userBody {
				t.Errorf("user SKILL.md mutated\n want: %q\n  got: %q", userBody, string(gotBody))
			}
			gotExtra, err := os.ReadFile(userExtra)
			if err != nil {
				t.Fatalf("user extra file went missing: %v", err)
			}
			if string(gotExtra) != "private notes" {
				t.Errorf("user extra file mutated\n want: %q\n  got: %q", "private notes", string(gotExtra))
			}
		})
	}
}

// TestPrepareThenCleanupSidecarsIssueContextCollisionPerProvider is
// the matching byte-exact matrix for `.agent_context/issue_context.md`
// — a Multica-only namespace file. If the user already has a file at
// that path, the writer must refuse to overwrite it (the runtime
// brief carries the same facts anyway) and CleanupSidecars must
// leave the user's file alone. This covers EVERY file-based provider
// (including Codex, whose skills don't live in workdir but whose
// .agent_context/ write goes through the same path).
func TestPrepareThenCleanupSidecarsIssueContextCollisionPerProvider(t *testing.T) {
	t.Parallel()
	for _, provider := range allFileBasedProviders {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()

			if err := os.MkdirAll(filepath.Join(workDir, ".agent_context"), 0o755); err != nil {
				t.Fatalf("seed dir: %v", err)
			}
			userBody := "# user-authored issue_context.md\n\nDo not touch.\n"
			userPath := filepath.Join(workDir, ".agent_context", "issue_context.md")
			if err := os.WriteFile(userPath, []byte(userBody), 0o644); err != nil {
				t.Fatalf("seed user file: %v", err)
			}

			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
			}
			runPrepareLikeCycle(t, workDir, envRoot, provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, provider, before, after)

			got, err := os.ReadFile(userPath)
			if err != nil {
				t.Fatalf("user issue_context.md went missing: %v", err)
			}
			if string(got) != userBody {
				t.Errorf("user issue_context.md mutated\n want: %q\n  got: %q", userBody, string(got))
			}
		})
	}
}

// TestPrepareThenCleanupSidecarsProjectResourcesCollisionPerProvider
// is the matching byte-exact matrix for `.multica/project/
// resources.json` — the other Multica-only namespace file. Same
// invariant: pre-existing user content survives the round-trip
// untouched even when the task ships project resources of its own.
func TestPrepareThenCleanupSidecarsProjectResourcesCollisionPerProvider(t *testing.T) {
	t.Parallel()
	for _, provider := range allFileBasedProviders {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()

			if err := os.MkdirAll(filepath.Join(workDir, ".multica", "project"), 0o755); err != nil {
				t.Fatalf("seed dir: %v", err)
			}
			userBody := `{"user":"owns this file"}`
			userPath := filepath.Join(workDir, ".multica", "project", "resources.json")
			if err := os.WriteFile(userPath, []byte(userBody), 0o644); err != nil {
				t.Fatalf("seed user file: %v", err)
			}

			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID:      "11111111-2222-3333-4444-555555555555",
				ProjectID:    "proj-1",
				ProjectTitle: "Demo",
				ProjectResources: []ProjectResourceForEnv{
					{
						ID:           "res-1",
						ResourceType: "github_repo",
						ResourceRef:  []byte(`{"url":"https://github.com/example/repo"}`),
					},
				},
			}
			runPrepareLikeCycle(t, workDir, envRoot, provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, provider, before, after)

			got, err := os.ReadFile(userPath)
			if err != nil {
				t.Fatalf("user resources.json went missing: %v", err)
			}
			if string(got) != userBody {
				t.Errorf("user resources.json mutated\n want: %q\n  got: %q", userBody, string(got))
			}
		})
	}
}

// TestAllocateCollisionFreeSkillDir pins the slug-suffix policy:
// first try the natural slug, then `-multica`, then `-multica-2`,
// `-multica-3`, … The PR-review concern is "Multica skill must still
// be discoverable" — this test demonstrates that we pick a sibling
// path under the same skillsParent rather than dropping the skill or
// nesting it under the user's directory.
func TestAllocateCollisionFreeSkillDir(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()

	// 1) No collision → use the base slug as-is.
	slug, dir, err := allocateCollisionFreeSkillDir(parent, "issue-review")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slug != "issue-review" {
		t.Errorf("first allocation should use base slug; got %q", slug)
	}
	if dir != filepath.Join(parent, "issue-review") {
		t.Errorf("first allocation path = %q, want under parent", dir)
	}

	// 2) Pre-existing user dir at the base slug → bump to `-multica`.
	if err := os.MkdirAll(filepath.Join(parent, "issue-review"), 0o755); err != nil {
		t.Fatalf("seed user dir: %v", err)
	}
	slug, dir, err = allocateCollisionFreeSkillDir(parent, "issue-review")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slug != "issue-review-multica" {
		t.Errorf("second allocation should bump to `-multica`; got %q", slug)
	}
	if dir != filepath.Join(parent, "issue-review-multica") {
		t.Errorf("second allocation path = %q, want under parent", dir)
	}

	// 3) Pre-existing collision at the bumped slug too → bump again.
	if err := os.MkdirAll(filepath.Join(parent, "issue-review-multica"), 0o755); err != nil {
		t.Fatalf("seed bumped dir: %v", err)
	}
	slug, dir, err = allocateCollisionFreeSkillDir(parent, "issue-review")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slug != "issue-review-multica-2" {
		t.Errorf("third allocation should be `-multica-2`; got %q", slug)
	}
	if dir != filepath.Join(parent, "issue-review-multica-2") {
		t.Errorf("third allocation path = %q, want under parent", dir)
	}
}

// TestPrepareThenCleanupSidecarsMultiSkillCollisionFreeAllocation is
// the end-to-end coverage for the collision-free sibling: a user has
// `.claude/skills/issue-review/SKILL.md`, the task ships an
// `Issue Review` skill, the Multica sibling must land at a different
// slug (so the agent still sees it), AND Cleanup must remove the
// Multica sibling entirely without touching the user's.
func TestPrepareThenCleanupSidecarsMultiSkillCollisionFreeAllocation(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	envRoot := t.TempDir()

	userDir := filepath.Join(workDir, ".claude", "skills", "issue-review")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userBody := "user-authored skill\n"
	userFile := filepath.Join(userDir, "SKILL.md")
	if err := os.WriteFile(userFile, []byte(userBody), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// Run only the inject side first — verify the Multica skill
	// landed at a NEW path under the same parent, AND the user's
	// path is untouched.
	manifest := &sidecarManifest{}
	if err := writeContextFiles(workDir, "claude", TaskContextForEnv{
		IssueID: "11111111-2222-3333-4444-555555555555",
		AgentSkills: []SkillContextForEnv{
			{Name: "Issue Review", Content: "Multica's version\n"},
		},
	}, manifest); err != nil {
		t.Fatalf("writeContextFiles: %v", err)
	}

	multicaDir := filepath.Join(workDir, ".claude", "skills", "issue-review-multica")
	if _, err := os.Stat(filepath.Join(multicaDir, "SKILL.md")); err != nil {
		t.Errorf("Multica sibling skill should exist at %s: %v", multicaDir, err)
	}
	got, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatalf("user SKILL.md went missing during inject: %v", err)
	}
	if string(got) != userBody {
		t.Errorf("user SKILL.md mutated during inject\n want: %q\n  got: %q", userBody, string(got))
	}

	// Now persist manifest + run cleanup. After cleanup the
	// Multica sibling is gone; user's path survives.
	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := CleanupSidecars(envRoot); err != nil {
		t.Fatalf("CleanupSidecars: %v", err)
	}
	if _, err := os.Stat(multicaDir); !os.IsNotExist(err) {
		t.Errorf("Multica sibling should be removed by Cleanup; stat err=%v", err)
	}
	got, err = os.ReadFile(userFile)
	if err != nil {
		t.Fatalf("user SKILL.md went missing after cleanup: %v", err)
	}
	if string(got) != userBody {
		t.Errorf("user SKILL.md mutated after cleanup\n want: %q\n  got: %q", userBody, string(got))
	}
}

// TestCleanupSidecarsSwallowsMissingAndNonEmptyDirs pins the two
// branches that CleanupSidecars is expected to silently skip without
// surfacing an error:
//
//   - the recorded directory no longer exists (race-safe ENOENT)
//   - the recorded directory has been populated with user content
//     (ENOTEMPTY — preserving user content is the whole point)
func TestCleanupSidecarsSwallowsMissingAndNonEmptyDirs(t *testing.T) {
	t.Parallel()

	// Case 1: recorded dir is missing → ENOENT must be swallowed.
	envRoot1 := t.TempDir()
	missing := filepath.Join(t.TempDir(), "never-existed")
	if err := writeSidecarManifest(envRoot1, &sidecarManifest{Dirs: []string{missing}}); err != nil {
		t.Fatalf("write missing-dir manifest: %v", err)
	}
	if err := CleanupSidecars(envRoot1); err != nil {
		t.Errorf("CleanupSidecars(missing dir) should swallow ENOENT silently, got: %v", err)
	}

	// Case 2: recorded dir has user content → ENOTEMPTY must be
	// swallowed and the user content must survive.
	envRoot2 := t.TempDir()
	workDir2 := t.TempDir()
	recordedDir := filepath.Join(workDir2, "recorded")
	if err := os.MkdirAll(recordedDir, 0o755); err != nil {
		t.Fatalf("seed recorded dir: %v", err)
	}
	userFile := filepath.Join(recordedDir, "user.txt")
	if err := os.WriteFile(userFile, []byte("user content"), 0o644); err != nil {
		t.Fatalf("seed user file: %v", err)
	}
	if err := writeSidecarManifest(envRoot2, &sidecarManifest{Dirs: []string{recordedDir}}); err != nil {
		t.Fatalf("write non-empty-dir manifest: %v", err)
	}
	if err := CleanupSidecars(envRoot2); err != nil {
		t.Errorf("CleanupSidecars(non-empty dir) should swallow ENOTEMPTY silently, got: %v", err)
	}
	got, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatalf("user content went missing: %v", err)
	}
	if string(got) != "user content" {
		t.Errorf("user content mutated: %q", string(got))
	}
}

// TestCleanupSidecarsSurfacesEACCESOnEmptyRecordedDir is the directed
// integration test for the should-fix branch: rmdir fails (here with
// EACCES because the parent is read-only), the directory is verifiably
// empty, and CleanupSidecars must surface the original rmdir error
// instead of silently skipping it.
//
// Skipped when running as root because chmod is bypassed for uid 0 —
// the EACCES we want to trigger never materialises. CI's daemon runner
// is unprivileged, so the branch is exercised in CI.
func TestCleanupSidecarsSurfacesEACCESOnEmptyRecordedDir(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("chmod is bypassed for uid 0; cannot synthesize EACCES on rmdir")
	}

	workDir := t.TempDir()
	envRoot := t.TempDir()

	parent := filepath.Join(workDir, "parent")
	recorded := filepath.Join(parent, "empty-dir")
	if err := os.MkdirAll(recorded, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := writeSidecarManifest(envRoot, &sidecarManifest{Dirs: []string{recorded}}); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Strip write from parent so rmdir(recorded) fails EACCES.
	// rmdir requires write on the PARENT (it modifies parent's
	// directory entries); readdir(recorded) only requires read on
	// recorded itself, which we leave intact. That isolates this
	// test to the "empty + rmdir refused" branch.
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	// Restore parent permissions for t.TempDir() teardown.
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	err := CleanupSidecars(envRoot)
	if err == nil {
		t.Fatal("CleanupSidecars should surface the EACCES rmdir error, got nil")
	}
	if !strings.Contains(err.Error(), "empty-dir") {
		t.Errorf("expected surfaced error to reference recorded path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "rmdir") {
		t.Errorf("expected surfaced error to come from rmdir branch, got: %v", err)
	}
}

// TestCleanupSidecarsSurfacesEACCESWhenReadDirFailsToo is the matching
// test for the must-fix branch: rmdir fails AND the post-rmdir ReadDir
// also fails. CleanupSidecars must preserve the ORIGINAL rmdir error
// rather than swallowing it because dirHasEntries couldn't read the
// directory. This is the exact failure mode PR #3444 review surfaced
// — the v1 helper returned (true) on ReadDir error, which made
// CleanupSidecars treat the locked dir as "non-empty" and silently
// drop the rmdir EACCES.
//
// Skipped when running as root for the same reason as above.
func TestCleanupSidecarsSurfacesEACCESWhenReadDirFailsToo(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("chmod is bypassed for uid 0; cannot synthesize EACCES on rmdir + readdir")
	}

	workDir := t.TempDir()
	envRoot := t.TempDir()

	parent := filepath.Join(workDir, "parent")
	recorded := filepath.Join(parent, "locked-dir")
	if err := os.MkdirAll(recorded, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := writeSidecarManifest(envRoot, &sidecarManifest{Dirs: []string{recorded}}); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Strip both: read on recorded so ReadDir(recorded) fails EACCES,
	// write on parent so rmdir(recorded) also fails EACCES. The
	// helper must report ok=false; CleanupSidecars must surface the
	// rmdir error anyway.
	if err := os.Chmod(recorded, 0o000); err != nil {
		t.Fatalf("chmod recorded: %v", err)
	}
	if err := os.Chmod(parent, 0o555); err != nil {
		_ = os.Chmod(recorded, 0o755)
		t.Fatalf("chmod parent: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(parent, 0o755)
		_ = os.Chmod(recorded, 0o755)
	})

	err := CleanupSidecars(envRoot)
	if err == nil {
		t.Fatal("CleanupSidecars should surface the rmdir error even when ReadDir also fails, got nil")
	}
	if !strings.Contains(err.Error(), "locked-dir") {
		t.Errorf("expected surfaced error to reference recorded path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "rmdir") {
		t.Errorf("expected surfaced error to be the ORIGINAL rmdir error, not the ReadDir failure, got: %v", err)
	}
}

// TestDirHasEntries is the directed unit test for the helper Cleanup
// uses to tell ENOTEMPTY (silently skip) apart from genuine rmdir
// errors (surface). The helper returns (hasEntries, ok); CleanupSidecars
// surfaces the original rmdir error whenever ok=false so a ReadDir
// failure on a chmod'd / not-a-directory / faulted path doesn't get
// laundered into a phantom "non-empty directory" skip.
func TestDirHasEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatalf("seed empty: %v", err)
	}
	if has, ok := dirHasEntries(empty); !ok || has {
		t.Errorf("dirHasEntries(empty dir) = (%v, %v), want (false, true)", has, ok)
	}

	full := filepath.Join(root, "full")
	if err := os.Mkdir(full, 0o755); err != nil {
		t.Fatalf("seed full: %v", err)
	}
	if err := os.WriteFile(filepath.Join(full, "file"), []byte(""), 0o644); err != nil {
		t.Fatalf("seed full content: %v", err)
	}
	if has, ok := dirHasEntries(full); !ok || !has {
		t.Errorf("dirHasEntries(non-empty dir) = (%v, %v), want (true, true)", has, ok)
	}

	missing := filepath.Join(root, "missing")
	if has, ok := dirHasEntries(missing); !ok || has {
		t.Errorf("dirHasEntries(missing dir) = (%v, %v), want (false, true) — ENOENT collapses to empty so the rmdir-race resolves cleanly", has, ok)
	}

	// ReadDir failure path: pass a regular file. ReadDir returns
	// ENOTDIR, which is NOT ENOENT, so the helper must report
	// (false, false) — "couldn't tell" — and let CleanupSidecars
	// surface the underlying rmdir error rather than silently
	// skipping. This is the must-fix branch PR #3444 review caught:
	// the v1 helper returned (true) here, which made every ReadDir
	// failure look like ENOTEMPTY.
	regular := filepath.Join(root, "regular.txt")
	if err := os.WriteFile(regular, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("seed regular: %v", err)
	}
	if has, ok := dirHasEntries(regular); ok || has {
		t.Errorf("dirHasEntries(regular file) = (%v, %v), want (false, false) — ENOTDIR must NOT be laundered as ENOTEMPTY", has, ok)
	}
}
