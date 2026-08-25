package lark

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channelnotify"
)

type inboxSenderStoreStub struct {
	installation Installation
	err          error
	requestedID  pgtype.UUID
}

func (s *inboxSenderStoreStub) GetLarkInstallation(_ context.Context, id pgtype.UUID) (Installation, error) {
	s.requestedID = id
	return s.installation, s.err
}

type inboxCredentialsStub struct {
	secret string
	err    error
}

func (s inboxCredentialsStub) DecryptAppSecret(Installation) (string, error) {
	return s.secret, s.err
}

type inboxAPIClientStub struct {
	APIClient
	calls []SendDMCardParams
	err   error
}

func (s *inboxAPIClientStub) SendDMCard(_ context.Context, params SendDMCardParams) error {
	s.calls = append(s.calls, params)
	return s.err
}

func inboxSenderFixture(t *testing.T) (*InboxSender, *inboxSenderStoreStub, *inboxAPIClientStub, channelnotify.Target, channelnotify.Notification) {
	t.Helper()
	installationID := uuidFromString(t, "11111111-1111-4111-8111-111111111111")
	agentID := uuidFromString(t, "22222222-2222-4222-8222-222222222222")
	issueID := uuidFromString(t, "33333333-3333-4333-8333-333333333333")
	store := &inboxSenderStoreStub{installation: Installation{
		ID:        installationID,
		AgentID:   agentID,
		AppID:     "cli_app",
		TenantKey: pgtype.Text{String: "tenant", Valid: true},
		Status:    "active",
		Region:    "feishu",
	}}
	client := &inboxAPIClientStub{}
	sender := NewInboxSender(store, inboxCredentialsStub{secret: "plaintext-secret"}, client, "https://app.multica.test/", newDiscardLogger())
	target := channelnotify.Target{
		InstallationID: installationID,
		AgentID:        agentID,
		ChannelType:    channel.TypeFeishu,
		ChannelUserID:  "ou_recipient",
		WorkspaceSlug:  "acme",
	}
	notification := channelnotify.Notification{
		IssueID: issueID,
		Title:   "[click](https://evil.example)",
		Body:    "A teammate needs your review.",
	}
	return sender, store, client, target, notification
}

func TestInboxSenderUsesSelectedInstallationAndOpenID(t *testing.T) {
	sender, store, client, target, notification := inboxSenderFixture(t)
	if err := sender.SendInbox(context.Background(), target, notification); err != nil {
		t.Fatalf("SendInbox: %v", err)
	}
	if store.requestedID != target.InstallationID {
		t.Fatalf("requested installation = %v, want %v", store.requestedID, target.InstallationID)
	}
	if len(client.calls) != 1 {
		t.Fatalf("SendDMCard calls = %d, want 1", len(client.calls))
	}
	call := client.calls[0]
	if call.OpenID != OpenID(target.ChannelUserID) {
		t.Fatalf("OpenID = %q, want %q", call.OpenID, target.ChannelUserID)
	}
	if call.InstallationID.AppID != "cli_app" || call.InstallationID.AppSecret != "plaintext-secret" || call.InstallationID.TenantKey != "tenant" {
		t.Fatalf("unexpected installation credentials: %+v", call.InstallationID)
	}
}

func TestInboxSenderRendersInboxTitleBodyAndDeepLink(t *testing.T) {
	sender, _, client, target, notification := inboxSenderFixture(t)
	target.WorkspaceSlug = "team space"
	if err := sender.SendInbox(context.Background(), target, notification); err != nil {
		t.Fatalf("SendInbox: %v", err)
	}

	var card map[string]any
	if err := json.Unmarshal([]byte(client.calls[0].CardJSON), &card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	header := card["header"].(map[string]any)
	title := header["title"].(map[string]any)
	if title["content"] != notification.Title {
		t.Fatalf("title content = %v, want %q", title["content"], notification.Title)
	}
	elements := card["elements"].([]any)
	body := elements[0].(map[string]any)["text"].(map[string]any)
	if body["content"] != notification.Body {
		t.Fatalf("body content = %v, want %q", body["content"], notification.Body)
	}
	action := elements[1].(map[string]any)["actions"].([]any)[0].(map[string]any)
	wantURL := "https://app.multica.test/team%20space/inbox?issue=33333333-3333-4333-8333-333333333333"
	if action["url"] != wantURL {
		t.Fatalf("deep link = %v, want %q", action["url"], wantURL)
	}
}

func TestInboxSenderKeepsMemberTextPlain(t *testing.T) {
	sender, _, client, target, notification := inboxSenderFixture(t)
	if err := sender.SendInbox(context.Background(), target, notification); err != nil {
		t.Fatalf("SendInbox: %v", err)
	}
	card := client.calls[0].CardJSON
	if !strings.Contains(card, `"tag":"plain_text"`) || !strings.Contains(card, `[click](https://evil.example)`) {
		t.Fatalf("card did not preserve plain member text: %s", card)
	}
	if strings.Contains(card, `"tag":"lark_md"`) || strings.Contains(card, `"tag":"markdown"`) {
		t.Fatalf("card interprets member text as markdown: %s", card)
	}
}

func TestInboxSenderBoundsLongUnicodeCardContent(t *testing.T) {
	sender, _, client, target, notification := inboxSenderFixture(t)
	notification.Title = strings.Repeat("深", 200)
	notification.Body = strings.Repeat("界", 5000)

	if err := sender.SendInbox(context.Background(), target, notification); err != nil {
		t.Fatalf("SendInbox: %v", err)
	}
	cardJSON := client.calls[0].CardJSON
	if len(cardJSON) > 30*1024 {
		t.Fatalf("card size = %d bytes, want <= 30720", len(cardJSON))
	}

	var card map[string]any
	if err := json.Unmarshal([]byte(cardJSON), &card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	title := card["header"].(map[string]any)["title"].(map[string]any)["content"].(string)
	body := card["elements"].([]any)[0].(map[string]any)["text"].(map[string]any)["content"].(string)
	if want := strings.Repeat("深", 99) + "…"; title != want {
		t.Fatalf("title = %d runes, want 100-rune truncated title", utf8.RuneCountInString(title))
	}
	if want := strings.Repeat("界", 2999) + "…"; body != want {
		t.Fatalf("body = %d runes, want 3000-rune truncated body", utf8.RuneCountInString(body))
	}
	if !utf8.ValidString(title) || !utf8.ValidString(body) {
		t.Fatal("truncated card content is not valid UTF-8")
	}
}

func TestInboxSenderRejectsCardAboveFeishuPayloadLimit(t *testing.T) {
	sender, _, client, target, notification := inboxSenderFixture(t)
	sender.appURL = "https://app.multica.test/" + strings.Repeat("a", 31*1024)

	err := sender.SendInbox(context.Background(), target, notification)
	if err == nil || !strings.Contains(err.Error(), "card exceeds 30720-byte limit") {
		t.Fatalf("SendInbox error = %v, want card size limit error", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("SendDMCard calls = %d, want 0", len(client.calls))
	}
}

func TestInboxSenderRejectsRevokedOrMismatchedInstallation(t *testing.T) {
	for name, mutate := range map[string]func(*Installation, *channelnotify.Target){
		"revoked": func(installation *Installation, _ *channelnotify.Target) {
			installation.Status = "revoked"
		},
		"agent mismatch": func(_ *Installation, target *channelnotify.Target) {
			target.AgentID = pgtype.UUID{Bytes: [16]byte{9}, Valid: true}
		},
		"installation mismatch": func(installation *Installation, _ *channelnotify.Target) {
			installation.ID = pgtype.UUID{Bytes: [16]byte{8}, Valid: true}
		},
	} {
		t.Run(name, func(t *testing.T) {
			sender, store, client, target, notification := inboxSenderFixture(t)
			mutate(&store.installation, &target)
			if err := sender.SendInbox(context.Background(), target, notification); err == nil {
				t.Fatal("SendInbox accepted ineligible installation")
			}
			if len(client.calls) != 0 {
				t.Fatalf("SendDMCard calls = %d, want 0", len(client.calls))
			}
		})
	}
}

func TestInboxSenderReturnsAPIFailure(t *testing.T) {
	sender, _, client, target, notification := inboxSenderFixture(t)
	want := errors.New("Feishu API unavailable")
	client.err = want
	if err := sender.SendInbox(context.Background(), target, notification); !errors.Is(err, want) {
		t.Fatalf("SendInbox error = %v, want %v", err, want)
	}
}
