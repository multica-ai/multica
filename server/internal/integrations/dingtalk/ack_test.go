package dingtalk

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func sessionUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[0] = b
	return u
}

func newTestAck(now func() time.Time) (*ackNotifier, *[]string) {
	var sent []string
	n := &ackNotifier{
		logger:  slog.Default(),
		window:  5 * time.Second,
		now:     now,
		lastAck: map[string]time.Time{},
		sendText: func(_ context.Context, _ engine.ResolvedInstallation, _ channel.InboundMessage, text string) error {
			sent = append(sent, text)
			return nil
		},
	}
	return n, &sent
}

func TestAckNotifier_CoalescesBurstThenReacksAfterWindow(t *testing.T) {
	base := time.Unix(1700000000, 0)
	cur := base
	n, sent := newTestAck(func() time.Time { return cur })
	sid := sessionUUID(1)
	ctx := context.Background()

	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	if len(*sent) != 1 {
		t.Fatalf("a burst within the window must coalesce to one ack, got %d", len(*sent))
	}
	if (*sent)[0] != ackProcessingText {
		t.Errorf("ack text = %q, want %q", (*sent)[0], ackProcessingText)
	}

	cur = base.Add(6 * time.Second)
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	if len(*sent) != 2 {
		t.Fatalf("a message after the window must re-ack, got %d", len(*sent))
	}
}

func TestAckNotifier_SettleResetsDedup(t *testing.T) {
	cur := time.Unix(1700000000, 0)
	n, sent := newTestAck(func() time.Time { return cur })
	sid := sessionUUID(2)
	ctx := context.Background()

	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	n.OnSettled(ctx, sid)
	// Even within the window, a settled session acks its next turn immediately.
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	if len(*sent) != 2 {
		t.Fatalf("OnSettled must reset dedup so the next turn re-acks, got %d", len(*sent))
	}
}

func TestAckNotifier_DistinctSessionsAckIndependently(t *testing.T) {
	cur := time.Unix(1700000000, 0)
	n, sent := newTestAck(func() time.Time { return cur })
	ctx := context.Background()

	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sessionUUID(3))
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sessionUUID(4))
	if len(*sent) != 2 {
		t.Fatalf("distinct sessions must each ack, got %d", len(*sent))
	}
}
