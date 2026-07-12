package analytics

import (
	"context"
	"errors"
	"testing"
)

type runProjectorStub struct {
	runs []string
	err  error
}

func (p *runProjectorStub) ProjectRun(_ context.Context, runID string) error {
	p.runs = append(p.runs, runID)
	return p.err
}

func TestProjectAfterWriteProjectsOnlySuccessfulWrites(t *testing.T) {
	projector := &runProjectorStub{}
	writeErr := errors.New("write failed")

	if err := ProjectAfterWrite(context.Background(), projector, "run-1", func() error { return writeErr }); !errors.Is(err, writeErr) {
		t.Fatalf("ProjectAfterWrite() error = %v, want write error", err)
	}
	if len(projector.runs) != 0 {
		t.Fatalf("projected runs = %v, want none", projector.runs)
	}
}

func TestProjectAfterWriteReturnsProjectionErrorForRetry(t *testing.T) {
	projectErr := errors.New("projection failed")
	projector := &runProjectorStub{err: projectErr}

	if err := ProjectAfterWrite(context.Background(), projector, "run-1", func() error { return nil }); !errors.Is(err, projectErr) {
		t.Fatalf("ProjectAfterWrite() error = %v, want projection error", err)
	}
	if len(projector.runs) != 1 || projector.runs[0] != "run-1" {
		t.Fatalf("projected runs = %v, want [run-1]", projector.runs)
	}
}

func TestProjectAfterWriteAllowsUnwiredProjector(t *testing.T) {
	called := false
	if err := ProjectAfterWrite(context.Background(), nil, "run-1", func() error { called = true; return nil }); err != nil {
		t.Fatalf("ProjectAfterWrite() error = %v", err)
	}
	if !called {
		t.Fatal("write was not called")
	}
}
