package daemon

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// narratedEmptyOutputBackend streams narration and then completes with an
// empty Result.Output: the shape the Codex adapter reports when a turn ends
// with an empty final_answer after a commentary message.
type narratedEmptyOutputBackend struct{}

const narratedEmptyOutputText = "Posted the report to the target issue."

func (narratedEmptyOutputBackend) Execute(context.Context, string, agent.ExecOptions) (*agent.Session, error) {
	msgCh := make(chan agent.Message, 1)
	msgCh <- agent.Message{Type: agent.MessageText, Content: narratedEmptyOutputText}
	close(msgCh)
	resCh := make(chan agent.Result, 1)
	resCh <- agent.Result{Status: "completed", Output: ""}
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

// The daemon must not rebuild Result.Output from the transcript: narration is
// reported as task messages only, and the completed result keeps its empty
// output.
func TestExecuteAndDrain_EmptyOutputIsNotRebuiltFromNarration(t *testing.T) {
	t.Parallel()

	d, rec := newTranscriptRecorder(t)

	result, _, err := d.executeAndDrain(context.Background(), narratedEmptyOutputBackend{}, "p", agent.ExecOptions{}, slog.Default(), "task-empty-output", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("executeAndDrain: %v", err)
	}
	if result.Status != "completed" || result.Output != "" {
		t.Fatalf("result = %+v, want completed with empty output", result)
	}
	messages := rec.snapshot()
	if len(messages) != 1 || messages[0].Content != narratedEmptyOutputText {
		t.Fatalf("transcript = %+v, want the narration reported as one task message", messages)
	}
}

// A completed run with no output still reports success, with an empty output
// the server does not turn into a comment.
func TestReportTaskResult_CompletedWithEmptyOutputPostsEmptyOutput(t *testing.T) {
	t.Parallel()

	rec := &reportTaskResultRecorder{}
	srv := httptest.NewServer(rec.handler(t))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	d.reportTaskResult(context.Background(), "task-empty", TaskResult{Status: "completed", Comment: ""}, slog.Default())

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.path != "/api/daemon/tasks/task-empty/complete" {
		t.Fatalf("expected /complete endpoint, got %s", rec.path)
	}
	if output, ok := rec.payload["output"]; ok && output != "" {
		t.Fatalf("output = %v, want empty", output)
	}
}
