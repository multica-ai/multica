package slack

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeChatRetrier struct {
	calls  int
	parent pgtype.UUID
	actor  pgtype.UUID
	err    error
}

func (f *fakeChatRetrier) RetryFailedChatTask(_ context.Context, parentID, actorUserID pgtype.UUID) (db.AgentTaskQueue, error) {
	f.calls++
	f.parent, f.actor = parentID, actorUserID
	return db.AgentTaskQueue{}, f.err
}

const retryTestTaskID = "00000000-0000-0000-0000-000000000002"

func retryQueries() *fakeSlashQueries {
	return &fakeSlashQueries{
		inst:    activeSlashInstallation(),
		binding: db.ChannelUserBinding{MulticaUserID: slashTestUUID(9)},
		delivery: db.ChannelTaskDelivery{
			InstallationID: slashTestUUID(1), ChannelType: string(TypeSlack),
			ChannelChatID: "C1:1111.0", Config: []byte(`{"channel_id":"C1"}`),
		},
	}
}

func retryClick() slack.InteractionCallback {
	var cb slack.InteractionCallback
	cb.Type = slack.InteractionTypeBlockActions
	cb.APIAppID = "A1"
	cb.Team.ID = "T1"
	cb.User.ID = "U1"
	cb.Channel.ID = "C1"
	cb.ResponseURL = "https://hooks.slack.test/action"
	cb.ActionCallback.BlockActions = []*slack.BlockAction{{ActionID: retryTaskActionID, Value: retryTestTaskID}}
	cb.Message.Blocks.BlockSet = failureCardBlocks(slashTestUUID(2), "boom", "https://app.example/acme/chat")
	return cb
}

func newRetryProcessor(q slashQueries, r chatRetrier) (*SlashCommandProcessor, *string, *[]slack.Block) {
	p, ephemeral, _ := newTestSlashProcessor(q, &fakeQuickCreate{}, &fakeBindingMinter{})
	p.retry = r
	var replaced []slack.Block
	p.replaceCard = func(_ context.Context, _ string, blocks []slack.Block, _ string) error {
		replaced = blocks
		return nil
	}
	return p, ephemeral, &replaced
}

func TestRetryAction_RetriesAsMemberAndRetiresCard(t *testing.T) {
	r := &fakeChatRetrier{}
	p, ephemeral, replaced := newRetryProcessor(retryQueries(), r)

	p.HandleBlockAction(context.Background(), retryClick())

	if r.calls != 1 || r.actor != slashTestUUID(9) {
		t.Fatalf("retry calls=%d actor=%v, want one retry as the bound member", r.calls, r.actor)
	}
	if *ephemeral != "" {
		t.Fatalf("unexpected ephemeral reply %q", *ephemeral)
	}
	if len(*replaced) != 2 {
		t.Fatalf("replaced card blocks = %d, want section + retried note", len(*replaced))
	}
	for _, b := range *replaced {
		if b.BlockType() == slack.MBTAction {
			t.Fatal("retried card must drop its buttons")
		}
	}
	note := (*replaced)[1].(*slack.ContextBlock).ContextElements.Elements[0].(*slack.TextBlockObject).Text
	if !strings.Contains(note, "<@U1>") {
		t.Fatalf("retried note %q must name the clicker", note)
	}
}

func TestRetryAction_RefusesWithoutRetrying(t *testing.T) {
	cases := map[string]struct {
		mutate  func(q *fakeSlashQueries, cb *slack.InteractionCallback, r *fakeChatRetrier)
		want    string
		retries int
	}{
		"stale turn": {
			mutate: func(_ *fakeSlashQueries, _ *slack.InteractionCallback, r *fakeChatRetrier) {
				r.err = service.ErrChatRetryStale
			},
			want:    retryStaleText,
			retries: 1,
		},
		"other installation's task": {
			mutate: func(q *fakeSlashQueries, _ *slack.InteractionCallback, _ *fakeChatRetrier) {
				q.delivery.InstallationID = slashTestUUID(8)
			},
			want: retryStaleText,
		},
		"different channel": {
			mutate: func(_ *fakeSlashQueries, cb *slack.InteractionCallback, _ *fakeChatRetrier) { cb.Channel.ID = "C2" },
			want:   retryStaleText,
		},
		"unknown task": {
			mutate: func(q *fakeSlashQueries, _ *slack.InteractionCallback, _ *fakeChatRetrier) {
				q.deliveryErr = pgx.ErrNoRows
			},
			want: retryStaleText,
		},
		"not a member": {
			mutate: func(q *fakeSlashQueries, _ *slack.InteractionCallback, _ *fakeChatRetrier) {
				q.memberErr = pgx.ErrNoRows
			},
			want: slashNotMemberText,
		},
		"retry error": {
			mutate: func(_ *fakeSlashQueries, _ *slack.InteractionCallback, r *fakeChatRetrier) {
				r.err = errors.New("db down")
			},
			want:    retryErrorText,
			retries: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			q, cb, r := retryQueries(), retryClick(), &fakeChatRetrier{}
			tc.mutate(q, &cb, r)
			p, ephemeral, replaced := newRetryProcessor(q, r)

			p.HandleBlockAction(context.Background(), cb)

			if r.calls != tc.retries {
				t.Fatalf("retry calls = %d, want %d", r.calls, tc.retries)
			}
			if *ephemeral != tc.want || *replaced != nil {
				t.Fatalf("reply = %q replaced=%v, want ephemeral %q and the card untouched", *ephemeral, *replaced != nil, tc.want)
			}
		})
	}
}

func TestRetryAction_IgnoresOpenLinkClick(t *testing.T) {
	r := &fakeChatRetrier{}
	p, ephemeral, _ := newRetryProcessor(retryQueries(), r)
	cb := retryClick()
	cb.ActionCallback.BlockActions = []*slack.BlockAction{{ActionID: openChatActionID}}

	p.HandleBlockAction(context.Background(), cb)

	if r.calls != 0 || *ephemeral != "" {
		t.Fatalf("open-link click must be a no-op, got retries=%d reply=%q", r.calls, *ephemeral)
	}
}
