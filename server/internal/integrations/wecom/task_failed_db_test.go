package wecom

// task_failed_db_test.go — the same failure, answered by the real query layer:
// the delivery row, the origin stamp and the installation all come from
// Postgres, and the event travels through a real bus into the real typing
// indicator, which is what owns a run's ending (task_failed_test.go).

import (
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestTaskFailed_ReachesTheChatThroughTheRealQueryLayer(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)

	var instID pgtype.UUID
	if err := instID.Scan(turn.instID); err != nil {
		t.Fatalf("parse installation id: %v", err)
	}
	conn := &recordingConn{}
	reg := newSendersRegistry()
	reg.set(instID, conn.autoAck(newWSSender(conn, nil)))

	q := db.New(pool)
	bus := events.New()
	NewTypingIndicator(TypingIndicatorConfig{
		Senders: reg, Streams: NewStreamStore(),
		Tasks: q, Deliveries: q, Languages: q,
		Logger: slog.Default(),
	}).Register(bus)

	bus.Publish(events.Event{
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

	got := sentTexts(t, conn)
	if len(got) != 1 || got[0] != "⚠️ 上下文超出模型限制" {
		t.Fatalf("sent %q, want exactly the platform's reason, addressed through the delivery row", got)
	}
}
