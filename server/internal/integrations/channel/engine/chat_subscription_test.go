package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestParseSubscriptionCommand(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
		want SubscriptionCommand
	}{
		{body: "/subscribe MUL-12", ok: true, want: SubscriptionCommand{Kind: SubscriptionCommandSubscribe, Target: SubscriptionTargetIssue, Ref: "MUL-12"}},
		{body: "/subscribe issue MUL-12", ok: true, want: SubscriptionCommand{Kind: SubscriptionCommandSubscribe, Target: SubscriptionTargetIssue, Ref: "MUL-12"}},
		{body: "\n  /subscribe   MUL-12 please\nmore", ok: true, want: SubscriptionCommand{Kind: SubscriptionCommandSubscribe, Target: SubscriptionTargetIssue, Ref: "MUL-12"}},
		{body: "/unsubscribe #7", ok: true, want: SubscriptionCommand{Kind: SubscriptionCommandUnsubscribe, Target: SubscriptionTargetIssue, Ref: "#7"}},
		{body: "/subscribe", ok: true, want: SubscriptionCommand{Kind: SubscriptionCommandSubscribe}},
		{body: "/subscribe issue", ok: true, want: SubscriptionCommand{Kind: SubscriptionCommandSubscribe, Target: SubscriptionTargetIssue}},
		{body: "/subscribe\nMUL-12", ok: true, want: SubscriptionCommand{Kind: SubscriptionCommandSubscribe, Target: SubscriptionTargetIssue, Ref: "MUL-12"}},
		{body: "/subscribe Project  Web App \nnotes", ok: true, want: SubscriptionCommand{Kind: SubscriptionCommandSubscribe, Target: SubscriptionTargetProject, Ref: "Web App"}},
		{body: "/unsubscribe project", ok: true, want: SubscriptionCommand{Kind: SubscriptionCommandUnsubscribe, Target: SubscriptionTargetProject}},
		{body: "/subscribe https://app.example/acme/projects/abc", ok: true, want: SubscriptionCommand{Kind: SubscriptionCommandSubscribe, Target: SubscriptionTargetProject, Ref: "https://app.example/acme/projects/abc"}},
		{body: "/subscribe https://app.example/acme/issues/abc", ok: true, want: SubscriptionCommand{Kind: SubscriptionCommandSubscribe, Target: SubscriptionTargetIssue, Ref: "https://app.example/acme/issues/abc"}},
		{body: "/subscribers MUL-12"},
		{body: "/Subscribe MUL-12"},
		{body: "please /subscribe MUL-12"},
		{body: "/issue /subscribe MUL-12"},
	} {
		got, ok := ParseSubscriptionCommand(tc.body)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseSubscriptionCommand(%q) = %+v, %v; want %+v, %v", tc.body, got, ok, tc.want, tc.ok)
		}
	}
}

func groupSubscribeMessage(t *testing.T, text string) channel.InboundMessage {
	msg := p2pMessage(t)
	msg.Text = text
	msg.Source.ChatType = channel.ChatTypeGroup
	msg.AddressedToBot = true
	return msg
}

func subscriptionReply(t *testing.T, h *harness) Result {
	t.Helper()
	var got Result
	if !waitFor(time.Second, func() bool {
		for _, r := range h.replier.calls() {
			if r.Outcome == OutcomeIssueSubscription {
				got = r
				return true
			}
		}
		return false
	}) {
		t.Fatal("expected an issue subscription reply")
	}
	return got
}

// A subscription command is a control-plane turn: whatever the verdict, it is
// not stored in the Chat and does not start an agent run.
func assertSubscriptionCommandNotIngested(t *testing.T, h *harness) {
	t.Helper()
	if h.binder.appendedParams().Message.MessageID != "" {
		t.Fatal("subscription command must not be appended to the chat")
	}
	if h.tasks.calls() != 0 || h.issues.called {
		t.Fatal("subscription command must not run the agent or create an issue")
	}
	if h.dedup.marks() != 1 {
		t.Fatalf("subscription command must mark the message processed, marks=%d", h.dedup.marks())
	}
}

func TestRouter_SubscriptionCommand_WithoutPosterIsUnsupported(t *testing.T) {
	h := newHarness(t)
	if err := h.router.Handle(context.Background(), groupSubscribeMessage(t, "/subscribe MUL-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := subscriptionReply(t, h); got.Subscription.Action != SubscriptionUnsupported {
		t.Fatalf("action = %q, want unsupported", got.Subscription.Action)
	}
	assertSubscriptionCommandNotIngested(t, h)
}

func TestRouter_SubscriptionCommand_DirectMessageIsGroupOnly(t *testing.T) {
	h := newHarness(t)
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{Logger: discardLogger(), Subscriptions: unusedSubscriptionQueries{}})
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst, Identity: h.ident, Dedup: h.dedup, Session: h.binder,
		Audit: h.audit, Replier: h.replier, Poster: &fakePoster{},
	})
	msg := p2pMessage(t)
	msg.Text = "/subscribe MUL-1"
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := subscriptionReply(t, h); got.Subscription.Action != SubscriptionGroupOnly {
		t.Fatalf("action = %q, want group_only", got.Subscription.Action)
	}
	assertSubscriptionCommandNotIngested(t, h)
}

func TestSubscriptionReplyText_EscapesMemberText(t *testing.T) {
	escape := func(s string) string { return strings.ReplaceAll(s, "<", "&lt;") }
	got := SubscriptionReplyText(Result{
		Subscription: SubscriptionOutcome{Action: SubscriptionNotFound, Target: SubscriptionTargetProject, Ref: "<!channel>"},
	}, "", escape)
	if strings.Contains(got, "<!channel>") || !strings.HasPrefix(got, "No project matches") {
		t.Fatalf("member-typed ref must pass through the escaper: %q", got)
	}
	got = SubscriptionReplyText(Result{
		Subscription:    SubscriptionOutcome{Action: SubscriptionSubscribed, Target: SubscriptionTargetIssue},
		IssueIdentifier: "MUL-3", IssueTitle: "<b>", IssueWorkspaceSlug: "acme", IssueID: uid(3),
	}, "https://app.example", escape)
	if !strings.Contains(got, "MUL-3 — &lt;b>") || !strings.HasSuffix(got, "https://app.example/acme/issues/"+util.UUIDToString(uid(3))) {
		t.Fatalf("unexpected subscribed reply: %q", got)
	}
	got = SubscriptionReplyText(Result{
		Subscription: SubscriptionOutcome{
			Action: SubscriptionSubscribed, Target: SubscriptionTargetProject, ProjectID: uid(4), ProjectTitle: "<Web>",
		},
		IssueWorkspaceSlug: "acme",
	}, "https://app.example", escape)
	if !strings.Contains(got, "follows project &lt;Web>. New issues") || !strings.HasSuffix(got, "https://app.example/acme/projects/"+util.UUIDToString(uid(4))) {
		t.Fatalf("unexpected project subscribed reply: %q", got)
	}
}

// unusedSubscriptionQueries satisfies ChatSubscriptionQueries for paths that
// must answer before touching the database.
type unusedSubscriptionQueries struct{ ChatSubscriptionQueries }

type postedUpdate struct {
	installationID string
	target         ChatTarget
	text           string
}

type fakePoster struct {
	mu    sync.Mutex
	posts []postedUpdate
}

func (f *fakePoster) PostToChat(_ context.Context, inst db.ChannelInstallation, target ChatTarget, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, postedUpdate{installationID: util.UUIDToString(inst.ID), target: target, text: text})
	return nil
}

func (f *fakePoster) calls() []postedUpdate {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]postedUpdate(nil), f.posts...)
}
