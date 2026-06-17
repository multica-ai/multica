package workspacecopy

// Integration tests for the TECH-3732 slice: internal-reference rewriting and
// cascade copy. Same real-Postgres harness as store_test.go (srcWS prefix TECH,
// tgtWS prefix FIR). They skip cleanly when no test database is reachable.

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// freshWorkspaces creates an isolated source (prefix TECH) + target (prefix FIR)
// workspace pair so a test never disturbs the shared srcWS/tgtWS issue_counter
// that other tests assert absolute numbers against. Cleaned up after the test.
func freshWorkspaces(t *testing.T, ctx context.Context) (src, tgt pgtype.UUID) {
	t.Helper()
	src, tgt = newID(), newID()
	srcSlug := "wscopy-src-" + uuid.UUID(src.Bytes).String()
	tgtSlug := "wscopy-tgt-" + uuid.UUID(tgt.Bytes).String()
	if _, err := testPool.Exec(ctx, `INSERT INTO workspace (id, name, slug, issue_prefix) VALUES ($1,$2,$3,'TECH')`,
		src, "WSCopy Src Iso", srcSlug); err != nil {
		t.Fatalf("create src ws: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO workspace (id, name, slug, issue_prefix) VALUES ($1,$2,$3,'FIR')`,
		tgt, "WSCopy Tgt Iso", tgtSlug); err != nil {
		t.Fatalf("create tgt ws: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id IN ($1,$2)`, src, tgt)
	})
	return src, tgt
}

// seedIssue inserts a minimal issue and returns its id.
func seedIssue(t *testing.T, ctx context.Context, ws pgtype.UUID, number int32, status, description string, parent, project pgtype.UUID) pgtype.UUID {
	t.Helper()
	id := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (id, workspace_id, title, description, status, priority, kind, creator_type, creator_id,
		                   number, position, parent_issue_id, project_id)
		VALUES ($1,$2,$3,$4,$5,'none','issue','member',$6,$7,0,$8,$9)`,
		id, ws, "Issue "+status, description, status, testUser, number, parent, project); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return id
}

func TestRewriteInternalReferences(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	srcWS, tgtWS := freshWorkspaces(t, ctx)

	// B is referenced from A both as a mention link (UUID) and a bare TECH-901 token.
	srcB := seedIssue(t, ctx, srcWS, 901, "todo", "", pgtype.UUID{}, pgtype.UUID{})
	bRef := "[TECH-901](mention://issue/" + uuid.UUID(srcB.Bytes).String() + ")"
	srcA := seedIssue(t, ctx, srcWS, 900, "todo",
		"See "+bRef+" and also bare TECH-901 for context. Unrelated OTHER-5 stays.",
		pgtype.UUID{}, pgtype.UUID{})
	// a comment on A that also references B
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1,$2,$3,'member',$4,$5)`,
		newID(), srcA, srcWS, testUser, "Blocked by TECH-901."); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	runID := newID()
	resA, err := s.CopyIssue(ctx, runID, tgtWS, srcA)
	if err != nil {
		t.Fatalf("copy A: %v", err)
	}
	resB, err := s.CopyIssue(ctx, runID, tgtWS, srcB)
	if err != nil {
		t.Fatalf("copy B: %v", err)
	}

	rew, err := s.RewriteInternalReferences(ctx, tgtWS)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if rew.IssuesRewritten != 1 || rew.CommentsRewritten != 1 {
		t.Fatalf("rewrite counts = %d issues / %d comments, want 1/1", rew.IssuesRewritten, rew.CommentsRewritten)
	}

	// copied A description: mention href -> copied B UUID; TECH-901 -> FIR-<B target number>.
	var tgtADesc string
	if err := testPool.QueryRow(ctx, `SELECT description FROM issue WHERE id=$1`, resA.TargetID).Scan(&tgtADesc); err != nil {
		t.Fatalf("load copied A: %v", err)
	}
	wantHref := "mention://issue/" + uuid.UUID(resB.TargetID.Bytes).String()
	if !strings.Contains(tgtADesc, wantHref) {
		t.Fatalf("copied A href not rewritten: %q missing %q", tgtADesc, wantHref)
	}
	wantIdent := "FIR-" + strconv.Itoa(int(resB.TargetNumber))
	if !strings.Contains(tgtADesc, wantIdent) {
		t.Fatalf("copied A identifier not rewritten to %q: %q", wantIdent, tgtADesc)
	}
	if strings.Contains(tgtADesc, "TECH-901") {
		t.Fatalf("copied A still contains source identifier TECH-901: %q", tgtADesc)
	}
	if !strings.Contains(tgtADesc, "OTHER-5") {
		t.Fatalf("unrelated identifier OTHER-5 should be untouched: %q", tgtADesc)
	}

	// source A is untouched (still points at the source UUID + TECH-901).
	var srcADesc string
	if err := testPool.QueryRow(ctx, `SELECT description FROM issue WHERE id=$1`, srcA).Scan(&srcADesc); err != nil {
		t.Fatalf("reload source A: %v", err)
	}
	if !strings.Contains(srcADesc, "TECH-901") || !strings.Contains(srcADesc, uuid.UUID(srcB.Bytes).String()) {
		t.Fatalf("source A mutated: %q", srcADesc)
	}

	// idempotent: a second pass rewrites nothing.
	rew2, err := s.RewriteInternalReferences(ctx, tgtWS)
	if err != nil {
		t.Fatalf("rewrite again: %v", err)
	}
	if rew2.IssuesRewritten != 0 || rew2.CommentsRewritten != 0 {
		t.Fatalf("second rewrite = %d/%d, want 0/0 (idempotent)", rew2.IssuesRewritten, rew2.CommentsRewritten)
	}
}

// TestRewriteInternalReferences_EqualPrefix locks the idempotency guard: when
// the source and target workspaces share an issue prefix, identifier-token
// rewriting is skipped (it would be ambiguous + non-idempotent across re-runs),
// but mention-link UUIDs still heal, and a second pass changes nothing.
func TestRewriteInternalReferences_EqualPrefix(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	src, tgt := newID(), newID()
	for _, w := range []struct {
		id   pgtype.UUID
		name string
	}{{src, "EqP Src"}, {tgt, "EqP Tgt"}} {
		slug := "wscopy-eqp-" + uuid.UUID(w.id.Bytes).String()
		if _, err := testPool.Exec(ctx, `INSERT INTO workspace (id, name, slug, issue_prefix) VALUES ($1,$2,$3,'SAME')`,
			w.id, w.name, slug); err != nil {
			t.Fatalf("create ws: %v", err)
		}
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id IN ($1,$2)`, src, tgt) })

	srcB := seedIssue(t, ctx, src, 555, "todo", "", pgtype.UUID{}, pgtype.UUID{})
	bRef := "[SAME-555](mention://issue/" + uuid.UUID(srcB.Bytes).String() + ")"
	srcA := seedIssue(t, ctx, src, 554, "todo", "See "+bRef+" and bare SAME-555.", pgtype.UUID{}, pgtype.UUID{})

	runID := newID()
	resA, err := s.CopyIssue(ctx, runID, tgt, srcA)
	if err != nil {
		t.Fatalf("copy A: %v", err)
	}
	resB, err := s.CopyIssue(ctx, runID, tgt, srcB)
	if err != nil {
		t.Fatalf("copy B: %v", err)
	}
	if _, err := s.RewriteInternalReferences(ctx, tgt); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	var desc string
	if err := testPool.QueryRow(ctx, `SELECT description FROM issue WHERE id=$1`, resA.TargetID).Scan(&desc); err != nil {
		t.Fatalf("load copied A: %v", err)
	}
	// mention href healed to copied B...
	if !strings.Contains(desc, "mention://issue/"+uuid.UUID(resB.TargetID.Bytes).String()) {
		t.Fatalf("equal-prefix: mention href not healed: %q", desc)
	}
	// ...but identifier token left as-is (skipped to stay idempotent).
	if !strings.Contains(desc, "SAME-555") {
		t.Fatalf("equal-prefix: identifier should NOT be rewritten, got %q", desc)
	}
	// second pass is a no-op (idempotent).
	rew2, err := s.RewriteInternalReferences(ctx, tgt)
	if err != nil {
		t.Fatalf("rewrite again: %v", err)
	}
	if rew2.IssuesRewritten != 0 || rew2.CommentsRewritten != 0 {
		t.Fatalf("equal-prefix second pass = %d/%d, want 0/0", rew2.IssuesRewritten, rew2.CommentsRewritten)
	}
}

func TestCopyIssueCascade(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	srcWS, tgtWS := freshWorkspaces(t, ctx)

	root := seedIssue(t, ctx, srcWS, 910, "todo", "", pgtype.UUID{}, pgtype.UUID{})
	child1 := seedIssue(t, ctx, srcWS, 911, "in_progress", "", root, pgtype.UUID{})
	grandchild := seedIssue(t, ctx, srcWS, 912, "todo", "", child1, pgtype.UUID{})
	_ = grandchild
	doneChild := seedIssue(t, ctx, srcWS, 913, "done", "", root, pgtype.UUID{})
	_ = doneChild

	res, err := s.CopyIssueCascade(ctx, newID(), tgtWS, root)
	if err != nil {
		t.Fatalf("CopyIssueCascade: %v", err)
	}
	// root + child1 + grandchild = 3 copied; root counted separately so cascade = 2.
	if res.CascadeCopied != 2 {
		t.Fatalf("cascade_copied = %d, want 2 (child + grandchild; done child excluded)", res.CascadeCopied)
	}

	// the done child must NOT have been copied.
	var doneCopies int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM cerebro_workspace_copy_map WHERE target_workspace_id=$1 AND entity_type='issue' AND source_id=$2`,
		tgtWS, doneChild).Scan(&doneCopies); err != nil {
		t.Fatalf("count done child copies: %v", err)
	}
	if doneCopies != 0 {
		t.Fatalf("done child was copied (%d), want 0", doneCopies)
	}

	// child1's copy is parented to root's copy (relink ran inside the cascade).
	var copiedChildParent, copiedRoot pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		SELECT m.target_id FROM cerebro_workspace_copy_map m
		WHERE m.target_workspace_id=$1 AND m.entity_type='issue' AND m.source_id=$2`, tgtWS, root).Scan(&copiedRoot); err != nil {
		t.Fatalf("find copied root: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT tgt.parent_issue_id FROM cerebro_workspace_copy_map m
		JOIN issue tgt ON tgt.id = m.target_id
		WHERE m.target_workspace_id=$1 AND m.entity_type='issue' AND m.source_id=$2`, tgtWS, child1).Scan(&copiedChildParent); err != nil {
		t.Fatalf("find copied child parent: %v", err)
	}
	if copiedChildParent != copiedRoot {
		t.Fatalf("copied child parent = %v, want copied root %v (relink failed)", copiedChildParent, copiedRoot)
	}
}

func TestCopyProjectCascade(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	srcWS, tgtWS := freshWorkspaces(t, ctx)

	proj := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO project (id, workspace_id, title, status) VALUES ($1,$2,'Migration','in_progress')`,
		proj, srcWS); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	openIssue := seedIssue(t, ctx, srcWS, 920, "todo", "", pgtype.UUID{}, proj)
	doneIssue := seedIssue(t, ctx, srcWS, 921, "done", "", pgtype.UUID{}, proj)
	_ = doneIssue

	res, err := s.CopyProjectCascade(ctx, newID(), tgtWS, proj)
	if err != nil {
		t.Fatalf("CopyProjectCascade: %v", err)
	}
	if res.CascadeCopied != 1 {
		t.Fatalf("cascade_copied = %d, want 1 (one open issue; done excluded)", res.CascadeCopied)
	}

	// the open issue's copy sits in the copied project.
	var copiedProj, issueProj pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		SELECT target_id FROM cerebro_workspace_copy_map WHERE target_workspace_id=$1 AND entity_type='project' AND source_id=$2`,
		tgtWS, proj).Scan(&copiedProj); err != nil {
		t.Fatalf("find copied project: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT tgt.project_id FROM cerebro_workspace_copy_map m
		JOIN issue tgt ON tgt.id = m.target_id
		WHERE m.target_workspace_id=$1 AND m.entity_type='issue' AND m.source_id=$2`, tgtWS, openIssue).Scan(&issueProj); err != nil {
		t.Fatalf("find copied issue project: %v", err)
	}
	if issueProj != copiedProj {
		t.Fatalf("copied issue project = %v, want copied project %v", issueProj, copiedProj)
	}
}
