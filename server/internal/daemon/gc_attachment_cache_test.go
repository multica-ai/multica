package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneAttachmentCacheExpiresUntouchedEntries(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	write := func(rel string, age time.Duration) string {
		p := filepath.Join(root, AttachmentCacheDirName, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("12345"), 0o644); err != nil {
			t.Fatal(err)
		}
		mtime := now.Add(-age)
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return p
	}
	stale := write("aa/aa11", 15*24*time.Hour)
	leftoverTemp := write("bb/bb22.tmp-1", 15*24*time.Hour)
	fresh := write("aa/aa33", time.Hour)

	removed, bytes := pruneAttachmentCache(root, attachmentCacheTTL, now)
	if removed != 2 || bytes != 10 {
		t.Fatalf("removed %d files / %d bytes, want 2 / 10", removed, bytes)
	}
	for _, p := range []string{stale, leftoverTemp} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s not removed: %v", p, err)
		}
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("recently used entry removed: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(leftoverTemp)); !os.IsNotExist(err) {
		t.Fatalf("emptied shard kept: %v", err)
	}
	if removed, _ := pruneAttachmentCache(t.TempDir(), attachmentCacheTTL, now); removed != 0 {
		t.Fatal("a root without a cache reported removals")
	}
}
