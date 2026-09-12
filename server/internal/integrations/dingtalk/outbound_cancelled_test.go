package dingtalk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	cancelledTestTaskID    = "33333333-3333-3333-3333-333333333333"
	cancelledTestSessionID = "44444444-4444-4444-4444-444444444444"
)

// boundOutboundQueries answers as a deployment where the task was asked in a
// DingTalk 1:1 chat through a live installation: a delivery row naming
// dingtalk, a channel-ingested input, and an active installation whose config
// decrypts with the nil Decrypter the test passes to NewOutbound.
type boundOutboundQueries struct {
	channelType string
	ingested    bool
	noDelivery  bool
}

func (q boundOutboundQueries) GetChannelTaskDelivery(context.Context, pgtype.UUID) (db.ChannelTaskDelivery, error) {
	if q.noDelivery {
		return db.ChannelTaskDelivery{}, pgx.ErrNoRows
	}
	channelType := q.channelType
	if channelType == "" {
		channelType = string(TypeDingTalk)
	}
	return db.ChannelTaskDelivery{
		ChannelType:   channelType,
		ChannelChatID: "cid-1",
		Config:        json.RawMessage(`{"conversation_type":"1","staff_id":"staff-1"}`),
	}, nil
}

func (q boundOutboundQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	// An owned input batch, so the provenance check reads the stamp below
	// rather than short-circuiting on a pre-sealing task.
	var owner pgtype.UUID
	_ = owner.Scan(cancelledTestTaskID)
	return db.AgentTaskQueue{ChatInputTaskID: owner}, nil
}

func (q boundOutboundQueries) TaskHasChannelIngestedMessages(context.Context, pgtype.UUID) (bool, error) {
	return q.ingested, nil
}

func (q boundOutboundQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return db.ChannelInstallation{
		Status: "active",
		Config: json.RawMessage(`{"app_id":"ak","robot_code":"robot-1","app_secret_encrypted":""}`),
	}, nil
}

// newCancelledBus wires an Outbound over q onto a bus, sending through d.
func newCancelledBus(t *testing.T, d *dingtalkSendServer, q outboundQueries) *events.Bus {
	t.Helper()
	bus := events.New()
	NewOutbound(q, nil, NewClient(nil, d.srv.URL), nil).Register(bus)
	return bus
}

func cancelledEvent() events.Event {
	return events.Event{
		Type:          protocol.EventTaskCancelled,
		TaskID:        cancelledTestTaskID,
		ChatSessionID: cancelledTestSessionID,
		Payload: map[string]any{
			"task_id":         cancelledTestTaskID,
			"chat_session_id": cancelledTestSessionID,
			"status":          "cancelled",
		},
	}
}

// A cancelled run is the one ending that produces no text of its own, so before
// this subscription the DingTalk conversation kept the "👀 On it" ack and never
// heard anything again.
//
// REVERSE VERIFICATION: drop the EventTaskCancelled line from Register and no
// handler runs, so the chat receives nothing and this reports it. go build and
// go vet stay silent — an unsubscribed event type is not a compile error.
func TestOutboundCancelled_PostsTheNoticeIntoTheDingTalkChat(t *testing.T) {
	d := newDingtalkSendServer(t)
	bus := newCancelledBus(t, d, boundOutboundQueries{ingested: true})

	bus.Publish(cancelledEvent())

	if d.lastPath != pathSendP2P {
		t.Fatalf("send path = %q, want %q — the cancellation reached no DingTalk chat, and the "+
			"ack that promised a reply is still the last thing in it", d.lastPath, pathSendP2P)
	}
	param, _ := d.lastBody["msgParam"].(string)
	var got markdownParam
	if err := json.Unmarshal([]byte(param), &got); err != nil {
		t.Fatalf("msgParam = %q: %v", param, err)
	}
	if got.Text != cancelledNoticeText {
		t.Errorf("text = %q, want %q", got.Text, cancelledNoticeText)
	}
}

// The control: the notice is owed to the room that asked, and nothing else.
func TestOutboundCancelled_StaysSilentWhenTheTurnIsNotDingTalks(t *testing.T) {
	cases := []struct {
		name  string
		q     outboundQueries
		event events.Event
	}{
		{
			// No delivery row: the turn was asked in the Multica web UI, or the
			// snapshot invariant was violated. Either way there is no room.
			"no delivery row",
			boundOutboundQueries{noDelivery: true, ingested: true},
			cancelledEvent(),
		},
		{
			// Another platform's turn, on the shared bus this subscriber reads.
			"another platform's delivery row",
			boundOutboundQueries{channelType: "slack", ingested: true},
			cancelledEvent(),
		},
		{
			// Asked in the Multica web UI on a session that originated in
			// DingTalk: the answer, and the silence, belong only in Multica.
			"web-origin turn on a bound session",
			boundOutboundQueries{ingested: false},
			cancelledEvent(),
		},
		{
			// The failure path's own control, unchanged: an auto-retry is
			// pending, the run has not ended, and the retry reports its own
			// outcome.
			"failure with a retry pending",
			boundOutboundQueries{ingested: true},
			events.Event{
				Type:          protocol.EventTaskFailed,
				TaskID:        cancelledTestTaskID,
				ChatSessionID: cancelledTestSessionID,
				Payload:       map[string]any{"error": "task timed out", "retry_pending": true},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDingtalkSendServer(t)
			newCancelledBus(t, d, tc.q).Publish(tc.event)
			if d.lastPath != "" {
				t.Fatalf("the adapter sent to %q, want nothing — this run's ending is not "+
					"DingTalk's to announce", d.lastPath)
			}
		})
	}
}
