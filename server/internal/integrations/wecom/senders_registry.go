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
	"sync"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// sendersRegistry is a goroutine-safe installation_id → wsSender map.
// SendersRegistry is exported only so boot can mint one before the relay's
// shard readers start; every method stays unexported.
type SendersRegistry = sendersRegistry

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
func NewSendersRegistry() *SendersRegistry { return newSendersRegistry() }

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
