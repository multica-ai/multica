package wecom

// rate_limit.go — everything that keeps aibot_send_msg near WeCom's published
// quota: a per-chat gate in front of the write, and one jittered retry for the
// two errcodes that mean "not now" rather than "never".
//
// WHAT THIS DELIVERS, AND WHAT IT DOES NOT. A conversation that stays under 30
// pushes a minute is untouched. One that goes a little over is DELAYED into
// the next free slot rather than refused — reserve waits, inside a budget. One
// that bursts far over is refused HERE, before the write: the 31st frame of a
// burst is not delayed, it is turned away with a definite error and nothing on
// the wire. Making that burst deliverable is not something this file can do;
// that needs somewhere to keep the frame until the window moves, and this tree
// has no such place since the outbound queue was retired.
//
// Refusing on our own side is still the improvement, because of what it
// replaces. Without the gate the same burst goes to WeCom and comes back as
// errcode 45009: ws_sender turns that into a *wecomAPIError, and every caller
// on the outbound path reads a stated refusal as final — provablyNotSent says
// no re-offer, classifyDrop files platform_refused, and the reply is gone. One
// throttled frame is one answer the person never sees, with nothing on their
// screen to say so. Our own refusal costs nothing on the platform side, spends
// none of the chat's allowance, and stays provably unsent so the relay can
// offer it elsewhere.

import (
	"context"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"sort"
	"sync"
	"time"
)

// WeCom's published quota for messages into one CONVERSATION: 30 per minute,
// 1000 per hour. The aibot page states it twice, once under aibot_respond_msg
// and once under aibot_send_msg, in the same sentence both times — "无论是回复
// 还是主动推送消息，总共给某个会话发消息的限制为 30 条/分钟，1000 条/小时"
// (developer.work.weixin.qq.com/document/path/101463). Over-quota comes back
// as errcode 45009, "接口调用超过限制" (.../path/90313).
//
// Two things follow from that sentence, and the gate is built on both. The
// unit is the conversation, not the member, which is why the windows below are
// keyed on chat id — in a group the whole room shares one allowance. And
// replies and pushes draw on the SAME allowance, so a deployment that also
// answers in-window is spending from this budget too.
//
// These are the documented ceilings THEMSELVES, not a margin under them, and
// the comment does not claim otherwise. The retry below is what covers the
// case where the real ceiling turns out to be lower or counted over some other
// unit: a refusal gets a second chance instead of costing an answer.
const (
	rateLimitPerMinute = 30
	rateLimitPerHour   = 1000
)

// rateWaitBudget caps how long the gate holds a caller waiting for a slot.
// The cap is not the whole answer: reserve also keeps its hands off the
// write's own budget, so the wait that actually happens is
// min(rateWaitBudget, deadline − ackTimeout).
//
// It has to work that way because the callers do not share one deadline. A
// reply gets ten seconds for the whole delivery (outbound.go:155), an inbox
// push gets five (outbound.go:415), an attachment gets five minutes
// (outbound_media.go:83). Three seconds out of ten leaves a write seven; three
// out of five leaves it two, and the ack alone is allowed five
// (ws_sender.go:111). That write gets cut off mid-flight and request returns a
// context error — "we may have written and cannot tell", which unconfirmedReason
// has to file as unknown and an operator resolves by resending something the
// person may already have. So on the five-second path the gate does not wait
// at all; it refuses, and refusing is the honest failure because nothing was
// written and the caller still has its whole budget to record that.
const rateWaitBudget = 3 * time.Second

// errRateLimited — this process's own gate declined to write, because the
// chat's quota is spent and a slot will not free up inside the caller's
// budget. Nothing reached the wire.
//
// Deliberately not a wrapped context error, even when it is a caller's
// deadline that ended the wait. A context error on this path means "we may
// have written and cannot tell" (unconfirmedReason), which is the opposite of
// what happened here: the gate is upstream of the socket, so this is provably
// nothing sent, and provablyNotSent's default answers it correctly. It lands
// on transport_error, whose reason text already covers "this delivery's own
// budget ran out before it got a turn on the wire".
//
// That default is also what makes one asymmetry worth knowing, and it is the
// relay's rather than this file's. A frame that reaches the socket THROUGH the
// relay is handed back to the scheduler when provablyNotSent says nothing was
// written (relay_outbound.go:1276), so a refusal here costs it a retry chain
// of roughly one and a half LeaseSettle and no more. The same frame produced
// on the replica that already holds the lease never goes near that chain:
// sendTextCtx returns the refusal to its caller and outbound.go files a drop
// (outbound.go:162). So the relayed copy gets a second chance the direct one
// does not. Giving the direct path a bounded re-offer of its own is a change
// to the outbound path rather than to the gate, and is deliberately not here.
var errRateLimited = errors.New("wecom: this chat's outbound quota is spent")

// WeCom errcodes that mean the frame was refused for a reason that passes.
// Both are throttles rather than verdicts on the content: the same bytes are
// accepted once the window moves. Named the same way the connect path already
// names them (wecom_channel.go, credential_probe.go), where they are likewise
// treated as "come back later" and not as a rejection.
const (
	errCodeAPIFreqLimit        = 45009 // api freq out of limit
	errCodeAPIConcurrencyLimit = 45033 // api concurrency out of limit
)

// sendRetryBackoff is the base delay before a throttled frame's one retry.
//
// One cheap attempt at a refusal that may be momentary. NOT a wait for a slot:
// this gate is a sliding count, so the next slot frees when the window's
// oldest entry ages out of it, which for a burst is most of a whole window —
// admit will happily report 59.999s. Waiting for a slot is what reserve is
// for, and deriving a fixed delay from "30 a minute is one every two seconds"
// would be reading an average off a limit that is deliberately not one.
//
// Two seconds is sized for 45033, the concurrency refusal, whose documented
// remedy is exactly this and nothing more: "企业微信出于系统保护的考虑，会对同一个
// 企业调用同一个接口做并发数的限制，出现这种限制错误后，请企业调低并发数"
// (developer.work.weixin.qq.com/document/path/90313). No window has to turn
// over for that one to lift — fewer callers at once is the entire fix.
//
// For 45009 it is a long shot, and the code treats it as one rather than
// pretending otherwise. The same page says a frequency block lasts as long as
// the period that earned it: "频率拦截时长一般与调用的限制时长相同，比如说是分钟
// 级别的限制，则在中频率后的1分钟后自动解除". Two seconds cannot clear a
// minute's block. What buys the one attempt anyway is the seam this gate
// cannot see — a reconnect mints a fresh window (see sendQuota), so a 45009
// can be raised against a count this process is not keeping. retryUnaffordable
// is what stops that from turning into hammering: when OUR OWN window says the
// next slot is far off, our count agrees with WeCom's, the block is the real
// one, and the frame is not sent a second time. That also keeps us on the
// right side of the same page's advice for this family of codes — "仅系统失败
// 需要重试。其余错误码，应该排查下调用失败原因".
//
// Jittered by retryDelay, because a concurrency refusal hits every concurrent
// caller at once and a fixed delay sends all of them back at the same instant,
// which is the opposite of 调低并发数.
//
// A field on wsSender rather than a constant read at the call site, for the
// same reason ackTimeout is one: a test that has to stand still for it is a
// test nobody runs.
const sendRetryBackoff = 2 * time.Second

// sendMsgFrame writes one aibot_send_msg body under this chat's quota.
//
// The single door for that command: the two producers of one — a piece of an
// agent's answer (ws_sender.go) and a media push (media_upload.go) — spend the
// same per-conversation allowance, so a gate on either alone would be a gate
// on neither.
//
// aibot_send_msg is also the only command this tree sends into a conversation
// with, so gating it gates everything there is. That is a fact about the tree
// and not a property of the quota: the sentence quoted above puts
// aibot_respond_msg on the same allowance, so a reply path that writes frames
// has to reserve here as well, and cannot be excused by carrying a throttle of
// its own.
//
// A refusal is retried once, and only for a throttle the caller can still
// afford (retryUnaffordable). Retrying a stated refusal is safe in a way
// retrying a timeout is not: a non-zero errcode is the server saying it did
// not act on the frame, so a second attempt cannot duplicate anything. A
// verdict that never came (errAckTimeout) says nothing of the kind and is
// returned untouched.
//
// Written as first attempt / decision / second attempt rather than a loop,
// because the two attempts do not report failure the same way. The first one's
// refusal is a FACT — the server named an errcode — and it stays in hand for
// the rest of the function. As a loop it could not: the second pass's return
// value was simply the function's, so a context error raised inside request
// replaced a definite refusal with an unknown outcome, which is the one thing
// on this path that sends a person to resend a message by hand.
//
// That fact has a limit, and the closing switch turns on it. The refusal is
// the outcome of the FIRST frame. It can answer for the second attempt only
// while that attempt put nothing new on the wire; once the second frame is
// written, the refusal says nothing about where it ended up, and the unknown
// the attempt came back with is the only honest report. request marks which
// of the two happened — see errAckAbandoned.
func (s *wsSender) sendMsgFrame(ctx context.Context, chatID string, body map[string]any) error {
	if err := s.quota.reserve(ctx, chatID); err != nil {
		// No chat id on either line. In a one-to-one chat the chat id IS
		// the person's userid, and nothing else in this package puts one
		// in the log; an operator needs to know the bot is at its ceiling,
		// not who was talking to it.
		s.log.Warn("wecom: a chat is at its outbound quota, frame not written",
			"per_minute", rateLimitPerMinute, "per_hour", rateLimitPerHour)
		return err
	}
	_, refusal := s.request(ctx, cmdSendMsg, body)
	if refusal == nil || !throttled(refusal) {
		return refusal
	}

	delay := s.retryDelay()
	if reason := s.retryUnaffordable(ctx, chatID, delay); reason != "" {
		s.log.Warn("wecom: push throttled, retry skipped",
			"reason", reason, "backoff", delay, "error", refusal)
		return refusal
	}
	s.log.Warn("wecom: push throttled, retrying once",
		"backoff", delay, "error", refusal)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		// The REFUSAL, not the context error. What we know at this point is
		// that the server refused the frame and nothing landed; reporting the
		// cancellation instead would downgrade a definite outcome to an
		// unknown one, and an unknown is the one an operator resolves by hand.
		return refusal
	}

	if err := s.quota.reserve(ctx, chatID); err != nil {
		return refusal
	}
	_, err := s.request(ctx, cmdSendMsg, body)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errAckTimeout), errors.Is(err, errWriteAttempted), errors.Is(err, errAckAbandoned):
		// The retry's own outcome is genuinely unknown, and unknown outranks
		// the first refusal here: the second frame may be in front of the
		// person right now, and saying "refused" would deny a delivery that
		// happened. This is the one direction in which the first refusal is
		// NOT the better answer.
		//
		// errAckAbandoned is the case that looks most like the arm below and
		// belongs here. It IS a context error — errors.Is answers yes for
		// context.Canceled — but request raises it only after s.write returned
		// without error, so the second frame is on the wire and the first
		// attempt's errcode establishes nothing about where it ended up
		// (ws_sender.go). Tested ahead of the context arm for that reason: the
		// same error satisfies both, and the specific answer is the true one.
		return err
	case errors.Is(err, errRateLimited), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// Nothing was written this time — the gate turned the frame away
		// before the socket, or request's pre-write check did — so the first
		// refusal is still the whole story and it is the better half of it. It
		// files as platform_refused, it is provablyNotSent(false) either way
		// so no duplicate can follow from it, and it does not send anybody to
		// resend a message by hand.
		return refusal
	default:
		return err
	}
}

// retryDelay is sendRetryBackoff spread over ±50%, so the callers a
// concurrency refusal caught together do not come back together.
func (s *wsSender) retryDelay() time.Duration {
	base := s.retryBackoff
	if base <= 0 {
		return 0
	}
	return base/2 + time.Duration(mathrand.Int64N(int64(base)))
}

// retryUnaffordable reports why the one retry should not be attempted, or ""
// when it should. Two ways it is not worth a frame, and both are answered from
// what this process knows rather than from hope.
//
// The caller has to be able to sit through it. request spends the caller's
// context on the write AND on the wait for the verdict, so an attempt needs
// delay + ackTimeout of deadline left; started with less, it comes back as a
// context error, which is an unknown outcome standing where a definite refusal
// used to be. The reply path has ten seconds for the whole delivery
// (outbound.go:155) and a throttle late in one can leave less than that.
//
// And the gate has to be able to hand out a slot when the delay is up — admit
// already knows when the next one frees, so ask it. A slot that is a minute
// away means our own count has reached the ceiling too, which means the
// refusal is the quota's rather than a stale count's, and WeCom holds a
// frequency block for the rest of the period it was earned in (doc 90313,
// quoted on sendRetryBackoff). Sending again into that costs a frame and
// changes nothing.
func (s *wsSender) retryUnaffordable(ctx context.Context, chatID string, delay time.Duration) string {
	if d, ok := ctx.Deadline(); ok && time.Until(d) < delay+ackTimeout {
		return "the caller has no budget for another attempt"
	}
	if wait := s.quota.nextSlot(chatID, time.Now().Add(delay)); wait > 0 {
		return fmt.Sprintf("this chat's own quota has no slot for it either, the next one is %s off",
			wait.Round(time.Millisecond))
	}
	return ""
}

// throttled reports whether a send failure was a throttle the platform will
// lift by itself.
func throttled(err error) bool {
	var apiErr *wecomAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Code == errCodeAPIFreqLimit || apiErr.Code == errCodeAPIConcurrencyLimit
}

// quotaWindow is one of WeCom's published windows: at most limit sends in any
// span-long stretch. limit is at least 1 — a window nothing can pass is a
// deployment with the bot switched off, which belongs upstream of here.
type quotaWindow struct {
	span  time.Duration
	limit int
}

// sendQuota admits aibot_send_msg frames at the published rate, counted per
// target chat.
//
// Why one map in one process is the whole accounting, with nothing shared:
// WeCom counts per (application, recipient), and aibot has no REST outbound
// path, so every frame for an installation is written by the replica holding
// the lease on that installation's socket. One sendQuota lives on one
// wsSender, which is one socket, which is one installation — so the frames
// this gate sees are exactly the frames WeCom is counting. The DB-backed gate
// this replaces needed shared state because the outbound queue let ANY replica
// claim a row and send it; with the queue gone, so is the reason.
//
// The scope has one seam: a reconnect mints a new wsSender with an empty
// window, so a socket that flaps mid-minute can put us over WeCom's count
// while ours reads clean. That is the case sendMsgFrame's retry exists for,
// and the reason this gate is not written as if it were exact.
//
// A sliding count rather than a token bucket, because the published figure is
// a count in a window: a bucket of 30 refilling at 30/minute admits up to 60
// inside one rolling minute, which is the number we are trying not to reach.
type sendQuota struct {
	mu sync.Mutex
	// sent holds each chat's send times, ascending, trimmed to the longest
	// window. Bounded by rateLimitPerHour entries per chat; quiet chats are
	// dropped by sweep.
	sent      map[string][]time.Time
	lastSweep time.Time

	windows []quotaWindow
	longest time.Duration
	maxWait time.Duration
	// writeBudget is what reserve refuses to spend out of a caller's deadline,
	// because the write that follows needs it. ackTimeout, since that is what
	// bounds the wait for the verdict (ws_sender.go).
	writeBudget time.Duration
}

func newSendQuota() *sendQuota {
	return newSendQuotaWith(rateWaitBudget,
		quotaWindow{span: time.Minute, limit: rateLimitPerMinute},
		quotaWindow{span: time.Hour, limit: rateLimitPerHour},
	)
}

// newSendQuotaWith builds a gate with windows of its own. Tests use it to
// exercise the waiting and refusing paths in milliseconds instead of minutes.
func newSendQuotaWith(maxWait time.Duration, windows ...quotaWindow) *sendQuota {
	q := &sendQuota{
		sent:        make(map[string][]time.Time),
		windows:     windows,
		maxWait:     maxWait,
		writeBudget: ackTimeout,
	}
	for _, w := range windows {
		if w.span > q.longest {
			q.longest = w.span
		}
	}
	return q
}

// reserve returns once this chat may take a slot, or fails without one.
//
// It waits when a slot is close enough to be worth waiting for — the point of
// a gate is that a burst arrives late rather than not at all — and gives up
// immediately when it is not, rather than spending a caller's whole budget to
// arrive at the same answer with no time left to record it.
func (q *sendQuota) reserve(ctx context.Context, chatID string) error {
	giveUpAt := time.Now().Add(q.maxWait)
	if d, ok := ctx.Deadline(); ok {
		// deadline − writeBudget, not the deadline. A wait that merely fits
		// inside the caller's deadline is not good enough: what comes after it
		// is a write plus a wait for the verdict, and ackTimeout is how long
		// that alone is allowed to take. Waiting past this point buys a turn
		// on the wire the caller can no longer sit through, and request then
		// returns a context error — an outcome nobody can classify, where the
		// gate could have returned a definite one for free. See rateWaitBudget
		// for the deadlines this actually bites on.
		if latest := d.Add(-q.writeBudget); latest.Before(giveUpAt) {
			giveUpAt = latest
		}
	}
	for {
		wait := q.admit(chatID, time.Now())
		if wait == 0 {
			// A slot was free, so no budget was needed. Deliberately checked
			// before giveUpAt: a caller down to its last moment still gets its
			// turn on the wire when the gate is not in the way.
			return nil
		}
		if time.Now().Add(wait).After(giveUpAt) {
			return fmt.Errorf("%w: the next slot is %s away", errRateLimited, wait.Round(time.Millisecond))
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: the caller gave up while waiting for a slot", errRateLimited)
		}
	}
}

// admit records one send against chatID and returns 0, or leaves the count
// untouched and returns how long until the earliest slot frees.
func (q *sendQuota) admit(chatID string, now time.Time) time.Duration {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sweep(now)

	sent := trimBefore(q.sent[chatID], now.Add(-q.longest))
	if wait := q.waitFor(sent, now); wait > 0 {
		q.sent[chatID] = sent
		return wait
	}
	q.sent[chatID] = append(sent, now)
	return 0
}

// nextSlot is how long after at this chat's next slot frees, WITHOUT taking
// one. The read-only twin of admit, for deciding whether an action is worth
// starting — asking admit would answer the question by spending the thing
// being asked about.
func (q *sendQuota) nextSlot(chatID string, at time.Time) time.Duration {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.waitFor(trimBefore(q.sent[chatID], at.Add(-q.longest)), at)
}

// waitFor is how long until sent has room in every window, 0 when it has room
// now. The one place the windows are read, so admit and nextSlot cannot drift
// into answering the same question differently. Caller holds mu.
func (q *sendQuota) waitFor(sent []time.Time, now time.Time) time.Duration {
	var wait time.Duration
	for _, w := range q.windows {
		first := indexAtOrAfter(sent, now.Add(-w.span))
		inWindow := len(sent) - first
		if inWindow < w.limit {
			continue
		}
		// The window has no room until enough of its oldest entries age out
		// of it — one of them for a window sitting exactly on the limit.
		if free := sent[first+inWindow-w.limit].Add(w.span).Sub(now); free > wait {
			wait = free
		}
	}
	return wait
}

// sweep drops chats with nothing left inside the longest window. Without it a
// process that has talked to many chats keeps a timestamp slice alive for
// every one of them for as long as it runs. Caller holds mu.
func (q *sendQuota) sweep(now time.Time) {
	if now.Sub(q.lastSweep) < q.longest {
		return
	}
	q.lastSweep = now
	cutoff := now.Add(-q.longest)
	for chat, sent := range q.sent {
		if len(sent) == 0 || !sent[len(sent)-1].After(cutoff) {
			delete(q.sent, chat)
		}
	}
}

// trimBefore drops the entries older than cutoff from an ascending slice.
func trimBefore(sent []time.Time, cutoff time.Time) []time.Time {
	return sent[indexAtOrAfter(sent, cutoff):]
}

// indexAtOrAfter is where cutoff falls in an ascending slice of send times.
func indexAtOrAfter(sent []time.Time, cutoff time.Time) int {
	return sort.Search(len(sent), func(i int) bool { return sent[i].After(cutoff) })
}
