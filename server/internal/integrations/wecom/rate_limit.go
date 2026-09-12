package wecom

// rate_limit.go — everything that keeps aibot_send_msg inside WeCom's
// published quota: a per-chat gate in front of the write, and one short retry
// for the two errcodes that mean "not now" rather than "never".
//
// Both halves answer the same failure. WeCom refuses an over-quota push with
// errcode 45009, ws_sender turns that into a *wecomAPIError, and every caller
// on the outbound path reads a stated refusal as final — provablyNotSent says
// no re-offer, classifyDrop files platform_refused, and the reply is gone. One
// throttled frame is one answer the person never sees, with nothing on their
// screen to say so. The gate makes reaching that unlikely; the retry makes
// reaching it survivable.

import (
	"context"
	"errors"
	"fmt"
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
//
// Sized against the budget the callers run on: the outbound subscriber gives a
// whole delivery ten seconds (outbound.go), and a gate that could eat all of
// it would turn "delayed" into "the context expired mid-write", which is an
// outcome nobody can classify — unconfirmedReason has to call that unknown,
// and an unknown tells an operator to resend something the user may already
// have. Refusing early is the honest failure: nothing was written, so the
// caller gets a definite answer with time left to record it.
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

// sendRetryBackoff is how long a throttled frame waits before its one retry.
//
// Two seconds is one slot's worth at the documented rate: 30 per minute is a
// send every two seconds on average, so this is the shortest wait that can
// expect an earlier send to have aged out of the window. It is also short
// enough to leave a delivery most of its ten-second budget, which is what
// decides the question — a longer wait that runs the budget out reports an
// outcome nobody can classify, and a lost answer is what we are here to avoid,
// not to relabel.
//
// A field on wsSender rather than a constant read at the call site, for the
// same reason ackTimeout is one: a test that has to stand still for it is a
// test nobody runs.
const sendRetryBackoff = 2 * time.Second

// sendMsgFrame writes one aibot_send_msg body under this chat's quota.
//
// The single door for that command: the two producers of one — a piece of an
// agent's answer (ws_sender.go) and a media push (media_upload.go) — spend the
// same per-recipient allowance, so a gate on either alone would be a gate on
// neither. Stream frames are NOT here on purpose: they ride aibot_respond_msg,
// which is a different command with its own backpressure (errStreamBusy) and
// its own 1.5s throttle at the source (progress_render.go).
//
// A refusal is retried once, and only for a throttle. Retrying a stated
// refusal is safe in a way retrying a timeout is not: a non-zero errcode is
// the server saying it did not act on the frame, so a second attempt cannot
// duplicate anything. A verdict that never came (errAckTimeout) says nothing
// of the kind and is returned untouched.
func (s *wsSender) sendMsgFrame(ctx context.Context, chatID string, body map[string]any) error {
	for attempt := 0; ; attempt++ {
		if err := s.quota.reserve(ctx, chatID); err != nil {
			// No chat id on either line. In a one-to-one chat the chat id IS
			// the person's userid, and nothing else in this package puts one
			// in the log; an operator needs to know the bot is at its ceiling,
			// not who was talking to it.
			s.log.Warn("wecom: a chat is at its outbound quota, frame not written",
				"per_minute", rateLimitPerMinute, "per_hour", rateLimitPerHour)
			return err
		}
		_, err := s.request(ctx, cmdSendMsg, body)
		if err == nil || attempt > 0 || !throttled(err) {
			return err
		}
		s.log.Warn("wecom: push throttled, retrying once",
			"backoff", s.retryBackoff, "error", err)
		// A context cut short during the backoff returns the REFUSAL, not the
		// context error. What we know at this point is that the server refused
		// the frame and nothing landed; reporting the cancellation instead
		// would downgrade a definite outcome to an unknown one, and an unknown
		// is the one an operator has to resolve by hand.
		timer := time.NewTimer(s.retryBackoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return err
		}
	}
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
	q := &sendQuota{sent: make(map[string][]time.Time), windows: windows, maxWait: maxWait}
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
	if d, ok := ctx.Deadline(); ok && d.Before(giveUpAt) {
		giveUpAt = d
	}
	for {
		wait := q.admit(chatID, time.Now())
		if wait == 0 {
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
	if wait > 0 {
		q.sent[chatID] = sent
		return wait
	}
	q.sent[chatID] = append(sent, now)
	return 0
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
