package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueWakeupLargeSingleFactPreservesReferences(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"comment_id": "source-comment", "changed_fields": strings.Repeat("字段", 20000)})
	w := db.IssueWakeup{Instruction: strings.Repeat("指", 4000)}
	note, _ := mergeWakeupEvidence(w, db.AgentTaskQueue{}, []db.IssueWakeupReceipt{{EventType: "comment.updated", Payload: payload}})
	if len(note) > wakeupNoteLimit || !utf8.ValidString(note) || !strings.Contains(note, w.Instruction) || !strings.Contains(note, "source-comment") || !strings.Contains(note, wakeupOmittedEvidence) {
		t.Fatalf("oversized fact not safely condensed: %d bytes", len(note))
	}
}

func TestIssueWakeupRegistrationRunDoesNotTriggerItself(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	source := f.Task(t, agent, testutil.Cols{"issue_id": issue, "status": "running", "runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')")})
	w, err := s.Create(context.Background(), issue, parseTestUUID(t, f.UserID), parseTestUUID(t, source), WakeupInput{
		AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created", "task.completed"}, Instruction: "Wait for new input",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.Comment(t, util.UUIDToString(issue), "Registered", testutil.Cols{"author_type": "agent", "author_id": agent, "source_task_id": source})
	f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1", source)
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID); n != 0 {
		t.Fatalf("registration run produced %d receipts for itself", n)
	}
	f.Comment(t, util.UUIDToString(issue), "External input")
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID); n != 1 {
		t.Fatalf("external input lost: %d", n)
	}
	wakeDispatch(t, s, w)
}

func TestIssueWakeupOversizedPendingEvidenceStillConsumesInputs(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}, Instruction: "Keep this instruction"})
	f.Comment(t, util.UUIDToString(issue), "first")
	wakeDispatch(t, s, w)
	task, err := f.q.FindPendingWakeupTask(context.Background(), util.UUIDToString(w.ID))
	if err != nil {
		t.Fatal(err)
	}
	f.Exec(t, "UPDATE agent_task_queue SET handoff_note=$2 WHERE id=$1", task.ID, strings.Repeat("旧", 20000))
	latest := f.Comment(t, util.UUIDToString(issue), "latest")
	wakeDispatch(t, s, w)
	got, err := f.q.GetAgentTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(got.HandoffNote.String) || len(got.HandoffNote.String) > 40000 || !strings.Contains(got.HandoffNote.String, latest) || !strings.Contains(got.HandoffNote.String, w.Instruction) {
		t.Fatalf("evidence not bounded with instruction/latest reference: %d bytes", len(got.HandoffNote.String))
	}
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); n != 0 {
		t.Fatalf("%d inputs stalled", n)
	}
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND task_id=$2", w.ID, task.ID); n != 2 {
		t.Fatalf("lost durable evidence: %d", n)
	}
	if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID)); n != 1 {
		t.Fatalf("duplicate task: %d", n)
	}
}

func TestIssueWakeupDispatchedWaitIsObservableAndResumes(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}, Instruction: "Wait for input"})
	f.Comment(t, util.UUIDToString(issue), "first")
	wakeDispatch(t, s, w)
	task, err := f.q.FindPendingWakeupTask(context.Background(), util.UUIDToString(w.ID))
	if err != nil {
		t.Fatal(err)
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='dispatched',dispatched_at=now()-interval '10 minutes',prepare_lease_expires_at=now()-interval '1 minute' WHERE id=$1", task.ID)
	f.Comment(t, util.UUIDToString(issue), "next")
	wakeDispatch(t, s, w)
	got, err := f.q.GetIssueWakeup(context.Background(), db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastError.Valid {
		t.Fatal("stale dispatched wait is invisible")
	}
	unchanged, _ := f.q.GetAgentTask(context.Background(), task.ID)
	if unchanged.HandoffNote != task.HandoffNote {
		t.Fatal("changed claimed prompt")
	}
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); n != 1 {
		t.Fatalf("pending input lost: %d", n)
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='running',started_at=now() WHERE id=$1", task.ID)
	wakeDispatch(t, s, w)
	got, err = f.q.GetIssueWakeup(context.Background(), db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if got.LastError.Valid || got.LastTaskID == task.ID {
		t.Fatal("did not resume/clear error after start")
	}
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); n != 0 {
		t.Fatalf("pending input stalled: %d", n)
	}
}
