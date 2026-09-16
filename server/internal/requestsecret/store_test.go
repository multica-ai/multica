package requestsecret

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func newTestStore() (*Store, *time.Time) {
	clock := time.Unix(1_700_000_000, 0)
	s := New(time.Minute)
	s.SetClock(func() time.Time { return clock })
	return s, &clock
}

func TestIssueReturnsOpaqueHandleNotSecret(t *testing.T) {
	s, _ := newTestStore()
	secret := "super-secret-jwt-value"
	h, err := s.Issue("user-1", "task-1", secret)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if h == "" {
		t.Fatal("expected non-empty handle")
	}
	if strings.Contains(h, secret) {
		t.Fatal("handle must not contain the secret")
	}
	if len(h) != 64 {
		t.Fatalf("expected 64-char hex handle, got %d", len(h))
	}
}

func TestConsumeHappyPath(t *testing.T) {
	s, _ := newTestStore()
	h, _ := s.Issue("user-1", "task-1", "jwt-1")
	got, err := s.Consume(h, "user-1", "task-1")
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got != "jwt-1" {
		t.Fatalf("expected jwt-1, got %q", got)
	}
}

func TestReplayRejected(t *testing.T) {
	s, _ := newTestStore()
	h, _ := s.Issue("user-1", "task-1", "jwt-1")
	if _, err := s.Consume(h, "user-1", "task-1"); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if _, err := s.Consume(h, "user-1", "task-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not-found on replay, got %v", err)
	}
	if s.Len() != 0 {
		t.Fatalf("expected entry removed after consume, have %d", s.Len())
	}
}

func TestCrossUserRejected(t *testing.T) {
	s, _ := newTestStore()
	h, _ := s.Issue("user-1", "task-1", "jwt-1")
	if _, err := s.Consume(h, "user-2", "task-1"); !errors.Is(err, ErrPrincipal) {
		t.Fatalf("expected principal mismatch, got %v", err)
	}
	got, err := s.Consume(h, "user-1", "task-1")
	if err != nil || got != "jwt-1" {
		t.Fatalf("legitimate owner must still consume after cross-user probe; got %q err %v", got, err)
	}
}

func TestCrossTaskRejected(t *testing.T) {
	s, _ := newTestStore()
	h, _ := s.Issue("user-1", "task-1", "jwt-1")
	if _, err := s.Consume(h, "user-1", "task-2"); !errors.Is(err, ErrTask) {
		t.Fatalf("expected task mismatch, got %v", err)
	}
}

func TestExpiryCleanup(t *testing.T) {
	s, clock := newTestStore()
	h, _ := s.Issue("user-1", "task-1", "jwt-1")
	*clock = clock.Add(2 * time.Minute)
	if _, err := s.Consume(h, "user-1", "task-1"); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected expired, got %v", err)
	}
	if s.Len() != 0 {
		t.Fatalf("expected expired entry swept, have %d", s.Len())
	}
}

func TestSweepRemovesExpiredOnly(t *testing.T) {
	s, clock := newTestStore()
	hOld, _ := s.Issue("user-1", "task-1", "jwt-old")
	*clock = clock.Add(90 * time.Second)
	hNew, _ := s.Issue("user-1", "task-2", "jwt-new")
	s.Sweep()
	if _, err := s.Consume(hOld, "user-1", "task-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected old handle swept, got %v", err)
	}
	if got, err := s.Consume(hNew, "user-1", "task-2"); err != nil || got != "jwt-new" {
		t.Fatalf("fresh handle must survive sweep; got %q err %v", got, err)
	}
}

func TestFailClosedBindingAndEmpty(t *testing.T) {
	s, _ := newTestStore()
	if _, err := s.Issue("", "task-1", "jwt"); !errors.Is(err, ErrUnbound) {
		t.Fatalf("expected unbound error for empty principal, got %v", err)
	}
	if _, err := s.Issue("user-1", "", "jwt"); !errors.Is(err, ErrUnbound) {
		t.Fatalf("expected unbound error for empty task, got %v", err)
	}
	if _, err := s.Issue("user-1", "task-1", ""); !errors.Is(err, ErrEmptySecret) {
		t.Fatalf("expected empty-secret error, got %v", err)
	}
}

func TestErrorsCarryNoSecret(t *testing.T) {
	s, _ := newTestStore()
	secret := "leak-me-if-you-can"
	h, _ := s.Issue("user-1", "task-1", secret)
	_, err := s.Consume(h, "attacker", "task-1")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error must not contain secret; got %v", err)
	}
}
