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

func (f *fakeSender) SendNotification(to, subject, text, html string) error {
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
	workspaceSlug string
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
