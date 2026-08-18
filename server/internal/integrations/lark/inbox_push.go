package lark

// inbox_push.go — delivery of inbox:new notifications to a member's Lark
// DM. Mirrors the WeCom smart-bot path (internal/integrations/wecom/
// outbound.go handleInboxNew): a member who has bound their Lark account
// gets the notification pushed to them; everyone else just sees it in the
// in-app inbox as before. Every miss is a no-op, never an error to the
// publisher — the inbox item is already written by the time we run.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// inboxPushTimeout bounds one delivery, so a Lark outage cannot pin the
// delivery goroutine (and its DB connection) indefinitely.
const inboxPushTimeout = 10 * time.Second

// handleInboxNew is the inbox:new subscriber. It delivers to the bound
// member's Lark DM and is a no-op on every miss: a non-member recipient
// (agents get nothing over chat channels), no Lark binding, a missing
// installation, or a send failure. Notification preferences are already
// applied upstream — a muted type never produces an inbox item, so
// nothing is re-checked here.
//
// Delivery is fire-and-forget on a goroutine. Bus delivery is synchronous
// and inbox:new is published on user-facing write paths (adding a comment,
// changing a status), so an outbound Lark HTTP call here would add its
// latency to those API responses — up to inboxPushTimeout per bound
// recipient when Lark is slow. The WeCom equivalent delivers inline, but
// its send is an in-process handoff to an already-connected WS sender;
// this one leaves the building.
func (p *Patcher) handleInboxNew(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return
	}
	if rt, _ := item["recipient_type"].(string); rt != "member" {
		return
	}
	recipientIDStr, _ := item["recipient_id"].(string)
	workspaceIDStr, _ := item["workspace_id"].(string)
	if recipientIDStr == "" || workspaceIDStr == "" {
		return
	}

	go func() {
		// The bus recovers panics in synchronous handlers; a goroutine
		// escapes that net, so it carries its own.
		defer func() {
			if r := recover(); r != nil {
				p.cfg.Logger.Error("lark inbox push: panic recovered", "recovered", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), inboxPushTimeout)
		defer cancel()
		p.tryDeliverInbox(ctx, item, recipientIDStr, workspaceIDStr)
	}()
}

// tryDeliverInbox is the delivery core. Reports whether the bot pushed the
// notification; the bool is for tests and logging, not for the caller's
// control flow.
func (p *Patcher) tryDeliverInbox(ctx context.Context, item map[string]any, recipientIDStr, workspaceIDStr string) bool {
	log := p.cfg.Logger
	recipientID, err := util.ParseUUID(recipientIDStr)
	if err != nil || !recipientID.Valid {
		return false
	}
	workspaceID, err := util.ParseUUID(workspaceIDStr)
	if err != nil || !workspaceID.Valid {
		return false
	}

	binding, err := p.queries.FindChannelBindingForMember(ctx, db.FindChannelBindingForMemberParams{
		WorkspaceID:   workspaceID,
		MulticaUserID: recipientID,
		// The binding rows this platform writes carry channel_type
		// "feishu" (channel_store.go), not "lark" — the package name and
		// the stored value differ.
		ChannelType: channelTypeFeishu,
	})
	if err != nil {
		// No binding is the ordinary case for an unbound member, not a
		// fault: they keep receiving the notification in-app.
		if !errors.Is(err, pgx.ErrNoRows) {
			log.WarnContext(ctx, "lark inbox push: lookup member binding failed",
				"error", err, "workspace_id", workspaceIDStr, "recipient_id", recipientIDStr)
		}
		return false
	}

	inst, err := p.queries.GetLarkInstallation(ctx, binding.InstallationID)
	if err != nil {
		log.WarnContext(ctx, "lark inbox push: load installation failed",
			"error", err, "recipient_id", recipientIDStr)
		return false
	}
	creds, err := p.installationCredentials(inst)
	if err != nil {
		log.WarnContext(ctx, "lark inbox push: resolve credentials failed",
			"error", err, "recipient_id", recipientIDStr)
		return false
	}

	// Best-effort slug for a readable link; the workspace UUID stands in
	// when the lookup fails.
	slug := ""
	if ws, err := p.queries.GetWorkspace(ctx, workspaceID); err == nil {
		slug = ws.Slug
	}
	item = p.backfillAssignmentBody(ctx, item)
	parts, ok := buildInboxParts(item, workspaceIDStr, slug)
	if !ok {
		return false
	}

	// A markdown card rather than plain text: notification bodies are
	// member/agent-authored markdown, and msg_type=text shows the syntax
	// literally while a bare URL is the only link form it can carry. The
	// binding's channel_user_id is the member's per-app open_id, so the
	// send addresses the person and Lark opens the 1:1 chat itself.
	if _, err := p.client.SendMarkdownCard(ctx, SendMarkdownCardParams{
		InstallationID: creds,
		ChatID:         ChatID(binding.ChannelUserID),
		Markdown:       composeInboxCard(parts.header, parts.body, nil, parts.link),
		Summary:        parts.summary,
		ReceiveIDType:  ReceiveIDOpenID,
	}); err != nil {
		log.WarnContext(ctx, "lark inbox push: send failed",
			"error", err, "recipient_open_id", binding.ChannelUserID)
		return false
	}
	log.DebugContext(ctx, "lark inbox push: delivered",
		"recipient_id", recipientIDStr, "inbox_type", item["type"])
	return true
}

// descriptionBackfillTypes are the notification types whose inbox item
// carries no body (cmd/server/notification_listeners.go passes "" for
// them) while the issue's description is exactly what the recipient needs
// to decide whether to act. Status flips and reactions stay body-less: a
// description repeated on every transition is spam, not signal.
var descriptionBackfillTypes = map[string]bool{
	"issue_assigned":   true,
	"assignee_changed": true,
}

// backfillAssignmentBody fills a body-less assignment notification with the
// issue's description. It works on a copy — the item map is shared with the
// event's other subscribers and this runs on the delivery goroutine.
// Best-effort: any miss returns the item unchanged and the card simply has
// no body, which is what it had anyway.
func (p *Patcher) backfillAssignmentBody(ctx context.Context, item map[string]any) map[string]any {
	if inboxItemBody(item) != "" {
		return item
	}
	if t, _ := item["type"].(string); !descriptionBackfillTypes[t] {
		return item
	}
	issueID := inboxItemIssueID(item)
	if issueID == "" {
		return item
	}
	iu, err := util.ParseUUID(issueID)
	if err != nil || !iu.Valid {
		return item
	}
	iss, err := p.queries.GetIssue(ctx, iu)
	if err != nil || !iss.Description.Valid || strings.TrimSpace(iss.Description.String) == "" {
		return item
	}
	clone := make(map[string]any, len(item)+1)
	for k, v := range item {
		clone[k] = v
	}
	clone["body"] = iss.Description.String
	return clone
}
