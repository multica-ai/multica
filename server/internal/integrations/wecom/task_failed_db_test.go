package wecom

// task_failed_db_test.go — the same failed run, answered by the real query
// layer: the task delivery row, the channel_ingested stamp on the user's
// message and the installation's status all come from Postgres, and the
// event travels through a real bus into the real subscriber.

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestTaskFailed_ReachesTheChatThroughTheRealQueryLayer(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)
	r := newReplica(t, pool, turn.instID, true)

	r.bus.Publish(events.Event{
		Type:          protocol.EventTaskFailed,
		ActorType:     "system",
		ChatSessionID: turn.sessionID,
		TaskID:        turn.taskID,
		Payload: map[string]any{
			"task_id":         turn.taskID,
			"chat_session_id": turn.sessionID,
			"status":          "failed",
			"retry_pending":   false,
			"error":           "上下文超出模型限制",
		},
	})

	got := sentTexts(t, r.conn)
	if len(got) != 1 || got[0] != "⚠️ 上下文超出模型限制" {
		t.Fatalf("sent %q, want exactly the failure notice", got)
	}
	if n := r.mx.get("outbound_delivered"); n != 1 {
		t.Errorf("outbound_delivered = %d, want 1", n)
	}
}
