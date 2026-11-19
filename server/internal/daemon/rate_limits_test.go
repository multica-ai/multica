package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func rateLimitSnap(used float64, at time.Time) agent.RateLimitSnapshot {
	return agent.RateLimitSnapshot{
		Status:     agent.RateLimitStatusAllowed,
		Windows:    []agent.RateLimitWindow{{Kind: agent.RateLimitWindowSession, UsedPercent: &used, WindowMinutes: 300}},
		ObservedAt: at,
	}
}

func TestRateLimitReporterPostsOnlyChanges(t *testing.T) {
	var posted []agent.RateLimitSnapshot
	var failNext error
	r := &rateLimitReporter{
		log: slog.Default(),
		post: func(_ context.Context, s agent.RateLimitSnapshot) error {
			if failNext != nil {
				err := failNext
				failNext = nil
				return err
			}
			posted = append(posted, s)
			return nil
		},
	}
	ctx := context.Background()
	start := time.Now()

	r.flush(ctx) // nothing observed yet
	if len(posted) != 0 {
		t.Fatalf("posted before any observation: %v", posted)
	}

	r.observe(rateLimitSnap(10, start))
	r.flush(ctx)
	// Only ObservedAt moved: nothing a reader sees changed.
	r.observe(rateLimitSnap(10, start.Add(time.Minute)))
	r.flush(ctx)
	if len(posted) != 1 {
		t.Fatalf("posts = %d, want 1", len(posted))
	}

	// A failed post is retried by the next flush.
	r.observe(rateLimitSnap(20, start.Add(2*time.Minute)))
	failNext = errors.New("network down")
	r.flush(ctx)
	r.flush(ctx)
	if len(posted) != 2 || *posted[1].Windows[0].UsedPercent != 20 {
		t.Fatalf("posted = %+v", posted)
	}
}

func TestRateLimitReporterStopsOnOlderServer(t *testing.T) {
	calls := 0
	r := &rateLimitReporter{
		log: slog.Default(),
		post: func(context.Context, agent.RateLimitSnapshot) error {
			calls++
			return &requestError{Method: http.MethodPost, Path: "/rate-limits", StatusCode: http.StatusNotFound}
		},
	}
	r.observe(rateLimitSnap(10, time.Now()))
	r.flush(context.Background())
	r.observe(rateLimitSnap(30, time.Now()))
	r.flush(context.Background())
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (a 404 means the server has no endpoint)", calls)
	}
}
