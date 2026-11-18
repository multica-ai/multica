//go:build !windows

package agent

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/kagent"
)

func TestKagentReviewFreshContextDoesNotResurrectHistory(t *testing.T) {
	f := newSliceHTTPFixture(t)
	first, _ := finishSliceKagent(t, executeSliceKagent(t, f.backend("before-clear"), context.Background()))
	fresh, known := finishSliceKagent(t, executeSliceKagent(t, f.backend("after-clear"), context.Background()))
	if !known || fresh.SessionID == first.SessionID {
		t.Fatalf("clear reused context: first=%+v fresh=%+v", first, fresh)
	}
	// Re-delivering the fresh claim observes its task without creating another context.
	retry, known := finishSliceKagent(t, executeSliceKagent(t, f.backend("after-clear"), context.Background()))
	if !known || retry.SessionID != fresh.SessionID {
		t.Fatalf("fresh claim retry: %+v", retry)
	}
	for _, opts := range []ExecOptions{
		{SystemPrompt: "inline runtime brief", ResumeSessionID: first.SessionID},
		{SystemPrompt: "inline runtime brief", ResumeExpected: true},
	} {
		if _, err := f.backend("stale-pointer").Execute(context.Background(), "harmless question", opts); err == nil {
			t.Fatal("stale/missing resume pointer accepted")
		}
	}
	if _, err := f.backend("before-clear").Execute(context.Background(), "harmless question", ExecOptions{SystemPrompt: "inline runtime brief"}); err == nil {
		t.Fatal("retired attempt resurrected history")
	}
	s, err := f.backend("continued").Execute(context.Background(), "harmless question", ExecOptions{SystemPrompt: "inline runtime brief", ResumeSessionID: fresh.SessionID, ResumeExpected: true})
	if err != nil {
		t.Fatal(err)
	}
	continued, known := finishSliceKagent(t, s)
	if !known || continued.SessionID != fresh.SessionID {
		t.Fatalf("continuation: %+v", continued)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) != 3 || f.sends[1].ContextID != "" || f.sends[2].ContextID != fresh.SessionID {
		t.Fatalf("wrong context requests: %+v", f.sends)
	}
}

func TestKagentReviewFirstCreateWaitsForControllerReadiness(t *testing.T) {
	f := newSliceHTTPFixture(t)
	f.unreadyGets = 3
	if f.agent != nil {
		t.Fatal("fixture must begin before first create")
	}
	result, known := finishSliceKagent(t, executeSliceKagent(t, f.backend("first-use"), context.Background()))
	if !known || result.Status != "completed" {
		t.Fatalf("first create failed: %+v", result)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.agentGets != 4 || len(f.sends) != 1 {
		t.Fatalf("gets=%d sends=%d", f.agentGets, len(f.sends))
	}
}

func TestKagentReviewReadinessBoundAndPermanentErrors(t *testing.T) {
	for _, mode := range []string{"bound", "parent-deadline", "parent-cancel", "forbidden"} {
		t.Run(mode, func(t *testing.T) {
			f := newSliceHTTPFixture(t)
			f.unreadyGets = 100000
			b := f.backend("waiting").(*kagentBackend)
			b.cfg.ReadinessTimeout = 20 * time.Millisecond
			ctx := context.Background()
			want := context.DeadlineExceeded
			switch mode {
			case "parent-deadline":
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 5*time.Millisecond)
				defer cancel()
			case "parent-cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				time.AfterFunc(5*time.Millisecond, cancel)
				want = context.Canceled
			case "forbidden":
				b.cfg.ReadinessTimeout = time.Second
				f.agentGetError = http.StatusForbidden
			}
			_, err := b.Execute(ctx, "question", ExecOptions{SystemPrompt: "brief"})
			if err == nil {
				t.Fatal("unready execution accepted")
			}
			if mode != "forbidden" && !errors.Is(err, want) {
				t.Fatalf("cancellation reason lost: %v", err)
			}
			f.mu.Lock()
			if len(f.sends) != 0 {
				t.Fatal("sent before ready")
			}
			if mode == "forbidden" && f.agentGets != 1 {
				t.Fatalf("permanent error retried %d times", f.agentGets)
			}
			f.mu.Unlock()
			// Every failure must release enrollment locks.
			lease, err := f.lifecycle.Acquire(context.Background(), sliceAgentID)
			if errors.Is(err, kagent.ErrBusy) {
				t.Fatal("readiness failure leaked lease")
			}
			if lease != nil {
				lease.Close()
			}
		})
	}
}

func TestKagentReviewDeadlineWithUnconfirmedCleanupRetainsFence(t *testing.T) {
	f := newSliceHTTPFixture(t)
	f.nextState, f.cancelState = kagent.StateWorking, kagent.StateWorking
	s, err := f.backend("deadline").Execute(context.Background(), "question", ExecOptions{SystemPrompt: "brief", Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	result, known := finishSliceKagent(t, s)
	if known || result.Status != "cleanup_unconfirmed" {
		t.Fatalf("timeout lost unknown-outcome fence: %+v", result)
	}
	if _, err := f.backend("fresh").Execute(context.Background(), "question", ExecOptions{SystemPrompt: "brief"}); !errors.Is(err, kagent.ErrUnknownOutcome) {
		t.Fatalf("fresh context bypassed fence: %v", err)
	}
}

func TestKagentReviewOldJournalFailsClosed(t *testing.T) {
	f := newSliceHTTPFixture(t)
	rec, err := f.journal.Lock(kagent.ConversationKey("workspace", sliceAgentID, "conversation"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Save(kagentConversation{Version: 1, SessionID: sliceSessionID, ActiveAttempt: "unconfirmed"}); err != nil {
		t.Fatal(err)
	}
	rec.Close()
	_, err = f.backend("after-clear").Execute(context.Background(), "question", ExecOptions{SystemPrompt: "brief"})
	if !errors.Is(err, kagent.ErrUnknownOutcome) {
		t.Fatalf("old journal must retain cleanup fence: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) != 0 {
		t.Fatal("old journal caused remote execution")
	}
}

func TestKagentReviewParentDeadlineReportsTimeoutAfterConfirmedCleanup(t *testing.T) {
	f := newSliceHTTPFixture(t)
	f.nextState = kagent.StateWorking
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, known := finishSliceKagent(t, executeSliceKagent(t, f.backend("parent-deadline"), ctx))
	if !known || result.Status != "timeout" || result.Error == "" {
		t.Fatalf("parent deadline misclassified: %+v", result)
	}
}
