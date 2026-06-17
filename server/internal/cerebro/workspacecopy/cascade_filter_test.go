package workspacecopy

// Integration test for the status-filtered project cascade (TECH-3582): "move
// all issues with these statuses under a project". Real migrated Postgres.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestCopyProjectCascadeFiltered_ByStatus(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	srcWS := isolatedWS(t, ctx, "wscopy-filt-src", "TECH")
	tgtWS := isolatedWS(t, ctx, "wscopy-filt-tgt", "FIR")

	proj := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO project (id, workspace_id, title) VALUES ($1,$2,'Filtered project')`,
		proj, srcWS); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	// three issues in the project: one todo, one in_progress, one done.
	type seed struct {
		id     pgtype.UUID
		status string
		num    int32
	}
	seeds := []seed{
		{newID(), "todo", 1},
		{newID(), "in_progress", 2},
		{newID(), "done", 3},
	}
	for _, sd := range seeds {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO issue (id, workspace_id, project_id, title, status, priority, kind, creator_type, creator_id, number, position)
			VALUES ($1,$2,$3,'i','`+sd.status+`','medium','issue','member',$4,$5,1.0)`,
			sd.id, srcWS, proj, testUser, sd.num); err != nil {
			t.Fatalf("seed issue %s: %v", sd.status, err)
		}
	}

	// filter to in_progress only: project copied + exactly that one issue.
	runID := newID()
	res, err := s.CopyProjectCascadeFiltered(ctx, runID, tgtWS, proj, []string{"in_progress"})
	if err != nil {
		t.Fatalf("CopyProjectCascadeFiltered: %v", err)
	}
	if res.CascadeCopied != 1 {
		t.Fatalf("filtered cascade copied = %d, want 1 (only the in_progress issue)", res.CascadeCopied)
	}
	var gotStatuses []string
	rows, err := testPool.Query(ctx, `SELECT status FROM issue WHERE workspace_id = $1`, tgtWS)
	if err != nil {
		t.Fatalf("load target issues: %v", err)
	}
	for rows.Next() {
		var st string
		if err := rows.Scan(&st); err != nil {
			rows.Close()
			t.Fatalf("scan: %v", err)
		}
		gotStatuses = append(gotStatuses, st)
	}
	rows.Close()
	if len(gotStatuses) != 1 || gotStatuses[0] != "in_progress" {
		t.Fatalf("target issue statuses = %v, want [in_progress]", gotStatuses)
	}

	// source project still has all three issues (untouched).
	var srcCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE project_id = $1 AND workspace_id = $2`, proj, srcWS).Scan(&srcCount); err != nil {
		t.Fatalf("count source issues: %v", err)
	}
	if srcCount != 3 {
		t.Fatalf("source project issue count = %d, want 3 (untouched)", srcCount)
	}
}
