package gitlab

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// recordingEnqueuer is a test stub that implements TaskEnqueuer. It records
// each call (issueID + triggerCommentID) and returns a zero task row. Tests
// assert on the recorded calls to verify the webhook enqueue logic without
// pulling in the real *service.TaskService (which would require hub/bus).
type recordingEnqueuer struct {
	mu    sync.Mutex
	calls []enqueueCall
}

type enqueueCall struct {
	issueID string
}

func (r *recordingEnqueuer) EnqueueTaskForIssue(_ context.Context, issue db.Issue, _ ...pgtype.UUID) (db.AgentTaskQueue, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, enqueueCall{issueID: uuidString(issue.ID)})
	return db.AgentTaskQueue{}, nil
}

func (r *recordingEnqueuer) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// seedAgentNamed inserts a runtime + agent named `name` in the workspace.
// The slug in agent labels derives from the agent's name (lowercased,
// spaces → hyphens), so passing "handler-test-agent" lets a webhook payload
// carrying the label `agent::handler-test-agent` resolve to this agent.
func seedAgentNamed(t *testing.T, pool *pgxpool.Pool, workspaceID, name string) string {
	t.Helper()
	var runtimeID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
		VALUES ($1, $2, 'cloud', 'test')
		RETURNING id
	`, workspaceID, fmt.Sprintf("rt-%s", name)).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	var agentID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, visibility, max_concurrent_tasks, owner_id, runtime_id)
		VALUES ($1, $2, 'cloud', '{}'::jsonb, 'workspace', 1, NULL, $3)
		RETURNING id
	`, workspaceID, name, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	return agentID
}

func TestApplyIssueHookEvent_UpsertsCachedIssue(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)

	queries := db.New(pool)
	deps := WebhookDeps{Queries: queries, WorkspaceID: mustPGUUID(t, wsID), ProjectID: 7}

	body := []byte(`{
		"object_kind": "issue",
		"object_attributes": {
			"iid": 42,
			"title": "From webhook",
			"description": "body",
			"state": "opened",
			"updated_at": "2026-04-17T10:00:00Z",
			"labels": [{"title": "status::in_progress"}]
		}
	}`)

	if err := ApplyIssueHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyIssueHookEvent: %v", err)
	}

	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: mustPGUUID(t, wsID),
		GitlabIid:   pgtype.Int4{Int32: 42, Valid: true},
	})
	if err != nil {
		t.Fatalf("GetIssueByGitlabIID: %v", err)
	}
	if row.Title != "From webhook" {
		t.Errorf("title = %q, want From webhook", row.Title)
	}
	if row.Status != "in_progress" {
		t.Errorf("status = %q, want in_progress", row.Status)
	}
}

func TestApplyIssueHookEvent_SkipsWhenCacheNewer(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)

	queries := db.New(pool)
	wsUUID := mustPGUUID(t, wsID)

	// Pre-seed a cache row with a newer external_updated_at.
	_, err := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:       wsUUID,
		GitlabIid:         pgtype.Int4{Int32: 42, Valid: true},
		GitlabProjectID:   pgtype.Int8{Int64: 7, Valid: true},
		Title:             "Already newer",
		Status:            "todo",
		Priority:          "none",
		ExternalUpdatedAt: parseTS("2026-04-18T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	deps := WebhookDeps{Queries: queries, WorkspaceID: wsUUID, ProjectID: 7}

	body := []byte(`{
		"object_kind": "issue",
		"object_attributes": {
			"iid": 42,
			"title": "Stale!",
			"state": "opened",
			"updated_at": "2026-04-17T10:00:00Z"
		}
	}`)

	if err := ApplyIssueHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyIssueHookEvent: %v", err)
	}

	row, _ := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: wsUUID,
		GitlabIid:   pgtype.Int4{Int32: 42, Valid: true},
	})
	if row.Title != "Already newer" {
		t.Errorf("title = %q, expected stale event to be skipped", row.Title)
	}
}

func TestApplyNoteHookEvent_UpsertsComment(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)

	queries := db.New(pool)

	row, _ := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:     wsUUID,
		GitlabIid:       pgtype.Int4{Int32: 42, Valid: true},
		GitlabProjectID: pgtype.Int8{Int64: 7, Valid: true},
		Title:           "Parent issue",
		Status:          "todo",
		Priority:        "none",
	})

	deps := WebhookDeps{Queries: queries, WorkspaceID: wsUUID, ProjectID: 7}

	body := []byte(`{
		"object_kind": "note",
		"object_attributes": {
			"id": 100,
			"note": "Looks good!",
			"system": false,
			"updated_at": "2026-04-17T11:00:00Z",
			"noteable_type": "Issue"
		},
		"issue": {"iid": 42},
		"user": {"id": 555}
	}`)

	if err := ApplyNoteHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyNoteHookEvent: %v", err)
	}

	var content string
	pool.QueryRow(context.Background(),
		`SELECT content FROM comment WHERE issue_id = $1::uuid AND gitlab_note_id = 100`,
		uuidString(row.ID)).Scan(&content)
	if content != "Looks good!" {
		t.Errorf("content = %q", content)
	}
}

func TestApplyNoteHookEvent_IgnoresNonIssueNotes(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)
	deps := WebhookDeps{Queries: queries, WorkspaceID: wsUUID, ProjectID: 7}

	body := []byte(`{
		"object_kind": "note",
		"object_attributes": {"id": 200, "note": "not an issue", "noteable_type": "MergeRequest"}
	}`)

	if err := ApplyNoteHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyNoteHookEvent: %v", err)
	}

	var count int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM comment WHERE gitlab_note_id = 200`).Scan(&count)
	if count != 0 {
		t.Errorf("MR-note should be ignored, but %d row(s) cached", count)
	}
}

func TestApplyEmojiHookEvent_UpsertsReaction(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	// Seed an issue with iid=42 BUT global gitlab_issue_id=1001 (different
	// from iid). The emoji event's awardable_id will be 1001 (the global id),
	// proving the handler uses the global-id lookup path.
	row, err := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:     wsUUID,
		GitlabIid:       pgtype.Int4{Int32: 42, Valid: true},
		GitlabProjectID: pgtype.Int8{Int64: 7, Valid: true},
		GitlabIssueID:   pgtype.Int8{Int64: 1001, Valid: true},
		Title:           "Parent",
		Status:          "todo",
		Priority:        "none",
	})
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	deps := WebhookDeps{Queries: queries, WorkspaceID: wsUUID, ProjectID: 7}

	body := []byte(`{
		"object_kind": "emoji",
		"object_attributes": {
			"id": 500,
			"name": "thumbsup",
			"awardable_type": "Issue",
			"awardable_id": 1001,
			"updated_at": "2026-04-17T12:00:00Z"
		},
		"user": {"id": 555}
	}`)

	if err := ApplyEmojiHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyEmojiHookEvent: %v", err)
	}

	var emoji string
	pool.QueryRow(context.Background(),
		`SELECT emoji FROM issue_reaction WHERE issue_id = $1::uuid AND gitlab_award_id = 500`,
		uuidString(row.ID)).Scan(&emoji)
	if emoji != "thumbsup" {
		t.Errorf("emoji = %q", emoji)
	}
}

// TestApplyEmojiHookEvent_IgnoresNonIssueNonNoteAwards asserts that awardable
// types outside of Issue/Note (MergeRequest, Snippet, unknown) are silently
// skipped — Multica only mirrors issues + their comments.
func TestApplyEmojiHookEvent_IgnoresNonIssueNonNoteAwards(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	deps := WebhookDeps{Queries: db.New(pool), WorkspaceID: mustPGUUID(t, wsID), ProjectID: 7}

	body := []byte(`{
		"object_kind": "emoji",
		"object_attributes": {"id": 600, "name": "tada", "awardable_type": "MergeRequest", "awardable_id": 99}
	}`)
	if err := ApplyEmojiHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyEmojiHookEvent: %v", err)
	}
	var issueCount, commentCount int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM issue_reaction WHERE gitlab_award_id = 600`).Scan(&issueCount)
	pool.QueryRow(context.Background(), `SELECT count(*) FROM comment_reaction WHERE gitlab_award_id = 600`).Scan(&commentCount)
	if issueCount != 0 || commentCount != 0 {
		t.Errorf("MR-level award should be ignored; issue_reaction=%d comment_reaction=%d", issueCount, commentCount)
	}
}

func TestApplyLabelHookEvent_CreateUpsertsLabel(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)
	deps := WebhookDeps{Queries: queries, WorkspaceID: wsUUID, ProjectID: 7}

	body := []byte(`{
		"object_kind": "label",
		"event_type": "label",
		"object_attributes": {
			"id": 700,
			"title": "needs-design",
			"color": "#ff8800",
			"description": "ux input required",
			"updated_at": "2026-04-17T13:00:00Z",
			"action": "create"
		}
	}`)

	if err := ApplyLabelHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyLabelHookEvent: %v", err)
	}

	rows, _ := queries.ListGitlabLabels(context.Background(), wsUUID)
	found := false
	for _, l := range rows {
		if l.GitlabLabelID == 700 && l.Name == "needs-design" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("label not found in cache; got %+v", rows)
	}
}

// TestApplyNoteHookEvent_ResolvesAuthorViaUserConnection asserts that when
// a note arrives from a GitLab user who registered a PAT in Multica, the
// handler writes comment.author_type='member' + author_id=<multica_user_uuid>.
func TestApplyNoteHookEvent_ResolvesAuthorViaUserConnection(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	userID := makeUser(t, pool, "note-author@example.com")
	seedUserGitlabConnection(t, pool, userID, wsID, 777, "alice")

	queries := db.New(pool)
	row, _ := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:     wsUUID,
		GitlabIid:       pgtype.Int4{Int32: 42, Valid: true},
		GitlabProjectID: pgtype.Int8{Int64: 7, Valid: true},
		Title:           "Parent",
		Status:          "todo",
		Priority:        "none",
	})

	deps := WebhookDeps{
		Queries:     queries,
		WorkspaceID: wsUUID,
		ProjectID:   7,
		Resolver:    newTestResolver(queries),
	}

	body := []byte(`{
		"object_kind": "note",
		"object_attributes": {
			"id": 500,
			"note": "Looks good!",
			"system": false,
			"updated_at": "2026-04-17T11:00:00Z",
			"noteable_type": "Issue"
		},
		"issue": {"iid": 42},
		"user": {"id": 777}
	}`)

	if err := ApplyNoteHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyNoteHookEvent: %v", err)
	}

	var authorType, authorID string
	var gitlabAuthorID int64
	err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(author_type, ''), COALESCE(author_id::text, ''), COALESCE(gitlab_author_user_id, 0)
		 FROM comment
		 WHERE issue_id = $1::uuid AND gitlab_note_id = 500`,
		uuidString(row.ID)).Scan(&authorType, &authorID, &gitlabAuthorID)
	if err != nil {
		t.Fatalf("select comment: %v", err)
	}
	if authorType != "member" {
		t.Errorf("author_type = %q, want member", authorType)
	}
	if authorID != userID {
		t.Errorf("author_id = %q, want %q", authorID, userID)
	}
	if gitlabAuthorID != 777 {
		t.Errorf("gitlab_author_user_id = %d, want 777", gitlabAuthorID)
	}
}

// TestApplyNoteHookEvent_UnmappedAuthorLeavesNullRefs asserts that when a
// note arrives from a GitLab user with no mapping, author_type/author_id
// stay NULL and gitlab_author_user_id retains the raw id.
func TestApplyNoteHookEvent_UnmappedAuthorLeavesNullRefs(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	row, _ := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:     wsUUID,
		GitlabIid:       pgtype.Int4{Int32: 42, Valid: true},
		GitlabProjectID: pgtype.Int8{Int64: 7, Valid: true},
		Title:           "Parent",
		Status:          "todo",
		Priority:        "none",
	})

	deps := WebhookDeps{
		Queries:     queries,
		WorkspaceID: wsUUID,
		ProjectID:   7,
		Resolver:    newTestResolver(queries),
	}

	body := []byte(`{
		"object_kind": "note",
		"object_attributes": {
			"id": 600,
			"note": "hi",
			"system": false,
			"updated_at": "2026-04-17T11:00:00Z",
			"noteable_type": "Issue"
		},
		"issue": {"iid": 42},
		"user": {"id": 999}
	}`)

	if err := ApplyNoteHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyNoteHookEvent: %v", err)
	}

	var authorType pgtype.Text
	var authorID pgtype.UUID
	var gitlabAuthorID int64
	err := pool.QueryRow(context.Background(),
		`SELECT author_type, author_id, COALESCE(gitlab_author_user_id, 0)
		 FROM comment
		 WHERE issue_id = $1::uuid AND gitlab_note_id = 600`,
		uuidString(row.ID)).Scan(&authorType, &authorID, &gitlabAuthorID)
	if err != nil {
		t.Fatalf("select comment: %v", err)
	}
	if authorType.Valid {
		t.Errorf("author_type = %+v, want NULL for unmapped user", authorType)
	}
	if authorID.Valid {
		t.Errorf("author_id = %+v, want NULL for unmapped user", authorID)
	}
	if gitlabAuthorID != 999 {
		t.Errorf("gitlab_author_user_id = %d, want 999", gitlabAuthorID)
	}
}

// TestApplyIssueHookEvent_ResolvesNativeAssigneeToMember asserts the issue
// hook reverse-resolves a native GitLab assignee to a Multica member.
func TestApplyIssueHookEvent_ResolvesNativeAssigneeToMember(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	userID := makeUser(t, pool, "assignee-member@example.com")
	seedUserGitlabConnection(t, pool, userID, wsID, 42, "bob")

	queries := db.New(pool)
	deps := WebhookDeps{
		Queries:     queries,
		WorkspaceID: wsUUID,
		ProjectID:   7,
		Resolver:    newTestResolver(queries),
	}

	body := []byte(`{
		"object_kind": "issue",
		"object_attributes": {
			"iid": 77,
			"title": "Assign me",
			"state": "opened",
			"updated_at": "2026-04-17T10:00:00Z",
			"labels": []
		},
		"assignees": [{"id": 42}],
		"user": {"id": 42}
	}`)
	if err := ApplyIssueHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyIssueHookEvent: %v", err)
	}

	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: wsUUID,
		GitlabIid:   pgtype.Int4{Int32: 77, Valid: true},
	})
	if err != nil {
		t.Fatalf("GetIssueByGitlabIID: %v", err)
	}
	if !row.AssigneeType.Valid || row.AssigneeType.String != "member" {
		t.Errorf("assignee_type = %+v, want member", row.AssigneeType)
	}
	if uuidString(row.AssigneeID) != userID {
		t.Errorf("assignee_id = %q, want %q", uuidString(row.AssigneeID), userID)
	}
}

// TestApplyIssueHookEvent_FallsBackToGitlabUserForUnmapped asserts that when
// the native GitLab assignee has no user_gitlab_connection but is cached as
// a gitlab_project_member, the hook writes assignee_type='gitlab_user' and
// assignee_id=<member_row_uuid>.
func TestApplyIssueHookEvent_FallsBackToGitlabUserForUnmapped(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	memberRowID := seedGitlabProjectMember(t, pool, wsID, 555, "carol", "Carol")

	queries := db.New(pool)
	deps := WebhookDeps{
		Queries:     queries,
		WorkspaceID: wsUUID,
		ProjectID:   7,
		Resolver:    newTestResolver(queries),
	}

	body := []byte(`{
		"object_kind": "issue",
		"object_attributes": {
			"iid": 88,
			"title": "External assignee",
			"state": "opened",
			"updated_at": "2026-04-17T10:00:00Z",
			"labels": []
		},
		"assignees": [{"id": 555}],
		"user": {"id": 555}
	}`)
	if err := ApplyIssueHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyIssueHookEvent: %v", err)
	}

	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: wsUUID,
		GitlabIid:   pgtype.Int4{Int32: 88, Valid: true},
	})
	if err != nil {
		t.Fatalf("GetIssueByGitlabIID: %v", err)
	}
	if !row.AssigneeType.Valid || row.AssigneeType.String != "gitlab_user" {
		t.Errorf("assignee_type = %+v, want gitlab_user", row.AssigneeType)
	}
	if uuidString(row.AssigneeID) != memberRowID {
		t.Errorf("assignee_id = %q, want %q", uuidString(row.AssigneeID), memberRowID)
	}
}

// TestApplyEmojiHookEvent_NoteLevelAwardUpsertsCommentReaction asserts that
// awardable_type='Note' awards are mirrored into comment_reaction (Phase 4
// lifted the Phase 2b filter that dropped them).
func TestApplyEmojiHookEvent_NoteLevelAwardUpsertsCommentReaction(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	// Seed a parent issue and a comment with gitlab_note_id=888.
	issueRow, _ := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:     wsUUID,
		GitlabIid:       pgtype.Int4{Int32: 42, Valid: true},
		GitlabProjectID: pgtype.Int8{Int64: 7, Valid: true},
		Title:           "Parent",
		Status:          "todo",
		Priority:        "none",
	})
	commentRow, err := queries.UpsertCommentFromGitlab(context.Background(), db.UpsertCommentFromGitlabParams{
		WorkspaceID:       wsUUID,
		IssueID:           issueRow.ID,
		Content:           "hi",
		Type:              "comment",
		GitlabNoteID:      pgtype.Int8{Int64: 888, Valid: true},
		ExternalUpdatedAt: parseTS("2026-04-17T10:00:00Z"),
	})
	if err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	deps := WebhookDeps{
		Queries:     queries,
		WorkspaceID: wsUUID,
		ProjectID:   7,
		Resolver:    newTestResolver(queries),
	}

	body := []byte(`{
		"object_kind": "emoji",
		"object_attributes": {
			"id": 1234,
			"name": "tada",
			"awardable_type": "Note",
			"awardable_id": 888,
			"updated_at": "2026-04-17T12:00:00Z"
		},
		"user": {"id": 7}
	}`)
	if err := ApplyEmojiHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyEmojiHookEvent: %v", err)
	}

	var emoji string
	var actorType pgtype.Text
	var gitlabActorID int64
	err = pool.QueryRow(context.Background(),
		`SELECT emoji, actor_type, COALESCE(gitlab_actor_user_id, 0)
		 FROM comment_reaction
		 WHERE comment_id = $1::uuid AND gitlab_award_id = 1234`,
		uuidString(commentRow.ID)).Scan(&emoji, &actorType, &gitlabActorID)
	if err != nil {
		t.Fatalf("select comment_reaction: %v", err)
	}
	if emoji != "tada" {
		t.Errorf("emoji = %q, want tada", emoji)
	}
	if actorType.Valid {
		t.Errorf("actor_type = %+v, want NULL for unmapped user", actorType)
	}
	if gitlabActorID != 7 {
		t.Errorf("gitlab_actor_user_id = %d, want 7", gitlabActorID)
	}
}

// TestApplyEmojiHookEvent_IssueLevelResolvesActorToMember asserts the actor
// reverse-resolution on issue-level awards also works.
func TestApplyEmojiHookEvent_IssueLevelResolvesActorToMember(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	userID := makeUser(t, pool, "reactor@example.com")
	seedUserGitlabConnection(t, pool, userID, wsID, 321, "dave")

	queries := db.New(pool)
	row, err := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:     wsUUID,
		GitlabIid:       pgtype.Int4{Int32: 42, Valid: true},
		GitlabProjectID: pgtype.Int8{Int64: 7, Valid: true},
		GitlabIssueID:   pgtype.Int8{Int64: 2001, Valid: true},
		Title:           "Parent",
		Status:          "todo",
		Priority:        "none",
	})
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	deps := WebhookDeps{
		Queries:     queries,
		WorkspaceID: wsUUID,
		ProjectID:   7,
		Resolver:    newTestResolver(queries),
	}

	body := []byte(`{
		"object_kind": "emoji",
		"object_attributes": {
			"id": 4321,
			"name": "heart",
			"awardable_type": "Issue",
			"awardable_id": 2001,
			"updated_at": "2026-04-17T12:00:00Z"
		},
		"user": {"id": 321}
	}`)
	if err := ApplyEmojiHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyEmojiHookEvent: %v", err)
	}

	var actorType string
	var actorID string
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(actor_type, ''), COALESCE(actor_id::text, '')
		 FROM issue_reaction
		 WHERE issue_id = $1::uuid AND gitlab_award_id = 4321`,
		uuidString(row.ID)).Scan(&actorType, &actorID)
	if err != nil {
		t.Fatalf("select issue_reaction: %v", err)
	}
	if actorType != "member" {
		t.Errorf("actor_type = %q, want member", actorType)
	}
	if actorID != userID {
		t.Errorf("actor_id = %q, want %q", actorID, userID)
	}
}

func TestApplyLabelHookEvent_DeleteRemovesLabel(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	queries.UpsertGitlabLabel(context.Background(), db.UpsertGitlabLabelParams{
		WorkspaceID:   wsUUID,
		GitlabLabelID: 800,
		Name:          "obsolete",
		Color:         "#000",
		Description:   "",
	})

	deps := WebhookDeps{Queries: queries, WorkspaceID: wsUUID, ProjectID: 7}
	body := []byte(`{
		"object_kind": "label",
		"event_type": "label",
		"object_attributes": {"id": 800, "action": "delete"}
	}`)
	if err := ApplyLabelHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyLabelHookEvent: %v", err)
	}

	rows, _ := queries.ListGitlabLabels(context.Background(), wsUUID)
	for _, l := range rows {
		if l.GitlabLabelID == 800 {
			t.Errorf("label 800 should be gone, but found %+v", l)
		}
	}
}

// TestApplyIssueHookEvent_EnqueuesAgentTaskOnNewAssignment asserts that when a
// GitLab webhook lands for an issue that was NOT previously agent-assigned
// and now carries ~agent::<slug>, the handler enqueues exactly one agent
// task (mirroring the POST/PATCH write-through path in handler/issue.go).
func TestApplyIssueHookEvent_EnqueuesAgentTaskOnNewAssignment(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	seedAgentNamed(t, pool, wsID, "builder")

	enq := &recordingEnqueuer{}
	deps := WebhookDeps{
		Queries:      queries,
		WorkspaceID:  wsUUID,
		ProjectID:    7,
		Resolver:     newTestResolver(queries),
		TaskEnqueuer: enq,
	}

	body := []byte(`{
		"object_kind": "issue",
		"object_attributes": {
			"iid": 900,
			"title": "Assign agent from GitLab UI",
			"state": "opened",
			"updated_at": "2026-04-17T10:00:00Z",
			"labels": [{"title": "agent::builder"}]
		}
	}`)
	if err := ApplyIssueHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyIssueHookEvent: %v", err)
	}

	if got := enq.callCount(); got != 1 {
		t.Fatalf("EnqueueTaskForIssue calls = %d, want 1", got)
	}

	// The enqueued issue should be the one just upserted.
	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: wsUUID,
		GitlabIid:   pgtype.Int4{Int32: 900, Valid: true},
	})
	if err != nil {
		t.Fatalf("GetIssueByGitlabIID: %v", err)
	}
	if enq.calls[0].issueID != uuidString(row.ID) {
		t.Errorf("enqueued issue_id = %q, want %q", enq.calls[0].issueID, uuidString(row.ID))
	}
}

// TestApplyIssueHookEvent_DoesNotEnqueueOnUnchangedAgent asserts that when a
// webhook replays (or delivers an unrelated update like title-only) while the
// same agent stays assigned, no additional task is enqueued. This guards the
// coalescing behavior — GitLab happily re-sends the same payload and also
// fires issue-hook on every attribute change.
func TestApplyIssueHookEvent_DoesNotEnqueueOnUnchangedAgent(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	agentID := seedAgentNamed(t, pool, wsID, "builder")

	// Pre-seed a cache row already assigned to the agent, with an older
	// external_updated_at than the incoming webhook (so the stale-event
	// guard doesn't short-circuit before the enqueue gate runs).
	assigneeUUID := mustPGUUID(t, agentID)
	_, err := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:       wsUUID,
		GitlabIid:         pgtype.Int4{Int32: 901, Valid: true},
		GitlabProjectID:   pgtype.Int8{Int64: 7, Valid: true},
		Title:             "Already agent-assigned",
		Status:            "todo",
		Priority:          "none",
		AssigneeType:      pgtype.Text{String: "agent", Valid: true},
		AssigneeID:        assigneeUUID,
		ExternalUpdatedAt: parseTS("2026-04-17T09:00:00Z"),
	})
	if err != nil {
		t.Fatalf("seed prior: %v", err)
	}

	enq := &recordingEnqueuer{}
	deps := WebhookDeps{
		Queries:      queries,
		WorkspaceID:  wsUUID,
		ProjectID:    7,
		Resolver:     newTestResolver(queries),
		TaskEnqueuer: enq,
	}

	// Title changed but same agent still assigned via the label.
	body := []byte(`{
		"object_kind": "issue",
		"object_attributes": {
			"iid": 901,
			"title": "Title edited — same agent",
			"state": "opened",
			"updated_at": "2026-04-17T10:00:00Z",
			"labels": [{"title": "agent::builder"}]
		}
	}`)
	if err := ApplyIssueHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyIssueHookEvent: %v", err)
	}

	if got := enq.callCount(); got != 0 {
		t.Fatalf("EnqueueTaskForIssue calls = %d, want 0 (same agent still assigned)", got)
	}
}

// TestApplyIssueHookEvent_DoesNotEnqueueOnNonAgentAssignee asserts that when
// an issue-hook carries no agent label (plain edit, human assignee, or no
// assignee at all), no task is enqueued.
func TestApplyIssueHookEvent_DoesNotEnqueueOnNonAgentAssignee(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	enq := &recordingEnqueuer{}
	deps := WebhookDeps{
		Queries:      queries,
		WorkspaceID:  wsUUID,
		ProjectID:    7,
		Resolver:     newTestResolver(queries),
		TaskEnqueuer: enq,
	}

	// No assignees, no agent label, just a vanilla issue.
	body := []byte(`{
		"object_kind": "issue",
		"object_attributes": {
			"iid": 902,
			"title": "Plain issue, no agent",
			"state": "opened",
			"updated_at": "2026-04-17T10:00:00Z",
			"labels": [{"title": "status::in_progress"}]
		}
	}`)
	if err := ApplyIssueHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyIssueHookEvent: %v", err)
	}

	if got := enq.callCount(); got != 0 {
		t.Fatalf("EnqueueTaskForIssue calls = %d, want 0 (no agent assignment)", got)
	}
}

// TestApplyIssueHookEvent_DoesNotEnqueueOnBacklogAgentAssignment asserts the
// backlog gate — an agent assigned to a backlog issue is parked, not run.
// Mirrors handler.shouldEnqueueAgentTask's status != "backlog" check so the
// webhook path and the Multica write-through path agree on intent.
func TestApplyIssueHookEvent_DoesNotEnqueueOnBacklogAgentAssignment(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	seedAgentNamed(t, pool, wsID, "builder")

	enq := &recordingEnqueuer{}
	deps := WebhookDeps{
		Queries:      queries,
		WorkspaceID:  wsUUID,
		ProjectID:    7,
		Resolver:     newTestResolver(queries),
		TaskEnqueuer: enq,
	}

	body := []byte(`{
		"object_kind": "issue",
		"object_attributes": {
			"iid": 903,
			"title": "Parked in backlog with agent",
			"state": "opened",
			"updated_at": "2026-04-17T10:00:00Z",
			"labels": [{"title": "agent::builder"}, {"title": "status::backlog"}]
		}
	}`)
	if err := ApplyIssueHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyIssueHookEvent: %v", err)
	}

	if got := enq.callCount(); got != 0 {
		t.Fatalf("EnqueueTaskForIssue calls = %d, want 0 (backlog status should park the agent)", got)
	}
}
