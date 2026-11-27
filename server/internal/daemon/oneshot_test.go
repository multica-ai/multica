package daemon

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestOneShotPromptAndGC(t *testing.T) {
	t.Parallel()
	request := "<request>\r\nReview this change.\r\n</request>\n"
	task := Task{ID: "run-1", OneShotPrompt: request}
	prompt := BuildPrompt(task, "codex")
	if !strings.HasSuffix(prompt, request) {
		t.Fatalf("prompt changed input: %q", prompt)
	}
	if strings.Contains(prompt, "multica issue create") {
		t.Fatal("one-shot prompt became quick-create")
	}
	if got, ok := gcMetaForTask(task); !ok || got.Kind != execenv.GCKindOneShot {
		t.Fatalf("GC kind: %s", got.Kind)
	}
}
