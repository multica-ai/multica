package email

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/resend/resend-go/v2"
)

// fakeSender records every call for assertions. Optional blockDur lets a test
// simulate a slow SMTP / Resend backend without involving a real transport.
type fakeSender struct {
	mu       sync.Mutex
	calls    []sendCall
	err      error
	blockDur time.Duration
}
type sendCall struct{ To, Subject, Text, HTML string }

func (f *fakeSender) SendNotification(_ context.Context, to, subject, text, html string) error {
	if f.blockDur > 0 {
		time.Sleep(f.blockDur)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, sendCall{to, subject, text, html})
	return f.err
}
func (f *fakeSender) Calls() []sendCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sendCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// fakeQueries implements NotifierQueries with in-memory state.
type fakeQueries struct {
	emails        map[string]string            // uuidString → email
	prefs         map[string]map[string]string // uuidString → prefs map
	prefsRaw      []byte
	prefsErr      error
	workspaceSlug string
	nonMembers    map[string]bool // uuidString → removed from the workspace
	memberErr     error
}

func (f *fakeQueries) GetUserEmail(ctx context.Context, userID pgtype.UUID) (string, error) {
	k := uuidString(userID)
	if e, ok := f.emails[k]; ok {
		return e, nil
	}
	return "", errors.New("user not found")
}
func (f *fakeQueries) GetNotificationPreference(ctx context.Context,
	arg GetNotificationPreferenceParams) ([]byte, error) {
	if f.prefsErr != nil {
		return nil, f.prefsErr
	}
	if f.prefsRaw != nil {
		return f.prefsRaw, nil
	}
	k := uuidString(arg.UserID)
	if p, ok := f.prefs[k]; ok {
		b, _ := json.Marshal(p)
		return b, nil
	}
	return []byte(`{}`), nil
}
func (f *fakeQueries) GetWorkspaceSlug(ctx context.Context, wsID pgtype.UUID) (string, error) {
	return f.workspaceSlug, nil
}
func (f *fakeQueries) IsWorkspaceMember(ctx context.Context, wsID, userID pgtype.UUID) (bool, error) {
	if f.memberErr != nil {
		return false, f.memberErr
	}
	return !f.nonMembers[uuidString(userID)], nil
}

func mustUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		panic(err)
	}
	return u
}

func uuidString(u pgtype.UUID) string {
	b, _ := u.MarshalJSON()
	return string(b)
}

// strPtr returns a pointer to s. Used to construct payload shapes that match
// production — inboxItemToResponse writes issue_id via util.UUIDToPtr, so the
// payload value at runtime is *string, not string.
func strPtr(s string) *string { return &s }

func TestNotifier_SendsEmailOnInboxNewForMember(t *testing.T) {
	bus := events.New()
	sender := &fakeSender{}
	q := &fakeQueries{
		emails: map[string]string{
			`"11111111-1111-1111-1111-111111111111"`: "alice@example.com",
		},
		workspaceSlug: "acme",
	}
	n := NewNotifier(q, sender, NotifierConfig{
		Renderer: NewRenderer("https://app.example.com"),
	})
	n.Register(bus)

	bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: "22222222-2222-2222-2222-222222222222",
		Payload: map[string]any{
			"item": map[string]any{
				"recipient_type": "member",
				"recipient_id":   "11111111-1111-1111-1111-111111111111",
				"type":           "mentioned",
				"title":          "Bug: panic on startup",
				// Production shape: util.UUIDToPtr returns *string.
				"issue_id": strPtr("33333333-3333-3333-3333-333333333333"),
			},
		},
	})
	n.WaitInflight()

	calls := sender.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 send, got %d", len(calls))
	}
	if calls[0].To != "alice@example.com" {
		t.Errorf("To = %q, want alice@example.com", calls[0].To)
	}
	if !contains(calls[0].Subject, "Bug: panic on startup") {
		t.Errorf("Subject missing issue title: %q", calls[0].Subject)
	}
	// P2 regression: the deep link must render. The earlier bug was a string
	// type-assertion on *string, which silently produced "" → no link.
	wantLink := "https://app.example.com/acme/issues/33333333-3333-3333-3333-333333333333"
	if !contains(calls[0].HTML, wantLink) {
		t.Errorf("HTML missing deep link %q; got %q", wantLink, calls[0].HTML)
	}
}

func TestNotifier_SkipsWhenEmailMuted(t *testing.T) {
	bus := events.New()
	sender := &fakeSender{}
	q := &fakeQueries{
		emails: map[string]string{
			`"11111111-1111-1111-1111-111111111111"`: "alice@example.com",
		},
		prefs: map[string]map[string]string{
			`"11111111-1111-1111-1111-111111111111"`: {"email_notifications": "muted"},
		},
		workspaceSlug: "acme",
	}
	n := NewNotifier(q, sender, NotifierConfig{Renderer: NewRenderer("")})
	n.Register(bus)

	bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: "22222222-2222-2222-2222-222222222222",
		Payload: map[string]any{
			"item": map[string]any{
				"recipient_type": "member",
				"recipient_id":   "11111111-1111-1111-1111-111111111111",
				"type":           "mentioned",
				"title":          "X",
				"issue_id":       strPtr("33333333-3333-3333-3333-333333333333"),
			},
		},
	})
	n.WaitInflight()

	if got := len(sender.Calls()); got != 0 {
		t.Errorf("expected 0 sends (muted), got %d", got)
	}
}

func TestNotifier_SkipsWhenPreferenceLookupFails(t *testing.T) {
	bus := events.New()
	sender := &fakeSender{}
	q := &fakeQueries{
		emails: map[string]string{
			`"11111111-1111-1111-1111-111111111111"`: "alice@example.com",
		},
		prefsErr: errors.New("database unavailable"),
	}
	n := NewNotifier(q, sender, NotifierConfig{Renderer: NewRenderer("")})
	n.Register(bus)

	publishMemberInboxEvent(bus)
	n.WaitInflight()

	if got := len(sender.Calls()); got != 0 {
		t.Errorf("expected 0 sends when preferences cannot be read, got %d", got)
	}
}

func TestNotifier_SkipsWhenPreferencesAreMalformed(t *testing.T) {
	bus := events.New()
	sender := &fakeSender{}
	q := &fakeQueries{
		emails: map[string]string{
			`"11111111-1111-1111-1111-111111111111"`: "alice@example.com",
		},
		prefsRaw: []byte(`{"email_notifications":`),
	}
	n := NewNotifier(q, sender, NotifierConfig{Renderer: NewRenderer("")})
	n.Register(bus)

	publishMemberInboxEvent(bus)
	n.WaitInflight()

	if got := len(sender.Calls()); got != 0 {
		t.Errorf("expected 0 sends for malformed preferences, got %d", got)
	}
}

func publishMemberInboxEvent(bus *events.Bus) {
	bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: "22222222-2222-2222-2222-222222222222",
		Payload: map[string]any{
			"item": map[string]any{
				"recipient_type": "member",
				"recipient_id":   "11111111-1111-1111-1111-111111111111",
				"type":           "mentioned",
				"title":          "X",
				"issue_id":       strPtr("33333333-3333-3333-3333-333333333333"),
			},
		},
	})
}

func TestNotifier_SkipsNonMemberRecipients(t *testing.T) {
	bus := events.New()
	sender := &fakeSender{}
	q := &fakeQueries{workspaceSlug: "acme"}
	n := NewNotifier(q, sender, NotifierConfig{Renderer: NewRenderer("")})
	n.Register(bus)

	bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: "22222222-2222-2222-2222-222222222222",
		Payload: map[string]any{
			"item": map[string]any{
				"recipient_type": "agent",
				"recipient_id":   "11111111-1111-1111-1111-111111111111",
				"type":           "task_completed",
				"title":          "X",
				"issue_id":       strPtr("33333333-3333-3333-3333-333333333333"),
			},
		},
	})
	n.WaitInflight()

	if got := len(sender.Calls()); got != 0 {
		t.Errorf("expected 0 sends for agent recipient, got %d", got)
	}
}

func TestNotifier_SendFailureDoesNotPanic(t *testing.T) {
	bus := events.New()
	sender := &fakeSender{err: errors.New("smtp down")}
	q := &fakeQueries{
		emails: map[string]string{
			`"11111111-1111-1111-1111-111111111111"`: "alice@example.com",
		},
		workspaceSlug: "acme",
	}
	n := NewNotifier(q, sender, NotifierConfig{Renderer: NewRenderer("")})
	n.Register(bus)

	done := make(chan struct{})
	go func() {
		defer close(done)
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: "22222222-2222-2222-2222-222222222222",
			Payload: map[string]any{
				"item": map[string]any{
					"recipient_type": "member",
					"recipient_id":   "11111111-1111-1111-1111-111111111111",
					"type":           "mentioned",
					"title":          "X",
					"issue_id":       strPtr("33333333-3333-3333-3333-333333333333"),
				},
			},
		})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish wedged after send error")
	}
	n.WaitInflight()
	if got := len(sender.Calls()); got != 1 {
		t.Errorf("expected 1 send attempt, got %d", got)
	}
}

// TestNotifier_DoesNotBlockBusPublisher is the regression test for the P1 bug
// codex review caught: events.Bus.Publish is synchronous, so a slow SMTP send
// inside the handler would have hung the HTTP request that triggered the inbox
// row. With the fix (goroutine inside Register's callback), Publish must return
// in milliseconds even when the sender blocks for seconds.
func TestNotifier_DoesNotBlockBusPublisher(t *testing.T) {
	bus := events.New()
	const slowSend = 2 * time.Second
	sender := &fakeSender{blockDur: slowSend}
	q := &fakeQueries{
		emails: map[string]string{
			`"11111111-1111-1111-1111-111111111111"`: "alice@example.com",
		},
		workspaceSlug: "acme",
	}
	n := NewNotifier(q, sender, NotifierConfig{
		Renderer: NewRenderer(""),
		Timeout:  10 * time.Second,
	})
	n.Register(bus)

	start := time.Now()
	bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: "22222222-2222-2222-2222-222222222222",
		Payload: map[string]any{
			"item": map[string]any{
				"recipient_type": "member",
				"recipient_id":   "11111111-1111-1111-1111-111111111111",
				"type":           "mentioned",
				"title":          "X",
				"issue_id":       strPtr("33333333-3333-3333-3333-333333333333"),
			},
		},
	})
	publishElapsed := time.Since(start)

	// Generous threshold (500ms) — the only synchronous work in Publish is the
	// goroutine spawn. Anything close to slowSend would mean the fix regressed.
	if publishElapsed > 500*time.Millisecond {
		t.Errorf("Publish took %v while sender was sleeping %v; expected goroutine to decouple them",
			publishElapsed, slowSend)
	}

	// Confirm the send eventually completes — proves the goroutine actually ran.
	n.WaitInflight()
	if got := len(sender.Calls()); got != 1 {
		t.Errorf("expected 1 send after WaitInflight, got %d", got)
	}
}

func contains(s, sub string) bool {
	return strings_Contains(s, sub)
}

func strings_Contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return len(sub) == 0
}

// scriptedSender returns the scripted errors in order (nil once exhausted)
// and records how many sends were in flight at the same time.
type scriptedSender struct {
	mu          sync.Mutex
	errs        []error
	calls       int
	inFlight    int
	maxInFlight int
	delay       time.Duration
}

func (s *scriptedSender) SendNotification(_ context.Context, to, subject, text, html string) error {
	s.mu.Lock()
	s.calls++
	s.inFlight++
	if s.inFlight > s.maxInFlight {
		s.maxInFlight = s.inFlight
	}
	var err error
	if len(s.errs) > 0 {
		err, s.errs = s.errs[0], s.errs[1:]
	}
	s.mu.Unlock()

	time.Sleep(s.delay)

	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()
	return err
}

func (s *scriptedSender) stats() (calls, maxInFlight int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.maxInFlight
}

func newDeliveryTestNotifier(sender EmailSender, cfg NotifierConfig) (*Notifier, *events.Bus) {
	bus := events.New()
	q := &fakeQueries{
		emails: map[string]string{
			`"11111111-1111-1111-1111-111111111111"`: "alice@example.com",
		},
	}
	cfg.Renderer = NewRenderer("")
	cfg.RetryBackoff = time.Millisecond
	if cfg.SendInterval == 0 {
		cfg.SendInterval = time.Nanosecond
	}
	n := NewNotifier(q, sender, cfg)
	n.Register(bus)
	return n, bus
}

func TestNotifier_RetriesRateLimitedSend(t *testing.T) {
	rateLimited := &resend.RateLimitError{Message: "too many requests"}
	sender := &scriptedSender{errs: []error{rateLimited, rateLimited}}
	n, bus := newDeliveryTestNotifier(sender, NotifierConfig{})

	publishMemberInboxEvent(bus)
	n.WaitInflight()

	if calls, _ := sender.stats(); calls != 3 {
		t.Fatalf("expected 2 rate-limited attempts then a success (3 calls), got %d", calls)
	}
}

func TestNotifier_GivesUpOnPersistentRateLimit(t *testing.T) {
	rateLimited := &resend.RateLimitError{Message: "too many requests"}
	sender := &scriptedSender{errs: []error{rateLimited, rateLimited, rateLimited, rateLimited}}
	n, bus := newDeliveryTestNotifier(sender, NotifierConfig{MaxAttempts: 3})

	publishMemberInboxEvent(bus)
	n.WaitInflight()

	if calls, _ := sender.stats(); calls != 3 {
		t.Fatalf("expected MaxAttempts=3 sends, got %d", calls)
	}
}

func TestNotifier_DoesNotRetryOtherSendErrors(t *testing.T) {
	sender := &scriptedSender{errs: []error{errors.New("smtp: 554 rejected")}}
	n, bus := newDeliveryTestNotifier(sender, NotifierConfig{})

	publishMemberInboxEvent(bus)
	n.WaitInflight()

	if calls, _ := sender.stats(); calls != 1 {
		t.Fatalf("a non-rate-limit error must not be retried (duplicate risk), got %d calls", calls)
	}
}

func TestNotifier_BoundsConcurrentDeliveries(t *testing.T) {
	sender := &scriptedSender{delay: 20 * time.Millisecond}
	n, bus := newDeliveryTestNotifier(sender, NotifierConfig{MaxConcurrent: 2})

	for i := 0; i < 6; i++ {
		publishMemberInboxEvent(bus)
	}
	n.WaitInflight()

	calls, maxInFlight := sender.stats()
	if calls != 6 {
		t.Fatalf("expected 6 sends, got %d", calls)
	}
	if maxInFlight > 2 {
		t.Fatalf("expected at most 2 concurrent sends, got %d", maxInFlight)
	}
}

func TestRetryWait_HonoursRetryAfterWithCap(t *testing.T) {
	fallback := 250 * time.Millisecond
	cases := []struct {
		name string
		err  error
		want time.Duration
	}{
		{"retry-after seconds", &resend.RateLimitError{RetryAfter: "2"}, 2 * time.Second},
		{"retry-after capped", &resend.RateLimitError{RetryAfter: "600"}, maxRetryWait},
		{"unparseable retry-after", &resend.RateLimitError{RetryAfter: "soon"}, fallback},
		{"no retry-after", &resend.RateLimitError{}, fallback},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryWait(tc.err, fallback); got != tc.want {
				t.Fatalf("retryWait() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNotifier_DropsWhenQueueFull(t *testing.T) {
	release := make(chan struct{})
	sender := &blockingSender{release: release, started: make(chan struct{}, 8)}
	n, bus := newDeliveryTestNotifier(sender, NotifierConfig{MaxConcurrent: 1, QueueSize: 1})

	publishMemberInboxEvent(bus) // taken by the only worker, which blocks
	<-sender.started
	publishMemberInboxEvent(bus) // fills the queue
	publishMemberInboxEvent(bus) // queue full: dropped, and Publish must not block
	close(release)
	n.WaitInflight()

	if got := sender.count(); got != 2 {
		t.Fatalf("expected 2 sends (1 running + 1 queued, 1 dropped), got %d", got)
	}
}

func TestNotifier_PacesSendsAcrossWorkers(t *testing.T) {
	sender := &scriptedSender{}
	n, bus := newDeliveryTestNotifier(sender, NotifierConfig{
		MaxConcurrent: 2,
		SendInterval:  30 * time.Millisecond,
	})

	start := time.Now()
	for i := 0; i < 4; i++ {
		publishMemberInboxEvent(bus)
	}
	n.WaitInflight()

	if elapsed := time.Since(start); elapsed < 90*time.Millisecond {
		t.Fatalf("4 sends at a 30ms interval finished in %v; want >= 90ms", elapsed)
	}
}

// blockingSender blocks every send until release is closed.
type blockingSender struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
}

func (b *blockingSender) SendNotification(_ context.Context, to, subject, text, html string) error {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	b.started <- struct{}{}
	<-b.release
	return nil
}

func (b *blockingSender) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func TestNotifier_TimeoutBoundsRateLimitRetries(t *testing.T) {
	rateLimited := &resend.RateLimitError{Message: "too many requests", RetryAfter: "10"}
	sender := &scriptedSender{errs: []error{rateLimited, rateLimited, rateLimited, rateLimited}}
	n, bus := newDeliveryTestNotifier(sender, NotifierConfig{Timeout: 50 * time.Millisecond})

	start := time.Now()
	publishMemberInboxEvent(bus)
	n.WaitInflight()

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("delivery ran %v past its 50ms timeout waiting on Retry-After", elapsed)
	}
	if calls, _ := sender.stats(); calls != 1 {
		t.Fatalf("expected the timeout to stop retries after 1 send, got %d", calls)
	}
}

func TestNotifier_SkipsRecipientsOutsideTheWorkspace(t *testing.T) {
	const alice = `"11111111-1111-1111-1111-111111111111"`
	cases := []struct {
		name string
		q    *fakeQueries
	}{
		{
			name: "removed member",
			q: &fakeQueries{
				emails:     map[string]string{alice: "alice@example.com"},
				nonMembers: map[string]bool{alice: true},
			},
		},
		{
			name: "membership lookup fails closed",
			q: &fakeQueries{
				emails:    map[string]string{alice: "alice@example.com"},
				memberErr: errors.New("db unavailable"),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := events.New()
			sender := &fakeSender{}
			n := NewNotifier(tc.q, sender, NotifierConfig{
				Renderer: NewRenderer("https://app.example.com"),
			})
			n.Register(bus)

			bus.Publish(events.Event{
				Type:        protocol.EventInboxNew,
				WorkspaceID: "22222222-2222-2222-2222-222222222222",
				Payload: map[string]any{
					"item": map[string]any{
						"recipient_type": "member",
						"recipient_id":   "11111111-1111-1111-1111-111111111111",
						"type":           "reaction_added",
						"title":          "Secret roadmap",
					},
				},
			})
			n.WaitInflight()

			if calls := sender.Calls(); len(calls) != 0 {
				t.Fatalf("expected no email, got %d", len(calls))
			}
		})
	}
}
