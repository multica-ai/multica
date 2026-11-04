package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestHealthReportsTheBudgetGateCacheAge checks that health reads the age of the
// gate's cached sample and names it as a risk once the sample is stale (spec 4.9).
func TestHealthReportsTheBudgetGateCacheAge(t *testing.T) {
	f := newGovFixture(t)
	ctx := context.Background()
	gate := NewBudgetGate(nil)

	if at, err := gate.CacheRefreshedAt(ctx, f.ws); err != nil || !at.IsZero() {
		t.Fatalf("a gate that has read nothing reports %v, %v; want the zero time", at, err)
	}
	read := f.now
	if _, err := gate.finishedCosts(ctx, f.q, f.ws, read); err != nil {
		t.Fatalf("read the cache: %v", err)
	}

	fresh, err := (&HealthService{Queries: f.q, BudgetGate: gate, Now: func() time.Time { return read.Add(90 * time.Second) }}).Report(ctx, f.ws)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if !fresh.BudgetGate.Reported || fresh.BudgetGate.CacheAgeSeconds == nil || *fresh.BudgetGate.CacheAgeSeconds != 90 {
		t.Fatalf("budget gate reading = %+v, want a cache age of 90 seconds", fresh.BudgetGate)
	}
	for _, r := range fresh.Risks {
		if strings.Contains(r, "budget gate") {
			t.Fatalf("a fresh cache is not a risk: %v", fresh.Risks)
		}
	}

	stale, err := (&HealthService{Queries: f.q, BudgetGate: gate, Now: func() time.Time { return read.Add(BudgetCacheStaleAfter + time.Minute) }}).Report(ctx, f.ws)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	found := false
	for _, r := range stale.Risks {
		found = found || strings.Contains(r, "budget gate")
	}
	if !found {
		t.Fatalf("a stale cache must be a risk line: %v", stale.Risks)
	}
}
