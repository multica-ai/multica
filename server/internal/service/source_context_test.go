package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

type sourceContextObjectStoreFake struct {
	deleted []string
	fail    bool
}

type concurrentSourceContextDeleteStore struct {
	arrived sync.WaitGroup
	release chan struct{}
	mu      sync.Mutex
	deleted []string
}

func (s *concurrentSourceContextDeleteStore) KeyFromURL(rawURL string) string {
	return strings.TrimPrefix(rawURL, "https://objects.example/")
}

func (s *concurrentSourceContextDeleteStore) DeleteObject(_ context.Context, key string) error {
	s.arrived.Done()
	<-s.release
	s.mu.Lock()
	s.deleted = append(s.deleted, key)
	s.mu.Unlock()
	return nil
}

func (s *sourceContextObjectStoreFake) KeyFromURL(rawURL string) string {
	return strings.TrimPrefix(rawURL, "https://objects.example/")
}

func (s *sourceContextObjectStoreFake) DeleteObject(_ context.Context, key string) error {
	if s.fail {
		return errors.New("delete failed")
	}
	s.deleted = append(s.deleted, key)
	return nil
}

func sourceContextTestSnapshot() SourceContextSnapshot {
	return SourceContextSnapshot{
		SourceIssue: SourceContextIssueSnapshot{
			ID: "11111111-1111-4111-8111-111111111111", Identifier: "MUL-1", Number: 1,
			Title: "Source", CreatedAt: "2026-08-21T00:00:00Z", UpdatedAt: "2026-08-21T01:00:00Z", Revision: 4,
		},
		CommentThread: []SourceContextCommentSnapshot{{
			ID: "22222222-2222-4222-8222-222222222222", Type: "comment", Content: "history",
			Author:    SourceContextAuthor{Type: "member", ID: "33333333-3333-4333-8333-333333333333", Name: "Alice"},
			CreatedAt: "2026-08-21T00:10:00Z", UpdatedAt: "2026-08-21T01:10:00Z", Revision: 9,
		}},
		AnchorCommentID: "22222222-2222-4222-8222-222222222222",
	}
}

func TestValidateSourceContextAgentInputBudget(t *testing.T) {
	maxInstructionBytes := SourceContextMaxAgentInputBytes - SourceContextMaxAgentSnapshotBytes
	if err := validateSourceContextAgentBytes(SourceContextMaxAgentSnapshotBytes, strings.Repeat("p", maxInstructionBytes)); err != nil {
		t.Fatalf("largest accepted agent input rejected: %v", err)
	}
	if err := validateSourceContextAgentBytes(SourceContextMaxAgentSnapshotBytes, strings.Repeat("p", maxInstructionBytes+1)); !errors.Is(err, ErrSourceContextTooLarge) {
		t.Fatalf("combined input above budget error = %v, want %v", err, ErrSourceContextTooLarge)
	}
	if err := validateSourceContextAgentBytes(SourceContextMaxAgentSnapshotBytes+1, ""); !errors.Is(err, ErrSourceContextTooLarge) {
		t.Fatalf("snapshot above agent budget error = %v, want %v", err, ErrSourceContextTooLarge)
	}
	if err := validateSourceContextAgentBytes(-1, ""); !errors.Is(err, ErrSourceContextTooLarge) {
		t.Fatalf("negative snapshot size error = %v, want %v", err, ErrSourceContextTooLarge)
	}
}

func TestValidateSourceContextAgentInputCountsPersistedCaptureMetadata(t *testing.T) {
	build := SourceContextBuild{Snapshot: sourceContextTestSnapshot()}
	capturedBy := issueUUID("33333333-3333-4333-8333-333333333333")
	projected, err := PrepareSourceContextCapture(
		build,
		pgtype.UUID{Valid: true},
		pgtype.UUID{Valid: true},
		capturedBy,
		time.Date(2000, time.January, 1, 0, 0, 0, 999999999, time.UTC),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(projected.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	prompt := strings.Repeat("p", SourceContextMaxAgentInputBytes-len(payload)+1)
	if err := ValidateSourceContextAgentInput(build, capturedBy, prompt); !errors.Is(err, ErrSourceContextTooLarge) {
		t.Fatalf("projected persisted snapshot overflow error = %v, want %v", err, ErrSourceContextTooLarge)
	}
}

func TestSourceContextDigestIgnoresRevisionMetadataButNotContent(t *testing.T) {
	base := sourceContextTestSnapshot()
	originalUpdatedAt := base.CommentThread[0].UpdatedAt
	originalRevision := base.CommentThread[0].Revision
	want, err := sourceContextDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	if base.CommentThread[0].UpdatedAt != originalUpdatedAt || base.CommentThread[0].Revision != originalRevision {
		t.Fatalf("digest mutated snapshot metadata: updated_at=%q revision=%d", base.CommentThread[0].UpdatedAt, base.CommentThread[0].Revision)
	}
	metadataOnly := base
	metadataOnly.SourceIssue.Revision++
	metadataOnly.SourceIssue.UpdatedAt = "2026-08-22T00:00:00Z"
	metadataOnly.CommentThread[0].Revision++
	metadataOnly.CommentThread[0].UpdatedAt = "2026-08-22T00:00:00Z"
	metadataOnly.CommentThread[0].Author.Name = "Alice Renamed"
	got, err := sourceContextDigest(metadataOnly)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("metadata-only digest = %s, want %s", got, want)
	}
	changed := sourceContextTestSnapshot()
	changed.CommentThread[0].Content = "changed history"
	got, err = sourceContextDigest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if got == want {
		t.Fatal("content change did not change source context digest")
	}
}

func TestPrepareSourceContextCaptureRewritesOnlySnapshotAttachmentIDs(t *testing.T) {
	rowID := dbid.NewV7()
	cloneID := dbid.NewV7()
	snapshot := sourceContextTestSnapshot()
	sourceURL := "/api/attachments/" + util.UUIDToString(rowID) + "/download"
	storageURL := "https://cdn.example.test/workspaces/ws/" + util.UUIDToString(rowID) + ".txt"
	description := "source file: !file[a.txt](" + sourceURL + ")\npublic image: ![a](" + storageURL + ")"
	snapshot.SourceIssue.Description = &description
	snapshot.SourceIssue.Attachments = []SourceContextAttachment{{ID: util.UUIDToString(rowID), Filename: "a.txt"}}
	snapshot.CommentThread[0].Content = "inline image: ![a](https://api.example.test" + sourceURL + "?preview=1)"
	snapshot.CommentThread[0].Attachments = []SourceContextAttachment{{ID: util.UUIDToString(rowID), Filename: "a.txt"}}
	build := SourceContextBuild{Snapshot: snapshot, Digest: "digest", Rows: []db.Attachment{{ID: rowID, Url: storageURL}}}
	capture, err := PrepareSourceContextCapture(build, dbid.NewV7(), dbid.NewV7(), dbid.NewV7(), time.Now(), []SourceContextClone{{ID: cloneID}})
	if err != nil {
		t.Fatal(err)
	}
	attachment := capture.Snapshot.SourceIssue.Attachments[0]
	if attachment.ID != util.UUIDToString(cloneID) || attachment.SourceAttachmentID != util.UUIDToString(rowID) {
		t.Fatalf("rewritten attachment = %#v", attachment)
	}
	cloneURL := "/api/attachments/" + util.UUIDToString(cloneID) + "/download"
	if capture.Snapshot.SourceIssue.Description == nil || !strings.Contains(*capture.Snapshot.SourceIssue.Description, cloneURL) {
		t.Fatalf("captured description did not reference clone: %#v", capture.Snapshot.SourceIssue.Description)
	}
	if strings.Contains(*capture.Snapshot.SourceIssue.Description, storageURL) {
		t.Fatalf("captured description retained source object URL: %#v", capture.Snapshot.SourceIssue.Description)
	}
	if !strings.Contains(capture.Snapshot.CommentThread[0].Content, cloneURL+"?preview=1") {
		t.Fatalf("captured comment did not reference clone: %q", capture.Snapshot.CommentThread[0].Content)
	}
	if build.Snapshot.SourceIssue.Attachments[0].ID != util.UUIDToString(rowID) {
		t.Fatal("capture mutated preview snapshot")
	}
	if build.Snapshot.SourceIssue.Description == nil || !strings.Contains(*build.Snapshot.SourceIssue.Description, sourceURL) {
		t.Fatal("capture mutated preview description")
	}
	if _, _, ok := ParseSourceContextToken("sha256:not-a-digest:bad"); ok {
		t.Fatal("invalid capture token parsed successfully")
	}
}

func TestEnqueueQuickCreateTaskWithSourceContextIsAtomic(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, sourceIssueID := seedAttributionFixture(t, pool)
	var sourceCommentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'captured history') RETURNING id
	`, sourceIssueID, workspaceID, userID).Scan(&sourceCommentID); err != nil {
		t.Fatalf("insert source comment: %v", err)
	}

	build, err := BuildSourceContext(ctx, q, util.MustParseUUID(workspaceID), util.MustParseUUID(sourceCommentID))
	if err != nil {
		t.Fatalf("build source context: %v", err)
	}
	newCapture := func() SourceContextCapture {
		capture, captureErr := PrepareSourceContextCapture(
			build,
			dbid.NewV7(),
			util.MustParseUUID(workspaceID),
			util.MustParseUUID(userID),
			time.Now().UTC(),
			nil,
		)
		if captureErr != nil {
			t.Fatalf("prepare source context: %v", captureErr)
		}
		return capture
	}

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	successCapture := newCapture()
	task, err := svc.EnqueueQuickCreateTaskWithSourceContext(
		ctx,
		util.MustParseUUID(workspaceID),
		util.MustParseUUID(userID),
		util.MustParseUUID(agentID),
		pgtype.UUID{},
		"create from context",
		"medium",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
		successCapture,
	)
	if err != nil {
		t.Fatalf("enqueue with source context: %v", err)
	}
	var state string
	var originTaskID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT state, origin_task_id FROM issue_source_context WHERE id = $1
	`, successCapture.ID).Scan(&state, &originTaskID); err != nil {
		t.Fatalf("load persisted pending context: %v", err)
	}
	if state != "pending" || originTaskID != task.ID {
		t.Fatalf("pending context state=%q origin_task_id=%s, want task %s", state, util.UUIDToString(originTaskID), util.UUIDToString(task.ID))
	}

	rollbackCapture := newCapture()
	if _, err := PersistSourceContext(ctx, q, rollbackCapture, util.MustParseUUID(sourceIssueID), pgtype.UUID{}); err != nil {
		t.Fatalf("seed duplicate context id: %v", err)
	}
	_, err = svc.EnqueueQuickCreateTaskWithSourceContext(
		ctx,
		util.MustParseUUID(workspaceID),
		util.MustParseUUID(userID),
		util.MustParseUUID(agentID),
		pgtype.UUID{},
		"must roll back",
		"medium",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
		rollbackCapture,
	)
	if err == nil {
		t.Fatal("duplicate context id enqueue succeeded")
	}
	var rolledBackTasks int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE context->>'source_context_id' = $1 AND context->>'prompt' = 'must roll back'
	`, util.UUIDToString(rollbackCapture.ID)).Scan(&rolledBackTasks); err != nil {
		t.Fatalf("count rolled-back tasks: %v", err)
	}
	if rolledBackTasks != 0 {
		t.Fatalf("rolled-back task count = %d, want 0", rolledBackTasks)
	}
}

func TestSourceContextRetryTransfersSingleAttachAuthority(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, sourceIssueID := seedAttributionFixture(t, pool)
	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load fixture runtime: %v", err)
	}

	contextID := dbid.NewV7()
	cloneID := dbid.NewV7()
	quickCreateJSON, err := json.Marshal(QuickCreateContext{
		Type: QuickCreateContextType, Prompt: "new work", RequesterID: userID,
		WorkspaceID: workspaceID, SourceContextID: util.UUIDToString(contextID),
	})
	if err != nil {
		t.Fatal(err)
	}
	var parentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, priority, attempt, max_attempts,
			failure_reason, context, originator_user_id, accountable_user_id
		) VALUES ($1, $2, 'failed', 0, 1, 3, 'agent_error.runtime_offline', $3, $4, $4)
		RETURNING id
	`, agentID, runtimeID, quickCreateJSON, userID).Scan(&parentID); err != nil {
		t.Fatalf("insert source quick-create task: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO issue_source_context (
			id, workspace_id, origin_task_id, source_issue_id, anchor_comment_id,
			captured_by_user_id, snapshot_version, snapshot, capture_digest, state
		) VALUES ($1, $2, $3, $4, gen_random_uuid(), $5, 1, '{}'::jsonb, 'digest', 'pending')
	`, contextID, workspaceID, parentID, sourceIssueID, userID); err != nil {
		t.Fatalf("insert pending source context: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO attachment (
			id, workspace_id, source_context_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		) VALUES ($1, $2, $3, 'member', $4, 'snapshot.txt', 'local://snapshot.txt', 'text/plain', 8)
	`, cloneID, workspaceID, contextID, userID); err != nil {
		t.Fatalf("insert source context clone: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM attachment WHERE source_context_id = $1`, contextID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_source_context WHERE id = $1`, contextID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id = $1 OR id = $1`, parentID)
	})

	child, err := q.CreateRetryTask(ctx, db.CreateRetryTaskParams{NewTaskID: dbid.NewV7(), ID: parentID})
	if err != nil {
		t.Fatalf("create retry child: %v", err)
	}
	parent, err := q.GetAgentTask(ctx, parentID)
	if err != nil {
		t.Fatalf("reload retry parent: %v", err)
	}
	if err := transferPendingSourceContextToRetry(ctx, q, parent, child); err != nil {
		t.Fatalf("transfer pending context: %v", err)
	}
	transferred, err := q.GetIssueSourceContextByID(ctx, db.GetIssueSourceContextByIDParams{
		WorkspaceID: util.MustParseUUID(workspaceID), ID: contextID,
	})
	if err != nil {
		t.Fatalf("load transferred context: %v", err)
	}
	if transferred.OriginTaskID != child.ID || transferred.State != "pending" {
		t.Fatalf("transferred context = origin %s state %q, want %s/pending", util.UUIDToString(transferred.OriginTaskID), transferred.State, util.UUIDToString(child.ID))
	}
	var cloneCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM attachment WHERE source_context_id = $1 AND id = $2`, contextID, cloneID).Scan(&cloneCount); err != nil || cloneCount != 1 {
		t.Fatalf("clone count after retry = %d, err=%v", cloneCount, err)
	}

	secondChild, err := q.CreateRetryTask(ctx, db.CreateRetryTaskParams{NewTaskID: dbid.NewV7(), ID: parentID})
	if err != nil {
		t.Fatalf("create competing retry child: %v", err)
	}
	if err := transferPendingSourceContextToRetry(ctx, q, parent, secondChild); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("second transfer error = %v, want pgx.ErrNoRows", err)
	}

	targetIssueID := util.MustParseUUID(sourceIssueID)
	if _, err := q.AttachIssueSourceContext(ctx, db.AttachIssueSourceContextParams{
		IssueID: targetIssueID, WorkspaceID: util.MustParseUUID(workspaceID), ID: contextID, OriginTaskID: child.ID,
	}); err != nil {
		t.Fatalf("attach from current retry owner: %v", err)
	}
	if _, err := q.AttachIssueSourceContext(ctx, db.AttachIssueSourceContextParams{
		IssueID: dbid.NewV7(), WorkspaceID: util.MustParseUUID(workspaceID), ID: contextID, OriginTaskID: secondChild.ID,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("competing attach error = %v, want pgx.ErrNoRows", err)
	}
}

func TestFailTaskDoesNotRetryAfterSourceContextAttached(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, sourceIssueID := seedAttributionFixture(t, pool)
	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load fixture runtime: %v", err)
	}

	contextID := dbid.NewV7()
	targetIssueID := dbid.NewV7()
	quickCreateJSON, err := json.Marshal(QuickCreateContext{
		Type: QuickCreateContextType, Prompt: "already created", RequesterID: userID,
		WorkspaceID: workspaceID, SourceContextID: util.UUIDToString(contextID),
	})
	if err != nil {
		t.Fatal(err)
	}
	var parentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, priority, attempt, max_attempts,
			context, originator_user_id, accountable_user_id
		) VALUES ($1, $2, 'running', 0, 1, 3, $3, $4, $4)
		RETURNING id
	`, agentID, runtimeID, quickCreateJSON, userID).Scan(&parentID); err != nil {
		t.Fatalf("insert running source-context task: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO issue (
			id, workspace_id, number, title, status, priority,
			creator_type, creator_id, position, origin_type, origin_id
		) VALUES ($1, $2, (SELECT max(number) + 1 FROM issue WHERE workspace_id = $2),
			'already-created contextual target', 'todo', 'none', 'agent', $3, 0,
			'quick_create', $4)
	`, targetIssueID, workspaceID, agentID, parentID); err != nil {
		t.Fatalf("insert target issue: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO issue_source_context (
			id, workspace_id, issue_id, origin_task_id, source_issue_id,
			anchor_comment_id, captured_by_user_id, snapshot_version,
			snapshot, capture_digest, state, attached_at
		) VALUES ($1, $2, $3, $4, $5, gen_random_uuid(), $6, 1,
			'{}'::jsonb, 'digest', 'attached', now())
	`, contextID, workspaceID, targetIssueID, parentID, sourceIssueID, userID); err != nil {
		t.Fatalf("insert attached source context: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM inbox_item WHERE details->>'task_id' = $1`, util.UUIDToString(parentID))
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_source_context WHERE id = $1`, contextID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id = $1 OR id = $1`, parentID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, targetIssueID)
	})

	svc := NewTaskService(q, pool, nil, events.New())
	failed, err := svc.FailTask(ctx, parentID, "runtime dropped after create", "", "", "", "runtime_offline", false, "", "")
	if err != nil {
		t.Fatalf("FailTask after context attach: %v", err)
	}
	if failed.Status != "failed" {
		t.Fatalf("parent status = %q, want failed", failed.Status)
	}
	var childCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, parentID).Scan(&childCount); err != nil {
		t.Fatalf("count retry children: %v", err)
	}
	if childCount != 0 {
		t.Fatalf("retry children after source context attached = %d, want 0", childCount)
	}
	var inboxType string
	if err := pool.QueryRow(ctx, `
		SELECT type FROM inbox_item
		WHERE details->>'task_id' = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, util.UUIDToString(parentID)).Scan(&inboxType); err != nil {
		t.Fatalf("load reconciled quick-create outcome: %v", err)
	}
	if inboxType != "quick_create_done" {
		t.Fatalf("quick-create outcome after attached context = %q, want quick_create_done", inboxType)
	}
}

func TestManualSourceContextRetryReusesPendingContextExactlyOnce(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, sourceIssueID := seedAttributionFixture(t, pool)
	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load fixture runtime: %v", err)
	}

	contextID := dbid.NewV7()
	cloneID := dbid.NewV7()
	payload, err := json.Marshal(QuickCreateContext{
		Type: QuickCreateContextType, Prompt: "retry this", RequesterID: userID,
		WorkspaceID: workspaceID, SourceContextID: util.UUIDToString(contextID),
	})
	if err != nil {
		t.Fatal(err)
	}
	var parentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, priority, context,
			originator_user_id, accountable_user_id
		) VALUES ($1, $2, 'failed', 0, $3, $4, $4)
		RETURNING id
	`, agentID, runtimeID, payload, userID).Scan(&parentID); err != nil {
		t.Fatalf("insert failed source-context task: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO issue_source_context (
			id, workspace_id, origin_task_id, source_issue_id, anchor_comment_id,
			captured_by_user_id, snapshot_version, snapshot, capture_digest, state
		) VALUES ($1, $2, $3, $4, gen_random_uuid(), $5, 1, '{}'::jsonb, 'digest', 'pending')
	`, contextID, workspaceID, parentID, sourceIssueID, userID); err != nil {
		t.Fatalf("insert pending context: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO attachment (
			id, workspace_id, source_context_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		) VALUES ($1, $2, $3, 'member', $4, 'snapshot.txt', 'local://snapshot.txt', 'text/plain', 8)
	`, cloneID, workspaceID, contextID, userID); err != nil {
		t.Fatalf("insert context attachment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM attachment WHERE source_context_id = $1`, contextID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_source_context WHERE id = $1`, contextID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE rerun_of_task_id = $1 OR id = $1`, parentID)
	})

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	if _, err := svc.RetrySourceContextQuickCreate(
		ctx, util.MustParseUUID(workspaceID), dbid.NewV7(), parentID,
		func(db.Agent) bool { return true },
	); !errors.Is(err, ErrSourceContextRetryUnavailable) {
		t.Fatalf("different requester retry error = %v, want ErrSourceContextRetryUnavailable", err)
	}
	if _, err := svc.RetrySourceContextQuickCreate(
		ctx, util.MustParseUUID(workspaceID), util.MustParseUUID(userID), parentID,
		func(db.Agent) bool { return false },
	); !errors.Is(err, ErrRerunInvokeNotAllowed) {
		t.Fatalf("blocked agent retry error = %v, want ErrRerunInvokeNotAllowed", err)
	}
	child, err := svc.RetrySourceContextQuickCreate(
		ctx, util.MustParseUUID(workspaceID), util.MustParseUUID(userID), parentID,
		func(db.Agent) bool { return true },
	)
	if err != nil {
		t.Fatalf("manual retry: %v", err)
	}
	if child.RerunOfTaskID != parentID || child.RetryOfTaskID.Valid {
		t.Fatalf("manual retry lineage rerun=%s retry=%s, want rerun=%s only",
			util.UUIDToString(child.RerunOfTaskID), util.UUIDToString(child.RetryOfTaskID), util.UUIDToString(parentID))
	}
	var childPayload QuickCreateContext
	var wantPayload QuickCreateContext
	if err := json.Unmarshal(child.Context, &childPayload); err != nil {
		t.Fatalf("decode retry context: %v", err)
	}
	if err := json.Unmarshal(payload, &wantPayload); err != nil {
		t.Fatalf("decode source context: %v", err)
	}
	if child.Attempt != 1 || !reflect.DeepEqual(childPayload, wantPayload) {
		t.Fatalf("manual retry attempt/context = %d/%+v, want 1/%+v", child.Attempt, childPayload, wantPayload)
	}
	stored, err := q.GetIssueSourceContextByID(ctx, db.GetIssueSourceContextByIDParams{
		WorkspaceID: util.MustParseUUID(workspaceID), ID: contextID,
	})
	if err != nil || stored.OriginTaskID != child.ID || stored.State != "pending" {
		t.Fatalf("transferred context = %+v, err=%v", stored, err)
	}
	var cloneCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM attachment WHERE source_context_id = $1`, contextID).Scan(&cloneCount); err != nil || cloneCount != 1 {
		t.Fatalf("clone count = %d, err=%v", cloneCount, err)
	}
	if _, err := svc.RetrySourceContextQuickCreate(
		ctx, util.MustParseUUID(workspaceID), util.MustParseUUID(userID), parentID,
		func(db.Agent) bool { return true },
	); !errors.Is(err, ErrSourceContextRetryUnavailable) {
		t.Fatalf("second manual retry error = %v, want ErrSourceContextRetryUnavailable", err)
	}
	var children int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE rerun_of_task_id = $1`, parentID).Scan(&children); err != nil || children != 1 {
		t.Fatalf("manual retry children = %d, err=%v", children, err)
	}
}

func TestCleanupSourceContextObjectIntentsDeletesOnlyUnreferencedObjects(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	// This sweeper is intentionally global. Handler and service packages run
	// concurrently against the same integration database, so serialize their
	// cleanup-oracle sections to prevent either fake object store from claiming
	// the other package's intent rows.
	cleanupLockConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire source-context cleanup test lock connection: %v", err)
	}
	if _, err := cleanupLockConn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(0x53434f4e54455854)); err != nil {
		cleanupLockConn.Release()
		t.Fatalf("lock source-context cleanup tests: %v", err)
	}
	t.Cleanup(func() {
		_, _ = cleanupLockConn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, int64(0x53434f4e54455854))
		cleanupLockConn.Release()
	})
	q := db.New(pool)
	workspaceID, userID, _, sourceIssueID := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	userUUID := util.MustParseUUID(userID)
	sourceIssueUUID := util.MustParseUUID(sourceIssueID)

	validContextID := dbid.NewV7()
	validAttachmentID := dbid.NewV7()
	orphanContextID := dbid.NewV7()
	orphanAttachmentID := dbid.NewV7()
	validKey := "source-context-test/valid-" + util.UUIDToString(validAttachmentID)
	orphanKey := "source-context-test/orphan-" + util.UUIDToString(orphanAttachmentID)
	validURL := "https://objects.example/" + validKey
	orphanURL := "https://objects.example/" + orphanKey

	if _, err := pool.Exec(ctx, `
		INSERT INTO issue_source_context (
			id, workspace_id, issue_id, source_issue_id, anchor_comment_id,
			captured_by_user_id, snapshot_version, snapshot, capture_digest,
			state, attached_at
		) VALUES ($1, $2, $3, $3, gen_random_uuid(), $4, 1, '{}'::jsonb, 'digest', 'attached', now())
	`, validContextID, workspaceUUID, sourceIssueUUID, userUUID); err != nil {
		t.Fatalf("insert referenced context: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO attachment (
			id, workspace_id, source_context_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		) VALUES ($1, $2, $3, 'member', $4, 'valid.txt', $5, 'text/plain', 5)
	`, validAttachmentID, workspaceUUID, validContextID, userUUID, validURL); err != nil {
		t.Fatalf("insert referenced attachment: %v", err)
	}
	// Keep the two due intents and the sweep in one table-locked transaction.
	// A developer server can be running against the same worktree database
	// while integration tests execute; without this fence its runtime sweeper
	// can legitimately claim one test row between the age UPDATE and the method
	// call, making the exact cleanup oracle nondeterministic.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin isolated intent cleanup: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `LOCK TABLE issue_source_context_object_intent IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		t.Fatalf("lock object intent table: %v", err)
	}
	qtx := q.WithTx(tx)
	for _, intent := range []db.RecordSourceContextObjectIntentParams{
		{StorageKey: validKey, WorkspaceID: workspaceUUID, SourceContextID: validContextID, AttachmentID: validAttachmentID, ObjectUrl: validURL},
		{StorageKey: orphanKey, WorkspaceID: workspaceUUID, SourceContextID: orphanContextID, AttachmentID: orphanAttachmentID, ObjectUrl: orphanURL},
	} {
		if _, err := qtx.RecordSourceContextObjectIntent(ctx, intent); err != nil {
			t.Fatalf("record intent: %v", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE issue_source_context_object_intent
		SET created_at = now() - interval '2 hours', next_attempt_at = '-infinity'::timestamptz
		WHERE storage_key = ANY($1::text[])
	`, []string{validKey, orphanKey}); err != nil {
		t.Fatalf("age intents: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_source_context_object_intent WHERE storage_key = ANY($1::text[])`, []string{validKey, orphanKey})
		_, _ = pool.Exec(context.Background(), `DELETE FROM attachment WHERE source_context_id = $1`, validContextID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_source_context WHERE id = $1`, validContextID)
	})

	store := &sourceContextObjectStoreFake{}
	svc := &TaskService{Queries: qtx, SourceContextStorage: store}
	cleaned, err := svc.CleanupSourceContextObjectIntents(ctx, 2)
	if err != nil {
		t.Fatalf("cleanup intents: %v", err)
	}
	if cleaned < 2 {
		t.Fatalf("cleaned intents = %d, want at least the two test intents", cleaned)
	}
	if len(store.deleted) != 1 || store.deleted[0] != orphanKey {
		t.Fatalf("deleted object keys = %#v, want only %q", store.deleted, orphanKey)
	}
	var validAttachmentExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM attachment WHERE id = $1 AND source_context_id = $2)`, validAttachmentID, validContextID).Scan(&validAttachmentExists); err != nil || !validAttachmentExists {
		t.Fatalf("referenced attachment retained = %v, err=%v", validAttachmentExists, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit isolated intent cleanup: %v", err)
	}
}

func TestSourceContextCaptureIntentCannotOutliveWorkspaceDelete(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	var workspaceID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('source context delete fence', $1)
		RETURNING id
	`, fmt.Sprintf("source-context-delete-fence-%d", time.Now().UnixNano())).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_source_context_object_intent WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workspace delete: %v", err)
	}
	defer tx.Rollback(ctx)
	qtx := q.WithTx(tx)
	if _, err := qtx.LockWorkspaceForDelete(ctx, workspaceID); err != nil {
		t.Fatalf("lock workspace for delete: %v", err)
	}

	insertDone := make(chan error, 1)
	go func() {
		_, insertErr := q.RecordSourceContextObjectIntent(ctx, db.RecordSourceContextObjectIntentParams{
			StorageKey: "source-context/delete-fence", WorkspaceID: workspaceID,
			SourceContextID: dbid.NewV7(), AttachmentID: dbid.NewV7(), ObjectUrl: "local://delete-fence",
		})
		insertDone <- insertErr
	}()
	select {
	case insertErr := <-insertDone:
		t.Fatalf("capture intent crossed workspace delete lock: %v", insertErr)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID); err != nil {
		t.Fatalf("delete locked workspace: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit workspace delete: %v", err)
	}
	if insertErr := <-insertDone; !errors.Is(insertErr, pgx.ErrNoRows) {
		t.Fatalf("capture intent after workspace delete = %v, want pgx.ErrNoRows", insertErr)
	}
	var intents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue_source_context_object_intent WHERE workspace_id = $1`, workspaceID).Scan(&intents); err != nil {
		t.Fatalf("count post-delete intents: %v", err)
	}
	if intents != 0 {
		t.Fatalf("post-delete capture intents = %d, want 0", intents)
	}
}

func TestCleanupAbandonedSourceContextsHonorsRetentionAndRetriesObjectDeletion(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, sourceIssueID := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	userUUID := util.MustParseUUID(userID)
	agentUUID := util.MustParseUUID(agentID)

	var runtimeID pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentUUID).Scan(&runtimeID); err != nil {
		t.Fatalf("load source-context cleanup runtime: %v", err)
	}

	type fixture struct {
		name         string
		taskStatus   string
		capturedAt   time.Time
		contextID    pgtype.UUID
		taskID       pgtype.UUID
		attachmentID pgtype.UUID
	}
	fixtures := []fixture{
		{name: "expired-terminal", taskStatus: "failed", capturedAt: time.Now().Add(-31 * 24 * time.Hour)},
		{name: "recent-terminal", taskStatus: "failed", capturedAt: time.Now().Add(-29 * 24 * time.Hour)},
		{name: "expired-active", taskStatus: "queued", capturedAt: time.Now().Add(-31 * 24 * time.Hour)},
	}
	for i := range fixtures {
		fixtures[i].contextID = dbid.NewV7()
		fixtures[i].taskID = dbid.NewV7()
		fixtures[i].attachmentID = dbid.NewV7()
		quickCreate, err := json.Marshal(QuickCreateContext{
			Type: QuickCreateContextType, WorkspaceID: workspaceID,
			RequesterID: userID, SourceContextID: util.UUIDToString(fixtures[i].contextID),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO agent_task_queue (
				id, agent_id, runtime_id, status, priority, context,
				originator_user_id, accountable_user_id
			) VALUES ($1, $2, $3, $4, 0, $5, $6, $6)
		`, fixtures[i].taskID, agentUUID, runtimeID, fixtures[i].taskStatus, quickCreate, userUUID); err != nil {
			t.Fatalf("insert %s task: %v", fixtures[i].name, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO issue_source_context (
				id, workspace_id, origin_task_id, source_issue_id, anchor_comment_id,
				captured_by_user_id, snapshot_version, snapshot, capture_digest,
				state, captured_at
			) VALUES ($1, $2, $3, $4, gen_random_uuid(), $5, 1, '{}'::jsonb,
				'digest', 'pending', $6)
		`, fixtures[i].contextID, workspaceUUID, fixtures[i].taskID, util.MustParseUUID(sourceIssueID), userUUID, fixtures[i].capturedAt); err != nil {
			t.Fatalf("insert %s context: %v", fixtures[i].name, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO attachment (
				id, workspace_id, source_context_id, uploader_type, uploader_id,
				filename, url, content_type, size_bytes
			) VALUES ($1, $2, $3, 'member', $4, $5, $6, 'text/plain', 7)
		`, fixtures[i].attachmentID, workspaceUUID, fixtures[i].contextID, userUUID,
			fixtures[i].name+".txt", "https://objects.example/source-context/"+fixtures[i].name); err != nil {
			t.Fatalf("insert %s clone: %v", fixtures[i].name, err)
		}
	}
	t.Cleanup(func() {
		contextIDs := make([]pgtype.UUID, 0, len(fixtures))
		taskIDs := make([]pgtype.UUID, 0, len(fixtures))
		for _, item := range fixtures {
			contextIDs = append(contextIDs, item.contextID)
			taskIDs = append(taskIDs, item.taskID)
		}
		_, _ = pool.Exec(context.Background(), `DELETE FROM attachment WHERE source_context_id = ANY($1::uuid[])`, contextIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_source_context WHERE id = ANY($1::uuid[])`, contextIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = ANY($1::uuid[])`, taskIDs)
	})

	store := &sourceContextObjectStoreFake{fail: true}
	svc := &TaskService{Queries: q, TxStarter: pool, SourceContextStorage: store}
	cleaned, err := svc.CleanupAbandonedSourceContexts(ctx, 10)
	if err != nil || cleaned != 0 {
		t.Fatalf("first abandoned cleanup = %d, err=%v, want no removed context after object failure", cleaned, err)
	}

	var expiredState string
	var expiredCloneExists bool
	if err := pool.QueryRow(ctx, `SELECT state FROM issue_source_context WHERE id = $1`, fixtures[0].contextID).Scan(&expiredState); err != nil {
		t.Fatalf("load expired context state: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM attachment WHERE id = $1)`, fixtures[0].attachmentID).Scan(&expiredCloneExists); err != nil {
		t.Fatalf("check retained clone after failed object delete: %v", err)
	}
	if expiredState != "abandoned" || !expiredCloneExists {
		t.Fatalf("failed-delete state=%q clone=%v, want abandoned with retained row", expiredState, expiredCloneExists)
	}
	for _, index := range []int{1, 2} {
		var state string
		if err := pool.QueryRow(ctx, `SELECT state FROM issue_source_context WHERE id = $1`, fixtures[index].contextID).Scan(&state); err != nil {
			t.Fatalf("load %s state: %v", fixtures[index].name, err)
		}
		if state != "pending" {
			t.Fatalf("%s state=%q, want pending", fixtures[index].name, state)
		}
	}

	store.fail = false
	cleaned, err = svc.CleanupAbandonedSourceContexts(ctx, 10)
	if err != nil || cleaned != 1 {
		t.Fatalf("retry abandoned cleanup = %d, err=%v, want one removed context", cleaned, err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "source-context/expired-terminal" {
		t.Fatalf("retried object deletes = %#v", store.deleted)
	}
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM attachment WHERE id = $1)`, fixtures[0].attachmentID).Scan(&expiredCloneExists); err != nil {
		t.Fatalf("check clone after successful retry: %v", err)
	}
	if expiredCloneExists {
		t.Fatal("expired clone row survived successful object cleanup retry")
	}
	var expiredContextExists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issue_source_context WHERE id = $1)`, fixtures[0].contextID).Scan(&expiredContextExists); err != nil {
		t.Fatalf("check context after successful cleanup retry: %v", err)
	}
	if expiredContextExists {
		t.Fatal("expired captured snapshot survived successful cleanup retry")
	}
}

func TestCleanupAbandonedSourceContextsRemovesExpiredContextWithoutAttachments(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, sourceIssueID := seedAttributionFixture(t, pool)
	contextID := dbid.NewV7()
	taskID := dbid.NewV7()
	var runtimeID pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, util.MustParseUUID(agentID)).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	quickCreate, err := json.Marshal(QuickCreateContext{
		Type: QuickCreateContextType, WorkspaceID: workspaceID,
		RequesterID: userID, SourceContextID: util.UUIDToString(contextID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue (
			id, agent_id, runtime_id, status, priority, context,
			originator_user_id, accountable_user_id
		) VALUES ($1, $2, $3, 'failed', 0, $4, $5, $5)
	`, taskID, util.MustParseUUID(agentID), runtimeID, quickCreate, util.MustParseUUID(userID)); err != nil {
		t.Fatalf("insert terminal task: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO issue_source_context (
			id, workspace_id, origin_task_id, source_issue_id, anchor_comment_id,
			captured_by_user_id, snapshot_version, snapshot, capture_digest,
			state, captured_at
		) VALUES ($1, $2, $3, $4, gen_random_uuid(), $5, 1, '{}'::jsonb,
			'digest', 'pending', now() - interval '31 days')
	`, contextID, util.MustParseUUID(workspaceID), taskID, util.MustParseUUID(sourceIssueID), util.MustParseUUID(userID)); err != nil {
		t.Fatalf("insert attachment-free context: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_source_context WHERE id = $1`, contextID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	svc := &TaskService{Queries: q, TxStarter: pool, SourceContextStorage: &sourceContextObjectStoreFake{}}
	removed, err := svc.CleanupAbandonedSourceContexts(ctx, 10)
	if err != nil || removed != 1 {
		t.Fatalf("attachment-free cleanup = %d, err=%v, want one removed context", removed, err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issue_source_context WHERE id = $1)`, contextID).Scan(&exists); err != nil {
		t.Fatalf("check attachment-free context: %v", err)
	}
	if exists {
		t.Fatal("attachment-free expired captured snapshot survived cleanup")
	}
}

func TestCleanupAbandonedSourceContextsConvergesAcrossConcurrentSweepers(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, _, sourceIssueID := seedAttributionFixture(t, pool)
	contextID := dbid.NewV7()
	attachmentID := dbid.NewV7()
	if _, err := pool.Exec(ctx, `
		INSERT INTO issue_source_context (
			id, workspace_id, source_issue_id, anchor_comment_id,
			captured_by_user_id, snapshot_version, snapshot, capture_digest,
			state, captured_at
		) VALUES ($1, $2, $3, gen_random_uuid(), $4, 1, '{}'::jsonb,
			'digest', 'abandoned', now() - interval '31 days')
	`, contextID, util.MustParseUUID(workspaceID), util.MustParseUUID(sourceIssueID), util.MustParseUUID(userID)); err != nil {
		t.Fatalf("insert abandoned context: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO attachment (
			id, workspace_id, source_context_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		) VALUES ($1, $2, $3, 'member', $4, 'context.txt',
			'https://objects.example/source-context/concurrent', 'text/plain', 7)
	`, attachmentID, util.MustParseUUID(workspaceID), contextID, util.MustParseUUID(userID)); err != nil {
		t.Fatalf("insert abandoned clone: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM attachment WHERE source_context_id = $1`, contextID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_source_context WHERE id = $1`, contextID)
	})

	store := &concurrentSourceContextDeleteStore{release: make(chan struct{})}
	store.arrived.Add(2)
	svc := &TaskService{Queries: q, TxStarter: pool, SourceContextStorage: store}
	type result struct {
		removed int
		err     error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			removed, err := svc.CleanupAbandonedSourceContexts(ctx, 10)
			results <- result{removed: removed, err: err}
		}()
	}
	store.arrived.Wait()
	close(store.release)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.removed+second.removed != 1 {
		t.Fatalf("concurrent cleanup results = %+v / %+v, want one total removal without errors", first, second)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issue_source_context WHERE id = $1)`, contextID).Scan(&exists); err != nil {
		t.Fatalf("check concurrently cleaned context: %v", err)
	}
	if exists {
		t.Fatal("concurrent sweepers left the abandoned captured snapshot behind")
	}
}

func TestBuildSourceContextRejectsCyclesAndEveryLimitWithoutTruncation(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, _, sourceIssueID := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)

	insertComment := func(content string, parent any) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, parent_id)
			VALUES ($1, $2, 'member', $3, $4, $5) RETURNING id
		`, sourceIssueID, workspaceID, userID, content, parent).Scan(&id); err != nil {
			t.Fatalf("insert comment: %v", err)
		}
		return id
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM attachment WHERE issue_id = $1`, sourceIssueID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, sourceIssueID)
	})

	cycleRoot := insertComment("cycle root", nil)
	cycleChild := insertComment("cycle child", cycleRoot)
	if _, err := pool.Exec(ctx, `UPDATE comment SET parent_id = $1 WHERE id = $2`, cycleChild, cycleRoot); err != nil {
		t.Fatalf("create cycle: %v", err)
	}
	if _, err := BuildSourceContext(ctx, q, workspaceUUID, util.MustParseUUID(cycleChild)); !errors.Is(err, ErrSourceContextInvalid) {
		t.Fatalf("cycle error = %v, want ErrSourceContextInvalid", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE comment SET parent_id = NULL WHERE id = $1`, cycleRoot); err != nil {
		t.Fatalf("break test cycle: %v", err)
	}

	var deepSelected string
	var parent any
	for i := 0; i < SourceContextMaxComments+1; i++ {
		deepSelected = insertComment("deep", parent)
		parent = deepSelected
	}
	deep, err := BuildSourceContext(ctx, q, workspaceUUID, util.MustParseUUID(deepSelected))
	if !errors.Is(err, ErrSourceContextTooLarge) || deep.Limits.CommentCount != SourceContextMaxComments+1 {
		t.Fatalf("deep thread = limits %+v err %v, want %d-comment rejection", deep.Limits, err, SourceContextMaxComments+1)
	}

	wideRoot := insertComment("wide root", nil)
	var wideSelected string
	for i := 0; i < SourceContextMaxComments; i++ {
		wideSelected = insertComment("wide reply", wideRoot)
	}
	wide, err := BuildSourceContext(ctx, q, workspaceUUID, util.MustParseUUID(wideSelected))
	if !errors.Is(err, ErrSourceContextTooLarge) || wide.Limits.CommentCount != SourceContextMaxComments+1 {
		t.Fatalf("wide thread = limits %+v err %v, want %d-comment rejection", wide.Limits, err, SourceContextMaxComments+1)
	}

	largeTextID := insertComment(strings.Repeat("x", SourceContextMaxTextBytes+1), nil)
	largeText, err := BuildSourceContext(ctx, q, workspaceUUID, util.MustParseUUID(largeTextID))
	if !errors.Is(err, ErrSourceContextTooLarge) || largeText.Limits.TextBytes <= SourceContextMaxTextBytes {
		t.Fatalf("large text = limits %+v err %v", largeText.Limits, err)
	}

	attachmentCommentID := insertComment("attachment limits", nil)
	for i := 0; i < SourceContextMaxAttachments+1; i++ {
		if _, err := pool.Exec(ctx, `
			INSERT INTO attachment (workspace_id, issue_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
			VALUES ($1, $2, 'member', $3, $4, $5, 'text/plain', 1)
		`, workspaceID, sourceIssueID, userID, fmt.Sprintf("count-%03d.txt", i), fmt.Sprintf("local://count-%03d", i)); err != nil {
			t.Fatalf("insert count attachment %d: %v", i, err)
		}
	}
	tooManyAttachments, err := BuildSourceContext(ctx, q, workspaceUUID, util.MustParseUUID(attachmentCommentID))
	if !errors.Is(err, ErrSourceContextTooLarge) || tooManyAttachments.Limits.AttachmentCount != SourceContextMaxAttachments+1 {
		t.Fatalf("attachment count = limits %+v err %v", tooManyAttachments.Limits, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM attachment WHERE issue_id = $1`, sourceIssueID); err != nil {
		t.Fatalf("clear count attachments: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO attachment (workspace_id, issue_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, $2, 'member', $3, 'huge.bin', 'local://huge', 'application/octet-stream', $4)
	`, workspaceID, sourceIssueID, userID, SourceContextMaxAttachmentBytes+1); err != nil {
		t.Fatalf("insert oversized attachment: %v", err)
	}
	tooManyBytes, err := BuildSourceContext(ctx, q, workspaceUUID, util.MustParseUUID(attachmentCommentID))
	if !errors.Is(err, ErrSourceContextTooLarge) || tooManyBytes.Limits.AttachmentBytes != SourceContextMaxAttachmentBytes+1 {
		t.Fatalf("attachment bytes = limits %+v err %v", tooManyBytes.Limits, err)
	}
}
