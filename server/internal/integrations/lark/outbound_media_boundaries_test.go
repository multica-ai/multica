package lark

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type controlledMedia struct {
	*fakeMediaAPIClient
	uploadImage func(context.Context, UploadImageParams) (string, error)
	sendImage   func(context.Context, SendImageParams) (string, error)
}

func (c *controlledMedia) UploadImage(ctx context.Context, p UploadImageParams) (string, error) {
	if c.uploadImage != nil {
		return c.uploadImage(ctx, p)
	}
	return c.fakeMediaAPIClient.UploadImage(ctx, p)
}
func (c *controlledMedia) SendImageMessage(ctx context.Context, p SendImageParams) (string, error) {
	if c.sendImage != nil {
		return c.sendImage(ctx, p)
	}
	return c.fakeMediaAPIClient.SendImageMessage(ctx, p)
}

func mediaFixture(t *testing.T) (*Patcher, *fakePatcherQueries, *fakeMediaAPIClient, string) {
	t.Helper()
	p, q, api := newMediaTestPatcher(t)
	p.spawn = func(f func()) { f() }
	p.SetAttachments(fakeObjectStore{objects: map[string][]byte{"chart.png": pngBytes}})
	q.attachments = []db.Attachment{imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "chart.png")}
	return p, q, api, "dddd4444-dddd-dddd-dddd-dddddddddddd"
}
func runMediaFixture(p *Patcher, q *fakePatcherQueries, id string) {
	p.deliverAttachments(context.Background(), testCreds(), q.binding, ReplyTarget{}, q.installation.WorkspaceID, id)
}

func TestOutboundImageDegradesToFile(t *testing.T) {
	for _, scenario := range []string{"svg", "oversize", "format-refused", "unknown-type"} {
		t.Run(scenario, func(t *testing.T) {
			p, q, api, id := mediaFixture(t)
			switch scenario {
			case "svg":
				q.attachments[0].ContentType = "IMAGE/SVG+XML; charset=utf-8"
			case "oversize":
				p.SetAttachments(fakeObjectStore{objects: map[string][]byte{"chart.png": make([]byte, larkImageUploadMaxBytes+1)}})
			case "format-refused":
				p.media = &controlledMedia{fakeMediaAPIClient: api, uploadImage: func(context.Context, UploadImageParams) (string, error) {
					return "", &APIError{Op: "upload image", Code: 234001, Msg: "format rejected"}
				}}
			case "unknown-type":
				q.attachments[0].ContentType = "image/future-format"
			}
			runMediaFixture(p, q, id)
			if len(api.files) != 1 || len(api.filesOut) != 1 || len(api.imagesOut) != 0 || len(api.textSent) != 0 {
				t.Fatalf("files=%d images=%d notices=%d", len(api.filesOut), len(api.imagesOut), len(api.textSent))
			}
		})
	}
}

func TestOutboundUncertainSendUsesSameUUID(t *testing.T) {
	for _, recovers := range []bool{true, false} {
		t.Run(map[bool]string{true: "lost-first-response", false: "both-responses-lost"}[recovers], func(t *testing.T) {
			p, q, api, id := mediaFixture(t)
			var requests []SendImageParams
			p.media = &controlledMedia{fakeMediaAPIClient: api, sendImage: func(_ context.Context, r SendImageParams) (string, error) {
				requests = append(requests, r)
				if recovers && len(requests) == 2 {
					return "accepted", nil
				}
				return "", errors.New("connection reset after write")
			}}
			runMediaFixture(p, q, id)
			if len(requests) != 2 || requests[0].UUID == "" || requests[0].UUID != requests[1].UUID {
				t.Fatalf("idempotency lost: %+v", requests)
			}
			if recovers {
				if len(api.textSent) != 0 {
					t.Fatal("successful reconciliation warned")
				}
			} else if len(api.textSent) != 1 || api.textSent[0].Text != mediaSendUnknownText {
				t.Fatalf("incorrect uncertainty notice: %+v", api.textSent)
			}
		})
	}
}

func TestOutboundDefiniteRefusalDoesNotRetry(t *testing.T) {
	p, q, api, id := mediaFixture(t)
	calls := 0
	p.media = &controlledMedia{fakeMediaAPIClient: api, sendImage: func(context.Context, SendImageParams) (string, error) {
		calls++
		return "", &APIError{Code: 230001, Msg: "refused"}
	}}
	runMediaFixture(p, q, id)
	if calls != 1 || len(api.textSent) != 1 || api.textSent[0].Text != mediaSendFailedText {
		t.Fatalf("calls=%d notices=%+v", calls, api.textSent)
	}
}

func TestDefiniteMediaRefusal(t *testing.T) {
	beingSent := &larkAPIStatusError{StatusCode: 400, Code: 230049, Msg: "The message is being sent."}
	beingSentAPI := &APIError{Code: 230049, Msg: "The message is being sent."}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"being-sent-http400", beingSent, false},
		{"being-sent-api-error", beingSentAPI, false},
		{"wrapped-being-sent-http400", fmt.Errorf("send image message: %w", beingSent), false},
		{"wrapped-being-sent-api-error", fmt.Errorf("send image message: %w", beingSentAPI), false},
		{"explicit-http400", &larkAPIStatusError{StatusCode: 400, Code: 230055}, true},
		{"explicit-http403", &larkAPIStatusError{StatusCode: 403, Code: 230035}, true},
		{"rate-limited-http429", &larkAPIStatusError{StatusCode: 429, Code: 230020}, true},
		{"http400-without-business-code", &larkAPIStatusError{StatusCode: 400}, true},
		{"explicit-api-error", &APIError{Code: 230001}, true},
		{"wrapped-explicit-refusal", fmt.Errorf("send image message: %w", &APIError{Code: 230001}), true},
		{"http502", &larkAPIStatusError{StatusCode: 502}, false},
		{"missing-message-id", &APIError{Code: 0}, false},
		{"transport-error", errors.New("response lost"), false},
		{"no-error", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := definiteMediaRefusal(tc.err); got != tc.want {
				t.Fatalf("definiteMediaRefusal(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestOutboundBeingSentKeepsOriginalDelivery(t *testing.T) {
	beingSent := &larkAPIStatusError{StatusCode: 400, Code: 230049, Msg: "The message is being sent."}
	beingSentAPI := &APIError{Code: 230049, Msg: "The message is being sent."}
	for _, response := range []struct {
		name string
		err  error
	}{
		{"http400", beingSent},
		{"api-error", beingSentAPI},
		{"wrapped-http400", fmt.Errorf("send image message: %w", beingSent)},
		{"wrapped-api-error", fmt.Errorf("send image message: %w", beingSentAPI)},
	} {
		t.Run(response.name, func(t *testing.T) {
			for _, outcome := range []string{"accepted-on-retry", "still-being-sent", "refused-on-retry"} {
				t.Run(outcome, func(t *testing.T) {
					p, q, api, id := mediaFixture(t)
					target := ReplyTarget{MessageID: "om_trigger", InThread: true}
					var requests []SendImageParams
					p.media = &controlledMedia{fakeMediaAPIClient: api, sendImage: func(_ context.Context, r SendImageParams) (string, error) {
						requests = append(requests, r)
						if len(requests) == 1 {
							return "", response.err
						}
						switch outcome {
						case "accepted-on-retry":
							return "om_accepted", nil
						case "refused-on-retry":
							return "", errThreadReplyClassified
						default:
							return "", response.err
						}
					}}
					p.deliverAttachments(context.Background(), testCreds(), q.binding, target, q.installation.WorkspaceID, id)
					if len(requests) != 2 {
						t.Fatalf("being-sent delivery must get one bounded retry: %+v", requests)
					}
					for _, request := range requests {
						if request.UUID == "" || request.UUID != requests[0].UUID || request.ReplyTarget != target || request.ChatID != outboundChatID(q.binding) {
							t.Fatalf("retry changed the delivery identity or destination: %+v", request)
						}
					}
					if outcome == "accepted-on-retry" {
						if len(api.textSent) != 0 {
							t.Fatalf("confirmed delivery must not warn: %+v", api.textSent)
						}
						return
					}
					if len(api.textSent) != 1 || api.textSent[0].Text != mediaSendUnknownText || api.textSent[0].ReplyTarget != target || api.textSent[0].ChatID != outboundChatID(q.binding) {
						t.Fatalf("unconfirmed delivery must remain unknown in the original topic: %+v", api.textSent)
					}
				})
			}
		})
	}
}

func TestOutboundUncertaintySurvivesClassifiedRetryRefusal(t *testing.T) {
	p, q, api, id := mediaFixture(t)
	var requests []SendImageParams
	p.media = &controlledMedia{fakeMediaAPIClient: api, sendImage: func(_ context.Context, r SendImageParams) (string, error) {
		requests = append(requests, r)
		switch len(requests) {
		case 1:
			return "", errors.New("response lost after message submission")
		case 2:
			return "", errThreadReplyClassified
		default:
			return "unexpected-parent-message", nil
		}
	}}
	target := ReplyTarget{MessageID: "om_trigger", InThread: true}
	p.deliverAttachments(context.Background(), testCreds(), q.binding, target, q.installation.WorkspaceID, id)
	if len(requests) != 2 {
		t.Fatalf("uncertain topic reply escaped to a new destination: %d requests", len(requests))
	}
	for _, request := range requests {
		if request.ReplyTarget != target || request.UUID == "" || request.UUID != requests[0].UUID {
			t.Fatalf("retry changed the target or idempotency key: %+v", request)
		}
	}
	if len(api.textSent) != 1 || api.textSent[0].Text != mediaSendUnknownText || api.textSent[0].ReplyTarget != target {
		t.Fatalf("uncertainty must remain visible in the original topic: %+v", api.textSent)
	}
}

func TestOutboundRevocationDuringUploadStopsAllWrites(t *testing.T) {
	for _, scenario := range []string{"revoked", "app-rebound", "session-archived", "workspace-changed"} {
		t.Run(scenario, func(t *testing.T) {
			p, q, api, id := mediaFixture(t)
			p.media = &controlledMedia{fakeMediaAPIClient: api, uploadImage: func(context.Context, UploadImageParams) (string, error) {
				switch scenario {
				case "revoked":
					q.installation.Status = "revoked"
				case "app-rebound":
					q.installation.AppID = "different-app"
				case "session-archived":
					q.mediaSession = &db.ChatSession{Status: "archived", WorkspaceID: q.installation.WorkspaceID}
				case "workspace-changed":
					q.installation.WorkspaceID = pgtype.UUID{}
				}
				return "uploaded-before-revocation", nil
			}}
			runMediaFixture(p, q, id)
			if len(api.imagesOut)+len(api.filesOut)+len(api.textSent) != 0 {
				t.Fatal("a write escaped after permission was withdrawn")
			}
		})
	}
}

type noticeContextClient struct {
	*fakeMediaAPIClient
	noticeContextError error
}

func (c *noticeContextClient) SendTextMessage(ctx context.Context, p SendTextParams) (string, error) {
	c.noticeContextError = ctx.Err()
	return c.fakeMediaAPIClient.SendTextMessage(ctx, p)
}

func TestOutboundQueueDeadlineStillSendsNotice(t *testing.T) {
	p, q, api, id := mediaFixture(t)
	client := &noticeContextClient{fakeMediaAPIClient: api}
	p.client = client
	for i := 0; i < cap(p.mediaSlots); i++ {
		p.mediaSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(p.mediaSlots); i++ {
			<-p.mediaSlots
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	p.deliverAttachments(ctx, testCreds(), q.binding, ReplyTarget{}, q.installation.WorkspaceID, id)
	if client.noticeContextError != nil || len(api.textSent) != 1 || api.textSent[0].Text != mediaSendFailedText {
		t.Fatalf("notice context=%v messages=%+v", client.noticeContextError, api.textSent)
	}
}

func TestOutboundCapacityBoundsLookupAndWaitsForSlot(t *testing.T) {
	t.Run("admission-before-spawn", func(t *testing.T) {
		p, q, _, id := mediaFixture(t)
		for i := 0; i < cap(p.mediaAdmission); i++ {
			p.mediaAdmission <- struct{}{}
		}
		spawned := false
		p.spawn = func(func()) { spawned = true }
		p.deliverAttachmentsAsync(context.Background(), testCreds(), q.binding, ReplyTarget{}, q.installation.WorkspaceID, map[string]any{"message_id": id, "task_id": id})
		if spawned || len(q.attachmentLookups) != 0 {
			t.Fatal("full admission still spawned or queried")
		}
	})
	t.Run("bounded-pending", func(t *testing.T) {
		p, q, api, id := mediaFixture(t)
		for i := 0; i < cap(p.mediaPending); i++ {
			p.mediaPending <- struct{}{}
		}
		runMediaFixture(p, q, id)
		if len(api.imagesOut) != 0 || len(api.textSent) != 1 {
			t.Fatal("pending overflow not reported")
		}
	})
	t.Run("busy-upload-waits", func(t *testing.T) {
		p, q, api, id := mediaFixture(t)
		for i := 0; i < cap(p.mediaSlots); i++ {
			p.mediaSlots <- struct{}{}
		}
		var release sync.Once
		free := func() {
			release.Do(func() {
				for i := 0; i < cap(p.mediaSlots); i++ {
					<-p.mediaSlots
				}
			})
		}
		defer free()
		done := make(chan struct{})
		go func() { defer close(done); runMediaFixture(p, q, id) }()
		waitFor(t, func() bool { return len(p.mediaPending) == 1 }, "pending slot")
		api.mu.Lock()
		n := len(api.textSent)
		api.mu.Unlock()
		if n != 0 {
			t.Fatal("busy slot shed instead of waiting")
		}
		free()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("delivery did not resume")
		}
		if len(api.imagesOut) != 1 || len(api.textSent) != 0 {
			t.Fatal("queued file was not delivered")
		}
	})
}

func TestOutboundRepeatedCompletionAndInterruptedBatch(t *testing.T) {
	t.Run("duplicate-event", func(t *testing.T) {
		p, q, api, id := mediaFixture(t)
		payload := map[string]any{"message_id": id, "task_id": id}
		p.deliverAttachmentsAsync(context.Background(), testCreds(), q.binding, ReplyTarget{}, q.installation.WorkspaceID, payload)
		p.deliverAttachmentsAsync(context.Background(), testCreds(), q.binding, ReplyTarget{}, q.installation.WorkspaceID, payload)
		if len(api.imagesOut) != 1 {
			t.Fatalf("duplicate attachment sends=%d", len(api.imagesOut))
		}
	})
	t.Run("crashed-in-flight-batch", func(t *testing.T) {
		p, q, api, id := mediaFixture(t)
		q.mediaLedger = map[string]db.ChannelReplyDelivery{id: {Phase: "terminal", SendState: "in_flight"}}
		p.deliverAttachmentsAsync(context.Background(), testCreds(), q.binding, ReplyTarget{}, q.installation.WorkspaceID, map[string]any{"message_id": id, "task_id": id})
		if len(api.imagesOut) != 0 || len(api.images) != 0 || len(api.textSent) != 1 || api.textSent[0].Text != mediaSendUnknownText {
			t.Fatalf("interrupted batch replayed or misreported: %+v", api.textSent)
		}
	})
	t.Run("database-unavailable", func(t *testing.T) {
		p, q, api, id := mediaFixture(t)
		q.mediaLedgerErr = errors.New("database unavailable")
		p.deliverAttachmentsAsync(context.Background(), testCreds(), q.binding, ReplyTarget{}, q.installation.WorkspaceID, map[string]any{"message_id": id, "task_id": id})
		if len(api.imagesOut)+len(api.images) != 0 {
			t.Fatal("sent without durable ownership")
		}
	})
}
