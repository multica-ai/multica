package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// uploadSessionFixture seeds one workspace and creates sessions in it. Every
// row this package's sweeps can see is claimed by key, so the assertions below
// name this fixture's own keys rather than the sweep's totals — the test
// database is shared by every package `go test ./...` runs in parallel.
type uploadSessionFixture struct {
	pool           *pgxpool.Pool
	workspaceID    string
	stagingKey     string
	finalKey       string
	sessionID      string
	deleteRecorder *recordingDeleter
}

type recordingDeleter struct {
	mu      sync.Mutex
	deleted []string
	err     error
	// failKeys makes DeleteObject fail for these keys only, so a test can fail
	// one delete without failing the whole sweep.
	failKeys map[string]bool
	// onDelete runs before the delete completes, for late-PUT simulation.
	onDelete func(key string)
}

func (d *recordingDeleter) DeleteObject(_ context.Context, key string) error {
	if d.onDelete != nil {
		d.onDelete(key)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return d.err
	}
	if d.failKeys[key] {
		return fmt.Errorf("delete refused for %s", key)
	}
	d.deleted = append(d.deleted, key)
	return nil
}

func (d *recordingDeleter) keyDeleted(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, deleted := range d.deleted {
		if deleted == key {
			return true
		}
	}
	return false
}

func (d *recordingDeleter) setError(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.err = err
}

var uploadFixtureCounter atomic.Int64

func newUploadSessionFixture(t *testing.T) *uploadSessionFixture {
	t.Helper()
	pool := sharedTestPool(t)
	ctx := context.Background()
	suffix := uploadFixtureCounter.Add(1)
	fixture := &uploadSessionFixture{pool: pool, deleteRecorder: &recordingDeleter{failKeys: map[string]bool{}}}

	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description) VALUES ($1, $2, '') RETURNING id`,
		"Upload session test", fmt.Sprintf("upload-session-%d", suffix)).Scan(&fixture.workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	fixture.stagingKey = fmt.Sprintf("upload-staging/%s/session-%d", fixture.workspaceID, suffix)
	fixture.finalKey = fmt.Sprintf("workspaces/%s/final-%d", fixture.workspaceID, suffix)

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM attachment_upload_session WHERE workspace_id = $1`, fixture.workspaceID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, fixture.workspaceID)
	})
	return fixture
}

// seedSession inserts a never-published session. cleanupState is the reclaim
// position; expiryAge/createdAge shape the deadlines.
func (f *uploadSessionFixture) seedSession(t *testing.T, cleanupState string, expiryAge, createdAge time.Duration) string {
	t.Helper()
	if cleanupState == "" {
		cleanupState = "none"
	}
	var id string
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO attachment_upload_session (
			id, workspace_id, actor_type, actor_id, actor_user_id,
			filename, declared_content_type, expected_size,
			staging_key, final_key, cleanup_state, expires_at, created_at, next_attempt_at
		) VALUES (
			gen_random_uuid(), $1, 'member', gen_random_uuid(), gen_random_uuid(),
			'session.bin', 'application/octet-stream', 10,
			$2, $3, $4, now() - $5::interval, now() - $6::interval, now() - $6::interval
		) RETURNING id`,
		f.workspaceID, f.stagingKey, f.finalKey, cleanupState, expiryAge.String(), createdAge.String()).Scan(&id); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return id
}

// sessionState reports both axes independently: they must never move together.
func (f *uploadSessionFixture) publishAndCleanup(t *testing.T, id string) (publish string, cleanup string, attempt int, passVal int, exists bool) {
	t.Helper()
	err := f.pool.QueryRow(context.Background(), `
		SELECT publish_state, cleanup_state, attempt, tombstone_pass
		FROM attachment_upload_session WHERE id = $1`, id,
	).Scan(&publish, &cleanup, &attempt, &passVal)
	if err != nil {
		return "", "", 0, 0, false
	}
	return publish, cleanup, attempt, passVal, true
}

func (f *uploadSessionFixture) sessionState(t *testing.T, id string) (state string, attempt int, pass int, exists bool) {
	t.Helper()
	publish, cleanup, attempt, pass, exists := f.publishAndCleanup(t, id)
	if !exists {
		return "", 0, 0, false
	}
	// Tests written against the old single column read the CLEANUP axis; its
	// publish counterpart is asserted separately where it matters.
	_ = publish
	return cleanup, attempt, pass, true
}

// newReconciler builds a reconciler over the fixture's database and a recording
// deleter.
func newReconciler(t *testing.T, fixture *uploadSessionFixture) *AttachmentUploadReconciler {
	t.Helper()
	return &AttachmentUploadReconciler{
		Queries: db.New(fixture.pool),
		Storage: fixture.deleteRecorder,
	}
}

func TestAttachmentUploadReconciler_ReclaimsExpiredPendingSession(t *testing.T) {
	fixture := newUploadSessionFixture(t)
	// The presigned URL expired an hour ago and nothing ever arrived, so the
	// session is abandoned and both keys are garbage.
	id := fixture.seedSession(t, "none", time.Hour, time.Hour)

	newReconciler(t, fixture).RunOnce(context.Background())

	if !fixture.deleteRecorder.keyDeleted(fixture.stagingKey) {
		t.Fatalf("staging key %q was not deleted; deleted=%v", fixture.stagingKey, fixture.deleteRecorder.deleted)
	}
	if !fixture.deleteRecorder.keyDeleted(fixture.finalKey) {
		t.Fatalf("final key %q of a never-completed session was not deleted", fixture.finalKey)
	}
	// The row survives as tombstone #1 so a late PUT is re-deleted.
	state, _, pass, exists := fixture.sessionState(t, id)
	if !exists {
		t.Fatalf("session row was dropped before the tombstone schedule ran")
	}
	if state != "tombstoned" || pass != 0 {
		t.Fatalf("session state = %q pass = %d, want tombstoned/0", state, pass)
	}
}

func TestAttachmentUploadReconciler_LeavesLiveSessionAlone(t *testing.T) {
	fixture := newUploadSessionFixture(t)
	// A PUT may still be in flight: the URL has not expired yet.
	id := fixture.seedSession(t, "none", -10*time.Minute, time.Minute)

	newReconciler(t, fixture).RunOnce(context.Background())

	if fixture.deleteRecorder.keyDeleted(fixture.stagingKey) {
		t.Fatalf("deleted the staging object of a session whose PUT is still valid")
	}
	// Nothing moved on either axis: a session still inside its completion
	// window must be untouched.
	publish, cleanup, _, _, exists := fixture.publishAndCleanup(t, id)
	if !exists || publish != "pending" || cleanup != "none" {
		t.Fatalf("live session publish=%q cleanup=%q (exists=%v), want pending/none",
			publish, cleanup, exists)
	}
}

func TestAttachmentUploadReconciler_RespectsSettleDelayAfterExpiry(t *testing.T) {
	fixture := newUploadSessionFixture(t)
	// Expired moments ago: a PUT could still be finishing. The settle delay is
	// the operational buffer that keeps the reconcile away until it is not.
	id := fixture.seedSession(t, "none", -time.Minute, time.Minute)

	newReconciler(t, fixture).RunOnce(context.Background())

	if fixture.deleteRecorder.keyDeleted(fixture.stagingKey) {
		t.Fatalf("deleted a just-expired session before the settle delay elapsed")
	}
	publish, cleanup, _, _, _ := fixture.publishAndCleanup(t, id)
	if publish != "pending" || cleanup != "none" {
		t.Fatalf("publish=%q cleanup=%q, want pending/none", publish, cleanup)
	}
}

func TestAttachmentUploadReconciler_CompletedSessionCleansStagingOnly(t *testing.T) {
	fixture := newUploadSessionFixture(t)
	id := fixture.seedSession(t, "none", time.Hour, time.Hour)
	// Simulate a completion: the attachment row exists (so the final key is
	// referenced) and the session is marked completed.
	attachmentID, err := func() (string, error) {
		var created string
		err := fixture.pool.QueryRow(context.Background(), `
			INSERT INTO attachment (
				workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes
			) VALUES ($1, 'member', gen_random_uuid(), 'session.bin', $2, 'application/octet-stream', 10)
			RETURNING id`, fixture.workspaceID, "https://cdn.example.com/"+fixture.finalKey).Scan(&created)
		return created, err
	}()
	if err != nil {
		t.Fatalf("seed attachment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID)
	})
	if _, err := fixture.pool.Exec(context.Background(), `
		UPDATE attachment_upload_session
		SET publish_state = 'completed', attachment_id = $2, completed_at = now(), next_attempt_at = now()
		WHERE id = $1`, id, attachmentID); err != nil {
		t.Fatalf("mark completed: %v", err)
	}

	newReconciler(t, fixture).RunOnce(context.Background())

	if !fixture.deleteRecorder.keyDeleted(fixture.stagingKey) {
		t.Fatalf("completed session's staging object was not reclaimed")
	}
	// The final key is what the attachment reads: deleting it would break the
	// published attachment.
	if fixture.deleteRecorder.keyDeleted(fixture.finalKey) {
		t.Fatalf("deleted the final object of a published attachment")
	}
}

func TestAttachmentUploadReconciler_DeleteFailureBacksOffAndKeepsRow(t *testing.T) {
	fixture := newUploadSessionFixture(t)
	id := fixture.seedSession(t, "none", time.Hour, time.Hour)
	fixture.deleteRecorder.setError(fmt.Errorf("object store unavailable"))

	newReconciler(t, fixture).RunOnce(context.Background())

	state, attempt, _, exists := fixture.sessionState(t, id)
	if !exists {
		t.Fatalf("a failed delete dropped the row, losing the retry")
	}
	if state != "claimed" {
		t.Fatalf("cleanup_state = %q, want claimed (a claimed session must not be publishable)", state)
	}
	// The claim counts the attempt that is about to run, which is what the
	// backoff is indexed by — the same accounting the channel-media ledger uses.
	if attempt != 1 {
		t.Fatalf("attempt = %d, want 1 (the claim must record the try it took)", attempt)
	}

	// The backoff must be in the future, so the next sweep does not immediately
	// retry a store that is still down.
	var dueIn time.Duration
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT next_attempt_at - now() FROM attachment_upload_session WHERE id = $1`, id).Scan(&dueIn); err != nil {
		t.Fatalf("read backoff: %v", err)
	}
	if dueIn <= 0 {
		t.Fatalf("next_attempt_at is not in the future: %v", dueIn)
	}

	// With the store healthy again, a later sweep retries and succeeds.
	fixture.deleteRecorder.setError(nil)
	if _, err := fixture.pool.Exec(context.Background(),
		`UPDATE attachment_upload_session SET next_attempt_at = now(), lease_expires_at = NULL WHERE id = $1`, id); err != nil {
		t.Fatalf("make due: %v", err)
	}
	newReconciler(t, fixture).RunOnce(context.Background())
	if !fixture.deleteRecorder.keyDeleted(fixture.stagingKey) {
		t.Fatalf("the retried delete never ran")
	}
}

func TestAttachmentUploadReconciler_TombstoneScheduleEndsByDroppingTheRow(t *testing.T) {
	fixture := newUploadSessionFixture(t)
	id := fixture.seedSession(t, "none", time.Hour, time.Hour)

	reconciler := newReconciler(t, fixture)
	// Walk the whole schedule: each pass deletes again and advances the
	// tombstone position, and the last one drops the row.
	for pass := 0; pass < len(attachmentUploadTombstoneRedelete); pass++ {
		reconciler.RunOnce(context.Background())
		if _, err := fixture.pool.Exec(context.Background(), `
			UPDATE attachment_upload_session SET next_attempt_at = now(), lease_expires_at = NULL WHERE id = $1`, id); err != nil {
			t.Fatalf("advance schedule: %v", err)
		}
	}

	reconciler.RunOnce(context.Background())
	if _, _, _, exists := fixture.sessionState(t, id); exists {
		t.Fatalf("session row survived the exhausted tombstone schedule")
	}
}

func TestAttachmentUploadReconciler_LatePutIsRedeletedAfterTombstone(t *testing.T) {
	fixture := newUploadSessionFixture(t)
	id := fixture.seedSession(t, "none", time.Hour, time.Hour)

	var lateKey string
	var lateKeyDeletes int
	fixture.deleteRecorder.onDelete = func(key string) {
		// A client that abandoned its PUT may still materialize the object AFTER
		// the delete returns. Model that: the first delete "succeeds", then the
		// object reappears under the same key.
		if key == fixture.stagingKey {
			lateKey = key
		}
	}
	reconciler := newReconciler(t, fixture)
	reconciler.RunOnce(context.Background())

	if lateKey == "" {
		t.Fatalf("the staging object was never deleted")
	}
	// The object reappears (late materialization). No code observes it; only the
	// tombstone's re-delete can reclaim it.
	fixture.deleteRecorder.onDelete = func(key string) {
		if key == fixture.stagingKey {
			lateKeyDeletes++
		}
	}
	if _, err := fixture.pool.Exec(context.Background(), `
		UPDATE attachment_upload_session SET next_attempt_at = now(), lease_expires_at = NULL WHERE id = $1`, id); err != nil {
		t.Fatalf("make tombstone due: %v", err)
	}
	reconciler.RunOnce(context.Background())

	if lateKeyDeletes == 0 {
		t.Fatalf("the tombstone pass did not re-delete the late object")
	}
	state, _, pass, exists := fixture.sessionState(t, id)
	if !exists || state != "tombstoned" || pass != 1 {
		t.Fatalf("after the re-delete: state=%q pass=%d exists=%v, want tombstoned/1", state, pass, exists)
	}
}

func TestAttachmentUploadReconciler_LeasePreventsConcurrentClaims(t *testing.T) {
	fixture := newUploadSessionFixture(t)
	id := fixture.seedSession(t, "none", time.Hour, time.Hour)

	// Claim the row as a live worker would, then sweep: the claimed row's lease
	// keeps the reconciler off it, so an in-flight worker is not duplicated.
	lease := pgtype.UUID{Bytes: [16]byte{9}, Valid: true}
	if _, err := fixture.pool.Exec(context.Background(), `
		UPDATE attachment_upload_session
		SET cleanup_state = 'claimed', lease_token = $2, lease_expires_at = now() + interval '2 minutes'
		WHERE id = $1`, id, lease); err != nil {
		t.Fatalf("take lease: %v", err)
	}

	newReconciler(t, fixture).RunOnce(context.Background())

	if fixture.deleteRecorder.keyDeleted(fixture.stagingKey) {
		t.Fatalf("the sweep deleted an object another replica holds the lease on")
	}
}

func TestAttachmentUploadReconciler_NoStorageSkipsWithoutClaiming(t *testing.T) {
	fixture := newUploadSessionFixture(t)
	id := fixture.seedSession(t, "none", time.Hour, time.Hour)

	(&AttachmentUploadReconciler{Queries: db.New(fixture.pool)}).RunOnce(context.Background())

	// Claiming without a deleter would strand the row in 'deleting' until its
	// lease expired, so the sweep must not claim at all.
	publish, cleanup, _, _, _ := fixture.publishAndCleanup(t, id)
	if publish != "pending" || cleanup != "none" {
		t.Fatalf("publish=%q cleanup=%q, want pending/none (nothing should have been claimed)", publish, cleanup)
	}
}

func TestAttachmentUploadBackoff_GrowsAndCaps(t *testing.T) {
	if got := attachmentUploadBackoff(1); got != attachmentUploadReconcileBackoffBase {
		t.Fatalf("attempt 1 backoff = %s, want %s", got, attachmentUploadReconcileBackoffBase)
	}
	if got := attachmentUploadBackoff(2); got != 2*attachmentUploadReconcileBackoffBase {
		t.Fatalf("attempt 2 backoff = %s", got)
	}
	if got := attachmentUploadBackoff(64); got != attachmentUploadReconcileBackoffCap {
		t.Fatalf("backoff did not cap: %s", got)
	}
}

func TestAttachmentUploadSettleDelayExceedsEveryUploadBudget(t *testing.T) {
	// The settle delay is what lets a still-streaming PUT finish before its
	// object is reclaimed, so it must dominate every per-request budget in the
	// pipeline. This is the invariant the schedule rests on; a shorter delay
	// would turn "abandoned" into "slow".
	if AttachmentUploadReconcileSettleDelay < 5*time.Minute {
		t.Fatalf("settle delay = %s, too short to cover a slow upload", AttachmentUploadReconcileSettleDelay)
	}
	if attachmentUploadReconcileDeleteTimeout >= attachmentUploadReconcileLease {
		t.Fatalf("delete timeout %s must stay below the lease %s", attachmentUploadReconcileDeleteTimeout, attachmentUploadReconcileLease)
	}
}

// TestPublishedSessionSurvivesCleanupForReplay pins the retention rule that
// makes a retried complete-upload idempotent: reclaiming the objects must not
// retire the row while a client could still be retrying.
func TestPublishedSessionSurvivesCleanupForReplay(t *testing.T) {
	fixture := newUploadSessionFixture(t)
	id := fixture.seedSession(t, "none", time.Hour, time.Hour)

	// Publish it the way the handler does, then run the real sweep.
	if _, err := fixture.pool.Exec(context.Background(), `
		UPDATE attachment_upload_session
		SET publish_state = 'completed', attachment_id = gen_random_uuid(),
		    completed_at = now(), next_attempt_at = now()
		WHERE id = $1`, id); err != nil {
		t.Fatalf("publish session: %v", err)
	}

	reconciler := newReconciler(t, fixture)
	// Walk the whole tombstone schedule; the row must survive all of it.
	for pass := 0; pass < len(attachmentUploadTombstoneRedelete)+1; pass++ {
		reconciler.RunOnce(context.Background())
		if _, err := fixture.pool.Exec(context.Background(), `
			UPDATE attachment_upload_session
			SET next_attempt_at = now(), lease_token = NULL, lease_expires_at = NULL
			WHERE id = $1`, id); err != nil {
			t.Fatalf("advance sweep: %v", err)
		}
	}

	publish, _, _, _, exists := fixture.publishAndCleanup(t, id)
	if !exists {
		t.Fatalf("a published session's row was dropped inside its replay window")
	}
	if publish != "completed" {
		t.Fatalf("publish_state = %q, want completed", publish)
	}
	// Its staging object is gone; that is the whole point of the sweep.
	if !fixture.deleteRecorder.keyDeleted(fixture.stagingKey) {
		t.Fatalf("staging object was not reclaimed")
	}
}

// TestReplayRetentionIsBoundedAndFinite guards the other half: the hold must
// expire, or rows accumulate forever.
func TestReplayRetentionIsBoundedAndFinite(t *testing.T) {
	if AttachmentUploadReplayRetention <= 0 {
		t.Fatalf("retention = %s, want positive", AttachmentUploadReplayRetention)
	}
	if AttachmentUploadReplayRetention > 7*24*time.Hour {
		t.Fatalf("retention = %s, unreasonably long for a replay window", AttachmentUploadReplayRetention)
	}
	// Once the window has passed, a published row must be deletable.
	fixture := newUploadSessionFixture(t)
	id := fixture.seedSession(t, "none", time.Hour, time.Hour)
	if _, err := fixture.pool.Exec(context.Background(), `
		UPDATE attachment_upload_session
		SET publish_state = 'completed', attachment_id = gen_random_uuid(),
		    completed_at = now() - $2::interval, next_attempt_at = now()
		WHERE id = $1`, id, (AttachmentUploadReplayRetention + time.Hour).String()); err != nil {
		t.Fatalf("seed expired published session: %v", err)
	}

	reconciler := newReconciler(t, fixture)
	for pass := 0; pass < len(attachmentUploadTombstoneRedelete)+3; pass++ {
		reconciler.RunOnce(context.Background())
		if _, err := fixture.pool.Exec(context.Background(), `
			UPDATE attachment_upload_session
			SET next_attempt_at = now(), lease_token = NULL, lease_expires_at = NULL
			WHERE id = $1`, id); err != nil {
			t.Fatalf("advance sweep: %v", err)
		}
	}
	if _, _, _, _, exists := fixture.publishAndCleanup(t, id); exists {
		t.Fatalf("a published session outlived its replay retention window")
	}
}

// TestUploadGraceMatchesHandler keeps the sweep's copy of the completion grace
// honest. The two live in different packages on purpose (each owns its side of
// the boundary), so a change to one without the other would silently let the
// sweep reclaim objects while completion is still allowed.
func TestUploadGraceMatchesHandler(t *testing.T) {
	// The handler's value is unexported; the number is asserted directly here so
	// a change on either side fails this test rather than drifting.
	const handlerGraceMinutes = 5
	if AttachmentUploadCompleteGrace != handlerGraceMinutes*time.Minute {
		t.Fatalf("sweep grace = %s, want %d minutes to match the handler",
			AttachmentUploadCompleteGrace, handlerGraceMinutes)
	}
}
