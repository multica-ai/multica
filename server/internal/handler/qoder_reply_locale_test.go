package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/wecom"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type noticeUserLookup struct {
	language string
	err      error
	reads    []pgtype.UUID
}

func (q *noticeUserLookup) GetUser(_ context.Context, id pgtype.UUID) (db.User, error) {
	q.reads = append(q.reads, id)
	return db.User{Language: pgtype.Text{String: q.language, Valid: q.language != ""}}, q.err
}

func TestChatNoticeLocaleFollowsDestination(t *testing.T) {
	previous := wecom.DeploymentLocale()
	t.Cleanup(func() { wecom.SetDeploymentLocale(string(previous)) })
	wecom.SetDeploymentLocale("en")
	reader := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	for _, tc := range []struct {
		name, channel, chatType, language, want string
		missing, lookupError, reads             bool
	}{
		{name: "Chinese web reader", language: "zh-Hans", want: "zh-Hans", reads: true},
		{name: "traditional Chinese IM reader", channel: "wecom", chatType: "p2p", language: "zh-Hant", want: "zh-Hant", reads: true},
		{name: "French IM reader", channel: "slack", chatType: "p2p", language: "fr", want: "fr", reads: true},
		{name: "WeCom room ignores personal profile", channel: "wecom", chatType: "group", language: "zh-Hans", want: "en"},
		{name: "unknown WeCom audience", channel: "wecom", language: "zh-Hans", want: "en"},
		{name: "Lark room", channel: "lark", chatType: "group", language: "en", want: "zh-Hans"},
		{name: "DingTalk room", channel: "dingtalk", chatType: "group", language: "en", want: "zh-Hans"},
		{name: "Slack room", channel: "slack", chatType: "group", language: "zh-Hans", want: "en"},
		{name: "unset profile", want: "en", reads: true},
		{name: "missing reader", missing: true, want: "en"},
		{name: "lookup failure", language: "zh-Hans", lookupError: true, want: "en", reads: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &noticeUserLookup{language: tc.language}
			if tc.lookupError {
				q.err = errors.New("profile unavailable")
			}
			id := reader
			if tc.missing {
				id = pgtype.UUID{}
			}
			if got := chatNoticeLocale(context.Background(), q, id, tc.channel, tc.chatType); got != tc.want {
				t.Fatalf("locale = %q, want %q", got, tc.want)
			}
			if tc.reads {
				if len(q.reads) != 1 || q.reads[0] != reader {
					t.Fatalf("wrong profile used: %v", q.reads)
				}
			} else if len(q.reads) != 0 {
				t.Fatalf("unexpected personal profile read: %v", q.reads)
			}
		})
	}
}

func TestOtherRuntimesDoNotLoadChatNoticeLocale(t *testing.T) {
	h := &Handler{}
	if got := h.qoderChatNoticeLocale(context.Background(), db.AgentRuntime{Provider: "codex"}, db.AgentTaskQueue{}, db.ChatSession{}, "", ""); got != "" {
		t.Fatalf("non-Qoder locale = %q", got)
	}
}
