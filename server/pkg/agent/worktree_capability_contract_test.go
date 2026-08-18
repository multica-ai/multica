package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// What the two sides must agree on is the capability TOKEN, not a version.
//
// This replaces a test that pinned MinLocalWorktreeCLIVersion against a
// frontend twin. That twin is gone: since MUL-5707 nothing on either side gates
// on the version, and the frontend no longer even quotes it (#7113), so the old
// contract was pinning a number the UI does not show. The server constant stays
// because the 422 payload still carries it, but it has no counterpart to match.
//
// The token does have one, and it is load-bearing in a way a version never was:
// the daemon advertises this exact string, the server stores it, and the UI
// looks for it. A typo on either side silently disables worktree mode for
// everyone, with no error anywhere — which is precisely the failure this whole
// area keeps producing.
func TestWorktreeCapabilityTokenMatchesFrontend(t *testing.T) {
	path := filepath.Join("..", "..", "..", "packages", "core", "runtimes", "cli-version.ts")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := regexp.MustCompile(`LOCAL_WORKTREE_CAPABILITY\s*=\s*"([^"]+)"`).FindSubmatch(src)
	if m == nil {
		t.Fatal("LOCAL_WORKTREE_CAPABILITY not found in packages/core/runtimes/cli-version.ts")
	}
	if got := string(m[1]); got != protocol.DaemonCapabilityLocalWorktreeV1 {
		t.Errorf("frontend looks for %q but the daemon advertises %q; worktree mode would be invisible to the UI",
			got, protocol.DaemonCapabilityLocalWorktreeV1)
	}
}
