package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// workpad.go implements the issue-target eval path (FIR-3659): an eval whose
// target kind is "issue" runs a deterministic, token-free check against the
// event issue's own row instead of going through the model-gateway executor.
// The first (and currently only) check is "workpad": the issue description
// must open with a `## Workpad` section containing at least one markdown
// checklist line. The verdict is persisted as a normal cerebro_eval_run, so
// eval.gate, eval.run and the eval_passed / eval_failed hook conditions all
// read it with zero special-casing.

// ErrIssueEvalNeedsIssue is returned when an issue-target eval is executed
// without an issue in scope (e.g. plain Run-now with no issue context). The
// eval can only judge a concrete issue row.
var ErrIssueEvalNeedsIssue = errors.New("issue-target eval requires an issue")

// workpadHeading anchors the section; the check is case-insensitive on the
// heading but strict on structure: the section must be the very first content
// of the description ("prepended", per the approved convention).
var workpadHeadingPattern = regexp.MustCompile(`(?i)^##\s+workpad\s*$`)

// workpadChecklistPattern matches one checklist step: `- [ ] ...` or
// `- [x] ...` with non-empty content after the checkbox.
var workpadChecklistPattern = regexp.MustCompile(`^\s*[-*] \[( |x|X)\]\s+\S`)

// workpadDividerPattern is the `---` line that ends the workpad section and
// protects the original description below it.
var workpadDividerPattern = regexp.MustCompile(`^\s*---\s*$`)

// HasWorkpad reports whether the description carries a valid workpad: a
// `## Workpad` heading as the first content line, with at least one checklist
// line before the `---` divider (or end of description). Checklist lines that
// only appear below the divider do not count — the workpad must be the
// prepended section, not buried in the original body.
func HasWorkpad(description string) (bool, string) {
	lines := strings.Split(strings.ReplaceAll(description, "\r\n", "\n"), "\n")
	headingSeen := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !headingSeen {
			if workpadHeadingPattern.MatchString(strings.TrimSpace(line)) {
				headingSeen = true
				continue
			}
			return false, "description does not start with a `## Workpad` section"
		}
		if workpadDividerPattern.MatchString(line) {
			return false, "workpad section has no checklist items before the `---` divider"
		}
		if workpadChecklistPattern.MatchString(line) {
			return true, "workpad checklist found"
		}
		// Non-checklist prose inside the section is tolerated; keep scanning
		// until a checklist line or the divider decides the verdict.
	}
	if headingSeen {
		return false, "workpad section has no checklist items"
	}
	return false, "description is empty"
}

// executeIssueEval runs an issue-target eval programmatically. spec.Ref picks
// the check; only "workpad" exists today. It reads the issue row scoped to the
// eval's workspace so a cross-workspace issue id can never produce a verdict.
func (s *Store) executeIssueEval(ctx context.Context, definition Eval, spec TargetSpec, issueID *uuid.UUID) (RunExecution, error) {
	if issueID == nil {
		return RunExecution{}, ErrIssueEvalNeedsIssue
	}
	check := spec.Ref
	if check == "" {
		check = "workpad"
	}
	if check != "workpad" {
		return RunExecution{}, fmt.Errorf("issue-target eval: unknown check %q", check)
	}
	startedAt := time.Now()
	var description *string
	err := s.pool.QueryRow(ctx, `SELECT description FROM issue WHERE id=$1 AND workspace_id=$2`,
		*issueID, definition.WorkspaceID).Scan(&description)
	if err != nil {
		return RunExecution{}, fmt.Errorf("issue-target eval: load issue: %w", err)
	}
	desc := ""
	if description != nil {
		desc = *description
	}
	passed, reason := HasWorkpad(desc)
	status := RunStatusFailed
	if passed {
		status = RunStatusPassed
	}
	completedAt := time.Now()
	results, err := json.Marshal(map[string]any{
		"check":  check,
		"passed": passed,
		"reason": reason,
	})
	if err != nil {
		return RunExecution{}, fmt.Errorf("issue-target eval: serialize results: %w", err)
	}
	return RunExecution{
		TargetVersion: definition.Version,
		Status:        status,
		Results:       results,
		CostCents:     0,
		LatencyMS:     completedAt.Sub(startedAt).Milliseconds(),
		StartedAt:     &startedAt,
		CompletedAt:   &completedAt,
	}, nil
}
