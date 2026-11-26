package issuesync

import (
	"context"
	"fmt"
	"io"
)

// MulticaStore is the Multica side of a run. It is an interface so the runner
// can be tested against a fake without a live server, and so a caller can
// substitute a read-only implementation for a dry run.
type MulticaStore interface {
	ListIssues(ctx context.Context, projectID string) ([]Issue, error)
	UpdateIssue(ctx context.Context, id string, fields []string, from Issue) error
	CreateIssue(ctx context.Context, from Issue, projectID string) (Issue, error)
}

// TrackerStore is the external tracker side of a run.
type TrackerStore interface {
	ListIssues(ctx context.Context) ([]Issue, error)
	UpdateIssue(ctx context.Context, iid int64, fields []string, from, current Issue) error
	CreateIssue(ctx context.Context, from Issue) (Issue, error)
}

// Options configures one run.
type Options struct {
	// ProjectID scopes the Multica side to one project. "" lists the whole
	// workspace, which is what a first run against an existing board wants.
	ProjectID string
	// TrackerProjectID is the tracker's numeric project id, stored in the
	// marker a Multica description carries so the pair can be re-linked.
	TrackerProjectID int64
	// DryRun reports what a run would do without writing anything.
	DryRun bool
	// CreateMissing makes a run create the counterpart of an unpaired issue.
	// Off by default: a first run against a large board should be inspected
	// before it doubles the number of issues in two systems.
	CreateMissing bool
	// Out receives the human-readable report. Nil discards it.
	Out io.Writer
}

// Report is what one run did, or would have done.
type Report struct {
	// Updated counts pairs where one side was copied to the other.
	Updated int
	// Conflicts lists pairs that disagree with no newer side. A run reports
	// them instead of picking a winner.
	Conflicts []ConflictPair
	// Created counts counterparts created for unpaired issues.
	Created int
	// Unpaired lists issues with no counterpart, whether or not the run
	// created one for them.
	Unpaired []Issue
	// Errors lists per-pair failures. A run continues past them: one issue the
	// tracker refuses must not stop the rest of the board from syncing.
	Errors []error
}

// ConflictPair is one pair a run could not resolve.
type ConflictPair struct {
	Multica Issue
	Tracker Issue
	Fields  []string
}

// Run performs one synchronization pass.
//
// The order is deliberate: link, then decide, then write. Every write is
// derived from a decision made before any write happened, so a run cannot
// observe its own output and cascade. That is what makes a second run over the
// same data a no-op.
func Run(ctx context.Context, multica MulticaStore, tracker TrackerStore, opts Options) (Report, error) {
	var report Report
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	multicaIssues, err := multica.ListIssues(ctx, opts.ProjectID)
	if err != nil {
		return report, fmt.Errorf("read multica issues: %w", err)
	}
	trackerIssues, err := tracker.ListIssues(ctx)
	if err != nil {
		return report, fmt.Errorf("read tracker issues: %w", err)
	}

	linked := Link(multicaIssues, trackerIssues)

	for _, pair := range linked.Pairs {
		decision := Decide(pair.Multica, pair.Tracker)
		switch decision.Kind {
		case NoChange:
			continue
		case Conflict:
			report.Conflicts = append(report.Conflicts, ConflictPair{
				Multica: pair.Multica,
				Tracker: pair.Tracker,
				Fields:  decision.Fields,
			})
			fmt.Fprintf(out, "conflict %s <-> %s: %v\n", pair.Multica.Key, pair.Tracker.Key, decision.Fields)
			continue
		}

		source, target := pair.Multica, pair.Tracker
		if decision.Source == SideGitLab {
			source, target = pair.Tracker, pair.Multica
		}
		fmt.Fprintf(out, "update %s -> %s: %v\n", source.Key, target.Key, decision.Fields)
		if opts.DryRun {
			report.Updated++
			continue
		}
		if err := writePair(ctx, multica, tracker, pair, decision, source, opts.TrackerProjectID); err != nil {
			report.Errors = append(report.Errors, err)
			fmt.Fprintf(out, "error %s <-> %s: %v\n", pair.Multica.Key, pair.Tracker.Key, err)
			continue
		}
		report.Updated++
	}

	for _, issue := range linked.MulticaOnly {
		report.Unpaired = append(report.Unpaired, issue)
		fmt.Fprintf(out, "unpaired multica %s\n", issue.Key)
		if !opts.CreateMissing || opts.DryRun {
			continue
		}
		// The marker goes into the create call itself. A counterpart created
		// without one is unpaired again on the next run, which would create a
		// second copy of it — and a third, once per run.
		from := issue
		from.Body = SetMarker(issue.Body, SideMultica, Marker{Identifier: issue.Key})
		created, err := tracker.CreateIssue(ctx, from)
		if err != nil {
			report.Errors = append(report.Errors, err)
			continue
		}
		report.Created++
		fmt.Fprintf(out, "created tracker issue %s for %s\n", created.Key, issue.Key)
	}

	for _, issue := range linked.TrackerOnly {
		report.Unpaired = append(report.Unpaired, issue)
		fmt.Fprintf(out, "unpaired tracker %s\n", issue.Key)
		if !opts.CreateMissing || opts.DryRun {
			continue
		}
		from := issue
		from.Body = SetMarker(issue.Body, SideGitLab, Marker{Project: opts.TrackerProjectID, IID: iidOf(issue)})
		created, err := multica.CreateIssue(ctx, from, opts.ProjectID)
		if err != nil {
			report.Errors = append(report.Errors, err)
			continue
		}
		report.Created++
		fmt.Fprintf(out, "created multica issue %s for tracker %s\n", created.Key, issue.Key)
	}

	return report, nil
}

// writePair copies the decided fields from source to the other side.
//
// The marker is written in the same call as the content, so a pair that was
// linked by only one side's marker ends the run with both markers present. A
// separate marker write would leave a window where a crash loses the link the
// run just established.
func writePair(ctx context.Context, multica MulticaStore, tracker TrackerStore, pair Pair, decision Decision, source Issue, projectID int64) error {
	if decision.Source == SideMultica {
		// The Multica side is newer, so the tracker is the one written. Its
		// description carries the Multica marker naming the counterpart.
		from := source
		from.Body = SetMarker(source.Body, SideMultica, Marker{Identifier: pair.Multica.Key})
		return tracker.UpdateIssue(ctx, iidOf(pair.Tracker), decision.Fields, from, pair.Tracker)
	}
	// The tracker is newer, so the Multica issue is written and carries the
	// tracker marker naming the project and issue number.
	from := source
	from.Body = SetMarker(source.Body, SideGitLab, Marker{Project: projectID, IID: iidOf(pair.Tracker)})
	return multica.UpdateIssue(ctx, pair.Multica.Key, decision.Fields, from)
}

// iidOf returns the tracker issue number as an integer. A key that is not a
// number yields 0, which the marker treats as absent rather than as issue 0.
func iidOf(issue Issue) int64 {
	var iid int64
	for _, r := range issue.Key {
		if r < '0' || r > '9' {
			return 0
		}
		iid = iid*10 + int64(r-'0')
	}
	return iid
}
