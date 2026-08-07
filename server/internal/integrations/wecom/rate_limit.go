package wecom

// rate_limit.go — WeCom's published per-target outbound quotas, expressed as
// windows for the shared outbox rate gate.
//
// The gate mechanism (advisory lock, sliding-window count, defer-without-
// spending-an-attempt) is channel-agnostic and lives in channel/outbox. The only
// WeCom-specific facts are the numbers below and the reason they are needed at
// all: aibot rejects an over-quota aibot_send_msg with errcode 45009, and a
// rejected reply is a user-visible non-answer. Backing off ourselves turns that
// into a short delay instead.

import (
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel/outbox"
)

// WeCom's aibot_send_msg quotas per target chat. Set below the documented
// ceilings on purpose: the platform counts on its own clock and its own view of
// what arrived, so a gate sitting exactly on the limit would still trip on
// rounding at the boundary.
const (
	rateLimitPerMinute = 30
	rateLimitPerHour   = 1000
)

// rateWindows are WeCom's quotas, shortest first so the common rejection is the
// cheapest to detect.
func rateWindows() []outbox.Window {
	return []outbox.Window{
		{Name: "minute", Duration: time.Minute, Limit: rateLimitPerMinute},
		{Name: "hour", Duration: time.Hour, Limit: rateLimitPerHour},
	}
}

// NewRateGate builds the WeCom outbound rate gate. Boot passes it to
// ChannelDeps.Outbox so every installation's consumer admits sends through it.
func NewRateGate(bind outbox.BindTx, tx outbox.TxStarter) (outbox.RateGate, error) {
	return outbox.NewWindowRateGate(bind, tx, rateWindows()...)
}
