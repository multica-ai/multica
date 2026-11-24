package slack

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// anchoredDeliveryQueries returns a delivery snapshot that DISAGREES with the
// live binding row. That divergence is the whole point of MUL-7234: a task
// pending across a `/clear` boundary is delivered while the binding cursor has
// already moved on to a newer message, and the reply must follow the snapshot
// the task was enqueued with, not the cursor.
//
// The base fake derives its delivery from the binding, which cannot express
// this case.
type anchoredDeliveryQueries struct {
	*fakeOutboundQueries
	delivery db.ChannelTaskDelivery
}

func (q *anchoredDeliveryQueries) GetChannelTaskDelivery(context.Context, pgtype.UUID) (db.ChannelTaskDelivery, error) {
	return q.delivery, nil
}

// TestOutbound_SendsToSnapshotThreadNotMovedBindingCursor is the consumer half
// of the MUL-7234 chain. The producer half —
// TestRouterAnchorsPendingGenerationToItsOwnTriggerAcrossClear in the channel
// engine — proves the pipeline freezes A's trigger onto channel_task_delivery
// while the binding cursor sits at B. This asserts what Slack then does with
// that row: the message actually handed to the sender must target A's thread.
//
// Both halves meet at channel_task_delivery, which is the contract between the
// shared enqueue path and every adapter. Slack matters here because it reads
// the thread for routing while showing no visible quote, so a wrong value
// silently lands the answer in another conversation instead of looking odd.
func TestOutbound_SendsToSnapshotThreadNotMovedBindingCursor(t *testing.T) {
	base := &fakeOutboundQueries{
		// The live route: the cursor has already advanced to B's thread.
		binding: db.ChannelChatSessionBinding{
			InstallationID: uid(1),
			ChannelChatID:  "C123:thread-B",
			Config:         []byte(`{"channel_id":"C123"}`),
			LastMessageID:  pgtype.Text{String: "msg-B", Valid: true},
			LastThreadID:   pgtype.Text{String: "thread-B", Valid: true},
		},
		inst: db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()},
	}
	q := &anchoredDeliveryQueries{
		fakeOutboundQueries: base,
		// The snapshot this task was enqueued with: anchored to A.
		delivery: db.ChannelTaskDelivery{
			BindingID:        base.binding.ID,
			InstallationID:   uid(1),
			ChannelType:      string(TypeSlack),
			ChannelChatID:    "C123:thread-A",
			ChannelMessageID: pgtype.Text{String: "msg-A", Valid: true},
			ChannelThreadID:  pgtype.Text{String: "thread-A", Valid: true},
			Config:           []byte(`{"channel_id":"C123"}`),
		},
	}
	fs := &fakeSender{}

	newTestOutbound(q, fs).handleEvent(chatDoneEvent("00000000-0000-0000-0000-000000000001", "答案"))

	if fs.called != 1 {
		t.Fatalf("sender called %d times, want 1", fs.called)
	}
	if fs.got.ThreadID != "thread-A" {
		t.Errorf("sent into thread %q, want thread-A — a reply must land in the thread of the message it answers, not wherever the conversation has since moved",
			fs.got.ThreadID)
	}
	if fs.got.ChatID != "C123" {
		t.Errorf("ChatID = %q, want the real channel from the snapshot config", fs.got.ChatID)
	}
}
