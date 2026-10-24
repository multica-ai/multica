package lark

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func inboxTestUUID(n byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	for i := range u.Bytes {
		u.Bytes[i] = n
	}
	return u
}

func inboxTestPatcher(q *fakePatcherQueries, c *fakeAPIClient) *Patcher {
	return NewPatcher(q, fakeCredentials{secret: "s"}, c, PatcherConfig{})
}

func inboxEvent(item map[string]any) events.Event {
	return events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: inboxTestUUID(2).String(),
		Payload:     map[string]any{"item": item},
	}
}

func assignedItem() map[string]any {
	return map[string]any{
		"id":             "item-1",
		"workspace_id":   inboxTestUUID(2).String(),
		"recipient_type": "member",
		"recipient_id":   inboxTestUUID(1).String(),
		"type":           "issue_assigned",
		"title":          "实现登录页",
		"body":           "验收标准见 issue 描述",
		"issue_id":       inboxTestUUID(3).String(),
	}
}

// A bound member receives a card addressed to their open_id — the whole
// point of the feature: before this, Lark only carried replies to
// conversations the member had already started.
func TestInboxNewSendsCardToBoundMember(t *testing.T) {
	q := &fakePatcherQueries{
		memberBinding: db.ChannelUserBinding{ChannelUserID: "ou_abc"},
		installation:  Installation{AppID: "cli_1"},
		workspace:     db.Workspace{Slug: "team"},
	}
	c := &fakeAPIClient{mdCardReturn: "msg-1"}
	inboxTestPatcher(q, c).handleInboxNew(inboxEvent(assignedItem()))

	if len(c.openIDSent) != 1 {
		t.Fatalf("open_id sends = %d, want 1", len(c.openIDSent))
	}
	got := c.openIDSent[0]
	if string(got.OpenID) != "ou_abc" {
		t.Errorf("OpenID = %q, want ou_abc", got.OpenID)
	}
	if !strings.Contains(got.Markdown, "实现登录页") {
		t.Errorf("card body missing title: %q", got.Markdown)
	}
	if !strings.Contains(got.Markdown, "任务指派") {
		t.Errorf("card body missing type label: %q", got.Markdown)
	}
	if got.Summary != "实现登录页" {
		t.Errorf("Summary = %q, want the title", got.Summary)
	}
}

// An unbound member must not produce a send, and must not be an error:
// the item is already in the in-app inbox.
func TestInboxNewSkipsUnboundMember(t *testing.T) {
	q := &fakePatcherQueries{memberBindingErr: pgx.ErrNoRows}
	c := &fakeAPIClient{}
	inboxTestPatcher(q, c).handleInboxNew(inboxEvent(assignedItem()))

	if len(c.openIDSent) != 0 {
		t.Fatalf("open_id sends = %d, want 0", len(c.openIDSent))
	}
}

// Agent recipients have no Lark binding to resolve.
func TestInboxNewIgnoresAgentRecipient(t *testing.T) {
	item := assignedItem()
	item["recipient_type"] = "agent"
	q := &fakePatcherQueries{memberBinding: db.ChannelUserBinding{ChannelUserID: "ou_abc"}}
	c := &fakeAPIClient{}
	inboxTestPatcher(q, c).handleInboxNew(inboxEvent(item))

	if len(c.openIDSent) != 0 {
		t.Fatalf("open_id sends = %d, want 0", len(c.openIDSent))
	}
}

// A send failure is logged by handleInboxNew, not propagated into the
// publisher, and it must not panic or retry.
func TestInboxNewSurvivesSendFailure(t *testing.T) {
	q := &fakePatcherQueries{
		memberBinding: db.ChannelUserBinding{ChannelUserID: "ou_abc"},
		installation:  Installation{AppID: "cli_1"},
	}
	c := &fakeAPIClient{openIDSendErr: errors.New("boom")}
	inboxTestPatcher(q, c).handleInboxNew(inboxEvent(assignedItem()))

	if len(c.openIDSent) != 1 {
		t.Fatalf("open_id sends = %d, want 1", len(c.openIDSent))
	}
}

// Member-authored text must not become a live link inside a card the
// recipient trusts.
func TestInboxCardBreaksMemberLinks(t *testing.T) {
	item := assignedItem()
	item["title"] = "[click](https://evil.example)"
	got := buildInboxMarkdown(item, "ws", "team")

	if strings.Contains(got, "](https://evil.example)") {
		t.Errorf("member link syntax survived: %q", got)
	}
}

// The deep link uses the workspace slug and carries the issue id.
func TestInboxCardLinksToIssue(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://app.example/")
	item := assignedItem()
	got := buildInboxMarkdown(item, "ws-id", "team")

	if !strings.Contains(got, "https://app.example/team/inbox?issue=") {
		t.Errorf("deep link missing or wrong: %q", got)
	}
}

// Without an app URL the link segment is dropped rather than emitted broken.
func TestInboxCardOmitsLinkWithoutAppURL(t *testing.T) {
	for _, name := range []string{"LARK_APP_URL", "MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
		t.Setenv(name, "")
	}
	got := buildInboxMarkdown(assignedItem(), "ws", "team")

	if strings.Contains(got, "查看详情") {
		t.Errorf("link segment present with no app URL: %q", got)
	}
	if !strings.Contains(got, "实现登录页") {
		t.Errorf("title dropped along with the link: %q", got)
	}
}

// A long body is truncated, but the title and the link survive so the
// card still points at the item.
func TestInboxCardTruncatesLongBody(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://app.example")
	item := assignedItem()
	item["body"] = strings.Repeat("很长的正文", 1000)
	got := buildInboxMarkdown(item, "ws", "team")

	if n := len([]rune(got)); n > inboxMarkdownMaxLen {
		t.Errorf("card is %d runes, over the %d budget", n, inboxMarkdownMaxLen)
	}
	if !strings.Contains(got, "实现登录页") {
		t.Error("title lost in truncation")
	}
	if !strings.Contains(got, "查看详情") {
		t.Error("link lost in truncation")
	}
}

// The event wiring is the part that silently does nothing if forgotten.
func TestPatcherRegistersInboxNew(t *testing.T) {
	bus := events.New()
	q := &fakePatcherQueries{
		memberBinding: db.ChannelUserBinding{ChannelUserID: "ou_abc"},
		installation:  Installation{AppID: "cli_1"},
	}
	c := &fakeAPIClient{}
	inboxTestPatcher(q, c).Register(bus)

	bus.Publish(inboxEvent(assignedItem()))

	if len(c.openIDSent) != 1 {
		t.Fatalf("open_id sends = %d, want 1 — is inbox:new subscribed?", len(c.openIDSent))
	}
}

// The notification filter is the difference between a useful channel and
// one people mute. Only the three interrupting types may push.
func TestInboxNewPushesOnlyInterruptingTypes(t *testing.T) {
	pushed := []string{"issue_assigned", "mentioned", "task_failed"}
	silent := []string{"new_comment", "status_changed", "unassigned", "assignee_changed", "issue_subscribed"}

	for _, typ := range pushed {
		item := assignedItem()
		item["type"] = typ
		q := &fakePatcherQueries{
			memberBinding: db.ChannelUserBinding{ChannelUserID: "ou_abc"},
			installation:  Installation{AppID: "cli_1"},
		}
		c := &fakeAPIClient{}
		inboxTestPatcher(q, c).handleInboxNew(inboxEvent(item))
		if len(c.openIDSent) != 1 {
			t.Errorf("%s: sends = %d, want 1", typ, len(c.openIDSent))
		}
	}

	for _, typ := range silent {
		item := assignedItem()
		item["type"] = typ
		q := &fakePatcherQueries{
			memberBinding: db.ChannelUserBinding{ChannelUserID: "ou_abc"},
			installation:  Installation{AppID: "cli_1"},
		}
		c := &fakeAPIClient{}
		inboxTestPatcher(q, c).handleInboxNew(inboxEvent(item))
		if len(c.openIDSent) != 0 {
			t.Errorf("%s: sends = %d, want 0 — this type must stay in the inbox", typ, len(c.openIDSent))
		}
	}
}
