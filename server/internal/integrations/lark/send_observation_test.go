package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestClassifySendObservation(t *testing.T) {
	at := time.Date(2026, 11, 2, 1, 2, 3, 4, time.UTC)
	for _, tc := range []struct {
		name, id, result, reason string
		err                      error
	}{
		{"confirmed", "om_id", "api_confirmed", "none", nil},
		{"missing id", "", "uncertain", "unconfirmed", nil},
		{"zero code", "", "uncertain", "unconfirmed", &APIError{Code: 0, Msg: "secret"}},
		{"sending", "", "uncertain", "unconfirmed", &APIError{Code: 230049}},
		{"timeout", "", "uncertain", "unconfirmed", context.DeadlineExceeded},
		{"unknown error", "", "uncertain", "unconfirmed", errors.New("secret")},
		{"unknown code", "", "uncertain", "unconfirmed", &APIError{Code: 999999}},
		{"server error with refusal code", "", "uncertain", "unconfirmed", &larkAPIStatusError{StatusCode: 500, Code: 230027}},
		{"group", "", "rejected", "bot_not_in_chat", &APIError{Code: 230002}},
		{"disabled", "", "rejected", "bot_disabled", &APIError{Code: 230006}},
		{"availability", "", "rejected", "app_unavailable", &APIError{Code: 230013}},
		{"permission", "", "rejected", "missing_permission", fmt.Errorf("wrapped: %w", &APIError{Code: 230027, Msg: "secret"})},
		{"http permission", "", "rejected", "chat_permission", &larkAPIStatusError{StatusCode: 400, Code: 230035, Raw: "secret"}},
		{"earlier uncertainty", "", "uncertain", "unconfirmed", &mediaSendUnconfirmed{err: &APIError{Code: 230027}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := classifySendObservation(at, tc.id, tc.err)
			if got.Result != tc.result || got.Reason != tc.reason || got.ObservedAt != "2026-11-02T01:02:03.000000004Z" {
				t.Fatalf("unexpected observation: %+v", got)
			}
			raw, _ := json.Marshal(got)
			if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "om_id") {
				t.Fatal("observation retained provider text or message identity")
			}
		})
	}
}

type sendObserverFunc func(context.Context, Installation, SendObservation) error

func (f sendObserverFunc) RecordSendObservation(ctx context.Context, i Installation, o SendObservation) error {
	return f(ctx, i, o)
}

type sendHookClient struct {
	APIClient
	hook func()
}

func (c sendHookClient) SendTextMessage(ctx context.Context, p SendTextParams) (string, error) {
	c.hook()
	return c.APIClient.SendTextMessage(ctx, p)
}

func TestSendObservationWiring(t *testing.T) {
	for _, mode := range []string{"text", "markdown", "fallback", "empty", "topic skip", "revoked", "cancelled", "failed task", "ownership skip"} {
		t.Run(mode, func(t *testing.T) {
			p, q, api := newTestPatcher(t)
			q.installation.BindingGeneration = "before-send"
			api.mdCardReturn = "om_card"
			var records []SendObservation
			p.cfg.SendObserver = sendObserverFunc(func(ctx context.Context, inst Installation, o SendObservation) error {
				if mode == "fallback" && len(api.textSent) != 2 {
					t.Fatal("diagnostic persistence ran before fallback completed")
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 250*time.Millisecond || ctx.Err() != nil {
					t.Fatal("observation must have an independent bounded context")
				}
				if inst.BindingGeneration != "before-send" {
					t.Fatal("captured a later binding generation")
				}
				records = append(records, o)
				return errors.New("storage unavailable")
			})
			p.client = sendHookClient{APIClient: api, hook: func() { q.installation.BindingGeneration = "after-send" }}
			e := events.Event{Type: protocol.EventChatDone, TaskID: "ee333333-ee33-ee33-ee33-eeeeeeeeeeee", ChatSessionID: uuidString(q.binding.ChatSessionID), Payload: protocol.ChatDonePayload{Content: "Hello"}}
			want := 1
			switch mode {
			case "markdown":
				e.Payload = protocol.ChatDonePayload{Content: "**Hello**"}
			case "fallback":
				q.binding.LastMessageID = pgtype.Text{String: "om_trigger", Valid: true}
				q.binding.LastThreadID = pgtype.Text{String: "omt_topic", Valid: true}
				api.threadReplyErr = errThreadReplyClassified
				want = 2
			case "empty":
				e.Payload = protocol.ChatDonePayload{}
				want = 0
			case "topic skip":
				q.binding.Config = []byte(`{"chat_id":"oc_parent"}`)
				want = 0
			case "revoked":
				q.installation.Status = "revoked"
				want = 0
			case "cancelled":
				e.Type = protocol.EventTaskCancelled
				want = 0
			case "failed task":
				e.Type = protocol.EventTaskFailed
				want = 0
			case "ownership skip":
				p.media = &fakeMediaAPIClient{}
				p.objects = fakeObjectStore{}
				q.mediaLedgerErr = errors.New("ownership unavailable")
				e.Payload = protocol.ChatDonePayload{Content: "Hello", TaskID: e.TaskID, MessageID: "dddd4444-dddd-dddd-dddd-dddddddddddd"}
				want = 0
			}
			if err := p.processEvent(context.Background(), e); err != nil {
				t.Fatal(err)
			}
			if len(records) != want {
				t.Fatalf("record count %d, want %d", len(records), want)
			}
			if want > 0 && records[want-1].Result != "api_confirmed" {
				t.Fatalf("last actual call not confirmed: %+v", records)
			}
			if mode == "fallback" && records[0].Result != "uncertain" {
				t.Fatal("unknown rejection code claimed a specific refusal")
			}
		})
	}
}

func TestSendObservationKeepsErrorAndSurvivesSendCancellation(t *testing.T) {
	p, q, _ := newTestPatcher(t)
	batch := &sendObservationBatch{installation: q.installation}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), sendObservationBatchKey{}, batch))
	sentinel := &APIError{Code: 230027}
	p.cfg.SendObserver = sendObserverFunc(func(recordCtx context.Context, _ Installation, o SendObservation) error {
		if recordCtx.Err() != nil || o.Result != "rejected" {
			t.Fatal("cancelled send lost refusal observation")
		}
		return errors.New("failed persistence")
	})
	id, err := p.observeSend(ctx, func() (string, error) { cancel(); return "", sentinel })
	p.recordSendObservations(ctx, batch)
	if id != "" || err != sentinel {
		t.Fatal("observation changed send outcome")
	}
}
