package repocache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiskBudget_PerAgentWorktreeCount(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Layout:
	//   <root>/agent-1            (legacy — NOT counted)
	//   <root>/agent-1-aaaaaaaa   (per-task — counted for agent-1)
	//   <root>/agent-1-bbbbbbbb   (per-task — counted for agent-1)
	//   <root>/agent-2-cccccccc   (per-task — counted for agent-2)
	//   <root>/some-file          (file, not dir — never counted)
	for _, d := range []string{"agent-1", "agent-1-aaaaaaaa", "agent-1-bbbbbbbb", "agent-2-cccccccc"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "some-file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := DiskBudget{WorktreesRoot: root}

	t.Run("agent-1 has 2 per-task worktrees", func(t *testing.T) {
		n, err := b.PerAgentWorktreeCount("agent-1")
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("got %d, want 2", n)
		}
	})

	t.Run("agent-2 has 1", func(t *testing.T) {
		n, err := b.PerAgentWorktreeCount("agent-2")
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("got %d, want 1", n)
		}
	})

	t.Run("unknown agent has 0", func(t *testing.T) {
		n, err := b.PerAgentWorktreeCount("agent-3")
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("got %d, want 0", n)
		}
	})

	t.Run("empty WorktreesRoot returns error", func(t *testing.T) {
		empty := DiskBudget{}
		if _, err := empty.PerAgentWorktreeCount("agent-1"); err == nil {
			t.Fatal("expected error for empty WorktreesRoot")
		}
	})

	t.Run("nonexistent path returns error", func(t *testing.T) {
		missing := DiskBudget{WorktreesRoot: filepath.Join(root, "does-not-exist")}
		if _, err := missing.PerAgentWorktreeCount("agent-1"); err == nil {
			t.Fatal("expected error for missing dir")
		}
	})
}

func TestDiskBudget_GlobalUsedBytes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Write known-size payloads.
	const (
		size1 = 1024 // bytes
		size2 = 512
	)
	if err := os.WriteFile(filepath.Join(root, "a.bin"), make([]byte, size1), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.bin"), make([]byte, size2), 0o644); err != nil {
		t.Fatal(err)
	}

	b := DiskBudget{WorktreesRoot: root, CacheTTL: 1 * time.Millisecond}
	used, err := b.GlobalUsedBytes()
	if err != nil {
		t.Fatal(err)
	}
	if used != size1+size2 {
		t.Fatalf("got %d, want %d", used, size1+size2)
	}
}

func TestDiskBudget_GlobalFreeBytes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	b := DiskBudget{WorktreesRoot: root}
	free, err := b.GlobalFreeBytes()
	if err != nil {
		t.Fatal(err)
	}
	// We can't assert a specific value, but free space on a tmpfs/tmpdir
	// should be > 0 on any sane CI runner.
	if free <= 0 {
		t.Fatalf("expected positive free bytes, got %d", free)
	}
}

func TestDiskBudget_GlobalCaching(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "first.bin"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}

	// Long TTL: a second call should NOT pick up a new file written after
	// the first call.
	b := DiskBudget{WorktreesRoot: root, CacheTTL: 1 * time.Hour}
	used1, err := b.GlobalUsedBytes()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "second.bin"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}

	used2, err := b.GlobalUsedBytes()
	if err != nil {
		t.Fatal(err)
	}
	if used2 != used1 {
		t.Fatalf("cache should have returned stale value: got %d, want %d (cache miss within TTL?)", used2, used1)
	}
}

func TestDiskBudget_GlobalFractionUsed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	b := DiskBudget{WorktreesRoot: root}
	frac, err := b.GlobalFractionUsed()
	if err != nil {
		t.Fatal(err)
	}
	if frac < 0 || frac > 1 {
		t.Fatalf("fraction out of [0,1]: %v", frac)
	}
}

// Sanity: PerTaskWorktreePath in per_task.go produces paths the
// PerAgentWorktreeCount above counts correctly. This catches drift between
// the path builder and the counter prefix.
func TestPerTaskWorktreePath_MatchesCounter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wt := PerTaskWorktreePath(root, "agent-1", "11111111-2222-3333-4444-555555555555")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(wt), "agent-1-") {
		t.Fatalf("expected agent-1- prefix, got %q", filepath.Base(wt))
	}
	b := DiskBudget{WorktreesRoot: root}
	n, err := b.PerAgentWorktreeCount("agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("got %d, want 1", n)
	}
}
