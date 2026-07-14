package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newExternalIdentityPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type externalIdentityFixture struct {
	workspaceID pgtype.UUID
	userID      pgtype.UUID
}

func createExternalIdentityFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) externalIdentityFixture {
	t.Helper()
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("external-id-%d@multica.ai", suffix)
	slug := fmt.Sprintf("external-id-%d", suffix)

	var userID, workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, "External ID Test", email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, "External ID Test", slug, "external identity test", "EXT").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return externalIdentityFixture{
		workspaceID: util.MustParseUUID(workspaceID),
		userID:      util.MustParseUUID(userID),
	}
}

func externalCreateParams(f externalIdentityFixture, title string) IssueCreateParams {
	return IssueCreateParams{
		WorkspaceID: f.workspaceID,
		Title:       title,
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   f.userID,
	}
}

func TestExternalIdentityLockKeyIsCollisionUnambiguousUTF8Text(t *testing.T) {
	workspaceID := util.MustParseUUID("11111111-1111-1111-1111-111111111111")
	left := externalIdentityLockKey(workspaceID, ExternalIdentityAlias{Namespace: "a", ExternalID: "bc"})
	right := externalIdentityLockKey(workspaceID, ExternalIdentityAlias{Namespace: "ab", ExternalID: "c"})
	if left == right {
		t.Fatal("length-prefixed identity lock keys collided")
	}
	if strings.ContainsRune(left, '\x00') || strings.ContainsRune(right, '\x00') {
		t.Fatal("identity lock key contains PostgreSQL-invalid NUL")
	}
}

func TestIssueExternalIdentityUpsertSameKeyConcurrentReturnsSameIssue(t *testing.T) {
	ctx := context.Background()
	pool := newExternalIdentityPool(t)
	queries := db.New(pool)
	fixture := createExternalIdentityFixture(t, ctx, pool)
	svc := NewIssueService(queries, pool, events.New(), nil, nil)

	params := IssueExternalIdentityUpsertParams{
		WorkspaceID:    fixture.workspaceID,
		Aliases:        []ExternalIdentityAlias{{Namespace: "github", ExternalID: "issue-123"}},
		Create:         externalCreateParams(fixture, "Imported GitHub issue"),
		MetadataPatch:  []byte(`{"source":"github"}`),
		CreatorType:    "member",
		CreatorID:      fixture.userID,
		IssueCreateOpt: IssueCreateOpts{},
	}

	const workers = 2
	start := make(chan struct{})
	results := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := svc.UpsertExternalIdentity(ctx, params)
			if err != nil {
				errs <- err
				return
			}
			results <- util.UUIDToString(res.Issue.ID)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("upsert: %v", err)
	}
	var first string
	for id := range results {
		if first == "" {
			first = id
			continue
		}
		if id != first {
			t.Fatalf("concurrent callers returned different issues: %s vs %s", first, id)
		}
	}
	var issueCount, identityCount, distinctIssues int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id = $1`, fixture.workspaceID).Scan(&issueCount); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*), count(DISTINCT issue_id)
		FROM issue_external_identity
		WHERE workspace_id = $1 AND namespace = 'github' AND external_id = 'issue-123'
	`, fixture.workspaceID).Scan(&identityCount, &distinctIssues); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if issueCount != 1 || identityCount != 1 || distinctIssues != 1 {
		t.Fatalf("same-key cardinality issues=%d identities=%d distinct_issue_ids=%d, want 1/1/1", issueCount, identityCount, distinctIssues)
	}
}

func TestIssueExternalIdentityUpsertConflictDoesNotMutate(t *testing.T) {
	ctx := context.Background()
	pool := newExternalIdentityPool(t)
	queries := db.New(pool)
	fixture := createExternalIdentityFixture(t, ctx, pool)
	svc := NewIssueService(queries, pool, events.New(), nil, nil)

	first, err := svc.UpsertExternalIdentity(ctx, IssueExternalIdentityUpsertParams{
		WorkspaceID:   fixture.workspaceID,
		Aliases:       []ExternalIdentityAlias{{Namespace: "jira", ExternalID: "A"}},
		Create:        externalCreateParams(fixture, "First"),
		MetadataPatch: []byte(`{"a":true}`),
		CreatorType:   "member",
		CreatorID:     fixture.userID,
	})
	if err != nil {
		t.Fatalf("seed first: %v", err)
	}
	second, err := svc.UpsertExternalIdentity(ctx, IssueExternalIdentityUpsertParams{
		WorkspaceID:   fixture.workspaceID,
		Aliases:       []ExternalIdentityAlias{{Namespace: "jira", ExternalID: "B"}},
		Create:        externalCreateParams(fixture, "Second"),
		MetadataPatch: []byte(`{"b":true}`),
		CreatorType:   "member",
		CreatorID:     fixture.userID,
	})
	if err != nil {
		t.Fatalf("seed second: %v", err)
	}

	conflictParams := IssueExternalIdentityUpsertParams{
		WorkspaceID:   fixture.workspaceID,
		Aliases:       []ExternalIdentityAlias{{Namespace: "jira", ExternalID: "A"}, {Namespace: "jira", ExternalID: "B"}},
		Create:        externalCreateParams(fixture, "Conflict"),
		MetadataPatch: []byte(`{"mutated":true}`),
		CreatorType:   "member",
		CreatorID:     fixture.userID,
	}
	const conflictWorkers = 2
	start := make(chan struct{})
	errs := make(chan error, conflictWorkers)
	var wg sync.WaitGroup
	for range conflictWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.UpsertExternalIdentity(ctx, conflictParams)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != ErrExternalIdentityConflict {
			t.Fatalf("concurrent conflict err = %v, want ErrExternalIdentityConflict", err)
		}
	}

	expectedMetadataKey := map[string]string{
		util.UUIDToString(first.Issue.ID):  "a",
		util.UUIDToString(second.Issue.ID): "b",
	}
	for _, issue := range []db.Issue{first.Issue, second.Issue} {
		var metadata []byte
		if err := pool.QueryRow(ctx, `SELECT metadata FROM issue WHERE id = $1`, issue.ID).Scan(&metadata); err != nil {
			t.Fatalf("read metadata: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(metadata, &decoded); err != nil {
			t.Fatalf("decode metadata: %v", err)
		}
		if _, exists := decoded["mutated"]; exists {
			t.Fatalf("conflict mutated issue %s", util.UUIDToString(issue.ID))
		}
		issueID := util.UUIDToString(issue.ID)
		expectedKey, ok := expectedMetadataKey[issueID]
		if !ok || decoded[expectedKey] != true {
			t.Fatalf("issue %s metadata=%v, want %s=true and no mutated key", issueID, decoded, expectedKey)
		}
	}
	var issueCount, identityCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id = $1`, fixture.workspaceID).Scan(&issueCount); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue_external_identity WHERE workspace_id = $1`, fixture.workspaceID).Scan(&identityCount); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if issueCount != 2 || identityCount != 2 {
		t.Fatalf("conflict cardinality issues=%d identities=%d, want 2/2", issueCount, identityCount)
	}
}

func TestIssueExternalIdentityUpsertVerifiesMappingAfterDoNothing(t *testing.T) {
	ctx := context.Background()
	pool := newExternalIdentityPool(t)
	fixture := createExternalIdentityFixture(t, ctx, pool)
	svc := NewIssueService(db.New(pool), pool, events.New(), nil, nil)
	target, err := svc.Create(ctx, externalCreateParams(fixture, "Competing mapped issue"), IssueCreateOpts{})
	if err != nil {
		t.Fatalf("create competing issue: %v", err)
	}
	const lockKey int64 = 360181
	if _, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION equ36_block_external_issue_create() RETURNS trigger AS $$
		BEGIN
			IF NEW.title = 'External mapping verify probe' THEN PERFORM pg_advisory_xact_lock(360181); END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		DROP TRIGGER IF EXISTS equ36_block_external_issue_create ON issue;
		CREATE TRIGGER equ36_block_external_issue_create AFTER INSERT ON issue
		FOR EACH ROW EXECUTE FUNCTION equ36_block_external_issue_create();
	`); err != nil {
		t.Fatalf("install mapping verification trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS equ36_block_external_issue_create ON issue`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS equ36_block_external_issue_create()`)
	})
	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Release()
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		t.Fatal(err)
	}
	defer blocker.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockKey)

	errCh := make(chan error, 1)
	go func() {
		_, upsertErr := svc.UpsertExternalIdentity(ctx, IssueExternalIdentityUpsertParams{
			WorkspaceID: fixture.workspaceID, Aliases: []ExternalIdentityAlias{{Namespace: "github-node", ExternalID: "do-nothing-conflict"}},
			Create: externalCreateParams(fixture, "External mapping verify probe"), CreatorType: "member", CreatorID: fixture.userID,
		})
		errCh <- upsertErr
	}()
	deadline := time.Now().Add(10 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND objid::bigint=$1 AND NOT granted)`, lockKey).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("upsert did not reach post-create insertion window")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO issue_external_identity(workspace_id, namespace, external_id, issue_id) VALUES($1,'github-node','do-nothing-conflict',$2)`, fixture.workspaceID, target.Issue.ID); err != nil {
		t.Fatalf("insert competing mapping: %v", err)
	}
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_unlock($1)`, lockKey); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; !errors.Is(err, ErrExternalIdentityConflict) {
		t.Fatalf("upsert error = %v, want ErrExternalIdentityConflict", err)
	}
	var mapped pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT issue_id FROM issue_external_identity WHERE workspace_id=$1 AND namespace='github-node' AND external_id='do-nothing-conflict'`, fixture.workspaceID).Scan(&mapped); err != nil {
		t.Fatal(err)
	}
	if mapped != target.Issue.ID {
		t.Fatalf("mapping changed to %s", util.UUIDToString(mapped))
	}
	var createdCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id=$1 AND title='External mapping verify probe'`, fixture.workspaceID).Scan(&createdCount); err != nil {
		t.Fatal(err)
	}
	if createdCount != 0 {
		t.Fatalf("conflicting upsert left %d created issues", createdCount)
	}
}

func TestIssueExternalIdentityUpsertExplicitTargetAndPreserveFields(t *testing.T) {
	ctx := context.Background()
	pool := newExternalIdentityPool(t)
	queries := db.New(pool)
	fixture := createExternalIdentityFixture(t, ctx, pool)
	svc := NewIssueService(queries, pool, events.New(), nil, nil)

	target, err := svc.Create(ctx, externalCreateParams(fixture, "Legacy target"), IssueCreateOpts{})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status = 'in_progress', priority = 'urgent' WHERE id = $1`, target.Issue.ID); err != nil {
		t.Fatalf("seed fields: %v", err)
	}

	res, err := svc.UpsertExternalIdentity(ctx, IssueExternalIdentityUpsertParams{
		WorkspaceID:    fixture.workspaceID,
		Aliases:        []ExternalIdentityAlias{{Namespace: "linear", ExternalID: "LIN-1"}},
		TargetIssueID:  target.Issue.ID,
		MetadataPatch:  []byte(`{"claimed":true}`),
		CreatorType:    "member",
		CreatorID:      fixture.userID,
		IssueCreateOpt: IssueCreateOpts{},
	})
	if err != nil {
		t.Fatalf("claim target: %v", err)
	}
	if util.UUIDToString(res.Issue.ID) != util.UUIDToString(target.Issue.ID) {
		t.Fatalf("claimed issue = %s, want target %s", util.UUIDToString(res.Issue.ID), util.UUIDToString(target.Issue.ID))
	}
	if res.Issue.Status != "in_progress" || res.Issue.Priority != "urgent" || res.Issue.Title != "Legacy target" {
		t.Fatalf("existing fields mutated: status=%s priority=%s title=%q", res.Issue.Status, res.Issue.Priority, res.Issue.Title)
	}
}

func TestIssueExternalIdentityUpsertOverlappingReverseOrderAliasesConverge(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool := newExternalIdentityPool(t)
	queries := db.New(pool)
	fixture := createExternalIdentityFixture(t, ctx, pool)
	svc := NewIssueService(queries, pool, events.New(), nil, nil)

	aliasSets := [][]ExternalIdentityAlias{
		{{Namespace: "taskthreads", ExternalID: "a"}, {Namespace: "taskthreads", ExternalID: "b"}},
		{{Namespace: "taskthreads", ExternalID: "c"}, {Namespace: "taskthreads", ExternalID: "b"}},
	}
	start := make(chan struct{})
	results := make(chan string, len(aliasSets))
	errs := make(chan error, len(aliasSets))
	var wg sync.WaitGroup
	for i, aliases := range aliasSets {
		wg.Add(1)
		go func(i int, aliases []ExternalIdentityAlias) {
			defer wg.Done()
			<-start
			res, err := svc.UpsertExternalIdentity(ctx, IssueExternalIdentityUpsertParams{
				WorkspaceID:   fixture.workspaceID,
				Aliases:       aliases,
				Create:        externalCreateParams(fixture, fmt.Sprintf("Overlapping import %d", i)),
				CreatorType:   "member",
				CreatorID:     fixture.userID,
				MetadataPatch: []byte(`{"source":"taskthreads"}`),
			})
			if err != nil {
				errs <- err
				return
			}
			results <- util.UUIDToString(res.Issue.ID)
		}(i, aliases)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("overlapping upsert: %v", err)
	}
	var issueID string
	for id := range results {
		if issueID == "" {
			issueID = id
		} else if id != issueID {
			t.Fatalf("overlapping callers returned different issues: %s vs %s", issueID, id)
		}
	}
	var issueCount, identityCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id = $1`, fixture.workspaceID).Scan(&issueCount); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue_external_identity WHERE workspace_id = $1`, fixture.workspaceID).Scan(&identityCount); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if issueCount != 1 || identityCount != 3 {
		t.Fatalf("issues=%d identities=%d, want 1/3", issueCount, identityCount)
	}
}

func TestIssueExternalIdentityUpsertConcurrentDisjointMetadataPatchesSurvive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool := newExternalIdentityPool(t)
	queries := db.New(pool)
	fixture := createExternalIdentityFixture(t, ctx, pool)
	svc := NewIssueService(queries, pool, events.New(), nil, nil)
	target, err := svc.Create(ctx, externalCreateParams(fixture, "Metadata target"), IssueCreateOpts{})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	patches := []struct {
		alias ExternalIdentityAlias
		patch []byte
	}{
		{alias: ExternalIdentityAlias{Namespace: "taskthreads", ExternalID: "meta-a"}, patch: []byte(`{"alpha":true}`)},
		{alias: ExternalIdentityAlias{Namespace: "taskthreads", ExternalID: "meta-b"}, patch: []byte(`{"beta":true}`)},
	}
	start := make(chan struct{})
	errs := make(chan error, len(patches))
	var wg sync.WaitGroup
	for _, item := range patches {
		wg.Add(1)
		go func(item struct {
			alias ExternalIdentityAlias
			patch []byte
		}) {
			defer wg.Done()
			<-start
			_, err := svc.UpsertExternalIdentity(ctx, IssueExternalIdentityUpsertParams{
				WorkspaceID:   fixture.workspaceID,
				Aliases:       []ExternalIdentityAlias{item.alias},
				TargetIssueID: target.Issue.ID,
				MetadataPatch: item.patch,
				CreatorType:   "member",
				CreatorID:     fixture.userID,
			})
			errs <- err
		}(item)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("metadata upsert: %v", err)
		}
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT metadata FROM issue WHERE id = $1`, target.Issue.ID).Scan(&raw); err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata["alpha"] != true || metadata["beta"] != true {
		t.Fatalf("disjoint patches lost: %s", raw)
	}
}

func TestIssueExternalIdentityUpsertRollbackLeavesNoIssueOrIdentityOrphan(t *testing.T) {
	ctx := context.Background()
	pool := newExternalIdentityPool(t)
	queries := db.New(pool)
	fixture := createExternalIdentityFixture(t, ctx, pool)
	svc := NewIssueService(queries, pool, events.New(), nil, nil)

	if _, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION equ4_reject_rollback_probe_metadata() RETURNS trigger AS $$
		BEGIN
			IF NEW.title = 'Rollback probe' AND NEW.metadata IS DISTINCT FROM OLD.metadata THEN
				RAISE EXCEPTION 'forced metadata failure for rollback probe';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER equ4_reject_rollback_probe_metadata
		BEFORE UPDATE OF metadata ON issue
		FOR EACH ROW EXECUTE FUNCTION equ4_reject_rollback_probe_metadata();
	`); err != nil {
		t.Fatalf("install rollback probe trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS equ4_reject_rollback_probe_metadata ON issue`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS equ4_reject_rollback_probe_metadata()`)
	})

	_, err := svc.UpsertExternalIdentity(ctx, IssueExternalIdentityUpsertParams{
		WorkspaceID:   fixture.workspaceID,
		Aliases:       []ExternalIdentityAlias{{Namespace: "taskthreads", ExternalID: "rollback"}},
		Create:        externalCreateParams(fixture, "Rollback probe"),
		MetadataPatch: []byte(`{"source":"taskthreads"}`),
		CreatorType:   "member",
		CreatorID:     fixture.userID,
	})
	if err == nil {
		t.Fatal("forced post-create metadata failure succeeded")
	}
	var issueCount, identityCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = 'Rollback probe'`, fixture.workspaceID).Scan(&issueCount); err != nil {
		t.Fatalf("count rollback issues: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue_external_identity WHERE workspace_id = $1 AND namespace = 'taskthreads' AND external_id = 'rollback'`, fixture.workspaceID).Scan(&identityCount); err != nil {
		t.Fatalf("count rollback identities: %v", err)
	}
	if issueCount != 0 || identityCount != 0 {
		t.Fatalf("rollback left orphans: issues=%d identities=%d", issueCount, identityCount)
	}
}

func TestIssueExternalIdentityUpsertProcessInterruptionRollsBackAllEffects(t *testing.T) {
	if os.Getenv("MULTICA_EXTERNAL_UPSERT_INTERRUPT_HELPER") == "1" {
		dsn := os.Getenv("MULTICA_EXTERNAL_UPSERT_INTERRUPT_DSN")
		cfg, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			t.Fatalf("parse helper dsn: %v", err)
		}
		if cfg.ConnConfig.RuntimeParams == nil {
			cfg.ConnConfig.RuntimeParams = make(map[string]string)
		}
		cfg.ConnConfig.RuntimeParams["application_name"] = os.Getenv("MULTICA_EXTERNAL_UPSERT_INTERRUPT_APP")
		pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
		if err != nil {
			t.Fatalf("helper pool: %v", err)
		}
		defer pool.Close()
		fixture := externalIdentityFixture{
			workspaceID: util.MustParseUUID(os.Getenv("MULTICA_EXTERNAL_UPSERT_INTERRUPT_WORKSPACE")),
			userID:      util.MustParseUUID(os.Getenv("MULTICA_EXTERNAL_UPSERT_INTERRUPT_USER")),
		}
		svc := NewIssueService(db.New(pool), pool, events.New(), nil, nil)
		_, err = svc.UpsertExternalIdentity(context.Background(), IssueExternalIdentityUpsertParams{
			WorkspaceID:   fixture.workspaceID,
			Aliases:       []ExternalIdentityAlias{{Namespace: "github-node", ExternalID: os.Getenv("MULTICA_EXTERNAL_UPSERT_INTERRUPT_EXTERNAL_ID")}},
			Create:        externalCreateParams(fixture, os.Getenv("MULTICA_EXTERNAL_UPSERT_INTERRUPT_TITLE")),
			MetadataPatch: []byte(`{"interrupt_probe":true}`), CreatorType: "member", CreatorID: fixture.userID,
		})
		if err != nil {
			t.Fatalf("helper upsert: %v", err)
		}
		return
	}

	ctx := context.Background()
	pool := newExternalIdentityPool(t)
	fixture := createExternalIdentityFixture(t, ctx, pool)
	token := fmt.Sprintf("interrupt-%d", time.Now().UnixNano())
	title := "Interrupt rollback " + token
	const lockKey int64 = 360180
	if _, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION equ36_block_external_upsert_interrupt() RETURNS trigger AS $$
		BEGIN
			IF NEW.metadata ? 'interrupt_probe' THEN
				PERFORM pg_advisory_xact_lock(360180);
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		DROP TRIGGER IF EXISTS equ36_block_external_upsert_interrupt ON issue;
		CREATE TRIGGER equ36_block_external_upsert_interrupt
		AFTER UPDATE OF metadata ON issue
		FOR EACH ROW EXECUTE FUNCTION equ36_block_external_upsert_interrupt();
	`); err != nil {
		t.Fatalf("install interruption trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS equ36_block_external_upsert_interrupt ON issue`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS equ36_block_external_upsert_interrupt()`)
	})

	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire blocker: %v", err)
	}
	defer blocker.Release()
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		t.Fatalf("lock blocker: %v", err)
	}
	defer blocker.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockKey)

	appName := "equ36-interrupt-" + token
	dsn := pool.Config().ConnString()
	cmd := exec.Command(os.Args[0], "-test.run=^TestIssueExternalIdentityUpsertProcessInterruptionRollsBackAllEffects$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"MULTICA_EXTERNAL_UPSERT_INTERRUPT_HELPER=1",
		"MULTICA_EXTERNAL_UPSERT_INTERRUPT_DSN="+dsn,
		"MULTICA_EXTERNAL_UPSERT_INTERRUPT_APP="+appName,
		"MULTICA_EXTERNAL_UPSERT_INTERRUPT_WORKSPACE="+util.UUIDToString(fixture.workspaceID),
		"MULTICA_EXTERNAL_UPSERT_INTERRUPT_USER="+util.UUIDToString(fixture.userID),
		"MULTICA_EXTERNAL_UPSERT_INTERRUPT_EXTERNAL_ID="+token,
		"MULTICA_EXTERNAL_UPSERT_INTERRUPT_TITLE="+title,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, appName).Scan(&blocked); err != nil {
			t.Fatalf("observe helper: %v", err)
		}
		if blocked {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !blocked {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		t.Fatal("helper did not block after alias mutation")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("terminate helper: %v", err)
	}
	_, _ = cmd.Process.Wait()
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_unlock($1)`, lockKey); err != nil {
		t.Fatalf("unlock blocker: %v", err)
	}

	var issueCount, aliasCount, metadataCount int
	for deadline = time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id=$1 AND title=$2`, fixture.workspaceID, title).Scan(&issueCount)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM issue_external_identity WHERE workspace_id=$1 AND namespace='github-node' AND external_id=$2`, fixture.workspaceID, token).Scan(&aliasCount)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id=$1 AND metadata @> '{"interrupt_probe":true}'::jsonb`, fixture.workspaceID).Scan(&metadataCount)
		if issueCount == 0 && aliasCount == 0 && metadataCount == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if issueCount != 0 || aliasCount != 0 || metadataCount != 0 {
		t.Fatalf("interrupted process left effects: issues=%d aliases=%d metadata=%d", issueCount, aliasCount, metadataCount)
	}
}

func TestIssueExternalIdentityUpsertRejectsCrossWorkspaceTarget(t *testing.T) {
	ctx := context.Background()
	pool := newExternalIdentityPool(t)
	queries := db.New(pool)
	first := createExternalIdentityFixture(t, ctx, pool)
	second := createExternalIdentityFixture(t, ctx, pool)
	svc := NewIssueService(queries, pool, events.New(), nil, nil)
	target, err := svc.Create(ctx, externalCreateParams(first, "Other workspace target"), IssueCreateOpts{})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	_, err = svc.UpsertExternalIdentity(ctx, IssueExternalIdentityUpsertParams{
		WorkspaceID:   second.workspaceID,
		Aliases:       []ExternalIdentityAlias{{Namespace: "taskthreads", ExternalID: "cross-workspace"}},
		TargetIssueID: target.Issue.ID,
		MetadataPatch: []byte(`{"source":"taskthreads"}`),
		CreatorType:   "member",
		CreatorID:     second.userID,
	})
	if !errors.Is(err, ErrExternalIdentityTargetNotFound) {
		t.Fatalf("cross-workspace target error = %v, want ErrExternalIdentityTargetNotFound", err)
	}
	var identityCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue_external_identity WHERE workspace_id = $1`, second.workspaceID).Scan(&identityCount); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if identityCount != 0 {
		t.Fatalf("cross-workspace target created %d identities", identityCount)
	}
}
