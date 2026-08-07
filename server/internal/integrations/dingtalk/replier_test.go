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

func TestCommandOutcomeText(t *testing.T) {
	for _, tc := range []struct {
		outcome engine.Outcome
		want    string
	}{
		{engine.OutcomeFreshPending, freshPendingText},
		{engine.OutcomeIssueUsage, "Please include an issue title. Use:\n\n`/issue <title>`\n\n`[description]` (optional)"},
		{engine.OutcomeIngested, ""},
	} {
		if got := commandOutcomeText(engine.Result{Outcome: tc.outcome}); got != tc.want {
			t.Errorf("outcome %s: got %q, want %q", tc.outcome, got, tc.want)
		}
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
