package analytics

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type projectorSourceStub struct {
	runs    map[string]RunProjection
	listed  []string
	loadErr error
	listErr error
}

func (s *projectorSourceStub) LoadRun(_ context.Context, runID string) (RunProjection, error) {
	if s.loadErr != nil {
		return RunProjection{}, s.loadErr
	}
	return s.runs[runID], nil
}

func (s *projectorSourceStub) ListRunIDs(_ context.Context, _ string, _ string, _ int) ([]string, error) {
	return s.listed, s.listErr
}

type projectorStoreStub struct {
	projected []RunProjection
	err       error
}

func (s *projectorStoreStub) UpsertRun(_ context.Context, run RunProjection) error {
	if s.err != nil {
		return s.err
	}
	s.projected = append(s.projected, run)
	return nil
}

func TestProjectRunLoadsAndUpsertsTheCompleteSnapshot(t *testing.T) {
	want := RunProjection{RunID: "run-1", WorkspaceID: "workspace-1", SourceType: "issue", Status: "completed"}
	source := &projectorSourceStub{runs: map[string]RunProjection{"run-1": want}}
	store := &projectorStoreStub{}
	projector := NewProjector(source, store)

	if err := projector.ProjectRun(context.Background(), "run-1"); err != nil {
		t.Fatalf("ProjectRun() error = %v", err)
	}
	if !reflect.DeepEqual(store.projected, []RunProjection{want}) {
		t.Fatalf("projected = %#v, want %#v", store.projected, []RunProjection{want})
	}
}

func TestProjectRunRejectsIncompleteSnapshots(t *testing.T) {
	source := &projectorSourceStub{runs: map[string]RunProjection{"run-1": {RunID: "run-1"}}}
	projector := NewProjector(source, &projectorStoreStub{})

	if err := projector.ProjectRun(context.Background(), "run-1"); !errors.Is(err, ErrIncompleteProjection) {
		t.Fatalf("ProjectRun() error = %v, want ErrIncompleteProjection", err)
	}
}

func TestBackfillWorkspaceProjectsEachRunAndReturnsStableCursor(t *testing.T) {
	runs := map[string]RunProjection{
		"run-1": {RunID: "run-1", WorkspaceID: "workspace-1", SourceType: "issue", Status: "completed"},
		"run-2": {RunID: "run-2", WorkspaceID: "workspace-1", SourceType: "chat", Status: "failed"},
	}
	source := &projectorSourceStub{runs: runs, listed: []string{"run-1", "run-2"}}
	store := &projectorStoreStub{}
	projector := NewProjector(source, store)

	next, count, err := projector.BackfillWorkspace(context.Background(), "workspace-1", "", 2)
	if err != nil {
		t.Fatalf("BackfillWorkspace() error = %v", err)
	}
	if next != "run-2" || count != 2 {
		t.Fatalf("BackfillWorkspace() = (%q, %d), want (%q, %d)", next, count, "run-2", 2)
	}
	if len(store.projected) != 2 {
		t.Fatalf("projected %d runs, want 2", len(store.projected))
	}
}

func TestBackfillWorkspaceStopsAtFirstProjectionError(t *testing.T) {
	source := &projectorSourceStub{listed: []string{"run-1"}, loadErr: errors.New("load failed")}
	projector := NewProjector(source, &projectorStoreStub{})

	next, count, err := projector.BackfillWorkspace(context.Background(), "workspace-1", "cursor-0", 25)
	if err == nil {
		t.Fatal("BackfillWorkspace() error = nil, want error")
	}
	if next != "cursor-0" || count != 0 {
		t.Fatalf("BackfillWorkspace() = (%q, %d), want (%q, 0)", next, count, "cursor-0")
	}
}
