package dingtalk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type noDeliveryOutboundQueries struct{}

func (noDeliveryOutboundQueries) ListChatInputMessages(context.Context, pgtype.UUID) ([]db.ChatMessage, error) {
	panic("must not read input without delivery")
}

func (noDeliveryOutboundQueries) GetChannelTaskDelivery(context.Context, pgtype.UUID) (db.ChannelTaskDelivery, error) {
	return db.ChannelTaskDelivery{}, pgx.ErrNoRows
}
func (noDeliveryOutboundQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	panic("GetAgentTask must not run without a task delivery snapshot")
}
func (noDeliveryOutboundQueries) TaskHasChannelIngestedMessages(context.Context, pgtype.UUID) (bool, error) {
	panic("TaskHasChannelIngestedMessages must not run without a task delivery snapshot")
}
func (noDeliveryOutboundQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	panic("GetChannelInstallation must not run without a task delivery snapshot")
}

type deliveryOnlyOutboundQueries struct {
	noDeliveryOutboundQueries
	delivery        db.ChannelTaskDelivery
	installation    db.ChannelInstallation
	task            db.AgentTaskQueue
	channelIngested bool
	input           []db.ChatMessage
	inputErr        error
}

type missingDeliveryInputQueries struct{ deliveryOnlyOutboundQueries }

func (missingDeliveryInputQueries) GetChannelTaskDelivery(context.Context, pgtype.UUID) (db.ChannelTaskDelivery, error) {
	return db.ChannelTaskDelivery{}, pgx.ErrNoRows
}

func (q deliveryOnlyOutboundQueries) ListChatInputMessages(context.Context, pgtype.UUID) ([]db.ChatMessage, error) {
	return q.input, q.inputErr
}

func (q deliveryOnlyOutboundQueries) GetChannelTaskDelivery(context.Context, pgtype.UUID) (db.ChannelTaskDelivery, error) {
	return q.delivery, nil
}

func (q deliveryOnlyOutboundQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return q.installation, nil
}

func (q deliveryOnlyOutboundQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	return q.task, nil
}

func (q deliveryOnlyOutboundQueries) TaskHasChannelIngestedMessages(context.Context, pgtype.UUID) (bool, error) {
	return q.channelIngested, nil
}

func TestOutboundFailsClosedWithoutTaskDeliverySnapshot(t *testing.T) {
	o := NewOutbound(noDeliveryOutboundQueries{}, nil, nil, nil, nil)
	event := events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        "11111111-1111-1111-1111-111111111111",
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload:       protocol.ChatDonePayload{Content: "must stay in Multica"},
	}
	if err := o.processEvent(context.Background(), event); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
}

func TestEventContent(t *testing.T) {
	cases := []struct {
		name  string
		event events.Event
		want  string
	}{
		{"chat done typed", events.Event{Type: protocol.EventChatDone, Payload: protocol.ChatDonePayload{Content: "reply"}}, "reply"},
		{"map round trip", events.Event{Type: protocol.EventChatDone, Payload: map[string]any{"content": "from map"}}, "from map"},
		{"empty map", events.Event{Type: protocol.EventChatDone, Payload: map[string]any{}}, ""},
		{"nil", events.Event{Type: protocol.EventChatDone}, ""},
		{
			"task failed with error",
			events.Event{Type: protocol.EventTaskFailed, Payload: map[string]any{"error": "task timed out", "retry_pending": false}},
			"⚠️ task timed out",
		},
		{
			// Retry-pending failures stay silent even if a mixed-version
			// publisher accidentally includes an error string.
			"task failed with retry pending",
			events.Event{Type: protocol.EventTaskFailed, Payload: map[string]any{"error": "task timed out", "failure_reason": "timeout", "retry_pending": true}},
			"",
		},
		{
			// Failure broadcasts without an error text have nothing safe to
			// deliver and stay silent.
			"task failed without error",
			events.Event{Type: protocol.EventTaskFailed, Payload: map[string]any{"failure_reason": "timeout", "retry_pending": false}},
			"",
		},
		{
			// task:failed payloads never carry "content"; it must not leak
			// through the chat-done branch.
			"task failed ignores content key",
			events.Event{Type: protocol.EventTaskFailed, Payload: map[string]any{"content": "not for delivery"}},
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eventContent(tc.event); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEventRetryPending(t *testing.T) {
	if eventRetryPending(events.Event{Type: protocol.EventTaskFailed}) {
		t.Fatal("missing payload marked retry pending")
	}
	if !eventRetryPending(events.Event{Type: protocol.EventTaskFailed, Payload: map[string]any{"retry_pending": true}}) {
		t.Fatal("retry-pending failure must keep the processing reaction")
	}
	if eventRetryPending(events.Event{Type: protocol.EventChatDone, Payload: map[string]any{"retry_pending": true}}) {
		t.Fatal("chat-done must be terminal")
	}
}

func TestOutboundTerminalReactionLifecycle(t *testing.T) {
	tests := []struct {
		name             string
		eventType        string
		payload          any
		sendFails        bool
		wantReply        bool
		wantDone         bool
		keepActive       bool
		inputUnavailable bool
		cancelOnRecall   bool
	}{
		{name: "completion", eventType: protocol.EventChatDone, payload: protocol.ChatDonePayload{Content: "answer"}, wantReply: true, wantDone: true},
		{name: "failed task", eventType: protocol.EventTaskFailed, payload: map[string]any{"error": "safe failure", "retry_pending": false}, wantReply: true},
		{name: "cleanup after reply budget", eventType: protocol.EventChatDone, payload: protocol.ChatDonePayload{Content: "answer"}, cancelOnRecall: true, wantReply: true},
		{name: "input lookup failure", eventType: protocol.EventChatDone, payload: protocol.ChatDonePayload{Content: "answer"}, inputUnavailable: true, wantReply: true, keepActive: true},
		{name: "failed delivery", eventType: protocol.EventChatDone, payload: protocol.ChatDonePayload{Content: "answer"}, sendFails: true, wantReply: true},
		{name: "cancelled", eventType: protocol.EventTaskCancelled},
		{name: "empty completion", eventType: protocol.EventChatDone, payload: protocol.ChatDonePayload{}},
		{name: "empty failure", eventType: protocol.EventTaskFailed, payload: map[string]any{"retry_pending": false}},
		{name: "retry pending", eventType: protocol.EventTaskFailed, payload: map[string]any{"error": "must remain private", "retry_pending": true}, keepActive: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var actions []string
			client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body, status := `{"accessToken":"token","expireIn":7200}`, http.StatusOK
				if r.URL.Path == pathSendGroup {
					actions = append(actions, "reply")
					body = `{"processQueryKey":"sent"}`
					if tc.sendFails {
						status = http.StatusInternalServerError
						body = `{"code":"failed","message":"test failure"}`
					}
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}, "https://dingtalk.test")
			config, err := json.Marshal(installConfig{AppID: "app", RobotCode: "robot", AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret"))})
			if err != nil {
				t.Fatal(err)
			}
			sid, taskID := sessionUUID(45), sessionUUID(46)
			q := deliveryOnlyOutboundQueries{
				delivery:     db.ChannelTaskDelivery{ChannelType: string(TypeDingTalk), ChannelChatID: "conversation", ChatType: string(channel.ChatTypeGroup), ChannelMessageID: pgtype.Text{String: "source", Valid: true}},
				installation: db.ChannelInstallation{ID: sessionUUID(47), Status: "active", Config: config},
				input:        []db.ChatMessage{{ID: sessionUUID(48), ChannelIngested: true, Content: "question"}},
				task:         db.AgentTaskQueue{ChatInputTaskID: taskID}, channelIngested: true,
			}
			if tc.inputUnavailable {
				q.inputErr = errors.New("input lookup unavailable")
			}
			ack := NewAckNotifier(client, nil, nil, nil)
			client.rememberReplySource(q.installation.ID, sessionUUID(48), groupReactionMessage("source"))
			ack.sendReaction = func(reactionCtx context.Context, _ engine.ResolvedInstallation, msg channel.InboundMessage, name string, recall bool) error {
				if tc.cancelOnRecall && recall {
					cancel()
				}
				if name == emotionDone && reactionCtx.Err() != nil {
					return reactionCtx.Err()
				}
				verb := "add:"
				if recall {
					verb = "recall:"
				}
				actions = append(actions, verb+msg.MessageID+":"+name)
				return nil
			}
			ack.OnIngested(context.Background(), engine.ResolvedInstallation{ID: q.installation.ID}, groupReactionMessage("source"), sid)
			o := NewOutbound(q, nil, client, ack, nil)
			err = o.processEvent(ctx, events.Event{Type: tc.eventType, TaskID: util.UUIDToString(taskID), ChatSessionID: util.UUIDToString(sid), Payload: tc.payload})
			if (err != nil) != tc.sendFails {
				t.Fatalf("processEvent error = %v, want send failure=%v", err, tc.sendFails)
			}
			want := []string{"add:source:" + emotionAcknowledged}
			if tc.wantReply {
				want = append(want, "reply")
			}
			if !tc.keepActive {
				want = append(want, "recall:source:"+emotionAcknowledged)
			}
			if tc.wantDone {
				want = append(want, "add:source:"+emotionDone)
			}
			if !slices.Equal(actions, want) {
				t.Fatalf("lifecycle actions = %v, want %v", actions, want)
			}
			if (len(ack.active) != 0) != tc.keepActive {
				t.Fatalf("active reactions = %d, keep=%v", len(ack.active), tc.keepActive)
			}
		})
	}
}

func TestOutboundTerminalWithoutDeliverySettlesOnlyOwnedInput(t *testing.T) {
	for _, eventType := range []string{protocol.EventChatDone, protocol.EventTaskFailed, protocol.EventTaskCancelled} {
		t.Run(eventType, func(t *testing.T) {
			ack, actions := newTestAckWithMessageIDs(time.Now)
			sid := sessionUUID(61)
			inst := engine.ResolvedInstallation{ID: sessionUUID(9)}
			for i, name := range []string{"a", "b", "other"} {
				ack.client.rememberReplySource(inst.ID, sessionUUID(byte(70+i)), groupReactionMessage(name))
			}
			ack.OnIngested(context.Background(), inst, groupReactionMessage("a"), sid)
			ack.OnIngested(context.Background(), inst, groupReactionMessage("b"), sid)
			ack.OnIngested(context.Background(), inst, groupReactionMessage("other"), sessionUUID(62))
			q := missingDeliveryInputQueries{deliveryOnlyOutboundQueries{
				task:  db.AgentTaskQueue{ChatInputTaskID: sessionUUID(63)},
				input: []db.ChatMessage{{ID: sessionUUID(70), ChannelIngested: true}},
			}}
			o := NewOutbound(q, nil, nil, ack, nil)
			bus := events.New()
			o.Register(bus)
			bus.Publish(events.Event{Type: eventType, TaskID: util.UUIDToString(sessionUUID(63)), ChatSessionID: util.UUIDToString(sid)})
			want := []string{"add:a:" + emotionAcknowledged, "add:b:" + emotionAcknowledged, "add:other:" + emotionAcknowledged, "recall:a:" + emotionAcknowledged}
			if !slices.Equal(*actions, want) {
				t.Fatalf("actions=%v want=%v", *actions, want)
			}
		})
	}
}

func TestOutboundPartialReplyFailureDoesNotMarkDone(t *testing.T) {
	var sends int
	client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, status := `{"accessToken":"token","expireIn":7200}`, http.StatusOK
		if r.URL.Path == pathSendGroup {
			sends++
			body = `{"processQueryKey":"sent"}`
			if sends == 2 {
				status = http.StatusInternalServerError
				body = `{}`
			}
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}, "https://dingtalk.test")
	config, err := json.Marshal(installConfig{AppID: "app", AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret"))})
	if err != nil {
		t.Fatal(err)
	}
	sid, tid := sessionUUID(64), sessionUUID(65)
	q := deliveryOnlyOutboundQueries{
		delivery:     db.ChannelTaskDelivery{ChannelType: string(TypeDingTalk), ChannelChatID: "group", ChannelMessageID: nullText("source")},
		installation: db.ChannelInstallation{ID: sessionUUID(66), Status: "active", Config: config}, task: db.AgentTaskQueue{ChatInputTaskID: tid}, channelIngested: true,
		input: []db.ChatMessage{{ID: sessionUUID(67), ChannelIngested: true, Content: "question"}},
	}
	ack, actions := newTestAckWithMessageIDs(time.Now)
	ack.client.rememberReplySource(q.installation.ID, q.input[0].ID, groupReactionMessage("source"))
	ack.OnIngested(context.Background(), engine.ResolvedInstallation{ID: q.installation.ID}, groupReactionMessage("source"), sid)
	err = NewOutbound(q, nil, client, ack, nil).processEvent(context.Background(), events.Event{
		Type: protocol.EventChatDone, TaskID: util.UUIDToString(tid), ChatSessionID: util.UUIDToString(sid), Payload: protocol.ChatDonePayload{Content: strings.Repeat("answer\n", 6000)},
	})
	if err == nil || sends != 2 {
		t.Fatalf("sends=%d error=%v", sends, err)
	}
	if !slices.Equal(*actions, []string{"add:source:" + emotionAcknowledged, "recall:source:" + emotionAcknowledged}) {
		t.Fatalf("partial reply marked complete: %v", *actions)
	}
}
