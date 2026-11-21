package sharecrm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

func TestReplyHelpPostsSharedCommandList(t *testing.T) {
	var gotText string
	mux := http.NewServeMux()
	mux.HandleFunc("/im-gateway/auth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"accessToken": "tok", "expiresIn": 3600, "tokenType": "Bearer"},
		})
	})
	mux.HandleFunc("/im-gateway/qixin/message/send", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotText, _ = body["text"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"message_id": "out-help"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	raw, err := json.Marshal(installConfig{
		AppID:              "app",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret")),
		GatewayBaseURL:     srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	replier := NewOutboundReplier(OutboundReplierConfig{Client: NewClient(srv.Client())})
	replier.Reply(context.Background(), engine.ResolvedInstallation{
		Platform: db.ChannelInstallation{Config: raw},
	}, channel.InboundMessage{
		Source: channel.Source{ChatID: "0:fs:s:", ChatType: channel.ChatTypeP2P},
		Text:   "/help",
	}, engine.Result{Outcome: engine.OutcomeHelp})

	want := stripMarkdown(engine.HelpTextEnglish)
	if gotText != want {
		t.Fatalf("help reply = %q, want %q", gotText, want)
	}
	for _, command := range []string{"/help", "/new", "/clear", "/issue"} {
		if !strings.Contains(gotText, command) {
			t.Fatalf("help reply missing %s: %q", command, gotText)
		}
	}
}

func TestCommandOutcomeCopy(t *testing.T) {
	// Pins the user-visible bare-/clear, bare-/new, and bare-/issue strings
	// that Reply posts for OutcomeFreshPending / OutcomeChatStarted /
	// OutcomeIssueUsage (MUL-6661). ShareCRM is text-first so there is no
	// media usage variant.
	if freshPendingText != "✅ Fresh start ready. Your next chat message will run without previous context." {
		t.Fatalf("freshPendingText = %q", freshPendingText)
	}
	if chatStartedText != "✅ Started a new Multica chat. Your next message will enter it." {
		t.Fatalf("chatStartedText = %q", chatStartedText)
	}
	if issueUsageText != "Please include an issue title. Use:\n\n`/issue <title>`\n\n`[description]` (optional)" {
		t.Fatalf("issueUsageText = %q", issueUsageText)
	}
	if bindingLinkAlreadySentText == "" {
		t.Fatal("bindingLinkAlreadySentText must not be empty")
	}
}

func TestDroppedReplyText(t *testing.T) {
	issueMsg := channel.InboundMessage{Text: "ignored", CommandText: "/issue login is broken", AddressedToBot: true}
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
