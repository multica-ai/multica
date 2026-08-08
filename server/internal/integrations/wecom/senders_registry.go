package wecom

// senders_registry.go — a small process-wide map from installation_id to
// live wsSender. wecomChannel.Connect adds an entry on entry and clears it
// on exit; OutboundReplier and Outbound look up by installation id to push
// aibot_send_msg over the same socket the inbound loop owns (aibot has no
// REST outbound path; every write goes over the WebSocket). wecomChannel.Send
// is not a reader — it returns ErrSendNotSupported.
//
// Why a registry rather than storing the sender on wecomChannel:
// OutboundReplier is created once at boot with the shared engine.Router
// and does not have per-installation Channel handles. When the engine
// invokes Replier.Reply, it passes engine.ResolvedInstallation carrying
// the installation id, not the Channel. The registry is the seam that
// lets the boot-time Replier reach the per-installation live connection
// without threading the Channel through the engine.

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// sendersRegistry is a goroutine-safe installation_id → wsSender map.
type sendersRegistry struct {
	mu    sync.RWMutex
	byKey map[string]*wsSender
}

// newSendersRegistry constructs an empty registry.
func newSendersRegistry() *sendersRegistry {
	return &sendersRegistry{byKey: make(map[string]*wsSender)}
}

// NewSendersRegistry is the public constructor boot uses to inject the
// same registry into both the wecom ChannelDeps (writer side) and the
// OutboundReplier (reader side). Kept exported so router.go can wire it
// without importing an unexported type.
func NewSendersRegistry() *sendersRegistry { return newSendersRegistry() }

func (r *sendersRegistry) set(id pgtype.UUID, s *wsSender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byKey[util.UUIDToString(id)] = s
}

// clear removes this installation's entry, but only if s is still the sender
// registered under it. A generation that is shutting down must not evict its
// own successor: Connect installs on entry and clears on a defer, so when a
// lease flips while the old socket is still draining, the two overlap and the
// loser's defer runs after the winner's set. Deleting unconditionally there
// leaves the registry empty while a healthy connection is up, and every
// outbound push resolves to nil — the bot goes silent with nothing in the log
// to say why, until the next reconnect happens to re-register.
//
// dingtalk_channel.go:74 guards the same handover with
// `CompareAndSwap(c, nil)`; slack and lark have no registry at all because
// their outbound is REST. WeCom was the one platform deleting unconditionally.
func (r *sendersRegistry) clear(id pgtype.UUID, s *wsSender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := util.UUIDToString(id)
	if cur, ok := r.byKey[key]; ok && cur != s {
		return
	}
	delete(r.byKey, key)
}

// get returns the live wsSender for an installation, or nil when no
// connection is currently held. Callers MUST treat nil as "connection not
// ready" — Supervisor may be mid-reconnect after a lease flip.
func (r *sendersRegistry) get(id pgtype.UUID) *wsSender {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byKey[util.UUIDToString(id)]
}

// stream writes one frame of a streaming reply to the bubble h describes.
//
// A stream frame is only meaningful while the req_id it echoes is still fresh,
// so a frame that cannot go out now is worthless later — there is nothing
// useful to do with it but report the failure and let the caller say the same
// words as an ordinary message instead.
func (r *sendersRegistry) stream(ctx context.Context, h streamHandle, content string, finish bool) error {
	sender := r.get(h.InstallationID)
	if sender == nil {
		return errNoLiveConnection
	}
	return sender.respondStream(ctx, h.ReqID, h.StreamID, content, finish)
}

// sendTextCtx pushes a plain message to a chat over the installation's live
// connection — the fallback every closing frame degrades to. Separate from
// stream because a message has no req_id to expire: this is the path that
// still works when the bubble is beyond saving.
func (r *sendersRegistry) sendTextCtx(ctx context.Context, id pgtype.UUID, chatID string, chatType int, content string) error {
	sender := r.get(id)
	if sender == nil {
		return errNoLiveConnection
	}
	return sender.sendTextCtx(ctx, chatID, chatType, content)
}
