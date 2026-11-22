package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestCodexInterruptedRunHandshakeTimeout(t *testing.T) {
	// Sequential: the negative seam changes the package cleanup override.
	t.Cleanup(func() { codexCleanupConfirmationOverride.Store(0) })
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	for _, confirmed := range []bool{true, false} {
		codexCleanupConfirmationOverride.Store(0)
		if !confirmed {
			codexCleanupConfirmationOverride.Store(-1)
		}
		for _, resume := range []string{"", "retained-thread"} {
			method := "thread/start"
			if resume != "" {
				method = "thread/resume"
			}
			name := method + "/confirmed"
			if !confirmed {
				name = method + "/unconfirmed"
			}
			t.Run(name, func(t *testing.T) {
				fake := writeFakeCodexAppServer(t, `read line
echo '{"jsonrpc":"2.0","id":1,"result":{}}'
read line
read line
# Keep the transport open without answering thread/start or thread/resume.
while read line; do :; done
`)
				work := t.TempDir()
				marker := filepath.Join(work, "existing-work.txt")
				if err := os.WriteFile(marker, []byte("retained work"), 0600); err != nil {
					t.Fatal(err)
				}
				result := executeFakeCodex(t, fake, ExecOptions{
					Cwd: work, ResumeSessionID: resume, Timeout: 15 * time.Second,
					HandshakeTimeout: 5 * time.Second, ThreadHandshakeTimeout: 200 * time.Millisecond,
				})
				wantStatus := "timeout"
				if !confirmed {
					wantStatus = "cleanup_unconfirmed"
				}
				if result.Status != wantStatus || !strings.Contains(result.Error, method+" did not respond after") {
					t.Fatalf("wrong failure boundary: %+v", result)
				}
				if got := taskfailure.Classify(result.Error); got != taskfailure.ReasonProcessCleanupUnconfirmed {
					t.Fatalf("raw handshake text must not authorize recovery: %q classified as %s", result.Error, got)
				}
				if data, err := os.ReadFile(marker); err != nil || string(data) != "retained work" {
					t.Fatalf("existing work changed: %q %v", data, err)
				}
			})
		}
	}
}
