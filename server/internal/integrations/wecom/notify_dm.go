package wecom

import (
	"context"

	"github.com/multica-ai/multica/server/internal/integrations/channel/notify"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// aibot markdown size cap. WeCom rejects the whole frame past ~4096 chars
// while still acking the send, so an over-long push is silently lost rather
// than reported. 4000 leaves headroom.
const inboxMarkdownMaxLen = 4000

var _ notify.DMDeliverer = (*Outbound)(nil)

// DeliverDM sends an inbox push to a bound member over the aibot socket.
//
// This is the body that used to live in tryDeliverInbox. The subscription,
// the whitelist and the binding lookup moved up into the notify package; what
// stays here is the part that is genuinely WeCom's — addressing a single chat
// by the bot-scoped T-* userid, and routing across replicas when this one
// does not hold the WS lease.
//
// The result never carries a MessageID. sendTextCtx reads the ack only for
// its error code and discards the body, and WeCom's inbound callback carries
// no reply context, so a WeCom push cannot be replied to no matter what we
// record. Reporting an empty id keeps that honest rather than writing a
// ledger row no inbound message will ever match.
//
// The two markdown adjustments below stay here rather than in notify.
// renderPush: both answer to this renderer specifically — the space trick in
// breakMemberLinks is verified against WeCom's parser and nothing else, and
// the cap is WeCom's frame limit — so a shared renderer that applied either
// would be imposing one platform's rules on every other.
func (o *Outbound) DeliverDM(ctx context.Context, ref notify.PushRef, binding db.ChannelUserBinding, text string) (notify.DeliverResult, error) {
	if text == "" {
		return notify.DeliverResult{}, nil
	}
	// renderPush splices a member-authored issue title and body into a
	// message that goes out signed by the bot, so it runs through the same
	// guard every other such caller does (markdown.go). Before the cap, not
	// after: each break inserts a rune.
	//
	// Whole-text rather than per-field: renderPush emits no markdown link of
	// its own — the deep link is a bare URL — so there is no "](" or "]:" in
	// the scaffolding for the guard to separate.
	//
	// FitPush rather than a plain tail cut: WeCom's pushes are never
	// replyable, so the deep link is the recipient's only route to the
	// notification, and a tail cut is exactly what takes it away.
	text = notify.FitPush(breakMemberLinks(text), inboxMarkdownMaxLen)
	var sender *wsSender
	if o.senders != nil {
		sender = o.senders.get(binding.InstallationID)
	}
	if sender == nil {
		if o.relay.publish(relayFrame{
			Kind:           relayKindInbox,
			InstallationID: util.UUIDToString(binding.InstallationID),
			ChatID:         binding.ChannelUserID,
			ChatType:       chatTypeSingleInt,
			Content:        text,
		}, relayInboxEventID(ref.InboxItemID, ref.RecipientUserID)) {
			o.logger.DebugContext(ctx, "wecom outbound: routed an inbox push to the replica holding the socket",
				"installation_id", uuidStringPub(binding.InstallationID))
			return notify.DeliverResult{State: notify.StateHandedOff}, nil
		}
		o.logger.WarnContext(ctx, "wecom outbound: inbox push not delivered and not routable",
			"installation_id", uuidStringPub(binding.InstallationID))
		return notify.DeliverResult{}, nil
	}
	if err := sender.sendTextCtx(ctx, binding.ChannelUserID, chatTypeSingleInt, text); err != nil {
		return notify.DeliverResult{}, err
	}
	o.logger.DebugContext(ctx, "wecom outbound: inbox delivered via bot",
		"installation_id", uuidStringPub(binding.InstallationID))
	return notify.DeliverResult{State: notify.StateDelivered}, nil
}

// AcceptsReplies is false for WeCom, for the same reason DeliverDM reports no
// MessageID: neither end of the loop exists. Closing it needs the aibot
// send ack to carry a msgid (sendTextCtx discards the ack body today) and an
// inbound quoted-reply field; both are unknown and out of scope.
func (o *Outbound) AcceptsReplies() bool { return false }
