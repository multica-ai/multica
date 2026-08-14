package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The worktree floor is no longer a gate — the capability is (MUL-5707). It
// survives as the version number shown to the user in the 422 payload and the
// UI hint, and those two must still quote the same release: telling someone to
// update to 0.4.24 in one place and 0.4.30 in another is its own bug.
func TestWorktreeCLIVersionFloorMatchesFrontend(t *testing.T) {
	path := filepath.Join("..", "..", "..", "packages", "core", "runtimes", "cli-version.ts")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := regexp.MustCompile(`MIN_LOCAL_WORKTREE_CLI_VERSION\s*=\s*"([^"]+)"`).FindSubmatch(src)
	if m == nil {
		t.Fatal("MIN_LOCAL_WORKTREE_CLI_VERSION not found in packages/core/runtimes/cli-version.ts")
	}
	if got := string(m[1]); got != MinLocalWorktreeCLIVersion {
		t.Errorf("frontend shows %s but the server's 422 quotes %s; the update hint must name one release",
			got, MinLocalWorktreeCLIVersion)
	}
}
