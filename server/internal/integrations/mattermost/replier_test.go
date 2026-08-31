package mattermost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type stubMinter struct {
	token string
	err   error

	mu       sync.Mutex
	mintedTo string
	calls    int
}

func (s *stubMinter) Mint(_ context.Context, _, _ pgtype.UUID, userID string) (BindingToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.mintedTo = userID
	if s.err != nil {
		return BindingToken{}, s.err
	}
	return BindingToken{Raw: s.token}, nil
}

// replierHarness wires an OutboundReplier against a fake Mattermost and
// records the posts it produces.
type replierHarness struct {
	replier *OutboundReplier
	inst    engine.ResolvedInstallation
	minter  *stubMinter

	mu    sync.Mutex
	posts []Post
}

func newReplierHarness(t *testing.T) *replierHarness {
	t.Helper()
	h := &replierHarness{minter: &stubMinter{token: "raw-token-value"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in Post
		_ = json.NewDecoder(r.Body).Decode(&in)
		h.mu.Lock()
		h.posts = append(h.posts, in)
		h.mu.Unlock()
		in.ID = "created"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(in)
	}))
	t.Cleanup(srv.Close)

	cfg, err := json.Marshal(installConfig{
		AppID:                testAppID,
		ServerURL:            srv.URL,
		BotUserID:            testBotID,
		BotUsername:          testBotUsername,
		AccessTokenEncrypted: base64.StdEncoding.EncodeToString([]byte("tok")),
	})
	if err != nil {
		t.Fatal(err)
	}
	h.inst = engine.ResolvedInstallation{Platform: db.ChannelInstallation{Config: cfg}}
	h.replier = NewOutboundReplier(OutboundReplierConfig{
		Binding:    h.minter,
		AppURL:     "https://app.multica.ai",
		HTTPClient: srv.Client(),
	})
	return h
}

func (h *replierHarness) sent() []Post {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Post(nil), h.posts...)
}

func dmInbound() channel.InboundMessage {
	return channel.InboundMessage{
		MessageID: "p1",
		Source: channel.Source{
			ChannelType: TypeMattermost, ChatID: "dm1",
			ChatType: channel.ChatTypeP2P, SenderID: "user1",
		},
	}
}

func groupInbound(text string) channel.InboundMessage {
	return channel.InboundMessage{
		MessageID:      "p1",
		Text:           text,
		CommandText:    text,
		AddressedToBot: true,
		Source: channel.Source{
			ChannelType: TypeMattermost, ChatID: "chan1",
			ChatType: channel.ChatTypeGroup, SenderID: "user1",
		},
	}
}

func TestReplierBindingPromptInDM(t *testing.T) {
	h := newReplierHarness(t)
	h.replier.Reply(context.Background(), h.inst, dmInbound(), engine.Result{
		Outcome: engine.OutcomeNeedsBinding,
		Sender:  "user1",
	})

	posts := h.sent()
	if len(posts) != 1 {
		t.Fatalf("sent %d posts, want 1", len(posts))
	}
	if !strings.Contains(posts[0].Message, "https://app.multica.ai/mattermost/bind?token=raw-token-value") {
		t.Errorf("prompt = %q, want the redeem link", posts[0].Message)
	}
	if h.minter.mintedTo != "user1" {
		t.Errorf("minted for %q, want user1", h.minter.mintedTo)
	}
}

// A bearer link posted in a channel could be redeemed by any member, binding
// the sender's identity to the wrong Multica user. The prompt must stay out of
// group conversations.
func TestReplierBindingPromptNeverLeaksIntoAGroup(t *testing.T) {
	h := newReplierHarness(t)
	h.replier.Reply(context.Background(), h.inst, groupInbound("hi"), engine.Result{
		Outcome: engine.OutcomeNeedsBinding,
		Sender:  "user1",
	})

	posts := h.sent()
	if len(posts) != 1 {
		t.Fatalf("sent %d posts, want 1", len(posts))
	}
	if strings.Contains(posts[0].Message, "token=") {
		t.Fatalf("group prompt carried a redeem token: %q", posts[0].Message)
	}
	if posts[0].Message != msgBindingGroupHint {
		t.Errorf("group prompt = %q, want the direct-chat hint", posts[0].Message)
	}
	if h.minter.calls != 0 {
		t.Errorf("minted %d tokens for a group prompt, want 0", h.minter.calls)
	}
}

func TestReplierOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		result  engine.Result
		msg     channel.InboundMessage
		want    string
		wantAny []string
		silent  bool
	}{
		{name: "agent offline", result: engine.Result{Outcome: engine.OutcomeAgentOffline}, msg: dmInbound(), want: msgAgentOffline},
		{name: "agent archived", result: engine.Result{Outcome: engine.OutcomeAgentArchived}, msg: dmInbound(), want: msgAgentArchived},
		{name: "fresh pending", result: engine.Result{Outcome: engine.OutcomeFreshPending}, msg: dmInbound(), want: msgFreshPending},
		{name: "chat started", result: engine.Result{Outcome: engine.OutcomeChatStarted}, msg: dmInbound(), want: msgChatStarted},
		{name: "issue usage", result: engine.Result{Outcome: engine.OutcomeIssueUsage}, msg: dmInbound(), want: msgIssueUsage},
		{
			name: "issue created",
			result: engine.Result{
				Outcome:         engine.OutcomeIngested,
				IssueID:         pgtype.UUID{Valid: true},
				IssueIdentifier: "MUL-42",
				IssueTitle:      "Fix the login bug",
			},
			msg:     dmInbound(),
			wantAny: []string{"MUL-42", "Fix the login bug"},
		},
		{
			name: "duplicate issue",
			result: engine.Result{
				Outcome:         engine.OutcomeIngested,
				IssueID:         pgtype.UUID{Valid: true},
				IssueIdentifier: "MUL-42",
				IssueTitle:      "Fix the login bug",
				IssueDuplicate:  true,
			},
			msg:     dmInbound(),
			wantAny: []string{"already exists", "MUL-42"},
		},
		{
			// An ordinary ingested message produces no verdict reply — the
			// agent's answer is the reply, delivered by the outbound path.
			name:   "plain ingest says nothing",
			result: engine.Result{Outcome: engine.OutcomeIngested},
			msg:    dmInbound(),
			silent: true,
		},
		{
			name:   "non-member drop refuses an addressed /issue",
			result: engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonNonWorkspaceMember},
			msg:    groupInbound("/issue Add dark mode"),
			want:   msgIssueNotMember,
		},
		{
			name:   "revoked install refuses an addressed /issue",
			result: engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonRevokedInstallation},
			msg:    groupInbound("/issue Add dark mode"),
			want:   msgIssueDisabled,
		},
		{
			// A drop for any other reason, or on a non-/issue message, stays
			// silent: the bot does not narrate every message it ignores.
			name:   "ordinary drop stays silent",
			result: engine.Result{Outcome: engine.OutcomeDropped, DropReason: engine.DropReasonNonWorkspaceMember},
			msg:    groupInbound("just chatting"),
			silent: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newReplierHarness(t)
			h.replier.Reply(context.Background(), h.inst, tc.msg, tc.result)
			posts := h.sent()
			if tc.silent {
				if len(posts) != 0 {
					t.Fatalf("sent %d posts, want silence: %+v", len(posts), posts)
				}
				return
			}
			if len(posts) != 1 {
				t.Fatalf("sent %d posts, want 1", len(posts))
			}
			if tc.want != "" && posts[0].Message != tc.want {
				t.Errorf("message = %q, want %q", posts[0].Message, tc.want)
			}
			for _, want := range tc.wantAny {
				if !strings.Contains(posts[0].Message, want) {
					t.Errorf("message = %q, want it to contain %q", posts[0].Message, want)
				}
			}
		})
	}
}

// Every reply threads under the triggering post so it stays attached in a busy
// channel.
func TestReplierThreadsItsReply(t *testing.T) {
	h := newReplierHarness(t)
	msg := groupInbound("hi")
	msg.Source.ThreadID = "root5"
	h.replier.Reply(context.Background(), h.inst, msg, engine.Result{Outcome: engine.OutcomeAgentOffline})

	posts := h.sent()
	if len(posts) != 1 {
		t.Fatalf("sent %d posts, want 1", len(posts))
	}
	if posts[0].RootID != "root5" {
		t.Errorf("RootID = %q, want root5", posts[0].RootID)
	}
}

// The replier runs detached from the inbound ACK path, so a delivery failure
// must be logged rather than propagated. Reply has no error return; the
// contract is that it does not panic and the pipeline continues.
func TestReplierSurvivesDeliveryFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg, err := json.Marshal(installConfig{ServerURL: srv.URL, BotUserID: testBotID})
	if err != nil {
		t.Fatal(err)
	}
	replier := NewOutboundReplier(OutboundReplierConfig{HTTPClient: srv.Client()})
	replier.Reply(context.Background(), engine.ResolvedInstallation{
		Platform: db.ChannelInstallation{Config: cfg},
	}, dmInbound(), engine.Result{Outcome: engine.OutcomeAgentOffline})
}

func TestReplierWithoutConfiguration(t *testing.T) {
	t.Run("missing binding service skips the prompt", func(t *testing.T) {
		h := newReplierHarness(t)
		h.replier.binding = nil
		h.replier.Reply(context.Background(), h.inst, dmInbound(), engine.Result{
			Outcome: engine.OutcomeNeedsBinding, Sender: "user1",
		})
		if posts := h.sent(); len(posts) != 0 {
			t.Fatalf("sent %+v, want nothing without a binding service", posts)
		}
	})

	t.Run("missing app url skips the prompt", func(t *testing.T) {
		h := newReplierHarness(t)
		h.replier.appURL = ""
		h.replier.Reply(context.Background(), h.inst, dmInbound(), engine.Result{
			Outcome: engine.OutcomeNeedsBinding, Sender: "user1",
		})
		if posts := h.sent(); len(posts) != 0 {
			t.Fatalf("sent %+v, want nothing without an app url", posts)
		}
	})

	t.Run("mint failure is not fatal", func(t *testing.T) {
		h := newReplierHarness(t)
		h.minter.err = errors.New("database down")
		h.replier.Reply(context.Background(), h.inst, dmInbound(), engine.Result{
			Outcome: engine.OutcomeNeedsBinding, Sender: "user1",
		})
		if posts := h.sent(); len(posts) != 0 {
			t.Fatalf("sent %+v, want nothing when minting failed", posts)
		}
	})

	t.Run("default binding path", func(t *testing.T) {
		r := NewOutboundReplier(OutboundReplierConfig{})
		if r.bindingPath != "/mattermost/bind" {
			t.Errorf("bindingPath = %q, want /mattermost/bind", r.bindingPath)
		}
		// A path configured without a leading slash is normalized rather than
		// producing "https://hostmattermost/bind".
		r = NewOutboundReplier(OutboundReplierConfig{BindingPath: "custom/bind"})
		if r.bindingPath != "/custom/bind" {
			t.Errorf("bindingPath = %q, want a leading slash added", r.bindingPath)
		}
	})
}

func TestIssueResultIdentifierFallbacks(t *testing.T) {
	if got := issueResultIdentifier(engine.Result{IssueIdentifier: "MUL-7"}); got != "MUL-7" {
		t.Errorf("identifier = %q, want MUL-7", got)
	}
	// No human identifier yet: the number is better than a bare UUID.
	if got := issueResultIdentifier(engine.Result{IssueNumber: 7}); got != "#7" {
		t.Errorf("identifier = %q, want #7", got)
	}
}
