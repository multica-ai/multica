package lark

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newSubscriptionTestReplier(stub *stubAPIClientWithRecorder) OutcomeReplier {
	return NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   stub,
		BindingSvc:  &BindingTokenService{},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{},
		AppURL:      "https://multica.test",
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestLarkOutcomeReplierIssueSubscriptionRepliesWithText(t *testing.T) {
	t.Parallel()
	stub := &stubAPIClientWithRecorder{configured: true}
	inst := Installation{AppID: "cli_x"}
	inst.ID = mustUUID("11111111-1111-1111-1111-111111111111")
	issueID := mustUUID("22222222-2222-2222-2222-222222222222")
	newSubscriptionTestReplier(stub).Reply(context.Background(), inst, InboundMessage{ChatID: "oc_group"}, DispatchResult{
		Outcome: OutcomeIssueSubscription, IssueID: issueID, IssueNumber: 42,
		IssueIdentifier: "MUL-42", IssueWorkspaceSlug: "demo-web", IssueTitle: "fix login",
		SubscriptionAction: engine.SubscriptionSubscribed,
	})

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.textOut) != 1 || len(stub.interactiveOut) != 0 {
		t.Fatalf("want one plain-text reply, got text=%d cards=%d", len(stub.textOut), len(stub.interactiveOut))
	}
	got := stub.textOut[0]
	if got.ChatID != "oc_group" || !strings.Contains(got.Text, "now follows MUL-42 — fix login") ||
		!strings.HasSuffix(got.Text, "https://multica.test/demo-web/issues/22222222-2222-2222-2222-222222222222") {
		t.Fatalf("subscription reply = %+v", got)
	}
}

func TestLarkOutcomeReplierIssueCreatedNotesChatSubscription(t *testing.T) {
	t.Parallel()
	stub := &stubAPIClientWithRecorder{configured: true}
	inst := Installation{AppID: "cli_x"}
	inst.ID = mustUUID("11111111-1111-1111-1111-111111111111")
	newSubscriptionTestReplier(stub).Reply(context.Background(), inst, InboundMessage{ChatID: "oc_group"}, DispatchResult{
		Outcome: OutcomeIngested, IssueID: mustUUID("22222222-2222-2222-2222-222222222222"),
		IssueNumber: 42, IssueIdentifier: "MUL-42", IssueWorkspaceSlug: "demo-web", IssueTitle: "fix login",
		IssueChatSubscribed: true,
	})

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.textOut) != 1 || !strings.HasSuffix(stub.textOut[0].Text, "Send /unsubscribe MUL-42 to stop.") {
		t.Fatalf("issue-created reply must end with the follow note: %+v", stub.textOut)
	}
}

func TestChatPosterRepliesInTopicThroughAnchor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		target engine.ChatTarget
		want   ReplyTarget
	}{
		{name: "chat level", target: engine.ChatTarget{ChatID: "oc_group"}},
		{
			name:   "topic",
			target: engine.ChatTarget{ChatID: "oc_group", ThreadID: "omt_1", AnchorMessageID: "om_anchor"},
			want:   ReplyTarget{MessageID: "om_anchor", InThread: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubAPIClientWithRecorder{configured: true}
			poster := NewChatPoster(stub, stubCredentialsResolver{secret: "s"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			row := db.ChannelInstallation{ID: mustUUID("11111111-1111-1111-1111-111111111111"), ChannelType: channelTypeFeishu, Config: []byte(`{"app_id":"cli_x"}`)}
			if err := poster.PostToChat(context.Background(), row, tc.target, "update"); err != nil {
				t.Fatalf("post: %v", err)
			}
			stub.mu.Lock()
			defer stub.mu.Unlock()
			if len(stub.textOut) != 1 || stub.textOut[0].ChatID != "oc_group" || stub.textOut[0].ReplyTarget != tc.want || stub.textOut[0].Text != "update" {
				t.Fatalf("sent %+v, want reply target %+v", stub.textOut, tc.want)
			}
		})
	}
}
