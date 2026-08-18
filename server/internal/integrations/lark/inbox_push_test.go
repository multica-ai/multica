package lark

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	testInboxRecipientID = "dddddddd-dddd-dddd-dddd-dddddddddddd"
	testInboxWorkspaceID = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	testInboxIssueID     = "ffffffff-ffff-ffff-ffff-ffffffffffff"
)

// inboxItem builds an inbox_item map shaped like the one notifyDirect
// publishes on inbox:new.
func inboxItem(overrides map[string]any) map[string]any {
	item := map[string]any{
		"recipient_type": "member",
		"recipient_id":   testInboxRecipientID,
		"workspace_id":   testInboxWorkspaceID,
		"type":           "issue_assigned",
		"title":          "Fix the login redirect",
		"body":           "assigned to you",
		"issue_id":       testInboxIssueID,
	}
	for k, v := range overrides {
		if v == nil {
			delete(item, k)
			continue
		}
		item[k] = v
	}
	return item
}

func inboxEvent(overrides map[string]any) events.Event {
	return events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: testInboxWorkspaceID,
		Payload:     map[string]any{"item": inboxItem(overrides)},
	}
}

// deliver runs the synchronous delivery core the async handler wraps.
func deliver(t *testing.T, p *Patcher, item map[string]any) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rid, _ := item["recipient_id"].(string)
	wid, _ := item["workspace_id"].(string)
	return p.tryDeliverInbox(ctx, item, rid, wid)
}

// withBoundMember wires the fake queries so the recipient has a Lark
// binding and the workspace resolves to a slug.
func withBoundMember(t *testing.T, q *fakePatcherQueries) {
	t.Helper()
	q.memberBinding = db.ChannelUserBinding{
		InstallationID: uuidFromString(t, "1111aaaa-1111-1111-1111-111111111111"),
		MulticaUserID:  uuidFromString(t, testInboxRecipientID),
		ChannelType:    channelTypeFeishu,
		ChannelUserID:  "ou_member_open_id",
	}
	q.workspace = db.Workspace{Slug: "acme"}
}

func TestInboxPushDeliversCardToBoundMemberByOpenID(t *testing.T) {
	p, q, api := newTestPatcher(t)
	withBoundMember(t, q)
	t.Setenv("LARK_APP_URL", "https://multica.example.com")

	if !deliver(t, p, inboxItem(nil)) {
		t.Fatal("expected delivery to succeed")
	}
	if len(api.mdCardSent) != 1 {
		t.Fatalf("expected 1 markdown card send, got %d", len(api.mdCardSent))
	}
	sent := api.mdCardSent[0]
	// Addressing the person, not a chat: this is what lets the bot open
	// the 1:1 conversation itself.
	if sent.ReceiveIDType != ReceiveIDOpenID {
		t.Errorf("ReceiveIDType: got %q, want %q", sent.ReceiveIDType, ReceiveIDOpenID)
	}
	if sent.ChatID != "ou_member_open_id" {
		t.Errorf("ChatID: got %q, want the binding's open_id", sent.ChatID)
	}
	if sent.ReplyTarget.IsSet() {
		t.Error("inbox push must not thread into a reply target")
	}
	for _, want := range []string{"**【任务指派】Fix the login redirect**", "assigned to you",
		"[查看详情](https://multica.example.com/acme/inbox?issue=" + testInboxIssueID + ")"} {
		if !strings.Contains(sent.Markdown, want) {
			t.Errorf("Markdown missing %q; got:\n%s", want, sent.Markdown)
		}
	}
	if sent.Summary != "【任务指派】Fix the login redirect" {
		t.Errorf("Summary: got %q", sent.Summary)
	}
}

// TestInboxPushHandlerDeliversAsync pins the bus-facing wiring: the
// registered handler parses the event and delivery lands without the
// caller waiting on it.
func TestInboxPushHandlerDeliversAsync(t *testing.T) {
	p, q, api := newTestPatcher(t)
	withBoundMember(t, q)

	p.handleInboxNew(inboxEvent(nil))

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		api.mu.Lock()
		n := len(api.mdCardSent)
		api.mu.Unlock()
		if n == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("async delivery never landed")
}

func TestInboxPushSkipsUnboundMember(t *testing.T) {
	p, q, api := newTestPatcher(t)
	// No binding row: the ordinary case for a member who never linked
	// their Lark account. They still get the in-app inbox item.
	q.memberBindingErr = pgx.ErrNoRows

	if deliver(t, p, inboxItem(nil)) {
		t.Fatal("expected no delivery for an unbound member")
	}
	if len(api.mdCardSent) != 0 {
		t.Fatalf("expected no send for an unbound member, got %d", len(api.mdCardSent))
	}
}

func TestInboxPushHandlerSkipsAgentRecipient(t *testing.T) {
	p, q, api := newTestPatcher(t)
	withBoundMember(t, q)

	// The handler filters non-member recipients before it ever spawns the
	// delivery goroutine, so asserting right after it returns is not racy.
	p.handleInboxNew(inboxEvent(map[string]any{"recipient_type": "agent"}))

	if len(api.mdCardSent) != 0 {
		t.Fatalf("agents receive nothing over chat channels, got %d sends", len(api.mdCardSent))
	}
}

func TestInboxPushHandlerSurvivesMalformedPayload(t *testing.T) {
	p, q, api := newTestPatcher(t)
	withBoundMember(t, q)

	// Each of these returns before the goroutine spawn, so the immediate
	// assertion below cannot race a delivery.
	p.handleInboxNew(events.Event{Type: protocol.EventInboxNew, Payload: "not-a-map"})
	p.handleInboxNew(events.Event{Type: protocol.EventInboxNew, Payload: map[string]any{}})
	p.handleInboxNew(inboxEvent(map[string]any{"recipient_id": ""}))

	if len(api.mdCardSent) != 0 {
		t.Fatalf("expected no send on malformed payloads, got %d", len(api.mdCardSent))
	}
}

func TestInboxCardOmitsLinkWithoutAppURL(t *testing.T) {
	// No LARK_APP_URL / MULTICA_APP_URL / FRONTEND_ORIGIN configured.
	t.Setenv("LARK_APP_URL", "")
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "")

	md, _ := buildInboxCard(inboxItem(nil), testInboxWorkspaceID, "acme")
	// A link-less card beats one carrying a broken link.
	if strings.Contains(md, "查看详情") || strings.Contains(md, "/inbox") {
		t.Errorf("expected no link line; got:\n%s", md)
	}
	if !strings.Contains(md, "Fix the login redirect") {
		t.Error("title must survive when the link is dropped")
	}
}

func TestInboxCardAcceptsHTTPAppURL(t *testing.T) {
	// Self-hosted deployments on an internal network commonly serve plain
	// http; silencing the link there would cost the notification its whole
	// point. This is the one place this path diverges from the WeCom one.
	t.Setenv("LARK_APP_URL", "http://multica.internal")
	link := inboxItemLink(map[string]any{"issue_id": testInboxIssueID}, testInboxWorkspaceID, "acme")
	if link != "http://multica.internal/acme/inbox?issue="+testInboxIssueID {
		t.Errorf("got %q", link)
	}
}

func TestInboxCardFallsBackToWorkspaceIDWithoutSlug(t *testing.T) {
	t.Setenv("LARK_APP_URL", "https://multica.example.com")
	link := inboxItemLink(map[string]any{}, testInboxWorkspaceID, "")
	if link != "https://multica.example.com/"+testInboxWorkspaceID+"/inbox" {
		t.Errorf("got %q", link)
	}
	// A chat-only item has no issue_id, so no query param either.
	if strings.Contains(link, "?issue=") {
		t.Errorf("expected no issue param; got %q", link)
	}
}

func TestInboxCardRendersBodyVerbatim(t *testing.T) {
	// The notification lands in the same conversation as the agent's chat
	// replies, which are markdown cards carrying their full body. The
	// inbox card must match: nothing stripped, nothing summarized.
	t.Setenv("LARK_APP_URL", "https://multica.example.com")
	body := "## 发布报告\n\n- **结果**: ✅成功\n- **版本**: `89bcd8fa` → `687f5eb7`\n\n```\n==> 当前版本: 89bcd8fa\n```\n\n详见 [MR 链接](https://git.example/mr/22)。"
	md, _ := buildInboxCard(inboxItem(map[string]any{"type": "new_comment", "body": body}), testInboxWorkspaceID, "acme")

	if !strings.Contains(md, body) {
		t.Errorf("body must ship verbatim; got:\n%s", md)
	}
	if !strings.HasPrefix(md, "**【新评论】Fix the login redirect**\n\n") {
		t.Errorf("header line malformed; got prefix:\n%s", md[:60])
	}
	if !strings.HasSuffix(md, "[查看详情](https://multica.example.com/acme/inbox?issue="+testInboxIssueID+")") {
		t.Errorf("link line malformed; got:\n%s", md)
	}
}

func TestInboxCardClampsPathologicalBodyOnLineBoundary(t *testing.T) {
	t.Setenv("LARK_APP_URL", "https://multica.example.com")
	// A body over the safety valve, with an open code fence right at the
	// cut: the clamp must cut on a line boundary, close the fence so the
	// rest of the card cannot be swallowed into a code block, and keep
	// the link line intact.
	body := strings.Repeat("正文行。\n", 300) + "```\n" + strings.Repeat("log line\n", 200)
	md, _ := buildInboxCard(inboxItem(map[string]any{
		"title": strings.Repeat("标", 500),
		"body":  body,
	}), testInboxWorkspaceID, "acme")

	lines := strings.Split(md, "\n")
	if n := len([]rune(lines[0])); n > inboxTitleMaxRunes+20 {
		t.Errorf("title line is %d runes", n)
	}
	if strings.Count(md, "```")%2 != 0 {
		t.Errorf("clamp left an unbalanced code fence:\n…%s", md[len(md)-120:])
	}
	if !strings.Contains(md, "已截断") {
		t.Error("a cut body must say so")
	}
	if !strings.HasSuffix(md, "/inbox?issue="+testInboxIssueID+")") {
		t.Error("link must survive truncation — it is the whole affordance")
	}
}

func TestInboxCardBreaksTitleLinksButKeepsBodyLinks(t *testing.T) {
	t.Setenv("LARK_APP_URL", "https://multica.example.com")
	md, _ := buildInboxCard(inboxItem(map[string]any{
		"title": "[click here](http://evil.example)",
		"body":  "详见 [MR](https://git.example/mr/22)",
	}), testInboxWorkspaceID, "acme")

	// The title is spliced into the bot's own bold line, so its link
	// syntax must not survive there…
	if strings.Contains(md, "](http://evil.example") {
		t.Errorf("member-authored title link survived into the header:\n%s", md)
	}
	// …while the body renders verbatim like the agent-reply cards beside
	// it, links included, and the card's own link renders too.
	if !strings.Contains(md, "[MR](https://git.example/mr/22)") {
		t.Errorf("body links must be preserved:\n%s", md)
	}
	if !strings.Contains(md, "[查看详情](https://multica.example.com/") {
		t.Errorf("the card's own link must still render:\n%s", md)
	}
}

func TestInboxPushBackfillsDescriptionForAssignment(t *testing.T) {
	// Upstream passes body="" for issue_assigned (notification_listeners.go),
	// yet the description is exactly what the recipient needs to decide
	// whether to act. The push fetches it.
	p, q, api := newTestPatcher(t)
	withBoundMember(t, q)
	t.Setenv("LARK_APP_URL", "https://multica.example.com")
	q.issue = db.Issue{Description: pgtype.Text{String: "## 目标\n完成登录重定向修复", Valid: true}}

	if !deliver(t, p, inboxItem(map[string]any{"body": nil})) {
		t.Fatal("expected delivery to succeed")
	}
	if got := api.mdCardSent[0].Markdown; !strings.Contains(got, "完成登录重定向修复") {
		t.Errorf("assignment card must carry the issue description; got:\n%s", got)
	}
}

func TestInboxPushDoesNotBackfillStatusChanges(t *testing.T) {
	// A description repeated on every status flip is spam, not signal.
	p, q, api := newTestPatcher(t)
	withBoundMember(t, q)
	q.issue = db.Issue{Description: pgtype.Text{String: "must not appear", Valid: true}}

	if !deliver(t, p, inboxItem(map[string]any{"type": "status_changed", "body": nil})) {
		t.Fatal("expected delivery to succeed")
	}
	if got := api.mdCardSent[0].Markdown; strings.Contains(got, "must not appear") {
		t.Errorf("status_changed must stay body-less; got:\n%s", got)
	}
}

func TestInboxCardUsesFallbackLabelForUnknownType(t *testing.T) {
	_, summary := buildInboxCard(inboxItem(map[string]any{"type": "some_future_type"}), testInboxWorkspaceID, "acme")
	if !strings.HasPrefix(summary, "【新消息】") {
		t.Errorf("unknown types need a readable fallback label; got %q", summary)
	}
}

func TestInboxDigestFoldsSameIssueEvents(t *testing.T) {
	// A burst on one issue must read as one growing card, not a card per
	// event: first event sends, the second patches the same message.
	p, q, api := newTestPatcher(t)
	withBoundMember(t, q)
	t.Setenv("LARK_APP_URL", "https://multica.example.com")
	api.mdCardReturn = "md_msg_1"

	if !deliver(t, p, inboxItem(nil)) {
		t.Fatal("first delivery failed")
	}
	if !deliver(t, p, inboxItem(map[string]any{"type": "status_changed", "body": nil})) {
		t.Fatal("second delivery failed")
	}

	if len(api.mdCardSent) != 1 {
		t.Fatalf("expected 1 send, got %d", len(api.mdCardSent))
	}
	if len(api.patched) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(api.patched))
	}
	patch := api.patched[0]
	if patch.LarkCardMessageID != "md_msg_1" {
		t.Errorf("patch targeted %q, want the first card", patch.LarkCardMessageID)
	}
	for _, want := range []string{"**【任务指派】Fix the login redirect**", "assigned to you", "**状态变更**", `<`} {
		if want == `<` {
			continue
		}
		if !strings.Contains(patch.CardJSON, want) {
			t.Errorf("patched card missing %q; got:\n%s", want, patch.CardJSON)
		}
	}
	if strings.Count(patch.CardJSON, "查看详情") != 1 {
		t.Errorf("digest card must keep exactly one 查看详情 line:\n%s", patch.CardJSON)
	}
	if !strings.Contains(patch.CardJSON, `"update_multi":true`) {
		t.Errorf("patched card must stay declared updatable:\n%s", patch.CardJSON)
	}
}

func TestInboxDigestKeepsDistinctIssuesApart(t *testing.T) {
	p, q, api := newTestPatcher(t)
	withBoundMember(t, q)
	api.mdCardReturn = "md_msg_1"

	if !deliver(t, p, inboxItem(nil)) {
		t.Fatal("first delivery failed")
	}
	if !deliver(t, p, inboxItem(map[string]any{"issue_id": "99999999-9999-9999-9999-999999999999"})) {
		t.Fatal("second delivery failed")
	}
	if len(api.mdCardSent) != 2 || len(api.patched) != 0 {
		t.Fatalf("distinct issues must get distinct cards: sends=%d patches=%d", len(api.mdCardSent), len(api.patched))
	}
}

func TestInboxDigestExpiresAfterWindow(t *testing.T) {
	p, q, api := newTestPatcher(t)
	withBoundMember(t, q)
	api.mdCardReturn = "md_msg_1"

	base := time.Now()
	p.cfg.Now = func() time.Time { return base }
	if !deliver(t, p, inboxItem(nil)) {
		t.Fatal("first delivery failed")
	}
	p.cfg.Now = func() time.Time { return base.Add(inboxDigestWindow + time.Minute) }
	if !deliver(t, p, inboxItem(map[string]any{"type": "status_changed", "body": nil})) {
		t.Fatal("second delivery failed")
	}
	if len(api.mdCardSent) != 2 || len(api.patched) != 0 {
		t.Fatalf("a cold issue must start a fresh card: sends=%d patches=%d", len(api.mdCardSent), len(api.patched))
	}
}

func TestInboxDigestPatchFailureFallsBackToFreshCard(t *testing.T) {
	p, q, api := newTestPatcher(t)
	withBoundMember(t, q)
	api.mdCardReturn = "md_msg_1"

	if !deliver(t, p, inboxItem(nil)) {
		t.Fatal("first delivery failed")
	}
	api.patchErr = errors.New("card recalled")
	if !deliver(t, p, inboxItem(map[string]any{"type": "status_changed", "body": nil})) {
		t.Fatal("second delivery must fall back, not fail")
	}
	if len(api.mdCardSent) != 2 {
		t.Fatalf("expected fresh-card fallback, sends=%d", len(api.mdCardSent))
	}
}
