package dingtalk

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func TestIssueCreatedText(t *testing.T) {
	issueID := pgtype.UUID{Valid: true}
	if got := issueCreatedText(engine.Result{IssueID: issueID, IssueIdentifier: "MUL-42", IssueTitle: "Fix login"}); got != "✅ Created MUL-42 — Fix login" {
		t.Fatalf("got %q", got)
	}
	if got := issueCreatedText(engine.Result{IssueID: issueID, IssueNumber: 7}); got != "✅ Created #7" {
		t.Fatalf("fallback got %q", got)
	}
}

func TestIssueDuplicateText(t *testing.T) {
	issueID := pgtype.UUID{Bytes: [16]byte{9}, Valid: true}
	got := issueDuplicateText(engine.Result{
		IssueID: issueID, IssueIdentifier: "MUL-42", IssueTitle: "Fix login", IssueDuplicate: true,
	})
	if got != "⚠️ Not created — active issue MUL-42 already exists: Fix login" {
		t.Fatalf("duplicate text = %q", got)
	}
}

func TestIssueUsageCopy(t *testing.T) {
	if issueUsageText != "Please include an issue title. Use:\n\n`/issue <title>`\n\n`[description]` (optional)" {
		t.Fatalf("plain issue usage copy = %q", issueUsageText)
	}
	if issueUsageWithMediaText != "Please add a title and resend with the image (*image can come before or after the command*):\n\n`/issue <title>`\n\n`[description]` (optional)" {
		t.Fatalf("media issue usage copy = %q", issueUsageWithMediaText)
	}
}

func TestDroppedReplyText(t *testing.T) {
	issueMsg := channel.InboundMessage{Text: "[Image]", CommandText: "/issue login is broken", AddressedToBot: true}
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := droppedReplyText(tc.res, tc.msg); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
