package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestEnvRootGuardSharesSymlinkAliases(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	assertEnvRootGuardSharesAliases(t, root, alias)
}

func TestGCSkipsActiveChatViaSymlinkedRoot(t *testing.T) {
	t.Parallel()
	const chatID = "aaaaaaaa-0000-0000-0000-000000000009"
	d := newGCTestDaemon(t, chatGCMux(chatID, "active"))
	root := d.cfg.WorkspacesRoot
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	d.cfg.WorkspacesRoot = alias
	taskDir := createTaskDir(t, root, "ws", "idle-chat", &execenv.GCMeta{
		Kind:          execenv.GCKindChat,
		ChatSessionID: chatID,
		WorkspaceID:   "ws",
		CompletedAt:   time.Now().Add(-2 * d.cfg.GCArtifactTTL),
	})
	const cacheDir = "workdir/repo/node_modules"
	writeFile(t, filepath.Join(taskDir, cacheDir, "dependency", "index.js"), 32)

	// A continued run uses the real path persisted by the previous turn,
	// while the GC scans the configured root through the symlink.
	release := d.markActiveEnvRoot(taskDir)
	d.gcWorkspace(t.Context(), filepath.Join(alias, "ws"), &gcStats{})
	assertKept(t, taskDir, cacheDir)
	release()

	d.gcWorkspace(t.Context(), filepath.Join(alias, "ws"), &gcStats{})
	assertGone(t, taskDir, cacheDir)
}

func assertEnvRootGuardSharesAliases(t *testing.T, root, alias string) {
	t.Helper()
	d := &Daemon{}
	taskRoot := filepath.Join(root, "workspace", "task")
	aliasedTaskRoot := filepath.Join(alias, "workspace", "task")

	// A fresh run is marked before its predicted directory exists. GC sees
	// the same directory after preparation, possibly through the other path.
	releaseAlias := d.markActiveEnvRoot(aliasedTaskRoot)
	if err := os.MkdirAll(taskRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if !d.isActiveEnvRoot(taskRoot) {
		t.Fatal("a fresh directory lost its active mark when accessed through its real path")
	}
	releaseTask := d.markActiveEnvRoot(taskRoot)
	releaseAlias()
	if !d.isActiveEnvRoot(aliasedTaskRoot) {
		t.Fatal("the active mark on the real path did not protect the aliased path")
	}
	if release, ok := d.reserveEnvRootForGC(aliasedTaskRoot); ok {
		release()
		t.Fatal("GC reserved an alias of an active directory")
	}
	releaseTask()

	started := make(chan struct{})
	entered := make(chan func(), 1)
	func() {
		releaseGC, ok := d.reserveEnvRootForGC(aliasedTaskRoot)
		if !ok {
			t.Fatal("GC could not reserve the inactive directory through its alias")
		}
		defer releaseGC()
		if release, ok := d.reserveEnvRootForGC(taskRoot); ok {
			release()
			t.Fatal("GC reserved the same directory twice using different paths")
		}
		go func() {
			close(started)
			entered <- d.markActiveEnvRoot(taskRoot)
		}()
		<-started
		select {
		case release := <-entered:
			release()
			t.Fatal("a task entered through the real path while GC held the alias")
		case <-time.After(50 * time.Millisecond):
		}
	}()
	select {
	case release := <-entered:
		release()
	case <-time.After(5 * time.Second):
		t.Fatal("task could not enter after GC released the alias")
	}

	releaseAlias = d.markActiveEnvRoot(aliasedTaskRoot)
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	releaseAlias()
	if d.isActiveEnvRoot(taskRoot) {
		t.Fatal("removing the alias before release stranded the original directory's mark")
	}
}
