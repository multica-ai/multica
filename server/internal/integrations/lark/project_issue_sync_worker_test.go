package lark

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestProjectSyncRetryDelay(t *testing.T) {
	tests := []struct {
		attempt int32
		want    time.Duration
	}{
		{attempt: 1, want: 5 * time.Second},
		{attempt: 2, want: 30 * time.Second},
		{attempt: 3, want: 2 * time.Minute},
		{attempt: 4, want: 10 * time.Minute},
		{attempt: 5, want: 30 * time.Minute},
	}
	for _, tt := range tests {
		if got := projectSyncRetryDelay(tt.attempt); got != tt.want {
			t.Fatalf("attempt %d: got %s want %s", tt.attempt, got, tt.want)
		}
	}
}

func TestSanitizeSyncError(t *testing.T) {
	got := sanitizeSyncError(errors.New("upstream\nsecret\rdetail"))
	if got != "upstream secret detail" {
		t.Fatalf("got %q", got)
	}
	long := sanitizeSyncError(errors.New(strings.Repeat("x", 700)))
	if len(long) != 500 {
		t.Fatalf("length = %d, want 500", len(long))
	}
}

func TestSafeFailureReason(t *testing.T) {
	if got := safeFailureReason(""); got != "Task execution failed" {
		t.Fatalf("empty reason = %q", got)
	}
	if got := safeFailureReason(strings.Repeat("x", 161)); got != "Task execution failed" {
		t.Fatalf("long reason = %q", got)
	}
	if got := safeFailureReason("worker exited"); got != "worker exited" {
		t.Fatalf("short reason = %q", got)
	}
}

func TestTopicRootMessageID(t *testing.T) {
	tests := []struct {
		name string
		msg  channel.InboundMessage
		want string
	}{
		{
			name: "reply root",
			msg: channel.InboundMessage{
				MessageID: "child",
				ReplyTo:   &channel.ReplyCtx{RootID: "root"},
			},
			want: "root",
		},
		{
			name: "thread root message",
			msg: channel.InboundMessage{
				MessageID: "root",
				Source:    channel.Source{ThreadID: "thread"},
			},
			want: "root",
		},
		{
			name: "plain group message",
			msg:  channel.InboundMessage{MessageID: "message"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := topicRootMessageID(tt.msg); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}
