package dingtalk

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func TestIngestedReplyText(t *testing.T) {
	cases := []struct {
		name string
		res  engine.Result
		want string
	}{
		{"queued", engine.Result{IssueQueued: true}, engine.IssueQueuedAckText},
		{"usage", engine.Result{IssueUsage: true}, engine.IssueUsageText},
		{"queue failed", engine.Result{IssueQueueFailed: true}, engine.IssueQueueFailedText},
		{"fresh reset", engine.Result{FreshReset: true}, freshResetText},
		{"plain chat stays silent", engine.Result{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ingestedReplyText(tc.res); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDroppedReplyText(t *testing.T) {
	issueMsg := channel.InboundMessage{Text: "/issue login is broken", AddressedToBot: true}
	cases := []struct {
		name string
		res  engine.Result
		msg  channel.InboundMessage
		want string
	}{
		{"non-member /issue gets refusal",
			engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonNonWorkspaceMember},
			issueMsg, issueNotMemberText},
		{"revoked installation /issue gets disconnected notice",
			engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonRevokedInstallation},
			issueMsg, issueDisabledText},
		{"duplicate /issue stays silent",
			engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonDuplicate},
			issueMsg, ""},
		{"non-member plain chat stays silent",
			engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonNonWorkspaceMember},
			channel.InboundMessage{Text: "hello", AddressedToBot: true}, ""},
		{"unaddressed group /issue stays silent",
			engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonNonWorkspaceMember},
			channel.InboundMessage{Text: "/issue x", AddressedToBot: false}, ""},
		{"media fetch failure gets a resend prompt even without /issue",
			engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonMediaFetchFailed},
			channel.InboundMessage{Text: "look at this", AddressedToBot: true}, mediaFailedText},
		{"unsupported media (no seam) says images aren't supported, not resend",
			engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonMediaUnsupported},
			channel.InboundMessage{Text: "look at this", AddressedToBot: true}, mediaUnsupportedText},
		{"unsupported kind gets a capability notice",
			engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonUnsupportedKind},
			channel.InboundMessage{Type: channel.MsgTypeAudio, AddressedToBot: true}, unsupportedKindText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := droppedReplyText(tc.res, tc.msg); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
