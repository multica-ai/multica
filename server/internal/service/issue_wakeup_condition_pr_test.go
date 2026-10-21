package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Exercise evaluation and diagnostic persistence even without local PostgreSQL.
// The DB-backed test covers the full registration/dispatch and parent-link path.
type prConditionTx struct {
	pgx.Tx
	issue     pgtype.UUID
	prs       [][]any
	queryErr  error
	lastError pgtype.Text
	next      pgtype.Timestamptz
	writes    int
}

func (tx *prConditionTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "UPDATE issue_wakeup_receipt") {
		return &sliceRows{}, nil
	}
	// The canned GitHub rows below describe an issue with no self-hosted VCS
	// pull requests; the VCS aggregation finds nothing to add. The VCS path
	// itself is covered against PostgreSQL by the condition test suite.
	if strings.Contains(sql, "FROM vcs_pull_request") {
		return &sliceRows{}, nil
	}
	if !strings.Contains(sql, "WHERE ipr.issue_id=$1") || len(args) != 1 || args[0] != tx.issue {
		return nil, fmt.Errorf("unexpected PR scope: %s %v", sql, args)
	}
	return &sliceRows{rows: tx.prs}, tx.queryErr
}

func (tx *prConditionTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(sql, "NoteWakeupFailure"):
		tx.lastError = args[0].(pgtype.Text)
		tx.writes++
	case strings.Contains(sql, "SetWakeupConditionState"):
		tx.next = args[1].(pgtype.Timestamptz)
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected write: %s", sql)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func TestWakeupConditionPREvaluation(t *testing.T) {
	row := func(state, head, checks string) []any { return []any{int32(1), state, head, checks} }
	for _, tc := range []struct {
		name, event string
		available   bool
		prs         [][]any
		wantErr     error
		wantMet     bool
	}{
		{"unconfigured", "checks_finished", false, nil, errPRSnapshotsUnavailable, false},
		{"unlinked", "checks_finished", true, nil, errPRNotLinked, false},
		{"unlinked merged", "merged", false, nil, errPRNotLinked, false},
		{"no snapshot", "checks_finished", true, [][]any{row("open", "", "SUCCESS")}, errPRSnapshotPending, false},
		{"pending", "checks_finished", true, [][]any{row("open", "head", "PENDING")}, nil, false},
		{"no checks", "checks_finished", true, [][]any{row("open", "head", "")}, nil, false},
		{"success", "checks_finished", true, [][]any{row("open", "head", "SUCCESS")}, nil, true},
		{"failure", "checks_finished", true, [][]any{row("open", "head", "FAILURE")}, nil, true},
		{"error", "checks_finished", true, [][]any{row("open", "head", "ERROR")}, nil, true},
		{"one matching PR", "checks_finished", true, [][]any{row("open", "", ""), row("open", "head", "SUCCESS")}, nil, true},
		{"merge without snapshots", "merged", false, [][]any{row("merged", "", "")}, nil, true},
		{"open without snapshots", "merged", false, [][]any{row("open", "", "")}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &IssueWakeupService{Tasks: &TaskService{PRSnapshotsEnabled: tc.available}}
			w := db.IssueWakeup{IssueID: dbid.NewV7(), Condition: condition(t, map[string]any{"type": "pull_request", "event": tc.event})}
			tx := &prConditionTx{issue: w.IssueID, prs: tc.prs}
			met, fingerprint, observed, err := s.evaluateCondition(context.Background(), tx, db.New(tx), w)
			if met != tc.wantMet || !errors.Is(err, tc.wantErr) {
				t.Fatalf("met=%t err=%v; want met=%t err=%v", met, err, tc.wantMet, tc.wantErr)
			}
			if met && (fingerprint == "" || len(observed["pull_requests"].([]map[string]any)) != 1) {
				t.Fatalf("missing terminal PR evidence: fingerprint=%q observed=%v", fingerprint, observed)
			}
		})
	}
}

func TestWakeupConditionPRDiagnosticPolling(t *testing.T) {
	ctx := context.Background()
	s := &IssueWakeupService{Tasks: &TaskService{}}
	w := db.IssueWakeup{ID: dbid.NewV7(), IssueID: dbid.NewV7(), Condition: condition(t, map[string]any{"type": "pull_request", "event": "checks_finished"})}
	tx := &prConditionTx{issue: w.IssueID}
	q := db.New(tx)
	now := time.Now()
	wantUnavailable := conditionDiagnosticPrefix + errPRSnapshotsUnavailable.Error()
	if err := s.baselineCondition(ctx, tx, q, w, now); err != nil || tx.lastError.String != wantUnavailable {
		t.Fatalf("registration must retain diagnostic without rejecting: %v, %+v", err, tx.lastError)
	}
	w.LastError = tx.lastError
	if err := s.baselineCondition(ctx, tx, q, w, now); err != nil || tx.writes != 1 {
		t.Fatalf("unchanged diagnostic must not be rewritten: err=%v writes=%d", err, tx.writes)
	}
	s.Tasks.PRSnapshotsEnabled = true
	for _, step := range []struct {
		prs  [][]any
		want error
	}{
		{nil, errPRNotLinked},
		{[][]any{{int32(1), "open", "", ""}}, errPRSnapshotPending},
		{[][]any{{int32(1), "open", "head", "PENDING"}}, nil},
	} {
		tx.prs = step.prs
		w.LastError = tx.lastError
		next, err := s.pollCondition(ctx, tx, q, w, now)
		if err != nil || !next.Valid || !next.Time.Equal(now.Add(conditionPollInterval)) {
			t.Fatalf("poll must remain scheduled: next=%+v err=%v", next, err)
		}
		if step.want == nil {
			if tx.lastError.Valid {
				t.Fatalf("recovery retained diagnostic: %+v", tx.lastError)
			}
		} else if tx.lastError.String != conditionDiagnosticPrefix+step.want.Error() {
			t.Fatalf("diagnostic=%+v, want %v", tx.lastError, step.want)
		}
	}
	// Real query failures still propagate; they are never treated as unmet facts.
	tx.queryErr = errors.New("database offline")
	if _, err := s.pollCondition(ctx, tx, q, w, now); !errors.Is(err, tx.queryErr) {
		t.Fatalf("database error swallowed: %v", err)
	}
}

func TestSaveConditionDiagnosticReplacesEarlierFailures(t *testing.T) {
	ctx := context.Background()
	tx := &prConditionTx{}
	q := db.New(tx)
	realFailure := pgtype.Text{String: "dispatch failed", Valid: true}
	tx.lastError = realFailure
	w := db.IssueWakeup{ID: dbid.NewV7(), LastError: realFailure}

	if err := saveConditionDiagnostic(ctx, q, w, errPRNotLinked); err != nil {
		t.Fatal(err)
	}
	if tx.writes != 1 || tx.lastError.String != conditionDiagnosticPrefix+errPRNotLinked.Error() {
		t.Fatalf("recovered evaluation kept the earlier dispatch failure: writes=%d error=%+v", tx.writes, tx.lastError)
	}
	if err := saveConditionDiagnostic(ctx, q, w, nil); err != nil {
		t.Fatal(err)
	}
	if tx.writes != 2 || tx.lastError.Valid {
		t.Fatalf("recovered evaluation did not clear the earlier dispatch failure: writes=%d error=%+v", tx.writes, tx.lastError)
	}

	w.LastError = pgtype.Text{String: conditionDiagnosticPrefix + errPRNotLinked.Error(), Valid: true}
	if err := saveConditionDiagnostic(ctx, q, w, errPRSnapshotPending); err != nil {
		t.Fatal(err)
	}
	if tx.writes != 3 || tx.lastError.String != conditionDiagnosticPrefix+errPRSnapshotPending.Error() {
		t.Fatalf("owned diagnostic was not updated: writes=%d error=%+v", tx.writes, tx.lastError)
	}
	w.LastError = tx.lastError
	if err := saveConditionDiagnostic(ctx, q, w, nil); err != nil {
		t.Fatal(err)
	}
	if tx.writes != 4 || tx.lastError.Valid {
		t.Fatalf("owned diagnostic was not cleared: writes=%d error=%+v", tx.writes, tx.lastError)
	}
}
