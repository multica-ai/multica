package delegationorigin

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeQueries is an in-memory Queries implementation for the resolver.
type fakeQueries struct {
	tasks  map[[16]byte]db.AgentTaskQueue
	issues map[[16]byte]db.Issue
	err    error
}

func (f *fakeQueries) GetAgentTask(_ context.Context, id pgtype.UUID) (db.AgentTaskQueue, error) {
	if f.err != nil {
		return db.AgentTaskQueue{}, f.err
	}
	t, ok := f.tasks[id.Bytes]
	if !ok {
		return db.AgentTaskQueue{}, errors.New("not found")
	}
	return t, nil
}

func (f *fakeQueries) GetIssue(_ context.Context, id pgtype.UUID) (db.Issue, error) {
	if f.err != nil {
		return db.Issue{}, f.err
	}
	i, ok := f.issues[id.Bytes]
	if !ok {
		return db.Issue{}, errors.New("not found")
	}
	return i, nil
}

func uuid(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[15] = b
	return u
}

func agentTaskOrigin(originID pgtype.UUID) db.Issue {
	return db.Issue{
		OriginType: pgtype.Text{String: "agent_task", Valid: true},
		OriginID:   originID,
	}
}

// Direct hit: an agent-created issue whose creating task carried a human.
func TestResolveHumanViaOrigin_DirectHit(t *testing.T) {
	human := uuid(0xAA)
	taskID := uuid(0x01)
	q := &fakeQueries{
		tasks: map[[16]byte]db.AgentTaskQueue{
			taskID.Bytes: {ID: taskID, OriginalUserID: human},
		},
	}
	got, ok := ResolveHumanViaOrigin(context.Background(), q, agentTaskOrigin(taskID))
	if !ok || got.Bytes != human.Bytes {
		t.Fatalf("expected human %v, got %v ok=%v", human.Bytes, got.Bytes, ok)
	}
}

// Transitive: origin task has no human, but its own issue's origin task does.
func TestResolveHumanViaOrigin_Transitive(t *testing.T) {
	human := uuid(0xBB)
	task1, task2 := uuid(0x01), uuid(0x02)
	issue2 := uuid(0x12)
	q := &fakeQueries{
		tasks: map[[16]byte]db.AgentTaskQueue{
			task1.Bytes: {ID: task1, IssueID: issue2}, // no human, points at issue2
			task2.Bytes: {ID: task2, OriginalUserID: human},
		},
		issues: map[[16]byte]db.Issue{
			issue2.Bytes: agentTaskOrigin(task2),
		},
	}
	got, ok := ResolveHumanViaOrigin(context.Background(), q, agentTaskOrigin(task1))
	if !ok || got.Bytes != human.Bytes {
		t.Fatalf("expected transitive human %v, got %v ok=%v", human.Bytes, got.Bytes, ok)
	}
}

// No origin at all → not found (the truly-humanless case the approval fallback covers).
func TestResolveHumanViaOrigin_NoOrigin(t *testing.T) {
	if _, ok := ResolveHumanViaOrigin(context.Background(), &fakeQueries{}, db.Issue{}); ok {
		t.Fatalf("expected no human for issue without origin")
	}
}

// A cycle must terminate and return not found, never loop forever.
func TestResolveHumanViaOrigin_CycleTerminates(t *testing.T) {
	task1 := uuid(0x01)
	issue1 := uuid(0x11)
	q := &fakeQueries{
		tasks: map[[16]byte]db.AgentTaskQueue{
			task1.Bytes: {ID: task1, IssueID: issue1}, // points back at issue1
		},
		issues: map[[16]byte]db.Issue{
			issue1.Bytes: agentTaskOrigin(task1), // issue1 → task1 → issue1 …
		},
	}
	if _, ok := ResolveHumanViaOrigin(context.Background(), q, agentTaskOrigin(task1)); ok {
		t.Fatalf("expected cycle to terminate without a human")
	}
}
