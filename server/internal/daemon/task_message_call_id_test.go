package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestExecuteAndDrain_ScopesToolCallIdentity(t *testing.T) {
	t.Parallel()
	d, rec := newTranscriptRecorder(t)
	var seq atomic.Int32
	// Both executions reuse provider IDs. The unfinished call in the first
	// attempt must never capture a result from the retry.
	for attempt := 0; attempt < 2; attempt++ {
		messages := make(chan agent.Message, 7)
		messages <- agent.Message{Type: agent.MessageToolUse, Tool: "Bash", CallID: "A"}
		messages <- agent.Message{Type: agent.MessageToolUse, Tool: "Bash", CallID: "B"}
		messages <- agent.Message{Type: agent.MessageToolResult, CallID: "B", Output: "B finished"}
		messages <- agent.Message{Type: agent.MessageToolResult, CallID: "A", Output: "A finished"}
		messages <- agent.Message{Type: agent.MessageToolUse, Tool: "Bash", CallID: "unfinished"}
		messages <- agent.Message{Type: agent.MessageToolResult, Tool: "Bash", CallID: "orphan"}
		messages <- agent.Message{Type: agent.MessageToolUse, Tool: "Read"}
		close(messages)
		results := make(chan agent.Result, 1)
		results <- agent.Result{Status: "completed"}
		close(results)
		backend := sessionBackend{session: &agent.Session{Messages: messages, Result: results}}
		if _, _, err := d.executeAndDrain(context.Background(), backend, "p", agent.ExecOptions{},
			slog.Default(), "task-call-id", "", &seq); err != nil {
			t.Fatal(err)
		}
	}
	reported := rec.snapshot()
	slices.SortFunc(reported, func(a, b TaskMessageData) int { return a.Seq - b.Seq })
	wire, err := json.Marshal(reported)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(wire, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 14 {
		t.Fatalf("got %d messages, want 14", len(rows))
	}
	seen := map[string]bool{}
	for attempt := 0; attempt < 2; attempt++ {
		batch := rows[attempt*7 : (attempt+1)*7]
		for _, i := range []int{0, 1, 4, 5} {
			id, _ := batch[i]["call_id"].(string)
			if id == "" || seen[id] {
				t.Fatalf("missing or reused call identity at attempt %d row %d: %q", attempt, i, id)
			}
			seen[id] = true
		}
		if batch[0]["call_id"] != batch[3]["call_id"] || batch[1]["call_id"] != batch[2]["call_id"] {
			t.Fatalf("out-of-order results lost their call identity: %+v", batch)
		}
		if batch[2]["tool"] != "Bash" || batch[3]["tool"] != "Bash" {
			t.Fatal("tool name resolution changed")
		}
		if _, ok := batch[6]["call_id"]; ok {
			t.Fatal("must not invent an identity when the provider supplies none")
		}
	}
}
