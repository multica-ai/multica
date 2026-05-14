package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeChannel records every Send call and returns whatever the
// returns chan supplies. Closes its outbox via mutex so concurrent
// tests stay safe.
type fakeChannel struct {
	name    string
	mu      sync.Mutex
	calls   int
	returns []error // pop one per call; nil if exhausted
}

func (f *fakeChannel) Name() string { return f.name }
func (f *fakeChannel) Send(_ context.Context, _ Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.returns) > 0 {
		err := f.returns[0]
		f.returns = f.returns[1:]
		return err
	}
	return nil
}
func (f *fakeChannel) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeCommentPoster struct {
	mu      sync.Mutex
	calls   int
	lastID  string
	lastMsg string
	err     error
}

func (f *fakeCommentPoster) PostComment(_ context.Context, issueID, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastID = issueID
	f.lastMsg = body
	return f.err
}

// zeroDelay disables retry sleep so unit tests are instant.
var zeroDelay = []time.Duration{0, 0, 0}

func TestBridge_SendOnFirstAttempt(t *testing.T) {
	ch := &fakeChannel{name: "slack"} // default nil err on first call
	poster := &fakeCommentPoster{}
	b := New([]Channel{ch}, poster, zeroDelay, nil)

	b.Send(context.Background(), Event{Type: EventPlanCompleted, IssueID: "i1"})

	if ch.Calls() != 1 {
		t.Fatalf("expected 1 call, got %d", ch.Calls())
	}
	if poster.calls != 0 {
		t.Fatalf("expected no fallback comment, got %d", poster.calls)
	}
}

func TestBridge_RetriesThenSucceeds(t *testing.T) {
	ch := &fakeChannel{
		name:    "slack",
		returns: []error{errors.New("transient 1"), errors.New("transient 2"), nil},
	}
	poster := &fakeCommentPoster{}
	b := New([]Channel{ch}, poster, zeroDelay, nil)

	b.Send(context.Background(), Event{Type: EventLoopGuardTripped, IssueID: "i1"})

	if ch.Calls() != 3 {
		t.Fatalf("expected 3 calls (2 failures + 1 success), got %d", ch.Calls())
	}
	if poster.calls != 0 {
		t.Fatalf("expected no fallback after recovery, got %d", poster.calls)
	}
}

func TestBridge_FallbackAfterExhaustion(t *testing.T) {
	// 4 failures (1 initial + 3 retries) → fallback fires.
	ch := &fakeChannel{
		name:    "slack",
		returns: []error{errors.New("e1"), errors.New("e2"), errors.New("e3"), errors.New("e4")},
	}
	poster := &fakeCommentPoster{}
	b := New([]Channel{ch}, poster, zeroDelay, nil)

	b.Send(context.Background(), Event{
		Type:     EventLoopGuardTripped,
		IssueID:  "issue-1",
		IssuePUL: "PUL-102",
		PRURL:    "https://github.com/owner/repo/pull/42",
	})

	if ch.Calls() != 4 {
		t.Fatalf("expected 4 attempts (1+3), got %d", ch.Calls())
	}
	if poster.calls != 1 {
		t.Fatalf("expected 1 fallback comment, got %d", poster.calls)
	}
	if poster.lastID != "issue-1" {
		t.Errorf("fallback to wrong issue id: %q", poster.lastID)
	}
	if !strings.Contains(poster.lastMsg, "loop_guard_tripped") {
		t.Errorf("fallback comment missing event type:\n%s", poster.lastMsg)
	}
	if !strings.Contains(poster.lastMsg, "PUL-102") {
		t.Errorf("fallback comment missing PUL identifier:\n%s", poster.lastMsg)
	}
	if !strings.Contains(poster.lastMsg, "github.com/owner/repo/pull/42") {
		t.Errorf("fallback comment missing PR URL:\n%s", poster.lastMsg)
	}
}

func TestBridge_PerChannelRetryIsolation(t *testing.T) {
	// Slack succeeds first try; Telegram fails forever. Slack's
	// retry budget must not be consumed by Telegram's failures.
	slack := &fakeChannel{name: "slack"}
	tg := &fakeChannel{
		name:    "telegram",
		returns: []error{errors.New("e1"), errors.New("e2"), errors.New("e3"), errors.New("e4")},
	}
	poster := &fakeCommentPoster{}
	b := New([]Channel{slack, tg}, poster, zeroDelay, nil)

	b.Send(context.Background(), Event{Type: EventStuck24h, IssueID: "i"})

	if slack.Calls() != 1 {
		t.Errorf("slack should have succeeded on first attempt: %d calls", slack.Calls())
	}
	if tg.Calls() != 4 {
		t.Errorf("telegram should have exhausted retries: %d calls", tg.Calls())
	}
	if poster.calls != 1 {
		t.Errorf("expected exactly one fallback for telegram, got %d", poster.calls)
	}
}

func TestBridge_NoChannelsConfiguredGoesToFallback(t *testing.T) {
	poster := &fakeCommentPoster{}
	b := New(nil, poster, zeroDelay, nil)

	b.Send(context.Background(), Event{Type: EventPlanCompleted, IssueID: "i"})

	if poster.calls != 1 {
		t.Fatalf("expected one fallback comment when no channels configured, got %d", poster.calls)
	}
	if !strings.Contains(poster.lastMsg, "no channels configured") {
		t.Errorf("expected fallback reason to mention missing config:\n%s", poster.lastMsg)
	}
}

func TestBridge_FallbackPosterErrorIsSwallowed(t *testing.T) {
	ch := &fakeChannel{
		name:    "slack",
		returns: []error{errors.New("e"), errors.New("e"), errors.New("e"), errors.New("e")},
	}
	poster := &fakeCommentPoster{err: errors.New("multica is down too")}
	b := New([]Channel{ch}, poster, zeroDelay, nil)

	// Must not panic / hang even when fallback also fails.
	b.Send(context.Background(), Event{Type: EventStuck24h, IssueID: "i"})

	if poster.calls != 1 {
		t.Fatalf("expected fallback attempt regardless of error, got %d", poster.calls)
	}
}

func TestBridge_ContextCancellationAbortsRetry(t *testing.T) {
	// Non-zero delays let context cancellation interrupt the sleep.
	ch := &fakeChannel{
		name:    "slack",
		returns: []error{errors.New("e1"), errors.New("e2")},
	}
	poster := &fakeCommentPoster{}
	b := New([]Channel{ch}, poster, []time.Duration{100 * time.Millisecond, 100 * time.Millisecond, 100 * time.Millisecond}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Cancel after the first attempt and before the second.
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	b.Send(ctx, Event{Type: EventStuck24h, IssueID: "i"})
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Fatalf("context cancellation did not abort retry loop: %v", elapsed)
	}
}

func TestDefaultRetryDelays_PlanCadence(t *testing.T) {
	// Plan calls for 1m → 5m → 15m. Pin so a refactor doesn't drift.
	want := []time.Duration{1 * time.Minute, 5 * time.Minute, 15 * time.Minute}
	if len(DefaultRetryDelays) != len(want) {
		t.Fatalf("DefaultRetryDelays length = %d, want %d", len(DefaultRetryDelays), len(want))
	}
	for i, d := range want {
		if DefaultRetryDelays[i] != d {
			t.Errorf("DefaultRetryDelays[%d] = %v, want %v", i, DefaultRetryDelays[i], d)
		}
	}
}

func TestBuildFallbackComment_Shape(t *testing.T) {
	got := buildFallbackComment(Event{
		Type:         EventRebaseConflict,
		IssuePUL:     "PUL-102",
		PRURL:        "https://github.com/x/y/pull/1",
		HumanContext: "conflict on commits.go",
	}, "channel slack exhausted retries: net/http: timeout")

	for _, want := range []string{
		"rebase_conflict",
		"PUL-102",
		"conflict on commits.go",
		"github.com/x/y/pull/1",
		fmt.Sprintf("channel slack exhausted retries"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("fallback comment missing %q\n--- full ---\n%s", want, got)
		}
	}
}
