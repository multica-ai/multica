package daemon

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoHandWrittenReplyPost keeps every reply-posting command on
// execenv.gatedReplyPost, the one place that renders the Windows variants
// (PowerShell 5.1 has no `&&`). Doc discussion replies (MUL-9933) and wakeup
// deliveries (MUL-10271) each shipped a hand-written bash one-liner that
// PowerShell agents could not run; a new run type that builds its own command
// fails here instead.
func TestNoHandWrittenReplyPost(t *testing.T) {
	const literal = "&& rm ./reply.md"
	allowed := filepath.Join("execenv", "reply_instructions.go")

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || path == allowed {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), literal) {
			t.Errorf("%s hand-writes a reply post (%q); build it with an execenv reply builder so Windows gets the PowerShell variant", path, literal)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
