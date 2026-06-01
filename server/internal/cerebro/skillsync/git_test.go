package skillsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteSkillFilesRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"SKILL.md":         "ok",
		"../../escape.md":  "nope",
		"/abs.md":          "nope",
		"nested/helper.py": "fine",
	}
	if err := writeSkillFiles(dir, "engineering", "demo", files); err != nil {
		t.Fatalf("writeSkillFiles: %v", err)
	}
	base := filepath.Join(dir, "engineering", "demo")
	if b, err := os.ReadFile(filepath.Join(base, "SKILL.md")); err != nil || string(b) != "ok" {
		t.Errorf("SKILL.md not written: %v %q", err, b)
	}
	if b, err := os.ReadFile(filepath.Join(base, "nested", "helper.py")); err != nil || string(b) != "fine" {
		t.Errorf("nested file not written: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.md")); !os.IsNotExist(err) {
		t.Errorf("escaping path was written outside the skill dir")
	}
}

func TestExistingSlugDomains(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "data", "alpha", "SKILL.md"), "x")
	mustWrite(t, filepath.Join(dir, "engineering", "beta", "SKILL.md"), "x")
	mustWrite(t, filepath.Join(dir, "engineering", "no-skill-md", "other.txt"), "x")

	got, err := existingSlugDomains(dir)
	if err != nil {
		t.Fatalf("existingSlugDomains: %v", err)
	}
	if got["alpha"] != "data" {
		t.Errorf("alpha domain = %q, want data", got["alpha"])
	}
	if got["beta"] != "engineering" {
		t.Errorf("beta domain = %q, want engineering", got["beta"])
	}
	if _, ok := got["no-skill-md"]; ok {
		t.Errorf("dir without SKILL.md should not be registered")
	}
}

// TestSyncRoundTripIdempotent drives the real git operations against a local
// bare "remote": first push creates a commit, an identical second push makes
// none. This is the FIR-2656 idempotency guarantee.
func TestSyncRoundTripIdempotent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	ctx := context.Background()
	root := t.TempDir()

	// Build a bare remote with an initial commit on main.
	remote := filepath.Join(root, "remote.git")
	seed := filepath.Join(root, "seed")
	runOrFail(t, "", "git", "init", "--bare", "-b", "main", remote)
	runOrFail(t, "", "git", "init", "-b", "main", seed)
	mustWrite(t, filepath.Join(seed, "README.md"), "archive")
	runOrFail(t, seed, "git", "-c", "user.email=t@t", "-c", "user.name=t", "add", "-A")
	runOrFail(t, seed, "git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "seed")
	runOrFail(t, seed, "git", "push", remote, "main")

	cfg := Config{
		Enabled: true, Repo: remote, Branch: "main",
		WorkDir: filepath.Join(root, "work"), DefaultDomain: "engineering",
		AuthorName: "Sync", AuthorEmail: "sync@t",
		Interval: time.Hour,
	}
	r := &repo{cfg: cfg}

	dir, err := r.ensureCheckout(ctx)
	if err != nil {
		t.Fatalf("ensureCheckout: %v", err)
	}
	if err := writeSkillFiles(dir, "engineering", "demo", map[string]string{"SKILL.md": "---\nname: demo\n---\nbody"}); err != nil {
		t.Fatalf("writeSkillFiles: %v", err)
	}
	pushed, err := r.commitAndPush(ctx, dir, "first")
	if err != nil {
		t.Fatalf("commitAndPush #1: %v", err)
	}
	if !pushed {
		t.Fatalf("first sync should push a commit")
	}

	// Second run, same content: a fresh checkout + identical write must yield
	// no commit.
	dir2, err := r.ensureCheckout(ctx)
	if err != nil {
		t.Fatalf("ensureCheckout #2: %v", err)
	}
	if err := writeSkillFiles(dir2, "engineering", "demo", map[string]string{"SKILL.md": "---\nname: demo\n---\nbody"}); err != nil {
		t.Fatalf("writeSkillFiles #2: %v", err)
	}
	pushed2, err := r.commitAndPush(ctx, dir2, "second")
	if err != nil {
		t.Fatalf("commitAndPush #2: %v", err)
	}
	if pushed2 {
		t.Fatalf("identical second sync must not push")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runOrFail(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
