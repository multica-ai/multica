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
