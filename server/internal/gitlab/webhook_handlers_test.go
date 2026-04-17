package gitlab

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

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

	row, _ := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:     wsUUID,
		GitlabIid:       pgtype.Int4{Int32: 42, Valid: true},
		GitlabProjectID: pgtype.Int8{Int64: 7, Valid: true},
		Title:           "Parent",
		Status:          "todo",
		Priority:        "none",
	})

	deps := WebhookDeps{Queries: queries, WorkspaceID: wsUUID, ProjectID: 7}

	body := []byte(`{
		"object_kind": "emoji",
		"object_attributes": {
			"id": 500,
			"name": "thumbsup",
			"awardable_type": "Issue",
			"awardable_id": 42,
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

func TestApplyEmojiHookEvent_IgnoresNonIssueAwards(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	deps := WebhookDeps{Queries: db.New(pool), WorkspaceID: mustPGUUID(t, wsID), ProjectID: 7}

	body := []byte(`{
		"object_kind": "emoji",
		"object_attributes": {"id": 600, "name": "tada", "awardable_type": "Note", "awardable_id": 99}
	}`)
	if err := ApplyEmojiHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyEmojiHookEvent: %v", err)
	}
	var count int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM issue_reaction WHERE gitlab_award_id = 600`).Scan(&count)
	if count != 0 {
		t.Errorf("note-level award should be ignored, %d row(s) cached", count)
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
