package wecom

// receipt.go — the one-frame receipt: on ingest, the person who asked sees
// that their message was heard.
//
// WeCom has no typing indicator. A question that takes the agent a minute to
// answer leaves the chat silent for that minute, which reads as "nothing
// happened". DingTalk's ack notifier solves the same problem with a plain
// "working on it" message; this is the WeCom shape of it, sent as a single
// aibot_respond_msg stream frame with finish=true.
//
// Why a stream frame rather than aibot_send_msg: the frame is addressed by the
// callback's own req_id, so it is a reply to that message inside WeCom's
// 24-hour reply window rather than a proactive push, and it is the transport a
// live bubble would need if one is ever built on top. It buys nothing on the
// per-conversation budget — document 101463 says replies and pushes share
// the same 30 per minute — so the choice is on those merits alone.
//
// What this deliberately is NOT: a bubble that is later replaced by the
// answer. The answer keeps going out through the existing outbound path,
// exactly as it does today. A receipt whose verdict is lost or misattributed
// costs the user a receipt — degraded, visible, harmless.

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

// receiptText is the receipt. Short, because it is a real message in the chat
// and not an indicator that goes away.
const receiptText = "收到，正在处理。"

// receiptCoalesceWindow suppresses duplicate receipts for one session. It sits
// just above the run debounce window, so a burst of messages that flush into
// one run yields a single receipt, while a genuinely later turn gets its own.
const receiptCoalesceWindow = 5 * time.Second

// receiptBudget bounds one receipt end to end: the wait for the writer, the
// write, and the wait for the verdict. OnIngested runs on the engine's
// detached goroutine, so nothing else bounds it.
const receiptBudget = 8 * time.Second

// receiptNotifier sends the receipt and coalesces bursts per session. It
// implements engine.TypingNotifier.
type receiptNotifier struct {
	senders *sendersRegistry
	logger  *slog.Logger
	window  time.Duration
	now     func() time.Time

	mu   sync.Mutex
	last map[string]time.Time
}

var _ engine.TypingNotifier = (*receiptNotifier)(nil)

// NewReceiptNotifier builds the receipt notifier over the shared senders
// registry — the same one the inbound loop installs its socket on.
func NewReceiptNotifier(senders *SendersRegistry, logger *slog.Logger) *receiptNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &receiptNotifier{
		senders: senders,
		logger:  logger,
		window:  receiptCoalesceWindow,
		now:     time.Now,
		last:    make(map[string]time.Time),
	}
}

// OnIngested sends the receipt unless one for the same session went out
// inside the coalesce window.
func (n *receiptNotifier) OnIngested(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID) {
	if n.senders == nil {
		return
	}
	wm, err := wecomMsgFromRaw(msg)
	if err != nil || wm.ReqID == "" {
		// Only a message callback's req_id can carry a reply; an event
		// callback's looks usable and is refused (846605). Nothing to say.
		return
	}
	if n.suppress(sessionID) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, receiptBudget)
	defer cancel()
	h := streamHandle{ReqID: wm.ReqID, StreamID: newStreamID(), InstallationID: inst.ID}
	if err := n.senders.stream(ctx, h, receiptText, true); err != nil {
		// Degraded, not broken: the answer still travels its own path. The
		// log is where a receipt that never lands becomes visible.
		n.logger.WarnContext(ctx, "wecom receipt: not sent",
			"installation_id", util.UUIDToString(inst.ID), "error", err)
	}
}

// OnSettled clears the session's coalescing entry so its next turn gets a
// receipt at once. Called for runs that enqueued no task; task-spawning
// sessions age out of the map instead (suppress prunes on a miss).
func (n *receiptNotifier) OnSettled(_ context.Context, sessionID pgtype.UUID) {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return
	}
	n.mu.Lock()
	delete(n.last, key)
	n.mu.Unlock()
}

// suppress reports whether a receipt for sessionID should be skipped, and
// otherwise records this one. Check-and-set under one lock, so concurrent
// ingests of one burst yield a single receipt.
func (n *receiptNotifier) suppress(sessionID pgtype.UUID) bool {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return false
	}
	now := n.now()
	n.mu.Lock()
	defer n.mu.Unlock()
	if last, ok := n.last[key]; ok && now.Sub(last) < n.window {
		return true
	}
	for k, last := range n.last {
		if now.Sub(last) >= n.window {
			delete(n.last, k)
		}
	}
	n.last[key] = now
	return false
}
