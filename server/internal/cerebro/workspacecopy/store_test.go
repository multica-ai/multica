package workspacecopy

// Integration tests for the non-destructive workspace-copy engine (TECH-3582).
//
// These run against a real migrated Postgres (the same DATABASE_URL CI uses) so
// they exercise the raw INSERT...SELECT column lists against the live schema —
// the one class of bug `go build` cannot catch. Each copier is checked for:
//   - success against the real schema (no "column does not exist" / NOT NULL),
//   - the source row staying byte-for-byte untouched (copy, never move),
//   - fidelity of carried fields (is_private, classification, comment type, …),
//   - idempotency (a second copy returns AlreadyDone, not a duplicate).
// Tests skip cleanly when no test database is reachable.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	testPool  *pgxpool.Pool
	srcWS     pgtype.UUID
	tgtWS     pgtype.UUID
	testUser  pgtype.UUID
	testEmail = "wscopy-test@multica.ai"
)

func u(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }
func newID() pgtype.UUID         { return u(uuid.New()) }

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping workspacecopy integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping workspacecopy integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}
	if err := setupFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to set up workspacecopy fixture: %v\n", err)
		_ = teardownFixture(ctx, pool)
		pool.Close()
		os.Exit(1)
	}
	testPool = pool
	code := m.Run()
	if err := teardownFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to tear down workspacecopy fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupFixture(ctx context.Context, pool *pgxpool.Pool) error {
	_ = teardownFixture(ctx, pool) // best-effort clean slate from a crashed prior run
	testUser = newID()
	if _, err := pool.Exec(ctx, `INSERT INTO "user" (id, name, email) VALUES ($1,$2,$3)`,
		testUser, "WSCopy Test", testEmail); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	srcWS = newID()
	tgtWS = newID()
	if _, err := pool.Exec(ctx, `INSERT INTO workspace (id, name, slug, issue_prefix) VALUES ($1,$2,$3,$4)`,
		srcWS, "WSCopy Src", "wscopy-src", "TECH"); err != nil {
		return fmt.Errorf("create src workspace: %w", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workspace (id, name, slug, issue_prefix) VALUES ($1,$2,$3,$4)`,
		tgtWS, "WSCopy Tgt", "wscopy-tgt", "FIR"); err != nil {
		return fmt.Errorf("create tgt workspace: %w", err)
	}
	return nil
}

func teardownFixture(ctx context.Context, pool *pgxpool.Pool) error {
	// workspace FK cascades clear issues/agents/projects/chats/autopilots/etc.
	// cerebro_workspace_copy_map cascades off workspace too.
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug IN ('wscopy-src','wscopy-tgt')`); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, testEmail); err != nil {
		return err
	}
	return nil
}

func TestCopyIssue_Fidelity_SourceUntouched_Idempotent(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)

	srcIssue := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (id, workspace_id, title, status, priority, kind, creator_type, creator_id,
		                   number, position, is_private, classification)
		VALUES ($1,$2,'Secret task','in_progress','high','issue','member',$3, 7, 3.5, true, 'secret')`,
		srcIssue, srcWS, testUser); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type, classification)
		VALUES ($1,$2,$3,'member',$4,'a status change','status_change','secret')`,
		newID(), srcIssue, srcWS, testUser); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_reaction (id, issue_id, workspace_id, actor_type, actor_id, emoji)
		VALUES ($1,$2,$3,'member',$4,'👍')`,
		newID(), srcIssue, srcWS, testUser); err != nil {
		t.Fatalf("seed reaction: %v", err)
	}
	srcLabel := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO issue_label (id, workspace_id, name, color) VALUES ($1,$2,'bug','#ff0000')`,
		srcLabel, srcWS); err != nil {
		t.Fatalf("seed label: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO issue_to_label (issue_id, label_id) VALUES ($1,$2)`, srcIssue, srcLabel); err != nil {
		t.Fatalf("link label: %v", err)
	}

	runID := newID()
	res, err := s.CopyIssue(ctx, runID, tgtWS, srcIssue)
	if err != nil {
		t.Fatalf("CopyIssue: %v", err)
	}
	if res.Comments != 1 || res.Reactions != 1 || res.Labels != 1 {
		t.Fatalf("children copied = comments %d reactions %d labels %d, want 1/1/1", res.Comments, res.Reactions, res.Labels)
	}
	if res.TargetNumber != 1 {
		t.Fatalf("target number = %d, want 1 (target issue_counter started at 0)", res.TargetNumber)
	}

	// target issue keeps privacy + classification + lands in target workspace
	var tgtPrivate bool
	var tgtClass string
	var tgtWSID pgtype.UUID
	var tgtNum int32
	if err := testPool.QueryRow(ctx, `SELECT is_private, classification, workspace_id, number FROM issue WHERE id = $1`, res.TargetID).
		Scan(&tgtPrivate, &tgtClass, &tgtWSID, &tgtNum); err != nil {
		t.Fatalf("load target issue: %v", err)
	}
	if !tgtPrivate || tgtClass != "secret" {
		t.Fatalf("target issue is_private=%v classification=%q, want true/secret (fidelity lost)", tgtPrivate, tgtClass)
	}
	if tgtWSID != tgtWS || tgtNum != 1 {
		t.Fatalf("target issue ws=%v num=%d, want tgt/1", tgtWSID, tgtNum)
	}

	// copied comment keeps its type + classification
	var cType, cClass string
	if err := testPool.QueryRow(ctx, `SELECT type, classification FROM comment WHERE issue_id = $1`, res.TargetID).
		Scan(&cType, &cClass); err != nil {
		t.Fatalf("load target comment: %v", err)
	}
	if cType != "status_change" || cClass != "secret" {
		t.Fatalf("target comment type=%q class=%q, want status_change/secret", cType, cClass)
	}

	// source issue is byte-for-byte untouched (still in src workspace, same number)
	var srcWSID pgtype.UUID
	var srcNum int32
	if err := testPool.QueryRow(ctx, `SELECT workspace_id, number FROM issue WHERE id = $1`, srcIssue).Scan(&srcWSID, &srcNum); err != nil {
		t.Fatalf("reload source issue: %v", err)
	}
	if srcWSID != srcWS || srcNum != 7 {
		t.Fatalf("source issue mutated: ws=%v num=%d, want src/7", srcWSID, srcNum)
	}

	// idempotency: a second copy is a no-op pointing at the same target
	res2, err := s.CopyIssue(ctx, runID, tgtWS, srcIssue)
	if err != nil {
		t.Fatalf("second CopyIssue: %v", err)
	}
	if !res2.AlreadyDone || res2.TargetID != res.TargetID {
		t.Fatalf("second copy = %+v, want AlreadyDone at same target", res2)
	}
	var dupCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id = $1`, tgtWS).Scan(&dupCount); err != nil {
		t.Fatalf("count target issues: %v", err)
	}
	if dupCount != 1 {
		t.Fatalf("target issue count = %d after re-copy, want 1 (no duplicate)", dupCount)
	}
}

func TestCopyIssue_Threads_Project_Parent_Attachments(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	runID := newID()

	// project + parent issue copied first, so the child issue's inline remap
	// links it to the copied project and copied parent.
	srcProject := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO project (id, workspace_id, title, status) VALUES ($1,$2,'Umbrella','in_progress')`,
		srcProject, srcWS); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	projRes, err := s.CopyProject(ctx, runID, tgtWS, srcProject)
	if err != nil {
		t.Fatalf("CopyProject: %v", err)
	}
	srcParentIssue := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (id, workspace_id, title, status, priority, kind, creator_type, creator_id, number, position)
		VALUES ($1,$2,'Parent','todo','none','issue','member',$3, 10, 1)`,
		srcParentIssue, srcWS, testUser); err != nil {
		t.Fatalf("seed parent issue: %v", err)
	}
	parentRes, err := s.CopyIssue(ctx, runID, tgtWS, srcParentIssue)
	if err != nil {
		t.Fatalf("CopyIssue (parent): %v", err)
	}

	// child issue: belongs to the project, is a sub-issue of the parent, and has
	// a threaded comment (reply) plus an issue-level and a comment-level attachment.
	srcChild := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (id, workspace_id, title, status, priority, kind, creator_type, creator_id,
		                   number, position, project_id, parent_issue_id)
		VALUES ($1,$2,'Child','todo','none','issue','member',$3, 11, 2, $4, $5)`,
		srcChild, srcWS, testUser, srcProject, srcParentIssue); err != nil {
		t.Fatalf("seed child issue: %v", err)
	}
	rootComment := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1,$2,$3,'member',$4,'root comment')`,
		rootComment, srcChild, srcWS, testUser); err != nil {
		t.Fatalf("seed root comment: %v", err)
	}
	replyComment := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, parent_id)
		VALUES ($1,$2,$3,'member',$4,'a reply',$5)`,
		replyComment, srcChild, srcWS, testUser, rootComment); err != nil {
		t.Fatalf("seed reply comment: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO attachment (id, workspace_id, issue_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1,$2,$3,'member',$4,'spec.pdf','https://blob/spec.pdf','application/pdf',1234)`,
		newID(), srcWS, srcChild, testUser); err != nil {
		t.Fatalf("seed issue attachment: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO attachment (id, workspace_id, comment_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1,$2,$3,'member',$4,'screenshot.png','https://blob/shot.png','image/png',555)`,
		newID(), srcWS, replyComment, testUser); err != nil {
		t.Fatalf("seed comment attachment: %v", err)
	}

	res, err := s.CopyIssue(ctx, runID, tgtWS, srcChild)
	if err != nil {
		t.Fatalf("CopyIssue (child): %v", err)
	}
	if res.Comments != 2 || res.Attachments != 2 {
		t.Fatalf("child copied comments=%d attachments=%d, want 2/2", res.Comments, res.Attachments)
	}

	// child links to the copied project + copied parent (inline remap)
	var gotProject, gotParent pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT project_id, parent_issue_id FROM issue WHERE id = $1`, res.TargetID).
		Scan(&gotProject, &gotParent); err != nil {
		t.Fatalf("load target child: %v", err)
	}
	if gotProject != projRes.TargetID {
		t.Fatalf("child project_id=%v, want copied project %v", gotProject, projRes.TargetID)
	}
	if gotParent != parentRes.TargetID {
		t.Fatalf("child parent_issue_id=%v, want copied parent %v", gotParent, parentRes.TargetID)
	}

	// reply thread survived: the copied reply's parent is the copied root comment
	var rootTgt, replyParent pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT id FROM comment WHERE issue_id = $1 AND parent_id IS NULL`, res.TargetID).Scan(&rootTgt); err != nil {
		t.Fatalf("load copied root comment: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT parent_id FROM comment WHERE issue_id = $1 AND content = 'a reply'`, res.TargetID).Scan(&replyParent); err != nil {
		t.Fatalf("load copied reply comment: %v", err)
	}
	if !replyParent.Valid || replyParent != rootTgt {
		t.Fatalf("copied reply parent_id=%v, want copied root %v (thread lost)", replyParent, rootTgt)
	}

	// both attachments copied into the target, comment-level one remapped to the copied comment
	var issueAttachments, commentAttachments int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM attachment WHERE issue_id = $1`, res.TargetID).Scan(&issueAttachments); err != nil {
		t.Fatalf("count issue attachments: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM attachment WHERE comment_id = (SELECT id FROM comment WHERE issue_id = $1 AND content = 'a reply')`, res.TargetID).Scan(&commentAttachments); err != nil {
		t.Fatalf("count comment attachments: %v", err)
	}
	if issueAttachments != 1 || commentAttachments != 1 {
		t.Fatalf("copied attachments issue=%d comment=%d, want 1/1", issueAttachments, commentAttachments)
	}

	// source attachments untouched (copy, never move)
	var srcAttachments int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM attachment WHERE workspace_id = $1`, srcWS).Scan(&srcAttachments); err != nil {
		t.Fatalf("count source attachments: %v", err)
	}
	if srcAttachments != 2 {
		t.Fatalf("source attachments=%d after copy, want 2 (untouched)", srcAttachments)
	}
}

func TestRelinkIssueRelations_OrderIndependent(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	runID := newID()

	// worst case: child issue copied BEFORE its parent, and an issue copied BEFORE
	// its project. Inline remap can't link them; the relink post-pass must.
	srcParent := newID()
	srcChild := newID()
	srcProject := newID()
	srcProjIssue := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO project (id, workspace_id, title, status) VALUES ($1,$2,'LateProject','in_progress')`,
		srcProject, srcWS); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (id, workspace_id, title, status, priority, kind, creator_type, creator_id, number, position)
		VALUES ($1,$2,'LateParent','todo','none','issue','member',$3, 20, 1)`,
		srcParent, srcWS, testUser); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (id, workspace_id, title, status, priority, kind, creator_type, creator_id, number, position, parent_issue_id)
		VALUES ($1,$2,'LateChild','todo','none','issue','member',$3, 21, 2, $4)`,
		srcChild, srcWS, testUser, srcParent); err != nil {
		t.Fatalf("seed child: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (id, workspace_id, title, status, priority, kind, creator_type, creator_id, number, position, project_id)
		VALUES ($1,$2,'InLateProject','todo','none','issue','member',$3, 22, 3, $4)`,
		srcProjIssue, srcWS, testUser, srcProject); err != nil {
		t.Fatalf("seed project issue: %v", err)
	}

	// copy child first (parent not yet copied), project-issue before project
	childRes, err := s.CopyIssue(ctx, runID, tgtWS, srcChild)
	if err != nil {
		t.Fatalf("CopyIssue child: %v", err)
	}
	projIssueRes, err := s.CopyIssue(ctx, runID, tgtWS, srcProjIssue)
	if err != nil {
		t.Fatalf("CopyIssue project-issue: %v", err)
	}
	// links can't be set yet
	var p, pr pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT parent_issue_id FROM issue WHERE id = $1`, childRes.TargetID).Scan(&p); err != nil {
		t.Fatalf("load child: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT project_id FROM issue WHERE id = $1`, projIssueRes.TargetID).Scan(&pr); err != nil {
		t.Fatalf("load project-issue: %v", err)
	}
	if p.Valid || pr.Valid {
		t.Fatalf("links set before counterparts copied: parent=%v project=%v, want null/null", p, pr)
	}

	// now copy the parent and the project
	parentRes, err := s.CopyIssue(ctx, runID, tgtWS, srcParent)
	if err != nil {
		t.Fatalf("CopyIssue parent: %v", err)
	}
	projRes, err := s.CopyProject(ctx, runID, tgtWS, srcProject)
	if err != nil {
		t.Fatalf("CopyProject: %v", err)
	}

	// relink heals both
	rel, err := s.RelinkIssueRelations(ctx, tgtWS)
	if err != nil {
		t.Fatalf("RelinkIssueRelations: %v", err)
	}
	if rel.Parents != 1 || rel.Projects != 1 {
		t.Fatalf("relinked parents=%d projects=%d, want 1/1", rel.Parents, rel.Projects)
	}
	if err := testPool.QueryRow(ctx, `SELECT parent_issue_id FROM issue WHERE id = $1`, childRes.TargetID).Scan(&p); err != nil {
		t.Fatalf("reload child: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT project_id FROM issue WHERE id = $1`, projIssueRes.TargetID).Scan(&pr); err != nil {
		t.Fatalf("reload project-issue: %v", err)
	}
	if p != parentRes.TargetID {
		t.Fatalf("child parent after relink=%v, want %v", p, parentRes.TargetID)
	}
	if pr != projRes.TargetID {
		t.Fatalf("project-issue project after relink=%v, want %v", pr, projRes.TargetID)
	}

	// relink is idempotent: a second pass heals nothing new
	rel2, err := s.RelinkIssueRelations(ctx, tgtWS)
	if err != nil {
		t.Fatalf("second RelinkIssueRelations: %v", err)
	}
	if rel2.Parents != 0 || rel2.Projects != 0 {
		t.Fatalf("second relink parents=%d projects=%d, want 0/0 (idempotent)", rel2.Parents, rel2.Projects)
	}
}

// TECH-3766: a blocks/blocked_by/related dependency between two copied issues
// must travel; a dependency pointing at an issue that was NOT copied is dropped
// (never points back into the source). The relink pass is idempotent.
func TestRelinkIssueRelations_Dependencies(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	runID := newID()

	srcA := newID()   // blocks srcB (both copied → must carry)
	srcB := newID()   // blocked_by srcA
	srcOut := newID()  // related to an issue we will NOT copy → must be dropped
	for _, seed := range []struct {
		id  pgtype.UUID
		num int32
	}{{srcA, 30}, {srcB, 31}, {srcOut, 32}} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO issue (id, workspace_id, title, status, priority, kind, creator_type, creator_id, number, position)
			VALUES ($1,$2,'DepIssue','todo','none','issue','member',$3,$4,1)`,
			seed.id, srcWS, testUser, seed.num); err != nil {
			t.Fatalf("seed issue: %v", err)
		}
	}
	// srcA blocks srcB; srcOut related to srcA's UNcopied counterpart srcOnly.
	srcOnly := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (id, workspace_id, title, status, priority, kind, creator_type, creator_id, number, position)
		VALUES ($1,$2,'Uncopied','todo','none','issue','member',$3,33,1)`,
		srcOnly, srcWS, testUser); err != nil {
		t.Fatalf("seed uncopied: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_dependency (id, issue_id, depends_on_issue_id, type) VALUES
		(gen_random_uuid(),$1,$2,'blocks'),
		(gen_random_uuid(),$3,$4,'related')`,
		srcA, srcB, srcOut, srcOnly); err != nil {
		t.Fatalf("seed dependency: %v", err)
	}

	// copy A, B and Out — but NOT srcOnly
	aRes, err := s.CopyIssue(ctx, runID, tgtWS, srcA)
	if err != nil {
		t.Fatalf("copy A: %v", err)
	}
	bRes, err := s.CopyIssue(ctx, runID, tgtWS, srcB)
	if err != nil {
		t.Fatalf("copy B: %v", err)
	}
	if _, err := s.CopyIssue(ctx, runID, tgtWS, srcOut); err != nil {
		t.Fatalf("copy Out: %v", err)
	}

	rel, err := s.RelinkIssueRelations(ctx, tgtWS)
	if err != nil {
		t.Fatalf("RelinkIssueRelations: %v", err)
	}
	// only the A->B 'blocks' dependency has both ends copied; the related one to
	// the uncopied issue must be skipped.
	if rel.Dependencies != 1 {
		t.Fatalf("dependencies relinked=%d, want 1 (the A->B blocks; the related-to-uncopied dropped)", rel.Dependencies)
	}
	var depType string
	if err := testPool.QueryRow(ctx,
		`SELECT type FROM issue_dependency WHERE issue_id=$1 AND depends_on_issue_id=$2`,
		aRes.TargetID, bRes.TargetID).Scan(&depType); err != nil {
		t.Fatalf("copied dependency missing: %v", err)
	}
	if depType != "blocks" {
		t.Fatalf("copied dependency type=%q, want blocks", depType)
	}

	// idempotent: a second pass adds nothing
	rel2, err := s.RelinkIssueRelations(ctx, tgtWS)
	if err != nil {
		t.Fatalf("second relink: %v", err)
	}
	if rel2.Dependencies != 0 {
		t.Fatalf("second relink dependencies=%d, want 0 (idempotent)", rel2.Dependencies)
	}
}

func TestCopyAgent_Parked_NameConflict(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)

	// happy path: a fresh agent copies "parked" (runtime_id null, status offline)
	srcAgent := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent (id, workspace_id, name, runtime_mode, owner_id, instructions)
		VALUES ($1,$2,'Karen','local',$3,'be helpful')`,
		srcAgent, srcWS, testUser); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	res, err := s.CopyAgent(ctx, newID(), tgtWS, srcAgent, conflictSkip)
	if err != nil {
		t.Fatalf("CopyAgent (regression: the bug was a non-existent permission_policy column): %v", err)
	}
	var runtimeID pgtype.UUID
	var status, name string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id, status, name FROM agent WHERE id = $1`, res.TargetID).
		Scan(&runtimeID, &status, &name); err != nil {
		t.Fatalf("load target agent: %v", err)
	}
	if runtimeID.Valid || status != "offline" || name != "Karen" {
		t.Fatalf("target agent runtime=%v status=%q name=%q, want null/offline/Karen", runtimeID.Valid, status, name)
	}

	// name conflict + policy. For each policy seed a target agent and a clashing
	// source agent with the same name (a name can exist once per workspace).
	mkClash := func(name string) (src, tgt pgtype.UUID) {
		tgt = newID()
		if _, err := testPool.Exec(ctx, `INSERT INTO agent (id, workspace_id, name, runtime_mode) VALUES ($1,$2,$3,'local')`,
			tgt, tgtWS, name); err != nil {
			t.Fatalf("seed target agent %q: %v", name, err)
		}
		src = newID()
		if _, err := testPool.Exec(ctx, `INSERT INTO agent (id, workspace_id, name, runtime_mode) VALUES ($1,$2,$3,'local')`,
			src, srcWS, name); err != nil {
			t.Fatalf("seed source agent %q: %v", name, err)
		}
		return src, tgt
	}

	// skip: reuse the existing target agent, create no new row.
	skipSrc, skipTgt := mkClash("Kathrine")
	skipRes, err := s.CopyAgent(ctx, newID(), tgtWS, skipSrc, conflictSkip)
	if err != nil {
		t.Fatalf("CopyAgent skip: %v", err)
	}
	if skipRes.TargetID != skipTgt || !skipRes.AlreadyDone {
		t.Fatalf("skip policy: target=%v alreadyDone=%v, want existing/true", skipRes.TargetID, skipRes.AlreadyDone)
	}

	// keep_both: create a new suffixed agent.
	keepSrc, _ := mkClash("Kristian")
	keepRes, err := s.CopyAgent(ctx, newID(), tgtWS, keepSrc, conflictKeepBoth)
	if err != nil {
		t.Fatalf("CopyAgent keep_both: %v", err)
	}
	var keepName string
	if err := testPool.QueryRow(ctx, `SELECT name FROM agent WHERE id = $1`, keepRes.TargetID).Scan(&keepName); err != nil {
		t.Fatalf("load keep_both agent: %v", err)
	}
	if keepName != "Kristian (2)" {
		t.Fatalf("keep_both name=%q, want \"Kristian (2)\"", keepName)
	}
}

func TestCopyProject_Members(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)

	srcProject := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO project (id, workspace_id, title, status) VALUES ($1,$2,'Roadmap','in_progress')`,
		srcProject, srcWS); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO project_member (workspace_id, project_id, user_id) VALUES ($1,$2,$3)`,
		srcWS, srcProject, testUser); err != nil {
		t.Fatalf("seed project member: %v", err)
	}
	res, err := s.CopyProject(ctx, newID(), tgtWS, srcProject)
	if err != nil {
		t.Fatalf("CopyProject: %v", err)
	}
	var memberCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM project_member WHERE project_id = $1`, res.TargetID).Scan(&memberCount); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if memberCount != 1 {
		t.Fatalf("copied project members = %d, want 1", memberCount)
	}
}

func TestCopyChat_RequiresAgentFirst(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)

	srcAgent := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO agent (id, workspace_id, name, runtime_mode) VALUES ($1,$2,'ChatBot','local')`,
		srcAgent, srcWS); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	srcChat := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO chat_session (id, workspace_id, agent_id, creator_id, title, status) VALUES ($1,$2,$3,$4,'A chat','active')`,
		srcChat, srcWS, srcAgent, testUser); err != nil {
		t.Fatalf("seed chat: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := testPool.Exec(ctx, `INSERT INTO chat_message (id, chat_session_id, role, content) VALUES ($1,$2,'user',$3)`,
			newID(), srcChat, fmt.Sprintf("msg %d", i)); err != nil {
			t.Fatalf("seed message: %v", err)
		}
	}

	// copying the chat before its agent must fail loudly
	if _, err := s.CopyChat(ctx, newID(), tgtWS, srcChat); err == nil {
		t.Fatalf("CopyChat before agent succeeded, want ErrAgentNotCopied")
	}
	// copy the agent, then the chat carries all its messages
	if _, err := s.CopyAgent(ctx, newID(), tgtWS, srcAgent, conflictSkip); err != nil {
		t.Fatalf("CopyAgent: %v", err)
	}
	res, err := s.CopyChat(ctx, newID(), tgtWS, srcChat)
	if err != nil {
		t.Fatalf("CopyChat: %v", err)
	}
	if res.Comments != 3 {
		t.Fatalf("copied chat messages = %d, want 3", res.Comments)
	}
}

func TestCopyAutopilot_TriggersDisarmed(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)

	srcAgent := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO agent (id, workspace_id, name, runtime_mode) VALUES ($1,$2,'Piloter','local')`,
		srcAgent, srcWS); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	srcAuto := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO autopilot (id, workspace_id, title, status, execution_mode, created_by_type, created_by_id, assignee_type, assignee_id, scope)
		VALUES ($1,$2,'Nightly','active','run_only','member',$3,'agent',$4,'workspace')`,
		srcAuto, srcWS, testUser, srcAgent); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO autopilot_trigger (id, autopilot_id, kind, enabled, provider, webhook_token)
		VALUES ($1,$2,'webhook',true,'github','live-secret-token')`,
		newID(), srcAuto); err != nil {
		t.Fatalf("seed trigger: %v", err)
	}

	// agent must be copied first so the assignee remaps
	if _, err := s.CopyAgent(ctx, newID(), tgtWS, srcAgent, conflictSkip); err != nil {
		t.Fatalf("CopyAgent: %v", err)
	}
	res, err := s.CopyAutopilot(ctx, newID(), tgtWS, srcAuto)
	if err != nil {
		t.Fatalf("CopyAutopilot: %v", err)
	}
	// the copied trigger must be disarmed with no webhook token
	var enabled bool
	var token pgtype.Text
	if err := testPool.QueryRow(ctx, `SELECT enabled, webhook_token FROM autopilot_trigger WHERE autopilot_id = $1`, res.TargetID).
		Scan(&enabled, &token); err != nil {
		t.Fatalf("load copied trigger: %v", err)
	}
	if enabled || token.Valid {
		t.Fatalf("copied trigger enabled=%v token.valid=%v, want disabled + no token", enabled, token.Valid)
	}
}
