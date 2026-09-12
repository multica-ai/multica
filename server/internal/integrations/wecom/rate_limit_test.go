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
	"sync"
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
}

func (c *quotaConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	c.mu.Lock()
	code := 0
	if env.Cmd == cmdSendMsg {
		var body map[string]any
		_ = json.Unmarshal(env.Body, &body)
		c.pushes = append(c.pushes, body)
		if c.writes < len(c.codes) {
			code = c.codes[c.writes]
		}
		c.writes++
	}
	s := c.sender
	c.mu.Unlock()
	if s != nil {
		s.routeResponse(frameEnvelope{
			Headers: frameHeaders{ReqID: env.Headers.ReqID},
			ErrCode: code,
			ErrMsg:  "scripted",
		})
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

// ---- the gate ----

// The gate is what keeps us from reaching a throttle at all, and its promise
// is that a burst arrives LATE rather than not at all. A reply that waits two
// seconds is a reply; a reply refused for quota is a silence.
//
// REVERSE VERIFICATION: drop the s.quota.reserve call from sendMsgFrame and
// this fails on the elapsed time (the third push goes out immediately), with
// the build, vet and the rest of the suite silent.
func TestABurstOverTheQuotaWaitsInsteadOfBeingLost(t *testing.T) {
	t.Parallel()
	s, conn := quotaSender()
	// Two per 150ms, so the third send has to wait out the first one's window
	// — the same arithmetic as 30 a minute, at a length a test can stand.
	s.quota = newSendQuotaWith(time.Second, quotaWindow{span: 150 * time.Millisecond, limit: 2})

	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := s.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "piece"); err != nil {
			t.Fatalf("send %d failed: %v", i+1, err)
		}
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("three sends against a two-per-150ms quota took %s — the third was never held back", elapsed)
	}
	if got := conn.pushCount(); got != 3 {
		t.Fatalf("%d push(es) reached the socket, want 3 — waiting must not cost a message", got)
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
