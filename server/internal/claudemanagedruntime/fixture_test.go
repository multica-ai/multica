package claudemanagedruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixture decodes a Claude Managed Agents response body recorded against the
// live API. IDs are normalized to the ones these tests use (sess_1, agent_1,
// env_1) and free text is replaced by "<redacted N chars>".
func fixture(t testing.TB, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

// recordedEvents returns the events of a recorded page, with the text of the
// n-th user.message replaced by prompts[n] so the bridge can find its run.
func recordedEvents(t testing.TB, name string, prompts ...string) []map[string]any {
	t.Helper()
	var events []map[string]any
	n := 0
	for _, raw := range fixture(t, name)["data"].([]any) {
		e := raw.(map[string]any)
		if e["type"] == "user.message" && n < len(prompts) {
			e["content"] = []map[string]string{{"type": "text", "text": prompts[n]}}
			n++
		}
		events = append(events, e)
	}
	return events
}

// recordedTurn summarizes the run events between a turn's user.message and
// its session.status_idle: the Multica messages they become and the last reply.
func recordedTurn(events []map[string]any, turn int) (messages int, reply string) {
	seen := -1
	for _, e := range events {
		switch e["type"] {
		case "user.message":
			seen++
		case "agent.message":
			if seen == turn {
				messages++
				reply = e["content"].([]any)[0].(map[string]any)["text"].(string)
			}
		case "agent.tool_use", "agent.tool_result":
			if seen == turn {
				messages++
			}
		}
	}
	return messages, reply
}

func runUntilDone(t *testing.T, h *harness) {
	t.Helper()
	for range 5 {
		if err := h.b.step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if h.complete != nil || h.failure != nil {
			return
		}
	}
	t.Fatal("run did not finish")
}

// The first poll after a create returns the prompt still queued, without
// processed_at; the bridge must not turn that into an empty time filter that
// loses its place, and must then complete from the recorded first turn.
func TestRecordedFirstTurn(t *testing.T) {
	h := setup(t)
	h.history = func(prompt string) []map[string]any { return recordedEvents(t, "events_queued.json", prompt) }
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := h.b.state.first(); r == nil || !r.Started || r.CursorAt != "" {
		t.Fatalf("queued prompt: run = %+v", r)
	}

	all := recordedEvents(t, "events_two_turns.json", h.prompt)
	end := 0
	for i, e := range all {
		if e["type"] == "session.status_idle" {
			end = i + 1
			break
		}
	}
	h.events = all[:end]
	runUntilDone(t, h)
	wantMessages, reply := recordedTurn(h.events, 0)
	if h.failure != nil || h.complete["output"] != reply || len(h.messages) != wantMessages {
		t.Fatalf("complete=%v failure=%v messages=%d, want %d ending %q", h.complete, h.failure, len(h.messages), wantMessages, reply)
	}
}

// A continued session's history holds the earlier run; only the events after
// this run's prompt may be reported.
func TestRecordedFollowUp(t *testing.T) {
	h := setup(t)
	h.followUp()
	h.history = func(prompt string) []map[string]any {
		return recordedEvents(t, "events_two_turns.json", "earlier run prompt", prompt)
	}
	runUntilDone(t, h)
	wantMessages, reply := recordedTurn(h.events, 1)
	if h.creates != 0 || h.sends != 1 || h.failure != nil || h.complete["output"] != reply || len(h.messages) != wantMessages {
		t.Fatalf("creates=%d sends=%d complete=%v failure=%v messages=%d, want %d ending %q", h.creates, h.sends, h.complete, h.failure, len(h.messages), wantMessages, reply)
	}
}
