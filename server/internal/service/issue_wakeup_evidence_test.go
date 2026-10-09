package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWakeupEvidenceStructuredMergeAndLegacyWriter(t *testing.T) {
	w := db.IssueWakeup{Kind: "event", Instruction: "Inspect current state"}
	fact := func(id string) []db.IssueWakeupReceipt {
		return []db.IssueWakeupReceipt{{EventType: "comment.created", Payload: json.RawMessage(`{"comment_id":"` + id + `","coalesced_count":1}`)}}
	}
	note, raw := mergeWakeupEvidence(w, db.AgentTaskQueue{}, fact("first"))
	contextJSON, _ := json.Marshal(map[string]json.RawMessage{"wakeup_evidence": raw})
	previous := db.AgentTaskQueue{Context: contextJSON, HandoffNote: pgtype.Text{String: note, Valid: true}}
	note, raw = mergeWakeupEvidence(w, previous, fact("second"))
	var summary wakeupEvidence
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Facts) != 2 || summary.Legacy != "" || strings.Count(note, "Instruction:") != 1 {
		t.Fatalf("facts were merged as rendered text: %s", raw)
	}
	// An older server only knows handoff_note. Preserve its new input on upgrade.
	previous.HandoffNote.String += "legacy-only-reference\n"
	note, raw = mergeWakeupEvidence(w, previous, fact("third"))
	if !strings.Contains(note, "legacy-only-reference") || !strings.Contains(note, "third") {
		t.Fatal("old writer's input was lost")
	}
	if err := json.Unmarshal(raw, &summary); err != nil || summary.Legacy != previous.HandoffNote.String {
		t.Fatalf("legacy text was parsed: %s", raw)
	}
}

func TestWakeupEvidenceJSONBFormattingAndTimerReplacement(t *testing.T) {
	w := db.IssueWakeup{Kind: "event", Instruction: "inspect"}
	receipt := db.IssueWakeupReceipt{EventType: "comment.created", Payload: json.RawMessage(`{"revision":9007199254740993,"comment_id":"first"}`)}
	note, _ := mergeWakeupEvidence(w, db.AgentTaskQueue{}, []db.IssueWakeupReceipt{receipt})
	previous := db.AgentTaskQueue{Context: json.RawMessage(`{"wakeup_evidence":{"version":1,"facts":[{"event_type":"comment.created","payload":{"revision":9007199254740993, "comment_id":"first"}}]}}`), HandoffNote: pgtype.Text{String: note, Valid: true}}
	_, raw := mergeWakeupEvidence(w, previous, nil)
	var summary wakeupEvidence
	_ = json.Unmarshal(raw, &summary)
	if summary.Legacy != "" || !strings.Contains(string(raw), "9007199254740993") {
		t.Fatalf("JSONB formatting lost structured data or number precision: %s", raw)
	}
	w.Kind = "every"
	note, _ = mergeWakeupEvidence(w, previous, []db.IssueWakeupReceipt{{EventType: "time.due", Payload: json.RawMessage(`{"planned_at":"next"}`)}})
	if strings.Contains(note, "first") || !strings.Contains(note, "next") {
		t.Fatal("timer should keep only latest tick")
	}
}

func TestIssueWakeupStructuredEvidenceSurvivesDatabaseRoundTrip(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}, Instruction: "read state"})
	first := f.Comment(t, util.UUIDToString(issue), "first")
	wakeDispatch(t, s, w)
	second := f.Comment(t, util.UUIDToString(issue), "second")
	wakeDispatch(t, s, w)
	task, err := f.q.FindPendingWakeupTask(t.Context(), util.UUIDToString(w.ID))
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Evidence wakeupEvidence `json:"wakeup_evidence"`
	}
	if err = json.Unmarshal(task.Context, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Evidence.Facts) != 2 || stored.Evidence.Legacy != "" || !strings.Contains(task.HandoffNote.String, first) || !strings.Contains(task.HandoffNote.String, second) {
		t.Fatalf("summary did not survive database round trip: %s", task.Context)
	}
}

func TestWakeupEvidenceInstructionEditDoesNotReintroduceOldPrompt(t *testing.T) {
	w := db.IssueWakeup{Kind: "event", Instruction: "old instructions"}
	first, raw := mergeWakeupEvidence(w, db.AgentTaskQueue{}, []db.IssueWakeupReceipt{{EventType: "comment.created", Payload: json.RawMessage(`{"comment_id":"first"}`)}})
	ctx, _ := json.Marshal(map[string]json.RawMessage{"wakeup_evidence": raw})
	w.Instruction = "new instructions"
	note, raw := mergeWakeupEvidence(w, db.AgentTaskQueue{Context: ctx, HandoffNote: pgtype.Text{String: first, Valid: true}}, []db.IssueWakeupReceipt{{EventType: "comment.created", Payload: json.RawMessage(`{"comment_id":"second"}`)}})
	var evidence wakeupEvidence
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Legacy != "" || len(evidence.Facts) != 2 || evidence.Instruction != w.Instruction || strings.Contains(note, "old instructions") || !strings.Contains(note, "new instructions") {
		t.Fatalf("prompt edit confused structured evidence: %s", note)
	}
}
