package issuesync

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeMultica records writes so a test can assert what a run decided to do.
type fakeMultica struct {
	issues  []Issue
	updates []fakeUpdate
	creates []Issue
	failOn  string
}

type fakeUpdate struct {
	key    string
	fields []string
	from   Issue
}

func (f *fakeMultica) ListIssues(context.Context, string) ([]Issue, error) {
	return f.issues, nil
}

func (f *fakeMultica) UpdateIssue(_ context.Context, key string, fields []string, from Issue) error {
	if f.failOn == key {
		return errors.New("multica refused the write")
	}
	f.updates = append(f.updates, fakeUpdate{key: key, fields: fields, from: from})
	return nil
}

func (f *fakeMultica) CreateIssue(_ context.Context, from Issue, _ string) (Issue, error) {
	f.creates = append(f.creates, from)
	created := from
	created.Side = SideMultica
	created.Key = "PROJECT-99"
	return created, nil
}

// fakeTracker is the tracker side of the same contract.
type fakeTracker struct {
	issues  []Issue
	updates []fakeTrackerUpdate
	creates []Issue
	failOn  int64
}

type fakeTrackerUpdate struct {
	iid     int64
	fields  []string
	from    Issue
	current Issue
}

func (f *fakeTracker) ListIssues(context.Context) ([]Issue, error) {
	return f.issues, nil
}

func (f *fakeTracker) UpdateIssue(_ context.Context, iid int64, fields []string, from, current Issue) error {
	if f.failOn == iid {
		return errors.New("tracker refused the write")
	}
	f.updates = append(f.updates, fakeTrackerUpdate{iid: iid, fields: fields, from: from, current: current})
	return nil
}

func (f *fakeTracker) CreateIssue(_ context.Context, from Issue) (Issue, error) {
	f.creates = append(f.creates, from)
	created := from
	created.Side = SideGitLab
	created.Key = "99"
	return created, nil
}

func TestRunWritesNothingWhenPairsAgree(t *testing.T) {
	multica := &fakeMultica{issues: []Issue{{
		Side: SideMultica, Key: "PROJECT-1", Title: "Fix", Body: "body", Status: "todo", UpdatedAt: at(1),
	}}}
	tracker := &fakeTracker{issues: []Issue{{
		Side: SideGitLab, Key: "7", Title: "Fix", Body: "body", Status: "todo", UpdatedAt: at(5),
		// The tracker carries the marker that links the pair.
	}}}
	tracker.issues[0].Body = SetMarker("body", SideMultica, Marker{Identifier: "PROJECT-1"})

	report, err := Run(context.Background(), multica, tracker, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Updated != 0 || len(multica.updates) != 0 || len(tracker.updates) != 0 {
		t.Errorf("report = %+v, multica writes = %d, tracker writes = %d; want no writes",
			report, len(multica.updates), len(tracker.updates))
	}
}

func TestRunCopiesNewerMulticaSideToTracker(t *testing.T) {
	multica := &fakeMultica{issues: []Issue{{
		Side: SideMultica, Key: "PROJECT-1", Title: "Fix", Body: "newer text", Status: "todo", UpdatedAt: at(5),
	}}}
	tracker := &fakeTracker{issues: []Issue{{
		Side: SideGitLab, Key: "7", Title: "Fix", Body: "older text", Status: "todo", UpdatedAt: at(1),
	}}}
	tracker.issues[0].Body = SetMarker("older text", SideMultica, Marker{Identifier: "PROJECT-1"})

	report, err := Run(context.Background(), multica, tracker, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Updated != 1 {
		t.Fatalf("Updated = %d, want 1", report.Updated)
	}
	if len(tracker.updates) != 1 {
		t.Fatalf("tracker writes = %d, want 1", len(tracker.updates))
	}
	got := tracker.updates[0]
	if got.iid != 7 {
		t.Errorf("iid = %d, want 7", got.iid)
	}
	if len(got.fields) != 1 || got.fields[0] != FieldDescription {
		t.Errorf("fields = %v, want [description]", got.fields)
	}
	if !strings.Contains(got.from.Body, "newer text") {
		t.Errorf("body = %q, want the newer text", got.from.Body)
	}
	// The write carries the Multica marker, so the pair stays linked even if
	// the tracker's own marker is later edited away.
	if _, ok := ParseMarker(got.from.Body, SideMultica); !ok {
		t.Errorf("body = %q, want a multica marker", got.from.Body)
	}
}

func TestRunCopiesNewerTrackerSideToMultica(t *testing.T) {
	multica := &fakeMultica{issues: []Issue{{
		Side: SideMultica, Key: "PROJECT-1", Title: "Fix", Body: "older text", Status: "todo", UpdatedAt: at(1),
	}}}
	tracker := &fakeTracker{issues: []Issue{{
		Side: SideGitLab, Key: "7", Title: "Fix", Body: "newer text", Status: "todo", UpdatedAt: at(5),
	}}}
	tracker.issues[0].Body = SetMarker("newer text", SideMultica, Marker{Identifier: "PROJECT-1"})

	report, err := Run(context.Background(), multica, tracker, Options{TrackerProjectID: 42})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Updated != 1 || len(multica.updates) != 1 {
		t.Fatalf("report = %+v, multica writes = %d; want one write", report, len(multica.updates))
	}
	got := multica.updates[0]
	if got.key != "PROJECT-1" {
		t.Errorf("key = %q, want PROJECT-1", got.key)
	}
	marker, ok := ParseMarker(got.from.Body, SideGitLab)
	if !ok {
		t.Fatalf("body = %q, want a gitlab marker", got.from.Body)
	}
	if marker.Project != 42 || marker.IID != 7 {
		t.Errorf("marker = %+v, want project 42 iid 7", marker)
	}
}

func TestRunReportsConflictWithoutWriting(t *testing.T) {
	multica := &fakeMultica{issues: []Issue{{
		Side: SideMultica, Key: "PROJECT-1", Title: "Fix", Body: "multica text", Status: "todo", UpdatedAt: at(3),
	}}}
	tracker := &fakeTracker{issues: []Issue{{
		Side: SideGitLab, Key: "7", Title: "Fix", Body: "tracker text", Status: "todo", UpdatedAt: at(3),
	}}}
	tracker.issues[0].Body = SetMarker("tracker text", SideMultica, Marker{Identifier: "PROJECT-1"})

	report, err := Run(context.Background(), multica, tracker, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Conflicts) != 1 {
		t.Fatalf("Conflicts = %d, want 1", len(report.Conflicts))
	}
	if len(multica.updates) != 0 || len(tracker.updates) != 0 {
		t.Errorf("a conflict must not write: multica %d, tracker %d", len(multica.updates), len(tracker.updates))
	}
}

func TestRunDryRunWritesNothing(t *testing.T) {
	multica := &fakeMultica{issues: []Issue{{
		Side: SideMultica, Key: "PROJECT-1", Title: "Fix", Body: "newer", Status: "todo", UpdatedAt: at(5),
	}}}
	tracker := &fakeTracker{issues: []Issue{{
		Side: SideGitLab, Key: "7", Title: "Fix", Body: "older", Status: "todo", UpdatedAt: at(1),
	}}}
	tracker.issues[0].Body = SetMarker("older", SideMultica, Marker{Identifier: "PROJECT-1"})

	report, err := Run(context.Background(), multica, tracker, Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Updated != 1 {
		t.Errorf("Updated = %d, want 1: a dry run still reports what it would do", report.Updated)
	}
	if len(multica.updates) != 0 || len(tracker.updates) != 0 {
		t.Errorf("a dry run must not write: multica %d, tracker %d", len(multica.updates), len(tracker.updates))
	}
}

func TestRunDoesNotCreateCounterpartsByDefault(t *testing.T) {
	multica := &fakeMultica{issues: []Issue{{Side: SideMultica, Key: "PROJECT-1", Title: "Only here"}}}
	tracker := &fakeTracker{issues: []Issue{{Side: SideGitLab, Key: "7", Title: "Only there"}}}

	report, err := Run(context.Background(), multica, tracker, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Unpaired) != 2 {
		t.Errorf("Unpaired = %d, want 2", len(report.Unpaired))
	}
	if report.Created != 0 || len(multica.creates) != 0 || len(tracker.creates) != 0 {
		t.Errorf("Created = %d, want 0 without CreateMissing", report.Created)
	}
}

func TestRunCreatesCounterpartsWhenAsked(t *testing.T) {
	multica := &fakeMultica{issues: []Issue{{Side: SideMultica, Key: "PROJECT-1", Title: "Only here"}}}
	tracker := &fakeTracker{issues: []Issue{{Side: SideGitLab, Key: "7", Title: "Only there"}}}

	report, err := Run(context.Background(), multica, tracker, Options{CreateMissing: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Created != 2 {
		t.Errorf("Created = %d, want 2", report.Created)
	}
	if len(tracker.creates) != 1 || len(multica.creates) != 1 {
		t.Errorf("creates = %d/%d, want 1/1", len(tracker.creates), len(multica.creates))
	}
}

func TestRunContinuesPastAFailedWrite(t *testing.T) {
	// The Multica side is newer in both pairs, so both writes go to the tracker.
	// The first one is refused; the second must still sync.
	multica := &fakeMultica{
		issues: []Issue{
			{Side: SideMultica, Key: "PROJECT-1", Title: "Fix", Body: "newer one", Status: "todo", UpdatedAt: at(5)},
			{Side: SideMultica, Key: "PROJECT-2", Title: "Fix", Body: "newer two", Status: "todo", UpdatedAt: at(5)},
		},
	}
	tracker := &fakeTracker{
		failOn: 7,
		issues: []Issue{
			{Side: SideGitLab, Key: "7", Body: SetMarker("older one", SideMultica, Marker{Identifier: "PROJECT-1"}), Title: "Fix", Status: "todo", UpdatedAt: at(1)},
			{Side: SideGitLab, Key: "8", Body: SetMarker("older two", SideMultica, Marker{Identifier: "PROJECT-2"}), Title: "Fix", Status: "todo", UpdatedAt: at(1)},
		},
	}

	report, err := Run(context.Background(), multica, tracker, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %d, want 1", len(report.Errors))
	}
	if report.Updated != 1 {
		t.Errorf("Updated = %d, want 1: the second pair must still sync", report.Updated)
	}
}

func TestRunIsIdempotent(t *testing.T) {
	// After a run copies the newer side, a second run over the resulting state
	// must decide nothing changed. This is the property that keeps a scheduled
	// sync from rewriting the same issues forever.
	multica := &fakeMultica{issues: []Issue{{
		Side: SideMultica, Key: "PROJECT-1", Title: "Fix", Body: "newer", Status: "todo", UpdatedAt: at(5),
	}}}
	tracker := &fakeTracker{issues: []Issue{{
		Side: SideGitLab, Key: "7", Title: "Fix", Body: "older", Status: "todo", UpdatedAt: at(1),
	}}}
	tracker.issues[0].Body = SetMarker("older", SideMultica, Marker{Identifier: "PROJECT-1"})

	if _, err := Run(context.Background(), multica, tracker, Options{}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	// Apply the write the run made, then run again.
	tracker.issues[0].Body = tracker.updates[0].from.Body
	tracker.issues[0].UpdatedAt = at(6)
	tracker.updates = nil

	report, err := Run(context.Background(), multica, tracker, Options{})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if report.Updated != 0 || len(tracker.updates) != 0 {
		t.Errorf("second run wrote again: Updated = %d, writes = %d", report.Updated, len(tracker.updates))
	}
}
