package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func uuidFromByte(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[15] = b
	return u
}

func TestPendingQuickCreateDuplicate(t *testing.T) {
	t.Parallel()

	agentA := uuidFromByte(1)
	agentB := uuidFromByte(2)
	userA := uuidFromByte(10)
	userB := uuidFromByte(11)
	ctx := []byte(`{"type":"quick_create","prompt":"add archive button"}`)
	ctxOther := []byte(`{"type":"quick_create","prompt":"something else"}`)

	task := func(id byte, agent, user pgtype.UUID, context []byte) db.AgentTaskQueue {
		return db.AgentTaskQueue{ID: uuidFromByte(id), AgentID: agent, OriginatorUserID: user, Context: context}
	}

	tests := []struct {
		name    string
		pending []db.AgentTaskQueue
		wantDup bool
		wantID  byte
	}{
		{
			name:    "identical request from same requester to same agent is a duplicate",
			pending: []db.AgentTaskQueue{task(100, agentA, userA, ctx)},
			wantDup: true,
			wantID:  100,
		},
		{
			name:    "different prompt is not a duplicate",
			pending: []db.AgentTaskQueue{task(100, agentA, userA, ctxOther)},
			wantDup: false,
		},
		{
			name:    "same request but different agent is not a duplicate",
			pending: []db.AgentTaskQueue{task(100, agentB, userA, ctx)},
			wantDup: false,
		},
		{
			name:    "same request but different requester is not a duplicate",
			pending: []db.AgentTaskQueue{task(100, agentA, userB, ctx)},
			wantDup: false,
		},
		{
			name:    "empty pending set is not a duplicate",
			pending: nil,
			wantDup: false,
		},
		{
			name: "matches the duplicate among unrelated pending tasks",
			pending: []db.AgentTaskQueue{
				task(100, agentB, userA, ctx),
				task(101, agentA, userB, ctx),
				task(102, agentA, userA, ctx),
			},
			wantDup: true,
			wantID:  102,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := pendingQuickCreateDuplicate(tt.pending, agentA, userA, ctx)
			if tt.wantDup {
				if got == nil {
					t.Fatalf("expected a duplicate, got nil")
				}
				if got.ID != uuidFromByte(tt.wantID) {
					t.Errorf("matched wrong task: got %v, want id byte %d", got.ID, tt.wantID)
				}
			} else if got != nil {
				t.Errorf("expected no duplicate, got task %v", got.ID)
			}
		})
	}
}
