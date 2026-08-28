package scheduler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// mustItem builds an agent_help_requested InboxItem with the given source
// details, for use in pure aggregation tests.
func mustHelpItem(t *testing.T, ws, recipient, taskID, agentID string, details map[string]any) db.InboxItem {
	t.Helper()
	raw, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("marshal details: %v", err)
	}
	it := db.InboxItem{
		ID:            dbid.NewV7(),
		WorkspaceID:   mustUUID(t, ws),
		RecipientType: "member",
		RecipientID:   mustUUID(t, recipient),
		Type:          InboxItemTypeAgentHelpRequested,
		Severity:      "action_required",
		Details:       raw,
	}
	return it
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	if s == "" {
		return pgtype.UUID{}
	}
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

func TestBuildHelpDigestDetails_CountsAndEntries(t *testing.T) {
	items := []db.InboxItem{
		mustHelpItem(t, "11111111-1111-1111-1111-111111111111",
			"aaaaaaaa-1111-1111-1111-111111111111",
			"task-1", "agent-1",
			map[string]any{
				"task_id":        "task-1",
				"agent_id":       "agent-1",
				"blocked_reason": "missing api key",
				"needs":          []string{"credential", "schema"},
				"confidence":     0.4,
			}),
		mustHelpItem(t, "11111111-1111-1111-1111-111111111111",
			"aaaaaaaa-1111-1111-1111-111111111111",
			"task-2", "agent-2",
			map[string]any{
				"task_id":  "task-2",
				"agent_id": "agent-2",
				"needs":    []string{"approval"},
			}),
	}

	det, err := buildHelpDigestDetails(items)
	if err != nil {
		t.Fatalf("buildHelpDigestDetails: %v", err)
	}
	if det.Count != 2 {
		t.Fatalf("count = %d, want 2", det.Count)
	}

	byTask := map[string]HelpDigestItem{}
	for _, it := range det.Items {
		byTask[it.TaskID] = it
	}
	if len(byTask) != 2 {
		t.Fatalf("items = %d, want 2 distinct task entries", len(byTask))
	}

	first := byTask["task-1"]
	if first.AgentID != "agent-1" {
		t.Errorf("agent_id = %q, want agent-1", first.AgentID)
	}
	if first.BlockedReason == nil || *first.BlockedReason != "missing api key" {
		t.Errorf("blocked_reason = %v, want \"missing api key\"", first.BlockedReason)
	}
	if len(first.Needs) != 2 || first.Needs[0] != "credential" || first.Needs[1] != "schema" {
		t.Errorf("needs = %v, want [credential schema]", first.Needs)
	}
	if first.Confidence == nil || *first.Confidence != 0.4 {
		t.Errorf("confidence = %v, want 0.4", first.Confidence)
	}

	second := byTask["task-2"]
	if second.AgentID != "agent-2" {
		t.Errorf("agent_id = %q, want agent-2", second.AgentID)
	}
	if len(second.Needs) != 1 || second.Needs[0] != "approval" {
		t.Errorf("needs = %v, want [approval]", second.Needs)
	}
	if second.BlockedReason != nil {
		t.Errorf("blocked_reason = %v, want nil", second.BlockedReason)
	}
	if second.Confidence != nil {
		t.Errorf("confidence = %v, want nil", second.Confidence)
	}
}

func TestBuildHelpDigestDetails_MalformedSourceIsError(t *testing.T) {
	bad := mustHelpItem(t, "11111111-1111-1111-1111-111111111111",
		"aaaaaaaa-1111-1111-1111-111111111111", "task-1", "agent-1", nil)
	bad.Details = []byte("not-json")
	if _, err := buildHelpDigestDetails([]db.InboxItem{bad}); err == nil {
		t.Fatal("expected error for malformed source details, got nil")
	}
}

func TestGroupHelpItems_PerWorkspaceRecipient(t *testing.T) {
	wsA := "11111111-1111-1111-1111-111111111111"
	wsB := "22222222-2222-2222-2222-222222222222"
	ra := "aaaaaaaa-1111-1111-1111-111111111111"
	rb := "bbbbbbbb-1111-1111-1111-111111111111"

	items := []db.InboxItem{
		mustHelpItem(t, wsA, ra, "t1", "ag", nil),
		mustHelpItem(t, wsA, ra, "t2", "ag", nil),
		mustHelpItem(t, wsA, rb, "t3", "ag", nil),
		mustHelpItem(t, wsB, ra, "t4", "ag", nil),
	}
	groups := groupHelpItems(items)
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3 (wsA/ra, wsA/rb, wsB/ra)", len(groups))
	}
	keyAR := helpDigestKey{WorkspaceID: wsA, RecipientType: "member", RecipientID: ra}
	if len(groups[keyAR]) != 2 {
		t.Errorf("wsA/ra group = %d, want 2", len(groups[keyAR]))
	}
	keyBR := helpDigestKey{WorkspaceID: wsB, RecipientType: "member", RecipientID: ra}
	if len(groups[keyBR]) != 1 {
		t.Errorf("wsB/ra group = %d, want 1", len(groups[keyBR]))
	}
}

func TestGroupHelpItems_NullWorkspaceNormalizedToGlobal(t *testing.T) {
	ra := "aaaaaaaa-1111-1111-1111-111111111111"
	items := []db.InboxItem{
		mustHelpItem(t, "", ra, "t1", "ag", nil),
		mustHelpItem(t, "", ra, "t2", "ag", nil),
	}
	groups := groupHelpItems(items)
	key := helpDigestKey{WorkspaceID: "", RecipientType: "member", RecipientID: ra}
	g, ok := groups[key]
	if !ok {
		t.Fatalf("expected global-bucket group for NULL workspace, got %+v", groups)
	}
	if len(g) != 2 {
		t.Errorf("global group = %d, want 2", len(g))
	}
}

func TestGroupHelpItems_EmptyClearsAllDigests(t *testing.T) {
	// An empty open set yields no groups, so the reconciler treats every
	// existing digest as stale and clears it.
	groups := groupHelpItems(nil)
	if len(groups) != 0 {
		t.Fatalf("groups = %d, want 0 (would clear all digests)", len(groups))
	}
}

// TestHelpDigestJob_ReconcilesAcrossDB is a DB-backed integration test that
// exercises the full reconcile loop: seed 3 open agent_help_requested items
// for one (workspace, recipient), run the job, assert exactly one digest
// summarizing all 3, then resolve (archive) the sources and assert the digest
// is gone. It skips unless a Postgres is reachable.
func TestHelpDigestJob_ReconcilesAcrossDB(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	q := db.New(pool)

	ws := mustUUID(t, uuid.NewString())
	recipient := mustUUID(t, uuid.NewString())

	// inbox_item.workspace_id is a FK to workspace, so seed a minimal
	// workspace row. Slug must be unique; use the workspace id string.
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspace (id, name, slug) VALUES ($1, $2, $3)
		 ON CONFLICT (id) DO NOTHING`,
		ws, "help-digest-test", util.UUIDToString(ws)); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx,
			`DELETE FROM inbox_item WHERE workspace_id = $1 OR (type = $2 AND recipient_id = $3)`,
			ws, InboxItemTypeAgentHelpDigest, recipient)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, ws)
	})

	taskIDs := []string{"task-1", "task-2", "task-3"}
	for _, tid := range taskIDs {
		det, err := json.Marshal(map[string]any{
			"task_id":        tid,
			"agent_id":       "agent-" + tid,
			"blocked_reason": "reason " + tid,
			"needs":          []string{"credential"},
			"confidence":     0.5,
		})
		if err != nil {
			t.Fatalf("marshal seed details: %v", err)
		}
		if _, err := q.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   ws,
			RecipientType: "member",
			RecipientID:   recipient,
			Type:          InboxItemTypeAgentHelpRequested,
			Severity:      "action_required",
			IssueID:       pgtype.UUID{},
			Title:         "Agent requested help",
			Body:          pgtype.Text{String: "blocked", Valid: true},
			ActorType:     pgtype.Text{String: "agent", Valid: true},
			ActorID:       ws,
			Details:       det,
		}); err != nil {
			t.Fatalf("seed help item %s: %v", tid, err)
		}
	}

	if _, err := RunHelpDigest(ctx, pool); err != nil {
		t.Fatalf("RunHelpDigest: %v", err)
	}

	if c := countOpenDigest(t, pool, ws, recipient); c != 1 {
		t.Fatalf("digest count after seed = %d, want 1", c)
	}
	det := loadOpenDigest(t, pool, ws, recipient)
	if det.Count != 3 {
		t.Fatalf("digest.Count = %d, want 3", det.Count)
	}
	seen := map[string]bool{}
	for _, it := range det.Items {
		seen[it.TaskID] = true
		if it.AgentID == "" {
			t.Errorf("digest item %s has empty agent_id", it.TaskID)
		}
		if len(it.Needs) != 1 || it.Needs[0] != "credential" {
			t.Errorf("digest item %s needs = %v, want [credential]", it.TaskID, it.Needs)
		}
	}
	for _, tid := range taskIDs {
		if !seen[tid] {
			t.Errorf("digest is missing task_id %q", tid)
		}
	}

	// Resolve: archive the source help items. A real resolution archives the
	// inbox row, which is what makes it drop out of the open set.
	if _, err := pool.Exec(ctx,
		`UPDATE inbox_item SET archived = true WHERE type = $1 AND workspace_id = $2 AND recipient_id = $3`,
		InboxItemTypeAgentHelpRequested, ws, recipient); err != nil {
		t.Fatalf("archive sources: %v", err)
	}
	if _, err := RunHelpDigest(ctx, pool); err != nil {
		t.Fatalf("RunHelpDigest after resolve: %v", err)
	}
	if c := countOpenDigest(t, pool, ws, recipient); c != 0 {
		t.Fatalf("digest count after resolve = %d, want 0 (stale digest cleared)", c)
	}
}

func countOpenDigest(t *testing.T, pool *pgxpool.Pool, ws, recipient pgtype.UUID) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM inbox_item
		 WHERE type = $1 AND archived = false AND workspace_id = $2 AND recipient_type = 'member' AND recipient_id = $3`,
		InboxItemTypeAgentHelpDigest, ws, recipient,
	).Scan(&n); err != nil {
		t.Fatalf("count digest: %v", err)
	}
	return n
}

func loadOpenDigest(t *testing.T, pool *pgxpool.Pool, ws, recipient pgtype.UUID) HelpDigestDetails {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(context.Background(),
		`SELECT details FROM inbox_item
		 WHERE type = $1 AND archived = false AND workspace_id = $2 AND recipient_type = 'member' AND recipient_id = $3
		 LIMIT 1`,
		InboxItemTypeAgentHelpDigest, ws, recipient,
	).Scan(&raw); err != nil {
		t.Fatalf("load digest: %v", err)
	}
	var det HelpDigestDetails
	if err := json.Unmarshal(raw, &det); err != nil {
		t.Fatalf("decode digest details: %v", err)
	}
	return det
}
