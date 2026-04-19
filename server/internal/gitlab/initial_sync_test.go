package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

// connectTestPool connects to the worktree DB. Test is skipped if unreachable.
// Registers t.Cleanup(pool.Close) so the pool stays open for any other
// cleanups registered later (workspace DELETE etc.) — t.Cleanup runs LIFO,
// so registering pool.Close FIRST means it runs LAST.
func connectTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil || pool.Ping(context.Background()) != nil {
		t.Skip("database not reachable")
	}
	t.Cleanup(pool.Close)
	return pool
}

func makeWorkspace(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, description)
		VALUES ('GL Sync Test', 'gl-sync-test-'||substr(gen_random_uuid()::text, 1, 8), '')
		RETURNING id
	`).Scan(&id); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
	})
	return id
}

func mustPGUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}
	return u
}

// makeUser inserts a standalone user row (no workspace membership) and
// returns the new user's UUID. Used by tests that seed a user_gitlab_connection
// so the connection's FK to user(id) is satisfied.
func makeUser(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Test User", email).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, id)
	})
	return id
}

// seedUserGitlabConnection upserts a user_gitlab_connection row so the
// Resolver's GetUserGitlabConnectionByGitlabUserID returns a mapping.
func seedUserGitlabConnection(t *testing.T, pool *pgxpool.Pool, userID, workspaceID string, gitlabUserID int64, gitlabUsername string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO user_gitlab_connection (user_id, workspace_id, gitlab_user_id, gitlab_username, pat_encrypted)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $5)
		 ON CONFLICT (user_id, workspace_id) DO UPDATE
		   SET gitlab_user_id = EXCLUDED.gitlab_user_id,
		       gitlab_username = EXCLUDED.gitlab_username`,
		userID, workspaceID, gitlabUserID, gitlabUsername, []byte{1, 2, 3})
	if err != nil {
		t.Fatalf("seed user_gitlab_connection: %v", err)
	}
}

// seedGitlabProjectMember inserts a gitlab_project_member row (no Multica
// mapping) and returns the new member row's UUID. Used to assert the
// "gitlab_user" assignee fallback path.
func seedGitlabProjectMember(t *testing.T, pool *pgxpool.Pool, workspaceID string, gitlabUserID int64, username, name string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO gitlab_project_member (workspace_id, gitlab_user_id, username, name, avatar_url, external_updated_at)
		 VALUES ($1::uuid, $2, $3, $4, '', now())
		 ON CONFLICT (workspace_id, gitlab_user_id) DO UPDATE
		   SET username = EXCLUDED.username
		 RETURNING id::text`,
		workspaceID, gitlabUserID, username, name).Scan(&id)
	if err != nil {
		t.Fatalf("seed gitlab_project_member: %v", err)
	}
	return id
}

// newTestResolver constructs a Resolver against the test pool for webhook/
// initial-sync tests. Uses a stub decrypter because reverse-resolution never
// calls decrypt (only the token-resolution path does).
func newTestResolver(queries *db.Queries) *Resolver {
	return NewResolver(queries, func(_ context.Context, b []byte) (string, error) {
		return string(b), nil
	})
}

func TestInitialSync_LabelsAndMembers(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/7/labels":
			if r.Method == http.MethodGet {
				json.NewEncoder(w).Encode([]gitlabapi.Label{
					{ID: 1, Name: "bug", Color: "#ff0000"},
				})
			} else {
				w.Write([]byte(`{"id":99,"name":"x","color":"#000"}`))
			}
		case "/api/v4/projects/7/members/all":
			json.NewEncoder(w).Encode([]gitlabapi.ProjectMember{
				{ID: 100, Username: "alice", Name: "Alice", AvatarURL: "https://x"},
			})
		case "/api/v4/projects/7/issues":
			json.NewEncoder(w).Encode([]gitlabapi.Issue{})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	queries := db.New(pool)
	deps := SyncDeps{Queries: queries, Client: gitlabapi.NewClient(srv.URL, srv.Client())}
	err := RunInitialSync(context.Background(), deps, RunInitialSyncInput{
		WorkspaceID: wsID,
		ProjectID:   7,
		Token:       "tok",
	})
	if err != nil {
		t.Fatalf("RunInitialSync: %v", err)
	}

	rows, _ := queries.ListGitlabLabels(context.Background(), mustPGUUID(t, wsID))
	if len(rows) == 0 {
		t.Errorf("no gitlab_label rows after sync")
	}

	members, _ := queries.ListGitlabProjectMembers(context.Background(), mustPGUUID(t, wsID))
	if len(members) != 1 || members[0].Username != "alice" {
		t.Errorf("members = %+v, want one alice", members)
	}
}

func TestInitialSync_IssuesNotesAwards(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)

	// Insert a runtime + agent NAMED "builder" so the agent::builder label resolves.
	// (Note: agent table has no slug column; we derive slug from name.)
	var runtimeID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
		VALUES ($1, 'test-runtime', 'cloud', 'test')
		RETURNING id
	`, wsID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	var agentID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'builder', 'cloud', '{}'::jsonb, 'workspace', 1, NULL)
		RETURNING id
	`, wsID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO agent_runtime_assignment (agent_id, runtime_id) VALUES ($1, $2)
	`, agentID, runtimeID); err != nil {
		t.Fatalf("insert agent runtime assignment: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v4/projects/7/labels" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode([]gitlabapi.Label{
				{ID: 10, Name: "status::in_progress", Color: "#3b82f6"},
				{ID: 11, Name: "agent::builder", Color: "#8b5cf6"},
			})
		case r.URL.Path == "/api/v4/projects/7/labels" && r.Method == http.MethodPost:
			w.Write([]byte(`{"id":999,"name":"x"}`))
		case r.URL.Path == "/api/v4/projects/7/members/all":
			json.NewEncoder(w).Encode([]gitlabapi.ProjectMember{})
		case r.URL.Path == "/api/v4/projects/7/issues":
			json.NewEncoder(w).Encode([]gitlabapi.Issue{
				{
					ID: 1001, IID: 42, Title: "First issue",
					Description: "body", State: "opened",
					Labels:    []string{"status::in_progress", "agent::builder"},
					UpdatedAt: "2026-04-17T10:00:00Z",
				},
			})
		case r.URL.Path == "/api/v4/projects/7/issues/42/notes":
			json.NewEncoder(w).Encode([]gitlabapi.Note{
				{ID: 1, Body: "hello", System: false,
					Author:    gitlabapi.User{ID: 100, Username: "alice"},
					UpdatedAt: "2026-04-17T10:01:00Z"},
			})
		case r.URL.Path == "/api/v4/projects/7/issues/42/award_emoji":
			json.NewEncoder(w).Encode([]gitlabapi.AwardEmoji{
				{ID: 5, Name: "thumbsup",
					User:      gitlabapi.User{ID: 100, Username: "alice"},
					UpdatedAt: "2026-04-17T10:02:00Z"},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	queries := db.New(pool)
	deps := SyncDeps{Queries: queries, Client: gitlabapi.NewClient(srv.URL, srv.Client())}
	err := RunInitialSync(context.Background(), deps, RunInitialSyncInput{
		WorkspaceID: wsID, ProjectID: 7, Token: "tok",
	})
	if err != nil {
		t.Fatalf("RunInitialSync: %v", err)
	}

	// Verify the issue exists with the right status + agent assignment.
	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: mustPGUUID(t, wsID),
		GitlabIid:   pgtype.Int4{Int32: 42, Valid: true},
	})
	if err != nil {
		t.Fatalf("GetIssueByGitlabIID: %v", err)
	}
	if row.Status != "in_progress" {
		t.Errorf("status = %q, want in_progress", row.Status)
	}
	if !row.AssigneeType.Valid || row.AssigneeType.String != "agent" {
		t.Errorf("assignee_type = %+v, want agent", row.AssigneeType)
	}
}

func TestInitialSync_PopulatesHumanNotesAndAwards(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v4/projects/9/labels" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode([]gitlabapi.Label{})
		case r.URL.Path == "/api/v4/projects/9/labels" && r.Method == http.MethodPost:
			w.Write([]byte(`{"id":1}`))
		case r.URL.Path == "/api/v4/projects/9/members/all":
			json.NewEncoder(w).Encode([]gitlabapi.ProjectMember{})
		case r.URL.Path == "/api/v4/projects/9/issues":
			json.NewEncoder(w).Encode([]gitlabapi.Issue{
				{ID: 2001, IID: 7, Title: "human-only", State: "opened", Labels: []string{}, UpdatedAt: "2026-04-17T12:00:00Z"},
			})
		case r.URL.Path == "/api/v4/projects/9/issues/7/notes":
			json.NewEncoder(w).Encode([]gitlabapi.Note{
				{ID: 100, Body: "Looks good!", System: false,
					Author:    gitlabapi.User{ID: 555, Username: "alice"},
					UpdatedAt: "2026-04-17T12:01:00Z"},
			})
		case r.URL.Path == "/api/v4/projects/9/issues/7/award_emoji":
			json.NewEncoder(w).Encode([]gitlabapi.AwardEmoji{
				{ID: 200, Name: "thumbsup",
					User:      gitlabapi.User{ID: 555, Username: "alice"},
					UpdatedAt: "2026-04-17T12:02:00Z"},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	queries := db.New(pool)
	deps := SyncDeps{Queries: queries, Client: gitlabapi.NewClient(srv.URL, srv.Client())}
	if err := RunInitialSync(context.Background(), deps, RunInitialSyncInput{
		WorkspaceID: wsID, ProjectID: 9, Token: "tok",
	}); err != nil {
		t.Fatalf("RunInitialSync: %v", err)
	}

	// Find the synced issue, then verify its comment + reaction.
	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: mustPGUUID(t, wsID),
		GitlabIid:   pgtype.Int4{Int32: 7, Valid: true},
	})
	if err != nil {
		t.Fatalf("GetIssueByGitlabIID: %v", err)
	}

	// pgtype.UUID parameter binding to raw pool.QueryRow doesn't always bind
	// the way you'd expect — convert to the string form so PG sees a regular
	// uuid literal.
	issueIDStr := uuidString(row.ID)

	var commentCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM comment WHERE issue_id = $1::uuid AND gitlab_note_id = 100 AND gitlab_author_user_id = 555 AND author_type IS NULL`,
		issueIDStr).Scan(&commentCount); err != nil {
		t.Fatalf("count comment: %v", err)
	}
	if commentCount != 1 {
		t.Errorf("expected 1 human comment cached, got %d", commentCount)
	}

	var awardCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM issue_reaction WHERE issue_id = $1::uuid AND gitlab_award_id = 200 AND gitlab_actor_user_id = 555 AND actor_type IS NULL`,
		issueIDStr).Scan(&awardCount); err != nil {
		t.Fatalf("count award: %v", err)
	}
	if awardCount != 1 {
		t.Errorf("expected 1 award cached, got %d", awardCount)
	}
}

// TestInitialSync_ReverseResolvesAssigneeNoteAndAwardActors asserts that when
// a Resolver is wired into SyncDeps, the initial sync reverse-resolves GitLab
// user IDs on notes, awards, and issue assignees to Multica members.
func TestInitialSync_ReverseResolvesAssigneeNoteAndAwardActors(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	userID := makeUser(t, pool, "resolved-user@example.com")
	seedUserGitlabConnection(t, pool, userID, wsID, 100, "alice")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v4/projects/7/labels" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode([]gitlabapi.Label{})
		case r.URL.Path == "/api/v4/projects/7/labels" && r.Method == http.MethodPost:
			w.Write([]byte(`{"id":1}`))
		case r.URL.Path == "/api/v4/projects/7/members/all":
			json.NewEncoder(w).Encode([]gitlabapi.ProjectMember{
				{ID: 100, Username: "alice", Name: "Alice"},
			})
		case r.URL.Path == "/api/v4/projects/7/issues":
			json.NewEncoder(w).Encode([]gitlabapi.Issue{
				{
					ID: 3001, IID: 42, Title: "assigned to alice",
					State: "opened", Labels: []string{},
					Assignees: []gitlabapi.User{{ID: 100, Username: "alice"}},
					Author:    gitlabapi.User{ID: 100, Username: "alice"},
					UpdatedAt: "2026-04-17T10:00:00Z",
				},
			})
		case r.URL.Path == "/api/v4/projects/7/issues/42/notes":
			json.NewEncoder(w).Encode([]gitlabapi.Note{
				{ID: 500, Body: "LGTM", System: false,
					Author:    gitlabapi.User{ID: 100, Username: "alice"},
					UpdatedAt: "2026-04-17T10:01:00Z"},
			})
		case r.URL.Path == "/api/v4/projects/7/issues/42/award_emoji":
			json.NewEncoder(w).Encode([]gitlabapi.AwardEmoji{
				{ID: 700, Name: "thumbsup",
					User:      gitlabapi.User{ID: 100, Username: "alice"},
					UpdatedAt: "2026-04-17T10:02:00Z"},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	queries := db.New(pool)
	deps := SyncDeps{
		Queries:  queries,
		Client:   gitlabapi.NewClient(srv.URL, srv.Client()),
		Resolver: newTestResolver(queries),
	}
	if err := RunInitialSync(context.Background(), deps, RunInitialSyncInput{
		WorkspaceID: wsID, ProjectID: 7, Token: "tok",
	}); err != nil {
		t.Fatalf("RunInitialSync: %v", err)
	}

	// Issue: assignee resolves to member; creator resolves to member.
	wsUUID := mustPGUUID(t, wsID)
	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: wsUUID,
		GitlabIid:   pgtype.Int4{Int32: 42, Valid: true},
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
	if !row.CreatorType.Valid || row.CreatorType.String != "member" {
		t.Errorf("creator_type = %+v, want member", row.CreatorType)
	}
	if uuidString(row.CreatorID) != userID {
		t.Errorf("creator_id = %q, want %q", uuidString(row.CreatorID), userID)
	}

	// Note: author_type='member', author_id=<userID>.
	var noteAuthorType, noteAuthorID string
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(author_type, ''), COALESCE(author_id::text, '')
		 FROM comment WHERE issue_id = $1::uuid AND gitlab_note_id = 500`,
		uuidString(row.ID)).Scan(&noteAuthorType, &noteAuthorID); err != nil {
		t.Fatalf("select comment: %v", err)
	}
	if noteAuthorType != "member" {
		t.Errorf("note author_type = %q, want member", noteAuthorType)
	}
	if noteAuthorID != userID {
		t.Errorf("note author_id = %q, want %q", noteAuthorID, userID)
	}

	// Award: actor_type='member', actor_id=<userID>.
	var awardActorType, awardActorID string
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(actor_type, ''), COALESCE(actor_id::text, '')
		 FROM issue_reaction WHERE issue_id = $1::uuid AND gitlab_award_id = 700`,
		uuidString(row.ID)).Scan(&awardActorType, &awardActorID); err != nil {
		t.Fatalf("select issue_reaction: %v", err)
	}
	if awardActorType != "member" {
		t.Errorf("award actor_type = %q, want member", awardActorType)
	}
	if awardActorID != userID {
		t.Errorf("award actor_id = %q, want %q", awardActorID, userID)
	}
}

// TestInitialSync_UnmappedAssigneeFallsBackToGitlabUser asserts that when
// the assignee has no user_gitlab_connection but IS cached as a
// gitlab_project_member, the sync writes assignee_type='gitlab_user'.
func TestInitialSync_UnmappedAssigneeFallsBackToGitlabUser(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v4/projects/7/labels" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode([]gitlabapi.Label{})
		case r.URL.Path == "/api/v4/projects/7/labels" && r.Method == http.MethodPost:
			w.Write([]byte(`{"id":1}`))
		case r.URL.Path == "/api/v4/projects/7/members/all":
			json.NewEncoder(w).Encode([]gitlabapi.ProjectMember{
				{ID: 555, Username: "carol", Name: "Carol"},
			})
		case r.URL.Path == "/api/v4/projects/7/issues":
			json.NewEncoder(w).Encode([]gitlabapi.Issue{
				{
					ID: 3002, IID: 55, Title: "external assignee",
					State: "opened", Labels: []string{},
					Assignees: []gitlabapi.User{{ID: 555, Username: "carol"}},
					Author:    gitlabapi.User{ID: 555, Username: "carol"},
					UpdatedAt: "2026-04-17T10:00:00Z",
				},
			})
		case r.URL.Path == "/api/v4/projects/7/issues/55/notes":
			json.NewEncoder(w).Encode([]gitlabapi.Note{})
		case r.URL.Path == "/api/v4/projects/7/issues/55/award_emoji":
			json.NewEncoder(w).Encode([]gitlabapi.AwardEmoji{})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	queries := db.New(pool)
	deps := SyncDeps{
		Queries:  queries,
		Client:   gitlabapi.NewClient(srv.URL, srv.Client()),
		Resolver: newTestResolver(queries),
	}
	if err := RunInitialSync(context.Background(), deps, RunInitialSyncInput{
		WorkspaceID: wsID, ProjectID: 7, Token: "tok",
	}); err != nil {
		t.Fatalf("RunInitialSync: %v", err)
	}

	wsUUID := mustPGUUID(t, wsID)
	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: wsUUID,
		GitlabIid:   pgtype.Int4{Int32: 55, Valid: true},
	})
	if err != nil {
		t.Fatalf("GetIssueByGitlabIID: %v", err)
	}
	if !row.AssigneeType.Valid || row.AssigneeType.String != "gitlab_user" {
		t.Errorf("assignee_type = %+v, want gitlab_user", row.AssigneeType)
	}
	// Member row id should match the inserted gitlab_project_member row.
	member, err := queries.GetGitlabProjectMember(context.Background(), db.GetGitlabProjectMemberParams{
		WorkspaceID:  wsUUID,
		GitlabUserID: 555,
	})
	if err != nil {
		t.Fatalf("GetGitlabProjectMember: %v", err)
	}
	if uuidString(row.AssigneeID) != uuidString(member.ID) {
		t.Errorf("assignee_id = %q, want member_row_id %q", uuidString(row.AssigneeID), uuidString(member.ID))
	}
}

func TestInitialSync_TransitionsStatusToConnected(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)

	// Insert a connection row in 'connecting' state.
	_, err := pool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 7, 'g/a', '\x'::bytea, 1, 'connecting')
	`, wsID)
	if err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	// Minimal happy-path fake gitlab.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v4/projects/7/labels" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode([]gitlabapi.Label{})
		case r.URL.Path == "/api/v4/projects/7/labels" && r.Method == http.MethodPost:
			w.Write([]byte(`{"id":1}`))
		case r.URL.Path == "/api/v4/projects/7/members/all":
			json.NewEncoder(w).Encode([]gitlabapi.ProjectMember{})
		case r.URL.Path == "/api/v4/projects/7/issues":
			json.NewEncoder(w).Encode([]gitlabapi.Issue{})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	queries := db.New(pool)
	deps := SyncDeps{Queries: queries, Client: gitlabapi.NewClient(srv.URL, srv.Client())}
	if err := RunInitialSync(context.Background(), deps, RunInitialSyncInput{
		WorkspaceID: wsID, ProjectID: 7, Token: "tok",
	}); err != nil {
		t.Fatalf("RunInitialSync: %v", err)
	}

	row, err := queries.GetWorkspaceGitlabConnection(context.Background(), mustPGUUID(t, wsID))
	if err != nil {
		t.Fatalf("GetWorkspaceGitlabConnection: %v", err)
	}
	if row.ConnectionStatus != "connected" {
		t.Errorf("status = %q, want connected", row.ConnectionStatus)
	}
}

func TestInitialSync_TransitionsStatusToErrorOnFailure(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)

	pool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 7, 'g/a', '\x'::bytea, 1, 'connecting')
	`, wsID)

	// Fake gitlab that 401s — sync should fail and transition to error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"401 Unauthorized"}`))
	}))
	defer srv.Close()

	queries := db.New(pool)
	deps := SyncDeps{Queries: queries, Client: gitlabapi.NewClient(srv.URL, srv.Client())}
	err := RunInitialSync(context.Background(), deps, RunInitialSyncInput{
		WorkspaceID: wsID, ProjectID: 7, Token: "bad",
	})
	if err == nil {
		t.Fatalf("expected error from failing sync")
	}

	row, _ := queries.GetWorkspaceGitlabConnection(context.Background(), mustPGUUID(t, wsID))
	if row.ConnectionStatus != "error" {
		t.Errorf("status = %q, want error", row.ConnectionStatus)
	}
	if !row.StatusMessage.Valid || row.StatusMessage.String == "" {
		t.Errorf("status_message should be populated, got %+v", row.StatusMessage)
	}
}
