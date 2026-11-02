package wecom

// rate_limit_test.go — the gate and the retry that stand between an
// over-quota aibot_send_msg and a reply the person never sees.
//
// WHY THESE TESTS ARE THE ONLY THING GUARDING THIS. When the outbound queue
// was retired, the rate gate and the queue consumer's backoff went with it,
// and nothing anywhere failed: the gate lived in channel/outbox, its wecom
// half was a file of two constants, and no test in this package ever asserted
// that a push was admitted by anything. `go build`, `go vet` and the whole
// suite stayed green while the last defence against errcode 45009 left the
// tree. Every test below is written to fail if that happens again — they
// assert on what the socket carried and on which error came back, not on any
// internal the next refactor is free to move.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// quotaConn answers each aibot_send_msg with the next errcode in codes,
// and 0 once the script runs out. Recording only the pushes is deliberate:
// these tests are about the command WeCom counts, and a ping or a stream
// frame sharing the socket must not move the numbers they assert on.
type quotaConn struct {
	mu     sync.Mutex
	sender *wsSender
	pushes []map[string]any
	codes  []int
	writes int

	// ackDelay holds every verdict back this long. A real server answers in a
	// few hundred milliseconds, so this is how a test makes a write outlast
	// whatever budget the caller had left for it — the failure the gate is
	// supposed to make impossible rather than merely unlikely.
	ackDelay time.Duration
	// silentFrom is the 1-based push from which nothing answers at all, so a
	// test can park the sender on a verdict that never comes.
	silentFrom int
	// onPush runs after each push is recorded, outside the lock, with its
	// 1-based number. A hook rather than a channel because the tests that use
	// it need to act DURING the write, not after it.
	onPush func(n int)
}

func (c *quotaConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	c.mu.Lock()
	code, n := 0, 0
	if env.Cmd == cmdSendMsg {
		var body map[string]any
		_ = json.Unmarshal(env.Body, &body)
		c.pushes = append(c.pushes, body)
		if c.writes < len(c.codes) {
			code = c.codes[c.writes]
		}
		c.writes++
		n = c.writes
	}
	s, delay, silent, hook := c.sender, c.ackDelay, c.silentFrom, c.onPush
	c.mu.Unlock()
	if s != nil && (silent == 0 || n < silent) {
		ack := frameEnvelope{
			Headers: frameHeaders{ReqID: env.Headers.ReqID},
			ErrCode: code,
			ErrMsg:  "scripted",
		}
		if delay > 0 {
			time.AfterFunc(delay, func() { s.routeResponse(ack) })
		} else {
			s.routeResponse(ack)
		}
	}
	if hook != nil && n > 0 {
		hook(n)
	}
	return nil
}

func (c *quotaConn) ReadMessage() (int, []byte, error) { return 0, nil, nil }
func (c *quotaConn) SetReadDeadline(time.Time) error   { return nil }
func (c *quotaConn) SetWriteDeadline(time.Time) error  { return nil }
func (c *quotaConn) Close() error                      { return nil }

func (c *quotaConn) pushCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pushes)
}

func (c *quotaConn) pushedTo(chatID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, b := range c.pushes {
		if b["chatid"] == chatID {
			n++
		}
	}
	return n
}

// quotaSender wires a sender to a quotaConn with the backoff shortened,
// so a test exercising the retry does not stand still for two seconds.
func quotaSender(codes ...int) (*wsSender, *quotaConn) {
	conn := &quotaConn{codes: codes}
	s := newWSSender(conn, testLogger())
	s.retryBackoff = time.Millisecond
	conn.sender = s
	return s, conn
}

// logHook calls fn the first time a record's message contains want, and passes
// every record on. A log line is an unusual place to hang a test hook, and on
// the retry path it is the only exact one: "the refusal is in hand, the retry
// is decided, and nothing has been written yet" is a window sendMsgFrame is
// inside for exactly one statement, and that statement is this WARN. Hooking
// the conn instead would race the FIRST attempt's ack — both would be ready in
// request's select and which one it takes is a coin toss.
type logHook struct {
	slog.Handler
	want  string
	fn    func()
	once  sync.Once
	fired atomic.Bool
}

func (h *logHook) Handle(ctx context.Context, r slog.Record) error {
	if strings.Contains(r.Message, h.want) {
		h.once.Do(func() {
			h.fired.Store(true)
			h.fn()
		})
	}
	return h.Handler.Handle(ctx, r)
}

// hookedSender is quotaSender with fn wired to run the moment msg is logged.
func hookedSender(msg string, fn func(), codes ...int) (*wsSender, *quotaConn, *logHook) {
	s, conn := quotaSender(codes...)
	h := &logHook{Handler: slog.NewTextHandler(io.Discard, nil), want: msg, fn: fn}
	s.log = slog.New(h)
	return s, conn, h
}

// spentMinute is a gate running on WeCom's real published windows whose
// per-minute allowance for chatID is already gone, with the next slot freeing
// freeIn from now. The only way to reach the interesting states at the
// published figures inside a test: a real minute is a real minute, and admit
// takes the clock as an argument precisely so the timestamps can be placed.
func spentMinute(chatID string, freeIn time.Duration) *sendQuota {
	q := newSendQuota()
	at := time.Now().Add(freeIn - time.Minute)
	for i := 0; i < rateLimitPerMinute; i++ {
		q.admit(chatID, at)
	}
	return q
}

// ---- what a cancellation from request establishes ----
//
// The retry's closing switch weighs the first attempt's refusal against
// whatever the second attempt came back with, and for a cancellation the
// answer flips on one fact: had the second frame been written yet. These two
// pin that fact at the only place that holds it.

// Cancelled before the write: request has to say so, and nothing may reach the
// socket. This is what lets sendMsgFrame keep reporting the first refusal —
// with no second frame on the wire there is no second delivery to deny.
//
// REVERSE VERIFICATION: delete the ctx.Err() check at the top of request and
// this fails with
//
//	1 frame(s) reached the socket after a cancelled context, want 0
func TestARequestCancelledBeforeTheWriteReportsNothingWritten(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.request(ctx, cmdSendMsg, map[string]any{"chatid": "CHAT_1"})

	if got := conn.pushCount(); got != 0 {
		t.Fatalf("%d frame(s) reached the socket after a cancelled context, want 0", got)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want the caller's own cancellation", err)
	}
	if errors.Is(err, errAckAbandoned) {
		t.Fatalf("error = %v claims the frame went out, and nothing was written", err)
	}
}

// The context lost after the write, waiting for the verdict: request has to
// mark it, whichever way it was lost. The frame is gone — WriteMessage took
// the bytes — so the outcome is the same unknown errAckTimeout stands for, and
// a caller weighing it against anything else in hand needs to see that from
// the error itself.
//
// Both ways, because they are the two the reply path actually ends on: a
// cancelled delivery and a deadline that ran out, and the write is equally
// gone under either.
//
// REVERSE VERIFICATION: return a bare ctx.Err() from request's post-write
// select and this fails with
//
//	error = context canceled does not carry errAckAbandoned, so a caller cannot tell it from a cancellation that wrote nothing
func TestARequestThatLosesItsContextAfterTheWriteSaysTheFrameWentOut(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		// ctxFor builds the caller's context and, for the cancelled case, the
		// hook that ends it once the frame is on the wire. onPush runs after
		// WriteMessage has taken the bytes, which is what makes "after the
		// write" a fact here rather than a hope.
		ctxFor func(t *testing.T, conn *quotaConn) context.Context
		want   error
	}{
		{
			name: "the caller cancelled",
			ctxFor: func(t *testing.T, conn *quotaConn) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				conn.onPush = func(n int) {
					if n == 1 {
						cancel()
					}
				}
				return ctx
			},
			want: context.Canceled,
		},
		{
			name: "the delivery's deadline ran out",
			ctxFor: func(t *testing.T, conn *quotaConn) context.Context {
				// Well inside ackTimeout, so the deadline is what ends the
				// wait and not the ack timer.
				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
				t.Cleanup(cancel)
				return ctx
			},
			want: context.DeadlineExceeded,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, conn := quotaSender()
			conn.silentFrom = 1 // nothing answers, so the wait is the caller's to lose
			ctx := tc.ctxFor(t, conn)

			_, err := s.request(ctx, cmdSendMsg, map[string]any{"chatid": "CHAT_1"})

			if got := conn.pushCount(); got != 1 {
				t.Fatalf("%d frame(s) reached the socket, want 1 — this test's premise is that the write happened", got)
			}
			if !errors.Is(err, errAckAbandoned) {
				t.Fatalf("error = %v does not carry errAckAbandoned, so a caller cannot tell it from a cancellation that wrote nothing", err)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v no longer reads as %v; every existing errors.Is on this path stops matching", err, tc.want)
			}
			// And it keeps landing on the side of the classifiers that means
			// "the user may already have this", which is what the wrapping is
			// for: the mark is for the one caller that has something else to
			// weigh it against, not a new outcome for everyone else.
			if r := unconfirmedReason(err); r == "" {
				t.Fatalf("error = %v files as a definite outcome", err)
			}
			if provablyNotSent(err) {
				t.Fatalf("error = %v is reported as provably unsent, which hands the frame back for another offer and duplicates it", err)
			}
		})
	}
}

// ---- the retry ----

// A throttle is the platform saying "not now". Everything downstream reads a
// stated refusal as final — provablyNotSent releases nothing, classifyDrop
// files platform_refused — so without a retry here, one 45009 is one answer
// that exists in the Multica transcript and nowhere on the person's screen.
//
// REVERSE VERIFICATION: delete the retry loop from sendMsgFrame (call
// s.request once and return) and this fails on the returned error, with the
// conn showing a single push. `go build`, `go vet` and `go test -race` on the
// rest of the package stay silent — no other test in the tree sends a frame
// WeCom refuses once and accepts next.
func TestAThrottledPushIsRetriedInsteadOfLost(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender(errCodeAPIFreqLimit)

	if err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "the answer"); err != nil {
		t.Fatalf("a push WeCom throttled once was reported as a failed reply: %v", err)
	}
	if got := conn.pushCount(); got != 2 {
		t.Fatalf("%d push(es) reached the socket, want 2 — the refusal was not retried", got)
	}
}

// 45033 is the other throttle on this route: concurrency rather than
// frequency, same "come back in a moment" meaning. The connect path already
// treats the pair alike (wecom_channel.go, credential_probe.go); the send path
// has to agree, or which of the two WeCom picks decides whether the reply
// survives.
func TestAConcurrencyRefusalIsRetriedToo(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender(errCodeAPIConcurrencyLimit)

	if err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "the answer"); err != nil {
		t.Fatalf("a push WeCom refused for concurrency was reported as a failed reply: %v", err)
	}
	if got := conn.pushCount(); got != 2 {
		t.Fatalf("%d push(es) reached the socket, want 2", got)
	}
}

// The retry is one, not a loop. A throttle that has not lifted by the second
// attempt is a throttle we make worse by hammering — 45009's own documentation
// says the ban lasts as long as the window it was earned in.
func TestAThrottleThatDoesNotLiftIsReportedNotRetriedForever(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender(errCodeAPIFreqLimit, errCodeAPIFreqLimit, errCodeAPIFreqLimit)

	err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "the answer")
	var apiErr *wecomAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != errCodeAPIFreqLimit {
		t.Fatalf("error = %v, want the 45009 refusal reported as it stands", err)
	}
	if got := conn.pushCount(); got != 2 {
		t.Fatalf("%d push(es) reached the socket, want exactly 2 — one attempt and one retry", got)
	}
}

// The guard on the retry. 45002 is the frame being too long: identical bytes
// earn an identical refusal, so a second attempt spends another slot out of
// the same quota this file exists to protect and cannot possibly help.
func TestARefusalThatIsNotAThrottleIsNotRetried(t *testing.T) {
	t.Parallel()
	const errCodeMsgTooLong = 45002
	s, conn := quotaSender(errCodeMsgTooLong)

	if err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "the answer"); err == nil {
		t.Fatal("a frame WeCom refused outright was reported as delivered")
	}
	if got := conn.pushCount(); got != 1 {
		t.Fatalf("%d push(es) reached the socket, want 1 — a permanent refusal was retried", got)
	}
}

// The retry has to be affordable before it is attempted, because the frame it
// costs is not the expensive part — the REPORT is. Once the backoff is over,
// the second attempt spends the caller's context on a write and on the wait
// for a verdict, and a context error raised in there used to become the
// function's return value, replacing a refusal we were certain of with
// "interrupted". unconfirmedReason files that as an unknown outcome, which
// tells an operator the person may already have the message and somebody
// should go and check — for a frame WeCom had stated, in so many words, that
// it did not act on.
//
// Numbers from the reply path rather than invented: a throttle arriving with
// 300ms left of a delivery cannot fund a 100ms backoff plus the five seconds
// ackTimeout allows the verdict.
//
// REVERSE VERIFICATION: delete the retryUnaffordable check from sendMsgFrame
// and this fails with
//
//	2 push(es) reached the socket, want 1 — the retry ran on a budget that could not hold it
//
// and with sendMsgFrame back in its reviewed shape (one loop, the second
// pass's return value winning) with the reviewer's own probe output:
//
//	error = context deadline exceeded, want the 45009 refusal the server stated
func TestARetryTheCallerCannotAffordKeepsTheRefusal(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender(errCodeAPIFreqLimit)
	s.retryBackoff = 100 * time.Millisecond
	// Nothing answers a retry, which is the point: an attempt started without
	// the budget to see it through does not get a verdict of its own, it gets
	// the caller's deadline.
	conn.silentFrom = 2

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := s.sendTextCtx(ctx, "CHAT_1", chatTypeSingleInt, "the answer")

	var apiErr *wecomAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != errCodeAPIFreqLimit {
		t.Fatalf("error = %v, want the 45009 refusal the server stated", err)
	}
	if r := unconfirmedReason(err); r != "" {
		t.Errorf("the refusal was filed as an unknown outcome (%q); "+
			"an operator reads that as 'the user may already have it' and resends by hand", r)
	}
	if got := conn.pushCount(); got != 1 {
		t.Fatalf("%d push(es) reached the socket, want 1 — the retry ran on a budget that could not hold it", got)
	}
}

// The retry was affordable and was about to run, and the caller went away
// BEFORE the second frame could reach the socket. Nothing new can have been
// delivered, so the first refusal is still the whole story — and it is the
// better half of it: definite where a context error is ambiguous, and only one
// of the two sends a person to resend a message by hand.
//
// The cancellation is driven off the retry's own WARN line, for the reason on
// logHook: it is the one moment in this function that is provably after the
// refusal and provably before the write.
//
// REVERSE VERIFICATION: return ctx.Err() rather than refusal from the context
// arm of sendMsgFrame's backoff select and this fails with
//
//	error = context canceled, want the 45009 refusal the server stated
func TestASecondAttemptCutShortBeforeItWroteReportsTheFirstRefusal(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, conn, hook := hookedSender("push throttled, retrying once", cancel, errCodeAPIFreqLimit)
	// Long enough that the backoff cannot elapse on its own: the only way out
	// of it here is the cancellation, so a second push means the write went
	// ahead after the caller was gone.
	s.retryBackoff = time.Hour

	err := s.sendTextCtx(ctx, "CHAT_1", chatTypeSingleInt, "the answer")

	if !hook.fired.Load() {
		t.Fatal("the retry was never started — this test's premise is a retry cut short, not one skipped")
	}
	var apiErr *wecomAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != errCodeAPIFreqLimit {
		t.Fatalf("error = %v, want the 45009 refusal the server stated", err)
	}
	if r := unconfirmedReason(err); r != "" {
		t.Errorf("the refusal was filed as an unknown outcome (%q); nothing was written, so nothing is unknown", r)
	}
	if got := conn.pushCount(); got != 1 {
		t.Fatalf("%d push(es) reached the socket, want 1", got)
	}
}

// The same cancellation one step later, and the answer is the opposite. The
// retry ran, the second frame IS on the wire, and the caller went away while
// its verdict was outstanding.
//
// The first refusal is a fact about the FIRST attempt — WeCom stated it did not
// act on that frame — and it establishes nothing about the second one, which
// may be in front of the person right now. Reporting it here files
// platform_refused and unconfirmedReason "", which together tell an operator
// the reply was refused and never shown; the operator resends, and the person
// gets the answer twice. The unknown is the honest report: nobody is told a
// delivery failed that may have happened.
//
// REVERSE VERIFICATION: drop errAckAbandoned from the unknown arm of
// sendMsgFrame's closing switch, so the context arm catches it again, and this
// fails with
//
//	error = wecom: aibot_send_msg rejected errcode=45009 errmsg=scripted, want the second attempt's own outcome — the first refusal is a fact about the first frame, and the second one is already out
func TestASecondAttemptCutShortAfterItWroteKeepsTheUnknown(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender(errCodeAPIFreqLimit)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Nothing answers the retry, and the caller gives up once its frame is on
	// the wire: onPush runs after WriteMessage has taken the bytes.
	conn.silentFrom = 2
	conn.onPush = func(n int) {
		if n == 2 {
			cancel()
		}
	}

	err := s.sendTextCtx(ctx, "CHAT_1", chatTypeSingleInt, "the answer")

	if got := conn.pushCount(); got != 2 {
		t.Fatalf("%d push(es) reached the socket, want 2 — this test's premise is that the retry ran", got)
	}
	var apiErr *wecomAPIError
	if errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want the second attempt's own outcome — the first refusal is a fact about the first frame, and the second one is already out", err)
	}
	if r := unconfirmedReason(err); r == "" {
		t.Fatalf("error = %v is filed as a definite outcome; the frame went out and no verdict came back, so this reads as unknown", err)
	}
	if provablyNotSent(err) {
		t.Fatalf("error = %v is reported as provably unsent, which hands the frame back for another offer and duplicates it", err)
	}
}

// A throttle raised while OUR OWN window is spent is the quota's, not a stale
// count's, and WeCom holds a frequency block for the rest of the period that
// earned it (doc 90313). So there is nothing to come back to in two seconds,
// and the decision is taken from admit — which already knows when the next
// slot frees — rather than from a fixed sleep.
//
// The real sendRetryBackoff on purpose: the proof is that the call returns
// long before two seconds have passed.
//
// REVERSE VERIFICATION: delete the slot arm from retryUnaffordable and this
// fails with
//
//	the call took 2.976430042s — it served a backoff instead of asking admit when the next slot frees
func TestAThrottleIsNotRetriedWhenOurOwnWindowIsSpentToo(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender(errCodeAPIFreqLimit)
	s.retryBackoff = sendRetryBackoff
	// 29 spent, so this send is the minute's 30th: admitted, then refused by
	// the server, with the window full behind it.
	q := newSendQuota()
	at := time.Now()
	for i := 0; i < rateLimitPerMinute-1; i++ {
		q.admit("CHAT_1", at)
	}
	s.quota = q

	start := time.Now()
	err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "the answer")
	elapsed := time.Since(start)

	var apiErr *wecomAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != errCodeAPIFreqLimit {
		t.Fatalf("error = %v, want the 45009 refusal the server stated", err)
	}
	if got := conn.pushCount(); got != 1 {
		t.Fatalf("%d push(es) reached the socket, want 1 — a frame was spent on a block that lasts the minute out", got)
	}
	if elapsed > sendRetryBackoff/2 {
		t.Fatalf("the call took %s — it served a backoff instead of asking admit when the next slot frees", elapsed)
	}
}

// The delay is spread, because a concurrency refusal catches every caller that
// was in flight at once and a fixed delay sends all of them back at the same
// instant. 45033's documented remedy is the opposite of that: "出现这种限制错误
// 后，请企业调低并发数" (doc 90313).
//
// REVERSE VERIFICATION: return s.retryBackoff unchanged from retryDelay and
// this fails with
//
//	50 draws produced 1 distinct delay(s) — every caller one refusal caught comes back at the same instant
func TestTheRetryBackoffIsSpreadAcrossCallers(t *testing.T) {
	t.Parallel()
	s, _ := quotaSender()
	s.retryBackoff = sendRetryBackoff

	low, high := s.retryBackoff/2, s.retryBackoff+s.retryBackoff/2
	seen := make(map[time.Duration]bool)
	for i := 0; i < 50; i++ {
		d := s.retryDelay()
		if d < low || d >= high {
			t.Fatalf("retryDelay() = %s, want inside [%s, %s)", d, low, high)
		}
		seen[d] = true
	}
	if len(seen) < 2 {
		t.Fatalf("50 draws produced %d distinct delay(s) — every caller one refusal caught comes back at the same instant", len(seen))
	}
}

// ---- the gate ----

// The gate waits for a slot, but never so long that the write it is waiting
// for can no longer be seen through. The wait has to fit inside the caller's
// deadline MINUS ackTimeout, not merely inside the deadline.
//
// The numbers are the inbox push path's, unchanged: five seconds for the whole
// delivery (outbound.go:415), and a slot 2.5s away — comfortably inside
// rateWaitBudget, so a gate that only checked the deadline would wait for it
// and hand the write the 2.5s that were left, for a verdict ackTimeout allows
// five seconds to arrive. That write is cut off mid-flight and reported as an
// unknown outcome. Refusing before it is the honest failure: nothing on the
// wire, a definite error, and the caller's whole budget still there to record
// it with.
//
// REVERSE VERIFICATION: drop the writeBudget subtraction from reserve (check
// the deadline itself) and this fails after the full five seconds with
//
//	error = context deadline exceeded, want errRateLimited
func TestTheGateLeavesTheWriteItsAckBudget(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender()
	conn.ackDelay = 3 * time.Second // slower than what 2.5s of waiting leaves
	s.quota = spentMinute("CHAT_1", 2500*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	err := s.sendTextCtx(ctx, "CHAT_1", chatTypeSingleInt, "you have a new item")
	elapsed := time.Since(start)

	if !errors.Is(err, errRateLimited) {
		t.Fatalf("error = %v, want errRateLimited", err)
	}
	if r := unconfirmedReason(err); r != "" {
		t.Errorf("a frame the gate never wrote was filed as an unknown outcome (%q)", r)
	}
	if got := conn.pushCount(); got != 0 {
		t.Fatalf("%d push(es) reached the socket, want 0 — the gate spent the write's own budget waiting", got)
	}
	if elapsed > time.Second {
		t.Fatalf("the refusal took %s — the gate waited for a slot it could not afford to use", elapsed)
	}
}

// The gate's promise, at the published figures and nowhere near them: a chat
// that goes a LITTLE over 30 a minute has its next frame delayed into the slot
// that is about to free, and the frame goes out.
//
// At WeCom's real windows, which is the whole point of this one — the same
// claim at a 150ms window and a limit of 2 is true by construction, because a
// wait that short cannot outlast any budget worth naming.
//
// REVERSE VERIFICATION: drop the s.quota.reserve call from sendMsgFrame and
// this fails with
//
//	the send returned after 439.417µs — the frame was never held back
func TestABurstJustOverThePublishedRateIsDelayedNotLost(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender()
	s.quota = spentMinute("CHAT_1", 400*time.Millisecond)

	start := time.Now()
	err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "piece")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("a send 400ms short of a free slot failed instead of waiting for it: %v", err)
	}
	if elapsed < 300*time.Millisecond {
		t.Fatalf("the send returned after %s — the frame was never held back", elapsed)
	}
	if got := conn.pushCount(); got != 1 {
		t.Fatalf("%d push(es) reached the socket, want 1 — waiting must not cost a message", got)
	}
}

// And the limit of that promise, pinned rather than described. A burst of 31
// into one chat at the published figures does NOT arrive late: the 31st frame
// is refused where it stands, because the next slot is a whole window away and
// no caller on this path has a minute to give. Nothing is written, nothing
// tells the person, and the drop is filed under transport_error.
//
// transport_error rather than platform_refused is deliberate and is asserted
// here so it stays deliberate: WeCom refused nothing, this process did, and
// the reason text on that counter already covers a delivery whose own budget
// ran out before it got a turn on the wire. What must not change is the last
// assertion — errRateLimited stays provably unsent, which is what lets the
// relay offer the frame somewhere else instead of writing it off.
//
// REVERSE VERIFICATION: drop the s.quota.reserve call from sendMsgFrame and
// this fails with
//
//	error = <nil>, want errRateLimited on frame 31 of a burst
func TestTheThirtyFirstFrameOfABurstIsRefusedNotDelayed(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender()

	for i := 0; i < rateLimitPerMinute; i++ {
		if err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "piece"); err != nil {
			t.Fatalf("send %d of the minute's %d failed: %v", i+1, rateLimitPerMinute, err)
		}
	}

	start := time.Now()
	err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "the one past the ceiling")
	elapsed := time.Since(start)

	if !errors.Is(err, errRateLimited) {
		t.Fatalf("error = %v, want errRateLimited on frame %d of a burst", err, rateLimitPerMinute+1)
	}
	if !strings.Contains(err.Error(), "the next slot is") {
		t.Errorf("error = %q, want it to say how far off the next slot is — that distance is the whole reason it was refused", err)
	}
	if elapsed >= rateWaitBudget {
		t.Errorf("the refusal took %s — a wait of nearly a whole window was served instead of refused", elapsed)
	}
	if got := conn.pushCount(); got != rateLimitPerMinute {
		t.Fatalf("%d push(es) reached the socket, want %d — the refused frame was written anyway", got, rateLimitPerMinute)
	}
	if got := classifyDrop(err); got != dropTransport {
		t.Errorf("classifyDrop = %q, want %q — WeCom refused nothing here, this process did", got, dropTransport)
	}
	if r := unconfirmedReason(err); r != "" {
		t.Errorf("a frame the gate never wrote was filed as an unknown outcome (%q)", r)
	}
	if !provablyNotSent(err) {
		t.Error("a frame the gate never wrote was not reported as provably unsent, so the relay will not offer it anywhere else")
	}
}

// When the wait would outlast the caller, the gate says so BEFORE writing.
// This is the one outcome where a reply is knowingly given up on, and it has
// to be the honest kind: nothing on the wire, a definite error, and a reason
// an operator can read. errRateLimited is deliberately not a context error —
// unconfirmedReason would file that as "may already have arrived" and send
// somebody to resend a message the user never got.
func TestAWaitLongerThanTheCallerHasIsRefusedBeforeTheWrite(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender()
	s.quota = newSendQuotaWith(0, quotaWindow{span: time.Hour, limit: 1})

	if err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "first"); err != nil {
		t.Fatalf("the first send failed: %v", err)
	}
	err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "second")
	if !errors.Is(err, errRateLimited) {
		t.Fatalf("error = %v, want errRateLimited", err)
	}
	if unconfirmedReason(err) != "" {
		t.Errorf("a frame the gate never wrote was filed as an unknown outcome (%q); "+
			"an operator reads that as 'the user may already have it'", unconfirmedReason(err))
	}
	if !provablyNotSent(err) {
		t.Error("a frame the gate never wrote was not reported as provably unsent, so the relay will not offer it anywhere else")
	}
	if got := conn.pushCount(); got != 1 {
		t.Fatalf("%d push(es) reached the socket, want 1 — the refused frame was written anyway", got)
	}
}

// The quota WeCom publishes is per recipient, so one busy conversation must
// not silence every other person the bot is talking to. Keyed per chat is
// also what makes the in-process gate sound: it counts what the platform
// counts.
func TestOneBusyChatDoesNotThrottleTheOthers(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender()
	s.quota = newSendQuotaWith(0, quotaWindow{span: time.Hour, limit: 1})

	if err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "first"); err != nil {
		t.Fatalf("CHAT_1's first send failed: %v", err)
	}
	if err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "second"); !errors.Is(err, errRateLimited) {
		t.Fatalf("CHAT_1's second send: error = %v, want errRateLimited — this test's premise is gone", err)
	}
	if err := s.sendTextCtx(context.Background(), "CHAT_2", chatTypeSingleInt, "hello"); err != nil {
		t.Fatalf("CHAT_2 was refused because CHAT_1 spent its own quota: %v", err)
	}
	if got := conn.pushedTo("CHAT_2"); got != 1 {
		t.Fatalf("%d push(es) reached CHAT_2, want 1", got)
	}
}

// A file and a piece of an answer are the same command to WeCom and spend the
// same allowance. A gate on the text route alone would be no gate at all on a
// turn that answers in words and then sends attachments one frame each.
func TestAMediaPushSpendsTheSameQuotaAsAnAnswer(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender()
	s.quota = newSendQuotaWith(0, quotaWindow{span: time.Hour, limit: 1})

	if err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "here it is"); err != nil {
		t.Fatalf("the answer failed: %v", err)
	}
	err := s.sendMedia(context.Background(), "CHAT_1", chatTypeSingleInt, mediaSend{Kind: mediaTypeImage, MediaID: "MEDIA_1"})
	if !errors.Is(err, errRateLimited) {
		t.Fatalf("media push error = %v, want errRateLimited — the file route is not behind the gate", err)
	}
	if got := conn.pushCount(); got != 1 {
		t.Fatalf("%d push(es) reached the socket, want 1", got)
	}
}

// ---- the published numbers ----

// The figures themselves, pinned to the windows the gate runs on: 30 a minute
// and 1000 an hour, per recipient (WeCom doc 90454, read 2026-08-22). Nothing
// else in the tree checks them — the file that held them last was deleted
// whole, with its numbers, and nothing went red.
func TestTheGateRunsOnWeComsPublishedFigures(t *testing.T) {
	t.Parallel()
	q := newSendQuota()
	base := time.Now()

	for i := 0; i < rateLimitPerMinute; i++ {
		if wait := q.admit("CHAT_1", base); wait != 0 {
			t.Fatalf("send %d of %d was held back by %s; the minute's allowance is not being spent", i+1, rateLimitPerMinute, wait)
		}
	}
	if wait := q.admit("CHAT_1", base); wait == 0 {
		t.Fatalf("send %d in the same instant was admitted; the per-minute ceiling is %d", rateLimitPerMinute+1, rateLimitPerMinute)
	}
	// A minute on, the first send has aged out and its slot is free again.
	if wait := q.admit("CHAT_1", base.Add(time.Minute+time.Millisecond)); wait != 0 {
		t.Fatalf("a send a minute later was held back by %s; the window does not slide", wait)
	}

	// The hour is the second ceiling, and it binds even when no minute does:
	// spaced three seconds apart, nothing ever comes close to 30 a minute.
	spread := newSendQuota()
	for i := 0; i < rateLimitPerHour; i++ {
		at := base.Add(time.Duration(i) * 3 * time.Second)
		if wait := spread.admit("CHAT_1", at); wait != 0 {
			t.Fatalf("send %d of %d was held back by %s; the hour's allowance is not being spent", i+1, rateLimitPerHour, wait)
		}
	}
	last := base.Add(time.Duration(rateLimitPerHour) * 3 * time.Second)
	if wait := spread.admit("CHAT_1", last); wait == 0 {
		t.Fatalf("send %d was admitted; the per-hour ceiling is %d", rateLimitPerHour+1, rateLimitPerHour)
	}
}

// Chats that go quiet must not stay in the map for the life of the process: a
// bot talks to a new person every day, and each one would keep its timestamps
// alive forever.
func TestQuietChatsAreForgotten(t *testing.T) {
	t.Parallel()
	q := newSendQuota()
	base := time.Now()
	q.admit("CHAT_OLD", base)

	q.admit("CHAT_NEW", base.Add(2*time.Hour))

	q.mu.Lock()
	_, stillThere := q.sent["CHAT_OLD"]
	q.mu.Unlock()
	if stillThere {
		t.Error("a chat with nothing inside the longest window is still holding memory")
	}
}
