package daemon

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestGC_PluginCachesAreExactReclaimablePaths(t *testing.T) {
	t.Parallel()
	for _, managedOnly := range []bool{false, true} {
		name := "artifact sweep"
		if managedOnly {
			name = "managed only"
		}
		t.Run(name, func(t *testing.T) {
			d := newGCTestDaemon(t, http.NewServeMux())
			taskDir := t.TempDir()
			for _, rel := range []string{"codex-home/.tmp/plugins", "codex-home/.tmp/marketplaces", "codex-home/cache/remote_plugin_catalog"} {
				writeFile(t, filepath.Join(taskDir, rel, "cache.json"), 64)
			}
			kept := []string{"codex-home/plugins/cache/keep", "codex-home/state_5.sqlite", "codex-home/state_5.sqlite-wal", "codex-home/logs_1.sqlite-wal", "codex-home/session_index.jsonl", "codex-home/.tmp/other/keep", "workdir/repo/cache/remote_plugin_catalog/keep", "workdir/repo/.tmp/plugins/keep"}
			for _, rel := range kept {
				writeFile(t, filepath.Join(taskDir, rel), 8)
			}
			var removed int
			var size int64
			if managedOnly {
				removed, size, _ = d.cleanManagedTaskArtifacts(taskDir)
			} else {
				removed, size, _ = d.cleanTaskArtifacts(context.Background(), taskDir, nil)
			}
			if removed != 3 || size != 192 {
				t.Fatalf("cleanup = %d paths, %d bytes; want 3, 192", removed, size)
			}
			assertKept(t, taskDir, kept...)
			if hasManagedArtifact(taskDir) {
				t.Fatal("reclaimed caches must not trigger another sweep")
			}
		})
	}
}

func TestGC_PluginCachesDoNotFollowLinks(t *testing.T) {
	t.Parallel()
	for _, rel := range []string{"codex-home", "codex-home/.tmp", "codex-home/.tmp/plugins", "codex-home/.tmp/marketplaces", "codex-home/cache", "codex-home/cache/remote_plugin_catalog"} {
		t.Run(rel, func(t *testing.T) {
			d := newGCTestDaemon(t, http.NewServeMux())
			taskDir, outside := t.TempDir(), t.TempDir()
			for _, keep := range []string{"keep", ".tmp/plugins/keep", ".tmp/marketplaces/keep", "plugins/keep", "marketplaces/keep", "cache/remote_plugin_catalog/keep", "remote_plugin_catalog/keep"} {
				writeFile(t, filepath.Join(outside, keep), 8)
			}
			link := filepath.Join(taskDir, rel)
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if removed, _, _ := d.cleanManagedTaskArtifacts(taskDir); removed != 0 {
				t.Fatalf("removed %d paths through a link", removed)
			}
			if removed, _, _ := d.cleanTaskArtifacts(context.Background(), taskDir, nil); removed != 0 {
				t.Fatalf("sweep removed %d paths through a link", removed)
			}
			assertKept(t, outside, "keep", ".tmp/plugins/keep", ".tmp/marketplaces/keep", "plugins/keep", "marketplaces/keep", "cache/remote_plugin_catalog/keep", "remote_plugin_catalog/keep")
		})
	}
}
