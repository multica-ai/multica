package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The capability token is a cross-version protocol contract: the daemon
// advertises it, the server persists it, and installed clients look for it.
// A typo on either side silently hides worktree mode. This file is also why
// packages/core/runtimes/cli-version.ts belongs in CI's backend path filter.
func TestWorktreeCapabilityTokenMatchesFrontend(t *testing.T) {
	path := filepath.Join("..", "..", "..", "packages", "core", "runtimes", "cli-version.ts")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	match := regexp.MustCompile(`LOCAL_WORKTREE_CAPABILITY\s*=\s*"([^"]+)"`).FindSubmatch(src)
	if match == nil {
		t.Fatal("LOCAL_WORKTREE_CAPABILITY not found in packages/core/runtimes/cli-version.ts")
	}
	if got := string(match[1]); got != protocol.DaemonCapabilityLocalWorktreeV1 {
		t.Errorf("frontend looks for %q but the daemon advertises %q; worktree mode would be invisible to the UI",
			got, protocol.DaemonCapabilityLocalWorktreeV1)
	}
}
