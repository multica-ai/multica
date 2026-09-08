package engine

import (
	"strings"
	"testing"
)

const (
	recipient = "11111111-1111-1111-1111-111111111111"
	outsider  = "22222222-2222-2222-2222-222222222222"
)

// The sender check is the security boundary of the whole reply path: a comment
// posted past it is authored under the recipient's name and carries their
// authority to wake an agent. It lives here, apart from the database write, so
// that it is pinned by a test that runs everywhere — internal/handler's suite
// needs Postgres and skips the entire binary without one.
func TestPushReplyPreconditionRefusesAnyoneButTheRecipient(t *testing.T) {
	content, denial, ok := PushReplyPrecondition(recipient, outsider, "确认审核")
	if ok {
		t.Fatal("a reply from someone the push was not addressed to was allowed through")
	}
	if content != "" {
		t.Errorf("returned content alongside a denial: %q", content)
	}
	if denial.Posted {
		t.Error("a denial reported the comment as posted")
	}
	if denial.Message != PushReplyDenied {
		t.Errorf("Message = %q, want %q", denial.Message, PushReplyDenied)
	}
}

// Both ids are rendered by the caller, and util.UUIDToString renders an invalid
// UUID as "". Two unparseable ids must not therefore compare equal into an
// authorization pass.
func TestPushReplyPreconditionDoesNotPairTwoEmptyIDs(t *testing.T) {
	if _, _, ok := PushReplyPrecondition("", "", "确认审核"); ok {
		t.Error("two empty ids compared equal and passed the sender check")
	}
}

func TestPushReplyPreconditionAdmitsTheRecipient(t *testing.T) {
	content, _, ok := PushReplyPrecondition(recipient, recipient, "确认审核")
	if !ok {
		t.Fatal("refused the person the push was addressed to")
	}
	if content != "确认审核" {
		t.Errorf("content = %q, want the message unchanged", content)
	}
}

// An empty reply is refused before the sender check, because there is nothing
// to post either way and the emptier answer is the more useful one.
func TestPushReplyPreconditionRejectsContentWithNothingInIt(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"spaces":         "   ",
		"newlines":       "\n\n\t ",
		"null bytes":     "\x00\x00",
		"nulls + spaces": " \x00 \n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			content, denial, ok := PushReplyPrecondition(recipient, recipient, in)
			if ok {
				t.Fatalf("accepted %q as a reply", in)
			}
			if content != "" {
				t.Errorf("content = %q, want empty", content)
			}
			if denial.Message != "回复内容为空，未提交。" {
				t.Errorf("Message = %q", denial.Message)
			}
		})
	}
}

// PostgreSQL rejects a NUL in a text value outright, so a reply carrying one
// has to be stripped rather than refused — the surrounding words are still what
// the member meant to say.
func TestPushReplyPreconditionStripsNullBytesFromRealContent(t *testing.T) {
	content, _, ok := PushReplyPrecondition(recipient, recipient, "  确认\x00审核  ")
	if !ok {
		t.Fatal("refused a reply that had real content around a NUL")
	}
	if strings.ContainsRune(content, 0) {
		t.Errorf("a NUL survived into the comment body: %q", content)
	}
	if content != "确认审核" {
		t.Errorf("content = %q, want %q", content, "确认审核")
	}
}
