package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/automation"
	"github.com/multica-ai/multica/server/internal/domainevent"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Deterministic regressions for the workspace-teardown / Event-Hooks-write race
// (MUL-4332 review).
//
// The six Event Hooks tables carry no FK to workspace (the workspace DB rule
// forbids FKs), so they get none of the implicit FOR KEY SHARE protection the
// CASCADE-backed tables rely on. Without the explicit protocol a writer commits
// inside DeleteWorkspace's window — the automation sweep runs, sees nothing, and
// the writer's rows outlive their workspace as orphans. The reviewer's repro was:
//
//  1. hook-create transaction takes member FOR SHARE, then pauses;
//  2. delete transaction takes workspace FOR UPDATE, sweeps automation (0 rows),
//     then blocks deleting member;
//  3. hook transaction writes hook + revision and commits;
//  4. delete proceeds — leaving workspace=0, member=0, hook=1, revision=1.
//
// The fix makes every automation writer take LockWorkspaceForAutomationWrite
// (FOR KEY SHARE) FIRST, which conflicts with the delete's FOR UPDATE and fixes a
// consistent lock order. These tests drive that exact interleaving.

// teardownFixture is one workspace with a member, plus a neighbour workspace used
// to prove tenant isolation.
type teardownFixture struct {
	pool     *pgxpool.Pool
	svc      *HookService
	ws       string
	userID   string
	issueID  string
	neighbor string
	// Child rows have no workspace_id; their ids are tracked so the residue check
	// still finds them once their parent is deleted.
	trackedRevisions map[string][]string
	trackedEffects   map[string][]string
}

func newTeardownFixture(t *testing.T) teardownFixture {
	t.Helper()
	pool := newTaskClaimRacePool(t) // skips if no DB
	ws, userID, _, issueID := seedAttributionFixture(t, pool)

	var neighbor string
	suffix := time.Now().UnixNano()
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO workspace (name, slug) VALUES ('neighbor ws', $1) RETURNING id`,
		fmt.Sprintf("neighbor-%d", suffix)).Scan(&neighbor); err != nil {
		t.Fatalf("seed neighbor workspace: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		pool.Exec(bg, `DELETE FROM hook_action_effect WHERE execution_id IN (SELECT id FROM hook_execution WHERE workspace_id = $1)`, neighbor)
		pool.Exec(bg, `DELETE FROM hook_execution WHERE workspace_id = $1`, neighbor)
		pool.Exec(bg, `DELETE FROM hook_revision WHERE hook_id IN (SELECT id FROM hook WHERE workspace_id = $1)`, neighbor)
		pool.Exec(bg, `DELETE FROM hook WHERE workspace_id = $1`, neighbor)
		pool.Exec(bg, `DELETE FROM automation_state WHERE workspace_id = $1`, neighbor)
		pool.Exec(bg, `DELETE FROM domain_event WHERE workspace_id = $1`, neighbor)
		pool.Exec(bg, `DELETE FROM workspace WHERE id = $1`, neighbor)
	})

	return teardownFixture{
		pool: pool, svc: NewHookService(db.New(pool), pool, nil, hookFlags(true, true)),
		ws: ws, userID: userID, issueID: issueID, neighbor: neighbor,
		trackedRevisions: map[string][]string{},
		trackedEffects:   map[string][]string{},
	}
}

// seedAllSixTables writes one row into each of the six Event Hooks tables for a
// workspace, bypassing the service so it works for the neighbour too.
func (f teardownFixture) seedAllSixTables(t *testing.T, workspaceID string) {
	t.Helper()
	ctx := context.Background()
	hookID, revID, execID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	var eventID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO domain_event (workspace_id, type, schema_version, subject_type, subject_id, actor_type, payload, correlation_id, hop_count)
		VALUES ($1, 'issue.status_changed', 1, 'issue', gen_random_uuid(), 'system', '{}'::jsonb, gen_random_uuid(), 0)
		RETURNING id`, workspaceID).Scan(&eventID); err != nil {
		t.Fatalf("seed domain_event: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO hook (id, workspace_id, name, enabled, active_revision_id, scope_type, origin, creator_actor_type, creator_actor_id, authorization_principal_user_id)
		VALUES ($1, $2, 'teardown hook', true, $3, 'workspace', 'user', 'member', $4, $4)`,
		hookID, workspaceID, revID, f.userID); err != nil {
		t.Fatalf("seed hook: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO hook_revision (id, hook_id, revision, event_type, match, conditions, fire_mode, actions, created_by_type, created_by_id)
		VALUES ($1, $2, 1, 'issue.status_changed', '{}'::jsonb, '[]'::jsonb, 'per_event', '[]'::jsonb, 'member', $3)`,
		revID, hookID, f.userID); err != nil {
		t.Fatalf("seed hook_revision: %v", err)
	}
	f.trackRevision(workspaceID, revID)
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO hook_execution (id, workspace_id, hook_id, hook_revision_id, event_id, correlation_id, status)
		VALUES ($1, $2, $3, $4, $5, gen_random_uuid(), 'queued')`,
		execID, workspaceID, hookID, revID, eventID); err != nil {
		t.Fatalf("seed hook_execution: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO hook_action_effect (effect_key, execution_id, action_index, action_type, status)
		VALUES ($1, $2, 0, 'set_issue_status', 'succeeded')`,
		"teardown:"+execID, execID); err != nil {
		t.Fatalf("seed hook_action_effect: %v", err)
	}
	f.trackEffect(workspaceID, "teardown:"+execID)
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO automation_state (workspace_id, state_kind, state_key, state)
		VALUES ($1, 'hook_edge', $2, '{"satisfied":true}'::jsonb)`,
		workspaceID, hookID); err != nil {
		t.Fatalf("seed automation_state: %v", err)
	}
}

// automationRowCounts returns the row count of each of the six tables for a
// workspace.
//
// hook_revision and hook_action_effect carry no workspace_id, so they must NOT be
// counted only through a surviving parent: once the parent hook/execution is gone an
// orphaned child is unreachable that way and silently counts as 0 — precisely the
// residue these tests exist to catch (review: "零残留断言须能识别孤儿子行"). They are
// therefore counted by the child ids recorded when the fixture created them, which
// stays correct after the parent disappears.
func (f teardownFixture) automationRowCounts(t *testing.T, workspaceID string) map[string]int {
	t.Helper()
	ctx := context.Background()
	out := map[string]int{}
	queries := map[string]string{
		"domain_event":     `SELECT count(*) FROM domain_event WHERE workspace_id = $1`,
		"hook":             `SELECT count(*) FROM hook WHERE workspace_id = $1`,
		"hook_execution":   `SELECT count(*) FROM hook_execution WHERE workspace_id = $1`,
		"automation_state": `SELECT count(*) FROM automation_state WHERE workspace_id = $1`,
		// Reachable-through-parent counts, kept as a floor. The orphan-safe counts
		// below are the authoritative ones.
		"hook_revision":      `SELECT count(*) FROM hook_revision WHERE hook_id IN (SELECT id FROM hook WHERE workspace_id = $1)`,
		"hook_action_effect": `SELECT count(*) FROM hook_action_effect WHERE execution_id IN (SELECT id FROM hook_execution WHERE workspace_id = $1)`,
	}
	for name, q := range queries {
		var n int
		if err := f.pool.QueryRow(ctx, q, workspaceID).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		out[name] = n
	}

	// Orphan-safe: count the child rows this workspace ever created, by id.
	if ids := f.trackedRevisions[workspaceID]; len(ids) > 0 {
		var n int
		if err := f.pool.QueryRow(ctx,
			`SELECT count(*) FROM hook_revision WHERE id = ANY($1::uuid[])`, ids).Scan(&n); err != nil {
			t.Fatalf("count tracked hook_revision: %v", err)
		}
		if n > out["hook_revision"] {
			out["hook_revision"] = n
		}
	}
	if keys := f.trackedEffects[workspaceID]; len(keys) > 0 {
		var n int
		if err := f.pool.QueryRow(ctx,
			`SELECT count(*) FROM hook_action_effect WHERE effect_key = ANY($1::text[])`, keys).Scan(&n); err != nil {
			t.Fatalf("count tracked hook_action_effect: %v", err)
		}
		if n > out["hook_action_effect"] {
			out["hook_action_effect"] = n
		}
	}
	return out
}

// trackRevision / trackEffect record child ids so the residue check can find them
// after their parent row is gone.
func (f teardownFixture) trackRevision(workspaceID, revisionID string) {
	f.trackedRevisions[workspaceID] = append(f.trackedRevisions[workspaceID], revisionID)
}

func (f teardownFixture) trackEffect(workspaceID, effectKey string) {
	f.trackedEffects[workspaceID] = append(f.trackedEffects[workspaceID], effectKey)
}

// trackHookRevisions records every revision a hook currently has, so revisions the
// service created (rather than the fixture) are also orphan-checked.
func (f teardownFixture) trackHookRevisions(t *testing.T, workspaceID string) {
	t.Helper()
	rows, err := f.pool.Query(context.Background(),
		`SELECT r.id::text FROM hook_revision r JOIN hook h ON h.id = r.hook_id WHERE h.workspace_id = $1`, workspaceID)
	if err != nil {
		t.Fatalf("track hook revisions: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan revision id: %v", err)
		}
		f.trackRevision(workspaceID, id)
	}
}

// isDeadlock reports whether err is PostgreSQL's serialization deadlock (40P01).
// A deadlock is a production defect even though PostgreSQL resolves it by killing
// one side: whichever transaction loses fails a real request. These tests therefore
// assert deadlock-freedom, not merely that no residue survives — a reverse lock
// order that deadlocks also leaves no residue, so residue alone cannot detect it.
func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

func assertNoDeadlock(t *testing.T, err error, side string) {
	t.Helper()
	if isDeadlock(err) {
		t.Fatalf("%s deadlocked (40P01): the automation writer and DeleteWorkspace take "+
			"their locks in opposite orders; the workspace lock must come first: %v", side, err)
	}
}

func (f teardownFixture) assertNoResidue(t *testing.T, workspaceID, label string) {
	t.Helper()
	for table, n := range f.automationRowCounts(t, workspaceID) {
		if n != 0 {
			t.Errorf("%s: %s still has %d row(s) after the workspace was deleted — orphaned automation data", label, table, n)
		}
	}
}

// deleteWorkspaceLikeHandler runs the teardown steps that matter for this race in
// the handler's order: workspace FOR UPDATE, the automation sweep, then member and
// the workspace row. Using the same queries as the handler keeps the protocol under
// test rather than a paraphrase of it.
func (f teardownFixture) deleteWorkspaceLikeHandler(ctx context.Context, workspaceID string) error {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := db.New(f.pool).WithTx(tx)
	wsID := util.MustParseUUID(workspaceID)

	if _, err := qtx.LockWorkspaceForDelete(ctx, wsID); err != nil {
		return err
	}
	if err := qtx.DeleteWorkspaceAutomation(ctx, wsID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1`, workspaceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1`, workspaceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (f teardownFixture) hookSpec(name string) automation.HookSpec {
	return automation.HookSpec{
		Name: name,
		When: automation.WhenSpec{Event: "issue.status_changed", Match: []byte(`{"to":"done"}`)},
		Fire: automation.FireSpec{Mode: automation.FirePerEvent},
		Do:   []automation.ActionSpec{{Type: automation.ActionSetIssueStatus, IssueID: f.issueID, Status: "done"}},
	}
}

// A hook create racing a workspace delete must not leave the hook behind. This is
// the reviewer's exact interleaving: the delete holds workspace FOR UPDATE while the
// create is in flight, so the create must block on the workspace lock and then fail
// closed, rather than slipping past the sweep.
//
// This case is genuinely exposed without the protocol: a create has no pre-existing
// row for the sweep to contend on, so nothing else blocks it. Removing the workspace
// lock makes this test fail with the reviewer's exact result — the hook commits after
// the sweep and outlives its workspace.
func TestHookCreateRacingWorkspaceDeleteLeavesNoResidue(t *testing.T) {
	f := newTeardownFixture(t)
	ctx := context.Background()

	// The delete transaction takes workspace FOR UPDATE and pauses there, holding
	// the window open exactly as the reviewer's repro did.
	deleteTx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTx.Rollback(ctx)
	dqtx := db.New(f.pool).WithTx(deleteTx)
	wsID := util.MustParseUUID(f.ws)
	if _, err := dqtx.LockWorkspaceForDelete(ctx, wsID); err != nil {
		t.Fatalf("lock workspace for delete: %v", err)
	}
	if err := dqtx.DeleteWorkspaceAutomation(ctx, wsID); err != nil {
		t.Fatalf("sweep automation: %v", err)
	}

	// The create runs concurrently; it must NOT commit while the delete holds the
	// workspace lock.
	created := make(chan error, 1)
	go func() {
		_, err := f.svc.CreateHook(ctx, wsID, f.hookSpec("racing hook"), HookAuthor{
			ActorType: "member", ActorID: util.MustParseUUID(f.userID),
			PrincipalUserID: util.MustParseUUID(f.userID),
		})
		created <- err
	}()

	select {
	case err := <-created:
		t.Fatalf("CreateHook completed (err=%v) while the delete held the workspace lock — "+
			"it did not join the teardown protocol and can commit an orphaned hook", err)
	case <-time.After(750 * time.Millisecond):
		// Expected: blocked on the workspace FOR KEY SHARE.
	}

	// Finish the delete. The blocked create then resumes and must fail closed.
	if _, err := deleteTx.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1`, f.ws); err != nil {
		t.Fatal(err)
	}
	if _, err := deleteTx.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1`, f.ws); err != nil {
		t.Fatal(err)
	}
	if _, err := deleteTx.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, f.ws); err != nil {
		t.Fatal(err)
	}
	if err := deleteTx.Commit(ctx); err != nil {
		t.Fatalf("commit delete: %v", err)
	}

	select {
	case err := <-created:
		if err == nil {
			t.Error("CreateHook succeeded against a deleted workspace")
		} else if !errors.Is(err, ErrWorkspaceGone) && !errors.Is(err, ErrHookNoPrincipal) {
			t.Logf("CreateHook failed as expected: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CreateHook never unblocked after the delete committed")
	}

	f.assertNoResidue(t, f.ws, "hook create race")
}

// The same protocol must hold for an update, which appends a hook_revision row.
//
// NOTE on what this proves: unlike the create and outbox cases below, an update is
// ALSO protected incidentally — it takes the hook row FOR UPDATE, and the sweep has
// already deleted that row uncommitted, so it blocks there even without the
// workspace lock. This test therefore pins the end-to-end outcome (no residue), not
// the workspace lock in isolation; the explicit lock makes that protection a stated
// protocol instead of a side effect of which rows happen to be locked.
func TestHookUpdateRacingWorkspaceDeleteLeavesNoResidue(t *testing.T) {
	f := newTeardownFixture(t)
	ctx := context.Background()
	wsID := util.MustParseUUID(f.ws)
	author := HookAuthor{
		ActorType: "member", ActorID: util.MustParseUUID(f.userID),
		PrincipalUserID: util.MustParseUUID(f.userID),
	}

	existing, err := f.svc.CreateHook(ctx, wsID, f.hookSpec("hook to update"), author)
	if err != nil {
		t.Fatalf("seed hook: %v", err)
	}
	f.trackHookRevisions(t, f.ws)

	deleteTx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTx.Rollback(ctx)
	dqtx := db.New(f.pool).WithTx(deleteTx)
	if _, err := dqtx.LockWorkspaceForDelete(ctx, wsID); err != nil {
		t.Fatal(err)
	}
	if err := dqtx.DeleteWorkspaceAutomation(ctx, wsID); err != nil {
		t.Fatal(err)
	}

	updated := make(chan error, 1)
	go func() {
		spec := f.hookSpec("hook to update")
		spec.Name = "renamed during teardown"
		_, err := f.svc.UpdateHook(ctx, wsID, existing.Hook.ID, spec, author)
		updated <- err
	}()

	select {
	case err := <-updated:
		t.Fatalf("UpdateHook completed (err=%v) while the delete held the workspace lock — "+
			"it can append an orphaned hook_revision", err)
	case <-time.After(750 * time.Millisecond):
	}

	for _, stmt := range []string{
		`DELETE FROM member WHERE workspace_id = $1`,
		`DELETE FROM issue WHERE workspace_id = $1`,
		`DELETE FROM workspace WHERE id = $1`,
	} {
		if _, err := deleteTx.Exec(ctx, stmt, f.ws); err != nil {
			t.Fatal(err)
		}
	}
	if err := deleteTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updated:
	case <-time.After(5 * time.Second):
		t.Fatal("UpdateHook never unblocked")
	}

	f.assertNoResidue(t, f.ws, "hook update race")
}

// A domain-event write (the always-on outbox producer, independent of any feature
// flag) racing a workspace delete must not leave an event behind.
//
// Like the create case, this is genuinely exposed without the protocol: domain_event
// has no FK and the writer takes no row lock the sweep contends on, so without the
// workspace lock the event commits after the sweep and is orphaned. Removing the lock
// makes this test fail.
func TestDomainEventWriteRacingWorkspaceDeleteLeavesNoResidue(t *testing.T) {
	f := newTeardownFixture(t)
	ctx := context.Background()
	wsID := util.MustParseUUID(f.ws)

	deleteTx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTx.Rollback(ctx)
	dqtx := db.New(f.pool).WithTx(deleteTx)
	if _, err := dqtx.LockWorkspaceForDelete(ctx, wsID); err != nil {
		t.Fatal(err)
	}
	if err := dqtx.DeleteWorkspaceAutomation(ctx, wsID); err != nil {
		t.Fatal(err)
	}

	written := make(chan error, 1)
	go func() {
		// A REAL fact + event: the callback locks the issue row and updates it, exactly
		// as the production issue-update path does. That row lock is what made the old
		// writer's order fact -> workspace (the reverse of the delete's) and deadlock;
		// a callback that only returned an event would not exercise it at all.
		written <- domainevent.WriteInTx(ctx, f.pool, db.New(f.pool), wsID, func(qtx *db.Queries) ([]domainevent.Event, error) {
			locked, err := qtx.LockIssueRowForUpdate(ctx, db.LockIssueRowForUpdateParams{
				ID: util.MustParseUUID(f.issueID), WorkspaceID: wsID,
			})
			if err != nil {
				return nil, err
			}
			updated, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
				ID: util.MustParseUUID(f.issueID), WorkspaceID: wsID, Status: "done",
			})
			if err != nil {
				return nil, err
			}
			return []domainevent.Event{
				domainevent.IssueStatusChanged(wsID, updated.ID, domainevent.SystemActor(),
					domainevent.IssueStatusChangedPayload{From: locked.Status, To: updated.Status}),
			}, nil
		})
	}()

	select {
	case err := <-written:
		t.Fatalf("the outbox write completed (err=%v) while the delete held the workspace "+
			"lock — a domain event can outlive its workspace", err)
	case <-time.After(750 * time.Millisecond):
	}

	for _, stmt := range []string{
		`DELETE FROM member WHERE workspace_id = $1`,
		`DELETE FROM issue WHERE workspace_id = $1`,
		`DELETE FROM workspace WHERE id = $1`,
	} {
		_, err := deleteTx.Exec(ctx, stmt, f.ws)
		assertNoDeadlock(t, err, "the delete transaction")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := deleteTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-written:
		// The write must fail because the workspace is gone — NOT because the two
		// transactions deadlocked on opposite lock orders.
		assertNoDeadlock(t, err, "the outbox write")
		if err == nil {
			t.Error("the outbox write succeeded against a deleted workspace")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the outbox write never unblocked")
	}

	f.assertNoResidue(t, f.ws, "domain event race")
}

// The matcher's decision transaction writes hook_execution / automation_state and
// claims a domain_event row; it must take the workspace lock BEFORE that claim.
//
// Interleaving matters here. The matcher must claim FIRST and only then reach for the
// workspace: that is the order in which a reverse-ordered matcher deadlocks against a
// teardown (matcher holds domain_event, wants workspace; delete holds workspace,
// wants domain_event). A trigger holds the matcher inside its claim statement so the
// delete reliably starts in that window. Driven through the PRODUCTION ClaimAndMatch
// entry point — the old test used MatchEvent, which happens to lock the workspace
// first and so never exercised the claim path at all.
func TestMatcherWriteRacingWorkspaceDeleteLeavesNoResidue(t *testing.T) {
	f := newTeardownFixture(t)
	ctx := context.Background()
	wsID := util.MustParseUUID(f.ws)

	// Drain other tests' events so this tick reaches ours.
	for i := 0; i < 60; i++ {
		n, err := f.svc.ClaimAndMatch(ctx, 500)
		if err != nil {
			t.Fatalf("drain: %v", err)
		}
		if n == 0 {
			break
		}
	}

	if _, err := f.svc.CreateHook(ctx, wsID, f.hookSpec("matcher hook"), HookAuthor{
		ActorType: "member", ActorID: util.MustParseUUID(f.userID),
		PrincipalUserID: util.MustParseUUID(f.userID),
	}); err != nil {
		t.Fatalf("seed hook: %v", err)
	}
	f.trackHookRevisions(t, f.ws)

	var eventID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO domain_event (workspace_id, type, schema_version, subject_type, subject_id, actor_type, actor_id, payload, correlation_id, hop_count)
		VALUES ($1, 'issue.status_changed', 1, 'issue', $2, 'member', $3, '{"from":"todo","to":"done"}'::jsonb, gen_random_uuid(), 0)
		RETURNING id`, f.ws, f.issueID, f.userID).Scan(&eventID); err != nil {
		t.Fatalf("seed domain_event: %v", err)
	}

	// Hold the matcher inside its claim statement, so the delete below starts while
	// the matcher owns the domain_event row.
	f.sleepDuringEventClaim(t, eventID, 1.0)

	matched := make(chan error, 1)
	go func() {
		_, err := f.svc.ClaimAndMatch(ctx, 5)
		matched <- err
	}()
	time.Sleep(300 * time.Millisecond) // let the matcher get into its claim

	// Now run the teardown. A reverse-ordered matcher deadlocks with this.
	deleteTx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTx.Rollback(ctx)
	dqtx := db.New(f.pool).WithTx(deleteTx)
	if _, err := dqtx.LockWorkspaceForDelete(ctx, wsID); err != nil {
		assertNoDeadlock(t, err, "the delete transaction")
		t.Fatal(err)
	}
	if err := dqtx.DeleteWorkspaceAutomation(ctx, wsID); err != nil {
		assertNoDeadlock(t, err, "the delete transaction")
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`DELETE FROM member WHERE workspace_id = $1`,
		`DELETE FROM issue WHERE workspace_id = $1`,
		`DELETE FROM workspace WHERE id = $1`,
	} {
		_, err := deleteTx.Exec(ctx, stmt, f.ws)
		assertNoDeadlock(t, err, "the delete transaction")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := deleteTx.Commit(ctx); err != nil {
		assertNoDeadlock(t, err, "the delete commit")
		t.Fatal(err)
	}

	select {
	case err := <-matched:
		assertNoDeadlock(t, err, "the matcher")
	case <-time.After(10 * time.Second):
		t.Fatal("the matcher never returned")
	}

	f.assertNoResidue(t, f.ws, "matcher race")
}

// sleepDuringEventClaim makes the matcher's claim UPDATE on one event pause, so a
// test can start a teardown while the matcher holds that event's row lock.
func (f teardownFixture) sleepDuringEventClaim(t *testing.T, eventID string, seconds float64) {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("de_claim_sleep_%d", time.Now().UnixNano())
	fn := name + "_fn"
	if _, err := f.pool.Exec(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_sleep(%f); RETURN NEW; END; $$;`, quoteIdent(fn), seconds)); err != nil {
		t.Fatalf("create claim sleep function: %v", err)
	}
	if _, err := f.pool.Exec(ctx, fmt.Sprintf(`
		CREATE TRIGGER %s BEFORE UPDATE ON domain_event
		FOR EACH ROW WHEN (NEW.id = %s::uuid AND NEW.dispatch_status = 'dispatching')
		EXECUTE FUNCTION %s();`, quoteIdent(name), quoteLiteral(eventID), quoteIdent(fn))); err != nil {
		t.Fatalf("create claim sleep trigger: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		f.pool.Exec(bg, fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON domain_event", quoteIdent(name)))
		f.pool.Exec(bg, fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", quoteIdent(fn)))
		f.pool.Exec(bg, `DELETE FROM domain_event WHERE id = $1`, eventID)
	})
}

// Deleting one workspace must leave an adjacent workspace's automation data — all
// six tables — completely intact.
func TestWorkspaceDeleteLeavesNeighborAutomationIntact(t *testing.T) {
	f := newTeardownFixture(t)
	ctx := context.Background()

	f.seedAllSixTables(t, f.ws)
	f.seedAllSixTables(t, f.neighbor)

	before := f.automationRowCounts(t, f.neighbor)
	for table, n := range before {
		if n != 1 {
			t.Fatalf("neighbour fixture wrong: %s = %d, want 1", table, n)
		}
	}

	if err := f.deleteWorkspaceLikeHandler(ctx, f.ws); err != nil {
		t.Fatalf("delete workspace: %v", err)
	}

	f.assertNoResidue(t, f.ws, "target workspace")
	for table, n := range f.automationRowCounts(t, f.neighbor) {
		if n != 1 {
			t.Errorf("neighbour %s = %d, want 1 — deleting one workspace touched another's automation data", table, n)
		}
	}
}

var _ = pgx.ErrNoRows

// The global matcher must make forward progress past an orphan: an event whose
// workspace was already deleted sits at the head of the queue, and a healthy event
// from a live workspace sits behind it. The orphan is finalized terminally and the
// healthy event is still dispatched in the same tick (review: 全局队列前进性).
func TestClaimAndMatchAdvancesPastOrphanedWorkspaceEvent(t *testing.T) {
	f := newTeardownFixture(t)
	ctx := context.Background()

	// Drain anything left by other tests so ordering here is ours.
	for i := 0; i < 60; i++ {
		n, err := f.svc.ClaimAndMatch(ctx, 500)
		if err != nil {
			t.Fatalf("drain: %v", err)
		}
		if n == 0 {
			break
		}
	}

	// 1. An event in a workspace that is then deleted outright — a true orphan.
	doomed := f.neighbor
	var orphanEventID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO domain_event (workspace_id, type, schema_version, subject_type, subject_id, actor_type, payload, correlation_id, hop_count)
		VALUES ($1, 'issue.status_changed', 1, 'issue', gen_random_uuid(), 'system', '{"from":"todo","to":"done"}'::jsonb, gen_random_uuid(), 0)
		RETURNING id`, doomed).Scan(&orphanEventID); err != nil {
		t.Fatalf("seed orphan event: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, doomed); err != nil {
		t.Fatalf("delete doomed workspace: %v", err)
	}
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM domain_event WHERE id = $1`, orphanEventID)
	})

	// 2. A healthy event BEHIND it, in a live workspace.
	var healthyEventID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO domain_event (workspace_id, type, schema_version, subject_type, subject_id, actor_type, actor_id, payload, correlation_id, hop_count)
		VALUES ($1, 'issue.status_changed', 1, 'issue', $2, 'member', $3, '{"from":"todo","to":"done"}'::jsonb, gen_random_uuid(), 0)
		RETURNING id`, f.ws, f.issueID, f.userID).Scan(&healthyEventID); err != nil {
		t.Fatalf("seed healthy event: %v", err)
	}
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM domain_event WHERE id = $1`, healthyEventID)
	})

	var orphanSeq, healthySeq int64
	f.pool.QueryRow(ctx, `SELECT seq FROM domain_event WHERE id = $1`, orphanEventID).Scan(&orphanSeq)
	f.pool.QueryRow(ctx, `SELECT seq FROM domain_event WHERE id = $1`, healthyEventID).Scan(&healthySeq)
	if orphanSeq >= healthySeq {
		t.Fatalf("fixture wrong: orphan seq %d must precede healthy seq %d", orphanSeq, healthySeq)
	}

	if _, err := f.svc.ClaimAndMatch(ctx, 50); err != nil {
		t.Fatalf("claim and match: %v", err)
	}

	var orphanStatus, healthyStatus string
	f.pool.QueryRow(ctx, `SELECT dispatch_status FROM domain_event WHERE id = $1`, orphanEventID).Scan(&orphanStatus)
	f.pool.QueryRow(ctx, `SELECT dispatch_status FROM domain_event WHERE id = $1`, healthyEventID).Scan(&healthyStatus)

	if orphanStatus != "failed" {
		t.Errorf("orphaned event dispatch_status = %q, want failed", orphanStatus)
	}
	if healthyStatus != "dispatched" {
		t.Errorf("healthy event behind the orphan = %q, want dispatched — the orphan is "+
			"blocking the head of the global queue", healthyStatus)
	}
}

// Two workers, two workspaces: a slow event in one workspace must NOT stop the other
// workspace's event from being dispatched in the same window (MUL-4332 review:
// convoy). The single-row unlocked peek regressed this — every worker retried the
// same head row and queued behind it, losing the cross-workspace parallel drain the
// original FOR UPDATE SKIP LOCKED provided.
func TestClaimAndMatchDoesNotConvoyBehindABusyWorkspace(t *testing.T) {
	f := newTeardownFixture(t)
	ctx := context.Background()

	for i := 0; i < 60; i++ {
		n, err := f.svc.ClaimAndMatch(ctx, 500)
		if err != nil {
			t.Fatalf("drain: %v", err)
		}
		if n == 0 {
			break
		}
	}

	// Event A (older) in the busy workspace; event B (newer) in the neighbour.
	var slowEventID, healthyEventID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO domain_event (workspace_id, type, schema_version, subject_type, subject_id, actor_type, actor_id, payload, correlation_id, hop_count)
		VALUES ($1, 'issue.status_changed', 1, 'issue', $2, 'member', $3, '{"from":"todo","to":"done"}'::jsonb, gen_random_uuid(), 0)
		RETURNING id`, f.ws, f.issueID, f.userID).Scan(&slowEventID); err != nil {
		t.Fatalf("seed slow event: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO domain_event (workspace_id, type, schema_version, subject_type, subject_id, actor_type, payload, correlation_id, hop_count)
		VALUES ($1, 'issue.status_changed', 1, 'issue', gen_random_uuid(), 'system', '{"from":"todo","to":"done"}'::jsonb, gen_random_uuid(), 0)
		RETURNING id`, f.neighbor).Scan(&healthyEventID); err != nil {
		t.Fatalf("seed healthy event: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		f.pool.Exec(bg, `DELETE FROM domain_event WHERE id = ANY($1::uuid[])`, []string{slowEventID, healthyEventID})
	})

	var slowSeq, healthySeq int64
	f.pool.QueryRow(ctx, `SELECT seq FROM domain_event WHERE id = $1`, slowEventID).Scan(&slowSeq)
	f.pool.QueryRow(ctx, `SELECT seq FROM domain_event WHERE id = $1`, healthySeq).Scan(&healthySeq)
	f.pool.QueryRow(ctx, `SELECT seq FROM domain_event WHERE id = $1`, healthyEventID).Scan(&healthySeq)
	if slowSeq >= healthySeq {
		t.Fatalf("fixture wrong: slow seq %d must precede healthy seq %d", slowSeq, healthySeq)
	}

	// Worker 1 holds the slow event for a while (a long decision).
	f.sleepDuringEventClaim(t, slowEventID, 1.5)
	worker1 := make(chan struct{})
	go func() {
		defer close(worker1)
		f.svc.ClaimAndMatch(ctx, 5)
	}()
	time.Sleep(400 * time.Millisecond) // let worker 1 take the slow event

	// Worker 2 must skip past it and dispatch the neighbour's event NOW, not wait.
	done := make(chan int, 1)
	go func() {
		n, err := f.svc.ClaimAndMatch(ctx, 5)
		if err != nil {
			t.Errorf("worker 2: %v", err)
		}
		done <- n
	}()

	select {
	case n := <-done:
		if n == 0 {
			t.Error("worker 2 dispatched nothing while worker 1 held an older event — " +
				"it convoyed behind another workspace instead of skipping past it")
		}
	case <-time.After(1200 * time.Millisecond):
		t.Fatal("worker 2 blocked behind worker 1's slow event (convoy): a busy workspace " +
			"is stalling every other workspace's drain")
	}

	var healthyStatus string
	f.pool.QueryRow(ctx, `SELECT dispatch_status FROM domain_event WHERE id = $1`, healthyEventID).Scan(&healthyStatus)
	if healthyStatus != "dispatched" {
		t.Errorf("neighbour event = %q, want dispatched while the busy workspace was still working", healthyStatus)
	}
	<-worker1
}

// A REAL terminal task transition racing a workspace delete must not deadlock.
// CompleteTask writes task.completed through a DIRECT domainevent.Write, so fixing
// WriteInTx alone did not cover it: it locked the task row first, giving
// task -> workspace against the delete's workspace -> task (MUL-4332 review).
func TestCompleteTaskRacingWorkspaceDeleteDoesNotDeadlock(t *testing.T) {
	f := newTeardownFixture(t)
	ctx := context.Background()

	// A running task on the fixture's issue/agent.
	var taskID, agentID string
	if err := f.pool.QueryRow(ctx, `SELECT id::text FROM agent WHERE workspace_id = $1 LIMIT 1`, f.ws).Scan(&agentID); err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id, accountable_user_id, originator_source)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'running', 0, $3, $3, 'delegation')
		RETURNING id`, agentID, f.issueID, f.userID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	taskSvc := &TaskService{Queries: db.New(f.pool), TxStarter: f.pool, Bus: events.New()}

	// Hold the completion inside its transaction so the teardown starts while it is
	// in flight — the interleaving in which a reverse-ordered writer deadlocks.
	f.sleepDuringTaskUpdate(t, taskID, 1.0)

	completed := make(chan error, 1)
	go func() {
		_, err := taskSvc.CompleteTask(ctx, util.MustParseUUID(taskID), []byte(`{}`), "", "", false, "")
		completed <- err
	}()
	time.Sleep(300 * time.Millisecond)

	deleteTx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTx.Rollback(ctx)
	dqtx := db.New(f.pool).WithTx(deleteTx)
	if _, err := dqtx.LockWorkspaceForDelete(ctx, util.MustParseUUID(f.ws)); err != nil {
		assertNoDeadlock(t, err, "the delete transaction")
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1)`,
		`DELETE FROM member WHERE workspace_id = $1`,
		`DELETE FROM issue WHERE workspace_id = $1`,
	} {
		_, err := deleteTx.Exec(ctx, stmt, f.ws)
		assertNoDeadlock(t, err, "the delete transaction")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := deleteTx.Commit(ctx); err != nil {
		assertNoDeadlock(t, err, "the delete commit")
		t.Fatal(err)
	}

	select {
	case err := <-completed:
		// It may fail (its workspace is being torn down) but must NOT deadlock.
		assertNoDeadlock(t, err, "CompleteTask")
	case <-time.After(10 * time.Second):
		t.Fatal("CompleteTask never returned")
	}
}

// sleepDuringTaskUpdate pauses the terminal UPDATE of one task row, so a test can
// start a teardown while the completion transaction holds that row.
func (f teardownFixture) sleepDuringTaskUpdate(t *testing.T, taskID string, seconds float64) {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("task_term_sleep_%d", time.Now().UnixNano())
	fn := name + "_fn"
	if _, err := f.pool.Exec(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_sleep(%f); RETURN NEW; END; $$;`, quoteIdent(fn), seconds)); err != nil {
		t.Fatalf("create task sleep function: %v", err)
	}
	if _, err := f.pool.Exec(ctx, fmt.Sprintf(`
		CREATE TRIGGER %s BEFORE UPDATE OF status ON agent_task_queue
		FOR EACH ROW WHEN (NEW.id = %s::uuid AND NEW.status = 'completed')
		EXECUTE FUNCTION %s();`, quoteIdent(name), quoteLiteral(taskID), quoteIdent(fn))); err != nil {
		t.Fatalf("create task sleep trigger: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		f.pool.Exec(bg, fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON agent_task_queue", quoteIdent(name)))
		f.pool.Exec(bg, fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", quoteIdent(fn)))
	})
}

// A REAL bulk sweep racing a workspace delete must not deadlock, and must still
// drain a healthy workspace in the same tick (MUL-4332 review: bulk deadlock window).
//
// The sweepers are global, so a tick spans workspaces. Locking the candidate TASK
// rows first — what the select-with-FOR-UPDATE did — is the reverse of
// DeleteWorkspace's workspace -> task order. The sweep now peeks unlocked, groups by
// resolved workspace, and runs one workspace-first transaction per group.
func TestBulkSweepRacingWorkspaceDeleteDoesNotDeadlock(t *testing.T) {
	f := newTeardownFixture(t)
	ctx := context.Background()

	// A running task in the workspace being torn down, and one in a healthy
	// neighbour workspace that must still be swept in the same tick.
	var agentID string
	if err := f.pool.QueryRow(ctx, `SELECT id::text FROM agent WHERE workspace_id = $1 LIMIT 1`, f.ws).Scan(&agentID); err != nil {
		t.Fatalf("load agent: %v", err)
	}
	seedRunning := func(issueID string) string {
		var id string
		if err := f.pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
			VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'running', 0, now() - interval '10 days')
			RETURNING id`, agentID, issueID).Scan(&id); err != nil {
			t.Fatalf("seed running task: %v", err)
		}
		t.Cleanup(func() {
			bg := context.Background()
			f.pool.Exec(bg, `DELETE FROM domain_event WHERE subject_id = $1`, id)
			f.pool.Exec(bg, `DELETE FROM agent_task_queue WHERE id = $1`, id)
		})
		return id
	}
	// A neighbour-workspace issue so its task resolves to the neighbour workspace.
	var neighborIssueID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, priority)
		VALUES ($1, 'neighbor issue', 'member', $2, 'medium') RETURNING id`,
		f.neighbor, f.userID).Scan(&neighborIssueID); err != nil {
		t.Fatalf("seed neighbor issue: %v", err)
	}
	t.Cleanup(func() { f.pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, neighborIssueID) })

	doomedTask := seedRunning(f.issueID)
	healthyTask := seedRunning(neighborIssueID)

	taskSvc := &TaskService{Queries: db.New(f.pool), TxStarter: f.pool, Bus: events.New()}

	// Hold the delete's workspace lock open so the sweep runs inside its window.
	deleteTx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTx.Rollback(ctx)
	dqtx := db.New(f.pool).WithTx(deleteTx)
	if _, err := dqtx.LockWorkspaceForDelete(ctx, util.MustParseUUID(f.ws)); err != nil {
		t.Fatal(err)
	}
	// NOTE: the teardown holds ONLY the workspace at this point. That is what makes
	// this test discriminate lock ORDER: a reverse-ordered sweep takes the doomed
	// workspace's TASK locks first (they are still free) and then blocks wanting the
	// workspace, while the teardown below blocks wanting those tasks — a deadlock.
	// The workspace-first sweep never takes those task locks at all.
	swept := make(chan error, 1)
	go func() {
		_, err := taskSvc.FailBulkTasksWithEvents(ctx,
			func(q *db.Queries) ([]db.AgentTaskQueue, error) {
				return q.PeekStaleTasksToFail(ctx, db.PeekStaleTasksToFailParams{
					DispatchedTimeoutSecs: 60,
					RunningTimeoutSecs:    60,
					RuntimeStaleSecs:      60,
					MaxPerTick:            100,
				})
			},
			func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
				return qtx.LockStaleTasksByIDsForFail(ctx, db.LockStaleTasksByIDsForFailParams{
					Ids:                   ids,
					DispatchedTimeoutSecs: 60,
					RunningTimeoutSecs:    60,
					RuntimeStaleSecs:      60,
				})
			},
			func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
				return qtx.FailAgentTasksByIDs(ctx, db.FailAgentTasksByIDsParams{
					Ids:           ids,
					Error:         pgtype.Text{String: "stale", Valid: true},
					FailureReason: pgtype.Text{String: "stale_timeout", Valid: true},
				})
			})
		swept <- err
	}()

	// Give the sweep time to reach the doomed workspace, then have the teardown take
	// its task rows. With a reverse-ordered sweep this is where the deadlock lands.
	time.Sleep(500 * time.Millisecond)
	_, delErr := deleteTx.Exec(ctx,
		`DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1)`, f.ws)
	assertNoDeadlock(t, delErr, "the delete transaction")
	if delErr != nil {
		t.Fatal(delErr)
	}

	select {
	case err := <-swept:
		assertNoDeadlock(t, err, "the bulk sweep")
	case <-time.After(10 * time.Second):
		t.Fatal("the bulk sweep never returned — it blocked on rows the teardown holds")
	}

	// The healthy neighbour's task was still swept while the other workspace was
	// locked: one workspace under teardown must not stall the rest of the tick.
	var healthyStatus string
	f.pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, healthyTask).Scan(&healthyStatus)
	if healthyStatus != "failed" {
		t.Errorf("healthy workspace task = %q, want failed — a workspace under teardown "+
			"stalled the whole global sweep", healthyStatus)
	}

	// Finish the teardown; it must not have deadlocked against the sweep.
	for _, stmt := range []string{
		`DELETE FROM member WHERE workspace_id = $1`,
		`DELETE FROM issue WHERE workspace_id = $1`,
	} {
		_, err := deleteTx.Exec(ctx, stmt, f.ws)
		assertNoDeadlock(t, err, "the delete transaction")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := deleteTx.Commit(ctx); err != nil {
		assertNoDeadlock(t, err, "the delete commit")
		t.Fatal(err)
	}
	_ = doomedTask
}
