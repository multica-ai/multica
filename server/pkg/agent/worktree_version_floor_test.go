package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The worktree version floor is enforced in three places that must agree:
//
//	server  — refuses to save a worktree resource below it, and refuses to hand
//	          a worktree task to a runtime below it at claim time;
//	frontend — disables the option and explains why before the user submits.
//
// If they drift, the UI offers a mode the server will refuse, or worse, the
// frontend permits a version the server no longer gates. Pin them together.
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
		t.Errorf("frontend floor %s != server floor %s; the save gate, the claim gate and the UI must agree",
			got, MinLocalWorktreeCLIVersion)
	}
}
