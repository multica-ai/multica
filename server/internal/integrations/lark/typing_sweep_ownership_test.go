package lark

import (
	"context"
	"testing"
)

func TestTypingSweepsPreserveForeignAppAndHuman(t *testing.T) {
	for _, durable := range []bool{false, true} {
		t.Run(map[bool]string{false: "fallback", true: "durable"}[durable], func(t *testing.T) {
			api := &fakeTypingAPIClient{listReturn: []MessageReaction{
				{ReactionID: "ours", OperatorType: "app", OperatorID: "cli_ours", EmojiType: typingEmoji},
				{ReactionID: "foreign", OperatorType: "app", OperatorID: "cli_other", EmojiType: typingEmoji},
				{ReactionID: "unknown", OperatorType: "app", EmojiType: typingEmoji},
				{ReactionID: "human", OperatorType: "user", OperatorID: "ou_user", EmojiType: typingEmoji},
			}}
			m := NewTypingIndicatorManager(api, fakeTypingCreds{}, &fakeTypingQueries{}, newDiscardLogger())
			creds := InstallationCredentials{AppID: "cli_ours"}
			if durable {
				if err := m.sweepForCleanup(context.Background(), creds, "message"); err != nil {
					t.Fatal(err)
				}
			} else {
				m.SweepMessage(context.Background(), creds, "message")
			}
			if len(api.deleteCalled) != 1 || api.deleteCalled[0].reactionID != "ours" {
				t.Fatalf("foreign reaction targeted: %+v", api.deleteCalled)
			}
		})
	}
}
