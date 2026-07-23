package analytics

import (
	"context"
	"errors"
	"testing"
	"time"
)

type postgresRunLoaderStub struct {
	run RunProjection
	err error
}

func (s postgresRunLoaderStub) loadRun(context.Context, string) (RunProjection, error) {
	return s.run, s.err
}

func TestPostgresProjectionSourceReturnsAuthoritativeRunSnapshot(t *testing.T) {
	want := RunProjection{
		RunID: "run-1", WorkspaceID: "workspace-1", Population: "agent",
		SourceType: "issue", Status: "completed", StartedAt: time.Unix(10, 0),
		Savings: []SavingProjection{{Type: "graphify", Mode: "on", Metric: "tokens", BaselineValue: 20, EffectiveValue: 5}},
		Quality: []QualityProjection{{Type: "skill_observation", Category: "verification", Verdict: "success"}},
	}
	source := newPostgresProjectionSourceWithLoader(postgresRunLoaderStub{run: want}.loadRun, nil)

	got, err := source.LoadRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("LoadRun() error = %v", err)
	}
	if got.RunID != want.RunID || len(got.Savings) != 1 || len(got.Quality) != 1 {
		t.Fatalf("LoadRun() = %#v, want authoritative snapshot %#v", got, want)
	}
}

func TestPostgresProjectionSourceWrapsLoadFailure(t *testing.T) {
	source := newPostgresProjectionSourceWithLoader(postgresRunLoaderStub{err: errors.New("db down")}.loadRun, nil)
	if _, err := source.LoadRun(context.Background(), "run-1"); err == nil {
		t.Fatal("LoadRun() error = nil, want error")
	}
}

func TestSummarizeUsageCostTracksRuntimeTokenUsage(t *testing.T) {
	gotCents, gotKind := summarizeUsageCost([]usageCostRow{
		{Model: "claude-sonnet-4-6", InputTokens: 1_000_000},
	})
	if gotCents == nil || *gotCents <= 0 || gotKind != "calculated" {
		t.Fatalf("summarizeUsageCost() = (%v, %q), want positive calculated cost", gotCents, gotKind)
	}

	exact := int64(42)
	gotCents, gotKind = summarizeUsageCost([]usageCostRow{{Model: "gateway", StoredCostCents: exact}})
	if gotCents == nil || *gotCents != exact || gotKind != "actual" {
		t.Fatalf("summarizeUsageCost() = (%v, %q), want (42, actual)", gotCents, gotKind)
	}

	gotCents, gotKind = summarizeUsageCost(nil)
	if gotCents != nil || gotKind != "missing" {
		t.Fatalf("summarizeUsageCost(nil) = (%v, %q), want (nil, missing)", gotCents, gotKind)
	}
}
