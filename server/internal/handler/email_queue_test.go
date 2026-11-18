package handler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// emailCall records one queued send: what it delivered and the context it ran
// under, which is how these tests check that a send outlived its request.
type emailCall struct {
	label string
	ctx   context.Context
}

// stubEmailSender parks every send until the test releases it. A handler that
// still waited on the relay would therefore fail these tests outright instead of
// merely slowing them down, which is the point.
type stubEmailSender struct {
	entered  chan emailCall
	finished chan emailCall
	release  chan struct{}

	err error

	mu    sync.Mutex
	calls []string
	once  sync.Once
}

func newStubEmailSender() *stubEmailSender {
	return &stubEmailSender{
		entered:  make(chan emailCall, emailQueueCapacity+emailSendConcurrency),
		finished: make(chan emailCall, emailQueueCapacity+emailSendConcurrency),
		release:  make(chan struct{}),
	}
}

func (s *stubEmailSender) send(ctx context.Context, label string) error {
	s.mu.Lock()
	s.calls = append(s.calls, label)
	s.mu.Unlock()

	s.entered <- emailCall{label: label, ctx: ctx}
	err := s.err
	select {
	case <-s.release:
	case <-ctx.Done():
		err = ctx.Err()
	}
	s.finished <- emailCall{label: label, ctx: ctx}
	return err
}

func (s *stubEmailSender) SendVerificationCode(ctx context.Context, to, code string) error {
	return s.send(ctx, "verification_code/"+to+"/"+code)
}

func (s *stubEmailSender) SendInvitationEmail(ctx context.Context, to, _, _, _ string) error {
	return s.send(ctx, "invitation/"+to)
}

func (s *stubEmailSender) waitEntered(t *testing.T) emailCall {
	t.Helper()
	select {
	case call := <-s.entered:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("no queued send started")
		return emailCall{}
	}
}

func (s *stubEmailSender) waitFinished(t *testing.T) emailCall {
	t.Helper()
	select {
	case call := <-s.finished:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("queued send never returned")
		return emailCall{}
	}
}

// releaseNow lets every parked send finish. Registered as a cleanup so a test
// that fails mid-flight cannot leave a worker blocked.
func (s *stubEmailSender) releaseNow() {
	s.once.Do(func() { close(s.release) })
}

func (s *stubEmailSender) callsSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// stubQueue returns a running queue whose sends park in the stub, plus its
// metrics and the cancel function for the worker context.
func stubQueue(t *testing.T, sender emailSender) (*EmailQueue, *obsmetrics.BusinessMetrics, context.CancelFunc) {
	t.Helper()
	m := obsmetrics.NewBusinessMetrics()
	q := NewEmailQueue(&Handler{Metrics: m})
	q.sender = sender
	ctx, cancel := context.WithCancel(context.Background())
	workersDone := make(chan struct{})
	go func() {
		defer close(workersDone)
		q.Run(ctx)
	}()
	t.Cleanup(func() {
		if s, ok := sender.(interface{ releaseNow() }); ok {
			s.releaseNow()
		}
		cancel()
		select {
		case <-workersDone:
		case <-time.After(emailSendTimeout + 5*time.Second):
			t.Error("EmailQueue.Run did not return after its context was cancelled")
		}
	})
	return q, m, cancel
}

// blockingRelaySender mimics a relay that takes the connection and then stops
// talking: the send returns only when the queue gives up on it.
type blockingRelaySender struct {
	started chan struct{}
	once    sync.Once
}

func (s *blockingRelaySender) SendVerificationCode(ctx context.Context, _, _ string) error {
	s.once.Do(func() { close(s.started) })
	<-ctx.Done()
	return ctx.Err()
}

func (s *blockingRelaySender) SendInvitationEmail(ctx context.Context, to, _, _, _ string) error {
	return s.SendVerificationCode(ctx, to, "")
}

// emailSendCount reads one multica_email_send_total label pair, which is how
// these tests distinguish a recorded failure from a recorded success. A family
// with no observations is not gathered at all, so absence reads as zero.
func emailSendCount(t *testing.T, m *obsmetrics.BusinessMetrics, kind, result string) float64 {
	t.Helper()
	family := obsmetrics.GatherForTest(t, m)["multica_email_send_total"]
	for _, metric := range family.GetMetric() {
		if labelValue(metric, "kind") == kind && labelValue(metric, "result") == result {
			return metric.GetCounter().GetValue()
		}
	}
	return 0
}

func labelValue(metric *dto.Metric, name string) string {
	for _, pair := range metric.GetLabel() {
		if pair.GetName() == name {
			return pair.GetValue()
		}
	}
	return ""
}

// TestEmailQueueSendOutlivesItsWorkerContext pins the trap of moving a send off
// the request: cancelling the trigger must not cancel the send, or every deploy —
// and every client that stops reading the response — discards a login code.
func TestEmailQueueSendOutlivesItsWorkerContext(t *testing.T) {
	sender := newStubEmailSender()
	q, _, cancel := stubQueue(t, sender)

	if err := q.SendVerificationCode("a@example.com", "123456", "req-1"); err != nil {
		t.Fatalf("queue: %v", err)
	}
	entered := sender.waitEntered(t)
	cancel()
	sender.releaseNow()
	done := sender.waitFinished(t)

	if entered.label != "verification_code/a@example.com/123456" {
		t.Fatalf("send payload = %q", entered.label)
	}
	if done.ctx.Err() != nil {
		t.Fatalf("send context died with its worker: %v", done.ctx.Err())
	}
	if _, ok := done.ctx.Deadline(); !ok {
		t.Fatal("send context has no deadline, so a wedged relay holds a worker forever")
	}
}

// TestEmailQueueGivesUpOnAStoppedRelay is the bounded-goroutine half of that
// deadline: a send that never returns must still end, and end as a failure.
func TestEmailQueueGivesUpOnAStoppedRelay(t *testing.T) {
	sender := &blockingRelaySender{started: make(chan struct{})}
	q, m, _ := stubQueue(t, sender)

	if err := q.SendVerificationCode("a@example.com", "123456", ""); err != nil {
		t.Fatalf("queue: %v", err)
	}
	<-sender.started
	start := time.Now()

	deadline := time.Now().Add(emailSendTimeout + 5*time.Second)
	for emailSendCount(t, m, "verification_code", "failure") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("a send that never returned was never recorded as a failure")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if elapsed := time.Since(start); elapsed < emailSendTimeout {
		t.Fatalf("gave up after %v, want the full %v budget", elapsed, emailSendTimeout)
	}
}

// TestEmailQueueRejectsWhenSaturated: a dropped slot has to reach the caller,
// because that is the only place it can still be reported.
func TestEmailQueueRejectsWhenSaturated(t *testing.T) {
	q := NewEmailQueue(&Handler{})
	q.sender = newStubEmailSender()
	// No Run: nothing drains the buffer, so its capacity is the limit.
	for i := 0; i < emailQueueCapacity; i++ {
		if err := q.SendVerificationCode("fill@example.com", "123456", ""); err != nil {
			t.Fatalf("send %d into an empty queue: %v", i, err)
		}
	}
	if err := q.SendVerificationCode("late@example.com", "123456", ""); !errors.Is(err, ErrEmailQueueFull) {
		t.Fatalf("saturated queue error = %v, want ErrEmailQueueFull", err)
	}
	if got := len(q.jobs); got != emailQueueCapacity {
		t.Fatalf("queue length = %d, want %d (a rejected job must not be half-accepted)", got, emailQueueCapacity)
	}
}

// TestEmailQueueReportsAFailedSend is the visible-failure half of the criteria:
// the client already has its 200, so the counter and the error log are all that
// remains of a code that never arrived.
func TestEmailQueueReportsAFailedSend(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	sender := newStubEmailSender()
	sender.err = errors.New("relay refused sender")
	q, m, _ := stubQueue(t, sender)

	if err := q.SendVerificationCode("a@example.com", "123456", "req-42"); err != nil {
		t.Fatalf("queue: %v", err)
	}
	sender.waitEntered(t)
	sender.releaseNow()
	sender.waitFinished(t)

	if got := emailSendCount(t, m, "verification_code", "failure"); got != 1 {
		t.Fatalf("failed sends = %v, want 1", got)
	}
	if got := emailSendCount(t, m, "verification_code", "success"); got != 0 {
		t.Fatalf("successful sends = %v, want 0", got)
	}
	for _, want := range []string{"email send failed", "verification_code", "a@example.com", "req-42", "relay refused sender"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("failure log missing %q; logs:\n%s", want, logs.String())
		}
	}
}

// TestSendCodeAnswersBeforeTheRelayIsCalled is the latency fix itself: the
// request must complete while the relay conversation is still running.
func TestSendCodeAnswersBeforeTheRelayIsCalled(t *testing.T) {
	const email = "queue-send-code@example.com"
	dbfx.Cleanup(t, `DELETE FROM verification_code WHERE email = $1`, email)

	sender := newStubEmailSender()
	q, m, _ := stubQueue(t, sender)
	h := *testHandler
	h.cfg = Config{AllowSignup: true}
	h.EmailQueue = q

	answered := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		answered <- sendCodeRequest(&h, email)
	}()

	var resp *httptest.ResponseRecorder
	select {
	case resp = <-answered:
	case <-time.After(5 * time.Second):
		// The stub only releases on cleanup, so a handler that waits on the
		// relay lands here instead of hanging the package.
		sender.releaseNow()
		t.Fatal("send-code did not answer while the relay call was still running")
	}
	if resp.Code != http.StatusOK {
		t.Fatalf("send-code status = %d, want 200 body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "Verification code sent") {
		t.Fatalf("response body = %s", resp.Body.String())
	}

	entered := sender.waitEntered(t)
	if prefix := "verification_code/" + email + "/"; !strings.HasPrefix(entered.label, prefix) {
		t.Fatalf("send payload = %q, want a verification code for %s", entered.label, email)
	}
	if code := storedVerificationCode(t, email); !strings.HasSuffix(entered.label, "/"+code) {
		t.Fatalf("queued code %q is not the stored code %q", entered.label, code)
	}
	sender.releaseNow()
	sender.waitFinished(t)

	if got := emailSendCount(t, m, "verification_code", "success"); got != 1 {
		t.Fatalf("successful sends = %v, want 1", got)
	}
	if got := len(sender.callsSnapshot()); got != 1 {
		t.Fatalf("relay calls = %d, want exactly 1 for one request", got)
	}
}

func sendCodeRequest(h *Handler, email string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.SendCode(rec, testutil.JSONRequest(http.MethodPost, "/auth/send-code", map[string]string{"email": email}))
	return rec
}

// TestSendCodeReportsAnUnavailableEmailQueue: saturation is the one email
// failure this endpoint can still report, and it must not answer 200 when
// nothing was handed to the relay.
func TestSendCodeReportsAnUnavailableEmailQueue(t *testing.T) {
	const email = "queue-saturated-send-code@example.com"
	dbfx.Cleanup(t, `DELETE FROM verification_code WHERE email = $1`, email)

	q := NewEmailQueue(&Handler{})
	q.sender = newStubEmailSender()
	h := *testHandler
	h.cfg = Config{AllowSignup: true}
	h.EmailQueue = q

	// No Run: the buffer fills, so the next request is the one that must fail.
	for i := 0; i < emailQueueCapacity; i++ {
		if err := q.SendVerificationCode("fill@example.com", "123456", ""); err != nil {
			t.Fatalf("send %d into an empty queue: %v", i, err)
		}
	}
	resp := testutil.Call(t, h.SendCode, testutil.JSONRequest(http.MethodPost, "/auth/send-code", map[string]string{"email": email})).Want(http.StatusServiceUnavailable)
	if !strings.Contains(resp.Body.String(), "try again") {
		t.Fatalf("response body = %s", resp.Body.String())
	}
}

func storedVerificationCode(t *testing.T, email string) string {
	t.Helper()
	var code string
	if err := testPool.QueryRow(context.Background(),
		`SELECT code FROM verification_code WHERE email = $1 ORDER BY created_at DESC LIMIT 1`, email,
	).Scan(&code); err != nil {
		t.Fatalf("read verification code for %s: %v", email, err)
	}
	return code
}
