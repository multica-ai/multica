package workspacecopy

// Integration tests for artifact (documents/notes) copy (TECH-3582). Run against
// a real migrated Postgres; skip cleanly when none is reachable.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// isolatedWS creates a throwaway workspace with the given prefix and registers
// its cleanup, so artifact tests never perturb the shared srcWS/tgtWS counters
// that order-fragile sibling tests assert on.
func isolatedWS(t *testing.T, ctx context.Context, slug, prefix string) pgtype.UUID {
	t.Helper()
	ws := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO workspace (id, name, slug, issue_prefix) VALUES ($1,$2,$3,$4)`,
		ws, slug, slug, prefix); err != nil {
		t.Fatalf("create workspace %s: %v", slug, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, ws)
	})
	return ws
}

// An issue-scoped artifact (a note) travels with its issue, lands on the copied
// issue in the target workspace, leaves the source untouched, and re-running is
// idempotent (no duplicate).
func TestCopyIssueArtifacts_TravelWithIssue_SourceUntouched_Idempotent(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	srcWS := isolatedWS(t, ctx, "wscopy-art-src1", "TECH")
	tgtWS := isolatedWS(t, ctx, "wscopy-art-tgt1", "FIR")

	srcIssue := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (id, workspace_id, title, status, priority, kind, creator_type, creator_id, number, position)
		VALUES ($1,$2,'Issue with a note','todo','medium','issue','member',$3, 11, 1.0)`,
		srcIssue, srcWS, testUser); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	srcArt := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO artifact (id, workspace_id, issue_id, kind, title, body, author_type, author_id, format)
		VALUES ($1,$2,$3,'note','Release notes','# Body here','member',$4,'md')`,
		srcArt, srcWS, srcIssue, testUser); err != nil {
		t.Fatalf("seed artifact: %v", err)
	}

	runID := newID()
	res, err := s.CopyIssue(ctx, runID, tgtWS, srcIssue)
	if err != nil {
		t.Fatalf("CopyIssue: %v", err)
	}
	if res.Artifacts != 1 {
		t.Fatalf("artifacts copied = %d, want 1", res.Artifacts)
	}

	// the copied artifact is attached to the copied issue, in the target ws, with fidelity.
	var gotWS, gotIssue pgtype.UUID
	var gotKind, gotTitle, gotBody, gotFormat string
	if err := testPool.QueryRow(ctx, `
		SELECT workspace_id, issue_id, kind, title, body, format
		FROM artifact WHERE workspace_id = $1 AND issue_id = $2`, tgtWS, res.TargetID).
		Scan(&gotWS, &gotIssue, &gotKind, &gotTitle, &gotBody, &gotFormat); err != nil {
		t.Fatalf("load target artifact: %v", err)
	}
	if gotWS != tgtWS || gotIssue != res.TargetID {
		t.Fatalf("target artifact ws=%v issue=%v, want tgt/%v", gotWS, gotIssue, res.TargetID)
	}
	if gotKind != "note" || gotTitle != "Release notes" || gotBody != "# Body here" || gotFormat != "md" {
		t.Fatalf("artifact fidelity lost: kind=%q title=%q body=%q format=%q", gotKind, gotTitle, gotBody, gotFormat)
	}

	// source artifact untouched (still scoped to the source issue in the source ws).
	var srcWSID, srcIssueID pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT workspace_id, issue_id FROM artifact WHERE id = $1`, srcArt).
		Scan(&srcWSID, &srcIssueID); err != nil {
		t.Fatalf("reload source artifact: %v", err)
	}
	if srcWSID != srcWS || srcIssueID != srcIssue {
		t.Fatalf("source artifact mutated: ws=%v issue=%v", srcWSID, srcIssueID)
	}

	// idempotency: copying the issue again adds no duplicate artifact.
	if _, err := s.CopyIssue(ctx, runID, tgtWS, srcIssue); err != nil {
		t.Fatalf("second CopyIssue: %v", err)
	}
	var cnt int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM artifact WHERE workspace_id = $1 AND issue_id = $2`, tgtWS, res.TargetID).
		Scan(&cnt); err != nil {
		t.Fatalf("count target artifacts: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("target artifact count = %d after re-copy, want 1 (no duplicate)", cnt)
	}
}

// The bulk pass copies the workspace folder tree (parents-first, remapped) and
// the workspace-scoped artifacts, placing each copied artifact in its copied
// folder. Source untouched; idempotent.
func TestCopyWorkspaceArtifacts_FoldersAndBulk(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	srcWS := isolatedWS(t, ctx, "wscopy-art-src2", "TECH")
	tgtWS := isolatedWS(t, ctx, "wscopy-art-tgt2", "FIR")

	parent := newID()
	child := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO artifact_folder (id, workspace_id, parent_id, name) VALUES ($1,$2,NULL,'Analyser')`,
		parent, srcWS); err != nil {
		t.Fatalf("seed parent folder: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO artifact_folder (id, workspace_id, parent_id, name) VALUES ($1,$2,$3,'2026')`,
		child, srcWS, parent); err != nil {
		t.Fatalf("seed child folder: %v", err)
	}
	wsArt := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO artifact (id, workspace_id, folder_id, kind, title, body, author_type, author_id, format)
		VALUES ($1,$2,$3,'report','Q1 analysis','data','agent',$4,'md')`,
		wsArt, srcWS, child, testUser); err != nil {
		t.Fatalf("seed workspace artifact: %v", err)
	}

	runID := newID()
	res, err := s.CopyWorkspaceArtifacts(ctx, runID, srcWS, tgtWS)
	if err != nil {
		t.Fatalf("CopyWorkspaceArtifacts: %v", err)
	}
	if res.CascadeCopied != 1 {
		t.Fatalf("workspace artifacts copied = %d, want 1", res.CascadeCopied)
	}

	// the copied artifact sits in a copied folder named '2026' whose parent is 'Analyser'.
	var gotFolder pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT folder_id FROM artifact WHERE workspace_id = $1 AND title = 'Q1 analysis'`, tgtWS).
		Scan(&gotFolder); err != nil {
		t.Fatalf("load target ws artifact: %v", err)
	}
	if !gotFolder.Valid {
		t.Fatalf("target artifact lost its folder")
	}
	var fName string
	var fParent pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT name, parent_id FROM artifact_folder WHERE id = $1`, gotFolder).Scan(&fName, &fParent); err != nil {
		t.Fatalf("load target folder: %v", err)
	}
	if fName != "2026" || !fParent.Valid {
		t.Fatalf("target folder name=%q parent valid=%v, want 2026/true", fName, fParent.Valid)
	}
	var pName string
	if err := testPool.QueryRow(ctx, `SELECT name FROM artifact_folder WHERE id = $1`, fParent).Scan(&pName); err != nil {
		t.Fatalf("load parent folder: %v", err)
	}
	if pName != "Analyser" {
		t.Fatalf("parent folder name=%q, want Analyser", pName)
	}

	// idempotency: re-running adds no duplicate folders or artifacts in the target.
	if _, err := s.CopyWorkspaceArtifacts(ctx, runID, srcWS, tgtWS); err != nil {
		t.Fatalf("second CopyWorkspaceArtifacts: %v", err)
	}
	var folderCnt, artCnt int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM artifact_folder WHERE workspace_id = $1`, tgtWS).Scan(&folderCnt); err != nil {
		t.Fatalf("count target folders: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM artifact WHERE workspace_id = $1 AND title = 'Q1 analysis'`, tgtWS).Scan(&artCnt); err != nil {
		t.Fatalf("count target ws artifacts: %v", err)
	}
	if folderCnt != 2 || artCnt != 1 {
		t.Fatalf("after re-copy folders=%d artifacts=%d, want 2/1 (no duplicates)", folderCnt, artCnt)
	}
}
