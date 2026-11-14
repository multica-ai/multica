package daemon

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscriptionQuotaDeduplicatesRepeatedDelivery(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	d, calls := localSkillReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(started) })
		<-release
		w.WriteHeader(http.StatusOK)
	})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		d.handleSubscriptionQuota(ctx, "runtime", "request")
		close(done)
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("first delivery did not report")
	}

	// The first report is still in flight. A repeated delivery should return
	// without either a second probe or a second report.
	duplicate := make(chan struct{})
	go func() {
		d.handleSubscriptionQuota(ctx, "runtime", "request")
		close(duplicate)
	}()
	select {
	case <-duplicate:
	case <-ctx.Done():
		t.Fatal("duplicate delivery did not return")
	}
	close(release)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("first report did not finish")
	}
	d.handleSubscriptionQuota(ctx, "runtime", "request")
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("expected one quota report, got %d", got)
	}
}
