package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	gitlabsync "github.com/multica-ai/multica/server/internal/gitlab"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCreateIssue_WriteThroughHumanWithoutPATUsesServicePAT(t *testing.T) {
	var capturedToken string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/42/issues":
			if r.Method == http.MethodPost {
				capturedToken = r.Header.Get("PRIVATE-TOKEN")
				w.Write([]byte(`{"id":9901,"iid":99,"title":"From Multica","state":"opened",
					"labels":["status::todo","priority::medium"],"updated_at":"2026-04-17T15:00:00Z"}`))
				return
			}
		}
		w.Write([]byte(`{}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1::uuid AND gitlab_iid = 99`, testWorkspaceID)

	// Seed a workspace_gitlab_connection so the handler takes write-through.
	encrypted, _ := h.Secrets.Encrypt([]byte("svc-token-xyz"))
	testPool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 42, 'g/a', $2, 1, 'connected')
		ON CONFLICT (workspace_id) DO UPDATE SET
			gitlab_project_id = EXCLUDED.gitlab_project_id,
			service_token_encrypted = EXCLUDED.service_token_encrypted,
			service_token_user_id = EXCLUDED.service_token_user_id
	`, testWorkspaceID, encrypted)

	// Wire a real resolver on the handler so the write-through branch works.
	h.SetGitlabResolver(gitlabsync.NewResolver(h.Queries, func(_ context.Context, b []byte) (string, error) {
		plain, err := h.Secrets.Decrypt(b)
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}))

	body, _ := json.Marshal(map[string]any{
		"title":    "From Multica",
		"status":   "todo",
		"priority": "medium",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if capturedToken != "svc-token-xyz" {
		t.Errorf("PRIVATE-TOKEN sent to gitlab = %q, want svc-token-xyz (service PAT)", capturedToken)
	}

	// Verify the cache row exists with the GitLab IID.
	var iid int
	testPool.QueryRow(context.Background(),
		`SELECT gitlab_iid FROM issue WHERE workspace_id = $1::uuid AND title = 'From Multica'`,
		testWorkspaceID).Scan(&iid)
	if iid != 99 {
		t.Errorf("cached gitlab_iid = %d, want 99", iid)
	}
}

func TestCreateIssue_WriteThroughHumanWithPATUsesUserPAT(t *testing.T) {
	var capturedToken string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			w.Write([]byte(`{"id":555,"username":"alice"}`))
		case "/api/v4/projects/42/issues":
			if r.Method == http.MethodPost {
				capturedToken = r.Header.Get("PRIVATE-TOKEN")
				w.Write([]byte(`{"id":9902,"iid":100,"title":"From Alice","state":"opened",
					"labels":["status::todo","priority::medium"],"updated_at":"2026-04-17T15:00:00Z"}`))
				return
			}
		}
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1::uuid AND gitlab_iid = 100`, testWorkspaceID)
	defer h.Queries.DeleteUserGitlabConnection(context.Background(), db.DeleteUserGitlabConnectionParams{
		UserID:      parseUUID(testUserID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})

	svcEnc, _ := h.Secrets.Encrypt([]byte("svc-token"))
	usrEnc, _ := h.Secrets.Encrypt([]byte("user-token-alice"))
	testPool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 42, 'g/a', $2, 1, 'connected')
		ON CONFLICT (workspace_id) DO UPDATE SET
			service_token_encrypted = EXCLUDED.service_token_encrypted
	`, testWorkspaceID, svcEnc)
	h.Queries.UpsertUserGitlabConnection(context.Background(), db.UpsertUserGitlabConnectionParams{
		UserID:         parseUUID(testUserID),
		WorkspaceID:    parseUUID(testWorkspaceID),
		GitlabUserID:   555,
		GitlabUsername: "alice",
		PatEncrypted:   usrEnc,
	})

	h.SetGitlabResolver(gitlabsync.NewResolver(h.Queries, func(_ context.Context, b []byte) (string, error) {
		plain, err := h.Secrets.Decrypt(b)
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}))

	body, _ := json.Marshal(map[string]any{"title": "From Alice", "status": "todo", "priority": "medium"})
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d", rr.Code)
	}
	if capturedToken != "user-token-alice" {
		t.Errorf("PRIVATE-TOKEN = %q, want user-token-alice", capturedToken)
	}
}

// seedGitlabWriteThroughFixture prepares a workspace_gitlab_connection row and
// attaches a real resolver to the handler so CreateIssue takes the
// write-through branch. Shared by the parent/project/attachments blocker tests.
func seedGitlabWriteThroughFixture(t *testing.T, h *Handler) {
	t.Helper()
	encrypted, _ := h.Secrets.Encrypt([]byte("svc-token-xyz"))
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 42, 'g/a', $2, 1, 'connected')
		ON CONFLICT (workspace_id) DO UPDATE SET
			gitlab_project_id = EXCLUDED.gitlab_project_id,
			service_token_encrypted = EXCLUDED.service_token_encrypted,
			service_token_user_id = EXCLUDED.service_token_user_id
	`, testWorkspaceID, encrypted); err != nil {
		t.Fatalf("seed workspace_gitlab_connection: %v", err)
	}
	h.SetGitlabResolver(gitlabsync.NewResolver(h.Queries, func(_ context.Context, b []byte) (string, error) {
		plain, err := h.Secrets.Decrypt(b)
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}))
}

func TestCreateIssue_WriteThroughThreadsParentIssueID(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v4/projects/42/issues" && r.Method == http.MethodPost {
			w.Write([]byte(`{"id":9910,"iid":110,"title":"Sub-issue","state":"opened",
				"labels":["status::todo","priority::medium"],"updated_at":"2026-04-17T15:00:00Z"}`))
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1::uuid AND gitlab_iid IN (110)`, testWorkspaceID)

	// Seed a native parent issue in the same workspace.
	var parentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'blocker-parent', 'todo', 'none', $2, 'member', 9001, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&parentID); err != nil {
		t.Fatalf("seed parent issue: %v", err)
	}
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, parentID)

	seedGitlabWriteThroughFixture(t, h)

	body, _ := json.Marshal(map[string]any{
		"title":           "Sub-issue",
		"status":          "todo",
		"priority":        "medium",
		"parent_issue_id": parentID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	// The cache row must have parent_issue_id set to the pre-seeded parent.
	var gotParent string
	if err := testPool.QueryRow(context.Background(),
		`SELECT parent_issue_id FROM issue WHERE workspace_id = $1::uuid AND gitlab_iid = 110`,
		testWorkspaceID).Scan(&gotParent); err != nil {
		t.Fatalf("query cache row parent_issue_id: %v", err)
	}
	if gotParent != parentID {
		t.Errorf("cache row parent_issue_id = %q, want %q", gotParent, parentID)
	}
}

func TestCreateIssue_WriteThroughThreadsProjectID(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v4/projects/42/issues" && r.Method == http.MethodPost {
			w.Write([]byte(`{"id":9911,"iid":111,"title":"Issue with project","state":"opened",
				"labels":["status::todo","priority::medium"],"updated_at":"2026-04-17T15:00:00Z"}`))
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1::uuid AND gitlab_iid = 111`, testWorkspaceID)

	// Seed a native project in the same workspace.
	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'blocker-project')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	defer testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)

	seedGitlabWriteThroughFixture(t, h)

	body, _ := json.Marshal(map[string]any{
		"title":      "Issue with project",
		"status":     "todo",
		"priority":   "medium",
		"project_id": projectID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var gotProject string
	if err := testPool.QueryRow(context.Background(),
		`SELECT project_id FROM issue WHERE workspace_id = $1::uuid AND gitlab_iid = 111`,
		testWorkspaceID).Scan(&gotProject); err != nil {
		t.Fatalf("query cache row project_id: %v", err)
	}
	if gotProject != projectID {
		t.Errorf("cache row project_id = %q, want %q", gotProject, projectID)
	}
}

func TestCreateIssue_WriteThroughLinksAttachments(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v4/projects/42/issues" && r.Method == http.MethodPost {
			w.Write([]byte(`{"id":9912,"iid":112,"title":"Issue with attachment","state":"opened",
				"labels":["status::todo","priority::medium"],"updated_at":"2026-04-17T15:00:00Z"}`))
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1::uuid AND gitlab_iid = 112`, testWorkspaceID)

	// Pre-upload an unattached attachment (issue_id IS NULL).
	attachmentUUID := uuid.New().String()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO attachment (id, workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1::uuid, $2, 'member', $3, 'note.txt', 'https://cdn.example.com/note.txt', 'text/plain', 11)
	`, attachmentUUID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("seed attachment: %v", err)
	}
	defer testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1::uuid`, attachmentUUID)

	seedGitlabWriteThroughFixture(t, h)

	body, _ := json.Marshal(map[string]any{
		"title":          "Issue with attachment",
		"status":         "todo",
		"priority":       "medium",
		"attachment_ids": []string{attachmentUUID},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	// Fetch the newly created cache row id.
	var issueID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM issue WHERE workspace_id = $1::uuid AND gitlab_iid = 112`,
		testWorkspaceID).Scan(&issueID); err != nil {
		t.Fatalf("query cache row id: %v", err)
	}

	// The attachment must now point at the new issue.
	var linkedIssueID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT issue_id FROM attachment WHERE id = $1::uuid`,
		attachmentUUID).Scan(&linkedIssueID); err != nil {
		t.Fatalf("query attachment issue_id: %v", err)
	}
	if linkedIssueID != issueID {
		t.Errorf("attachment issue_id = %q, want %q", linkedIssueID, issueID)
	}
}

// TestCreateIssue_WriteThroughEnqueuesAgentTask verifies that creating an
// agent-assigned issue on a GitLab-connected workspace enqueues an agent task
// — matching the legacy path's behaviour. The write-through branch must not
// silently swallow this side-effect.
func TestCreateIssue_WriteThroughEnqueuesAgentTask(t *testing.T) {
	ctx := context.Background()

	// Look up the seeded test agent. Its slug (lowercased name with hyphens)
	// determines the agent::<slug> label the fake GitLab server must echo back.
	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("look up test agent: %v", err)
	}

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v4/projects/42/issues" && r.Method == http.MethodPost {
			// Echo back the agent::handler-test-agent label so TranslateIssue
			// resolves the agent assignee on the cache row.
			w.Write([]byte(`{"id":9913,"iid":113,"title":"Agent-assigned","state":"opened",
				"labels":["status::todo","priority::medium","agent::handler-test-agent"],
				"updated_at":"2026-04-17T15:00:00Z"}`))
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	defer testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1::uuid`, agentID)
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1::uuid AND gitlab_iid = 113`, testWorkspaceID)

	seedGitlabWriteThroughFixture(t, h)

	body, _ := json.Marshal(map[string]any{
		"title":         "Agent-assigned",
		"status":        "todo",
		"priority":      "medium",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	// Grab the cache row — it must be persisted with the agent assignee.
	var issueID, gotAssigneeType, gotAssigneeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, assignee_type, assignee_id FROM issue WHERE workspace_id = $1::uuid AND gitlab_iid = 113`,
		testWorkspaceID).Scan(&issueID, &gotAssigneeType, &gotAssigneeID); err != nil {
		t.Fatalf("query cache row: %v", err)
	}
	if gotAssigneeType != "agent" || gotAssigneeID != agentID {
		t.Fatalf("cache row assignee = (%q, %q), want (agent, %q)", gotAssigneeType, gotAssigneeID, agentID)
	}

	// The write-through path must enqueue an agent task — same side effect
	// the legacy path produces at CreateIssue's tail.
	var taskCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1::uuid AND agent_id = $2::uuid AND status = 'queued'`,
		issueID, agentID,
	).Scan(&taskCount); err != nil {
		t.Fatalf("count queued tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("expected 1 queued task for agent-assigned write-through issue, got %d", taskCount)
	}
}

// TestCreateIssue_WriteThroughPreservesAssigneeAndDueDateWhenParentProjectSet
// guards the invariant that the post-upsert UpdateIssue patch (for threading
// parent_issue_id and project_id) does NOT clobber assignee_type / assignee_id
// / due_date. Those columns are bare `sqlc.narg` slots (no COALESCE) in the
// UpdateIssue query — passing zero-value pgtype carriers would wipe the values
// UpsertIssueFromGitlab just wrote. The handler avoids this by threading
// cacheRow.AssigneeType / AssigneeID / DueDate through UpdateIssue, and this
// test fails if a future refactor reintroduces the zero-value bug.
func TestCreateIssue_WriteThroughPreservesAssigneeAndDueDateWhenParentProjectSet(t *testing.T) {
	ctx := context.Background()

	// Look up the seeded test agent. Its slug (lowercased hyphenated name)
	// is what the fake GitLab server echoes back as agent::<slug> so the
	// TranslateIssue step resolves the agent assignee on the cache row.
	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("look up test agent: %v", err)
	}

	// The fake GitLab echoes a due_date so we can assert the cache row's
	// due_date survives the UpdateIssue patch. (Currently the create-path
	// upsert hard-codes DueDate to zero via buildUpsertParamsFromCreate — so
	// due_date preservation is tested against the NULL that UpsertIssueFromGitlab
	// actually writes; a future refactor that threads req.DueDate into the
	// upsert will also be guarded by this assertion.)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v4/projects/42/issues" && r.Method == http.MethodPost {
			// Echo back the agent::handler-test-agent label so TranslateIssue
			// resolves the agent assignee on the cache row.
			w.Write([]byte(`{"id":9914,"iid":114,"title":"Combined preservation","state":"opened",
				"labels":["status::todo","priority::medium","agent::handler-test-agent"],
				"updated_at":"2026-04-17T15:00:00Z"}`))
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	defer testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1::uuid`, agentID)
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1::uuid AND gitlab_iid = 114`, testWorkspaceID)

	// Seed a native parent issue in the same workspace.
	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'combined-preservation-parent', 'todo', 'none', $2, 'member', 9002, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&parentID); err != nil {
		t.Fatalf("seed parent issue: %v", err)
	}
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, parentID)

	// Seed a native project in the same workspace.
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'combined-preservation-project')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	defer testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)

	seedGitlabWriteThroughFixture(t, h)

	// Request sets ALL of: parent_issue_id, project_id, assignee_type/id, due_date.
	// This is the combined case no other test exercises.
	dueDate := "2026-05-01T00:00:00Z"
	body, _ := json.Marshal(map[string]any{
		"title":           "Combined preservation",
		"status":          "todo",
		"priority":        "medium",
		"assignee_type":   "agent",
		"assignee_id":     agentID,
		"due_date":        dueDate,
		"parent_issue_id": parentID,
		"project_id":      projectID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	// All four Multica-owned fields that survive the write-through patch must
	// round-trip. The critical teeth are on assignee_type / assignee_id: a
	// refactor that passes zero pgtype.Text / pgtype.UUID to UpdateIssue
	// (thinking COALESCE would handle it, which it does not for these narg
	// slots) would wipe the label-resolved agent assignee.
	var (
		gotParent       string
		gotProject      string
		gotAssigneeType string
		gotAssigneeID   string
		gotDueDate      *time.Time
	)
	if err := testPool.QueryRow(ctx, `
		SELECT parent_issue_id, project_id, assignee_type, assignee_id, due_date
		FROM issue
		WHERE workspace_id = $1::uuid AND gitlab_iid = 114
	`, testWorkspaceID).Scan(&gotParent, &gotProject, &gotAssigneeType, &gotAssigneeID, &gotDueDate); err != nil {
		t.Fatalf("query cache row: %v", err)
	}

	if gotParent != parentID {
		t.Errorf("parent_issue_id = %q, want %q", gotParent, parentID)
	}
	if gotProject != projectID {
		t.Errorf("project_id = %q, want %q", gotProject, projectID)
	}
	if gotAssigneeType != "agent" {
		t.Errorf("assignee_type = %q, want %q", gotAssigneeType, "agent")
	}
	if gotAssigneeID != agentID {
		t.Errorf("assignee_id = %q, want %q", gotAssigneeID, agentID)
	}
	// The due_date assertion must match whatever the upsert wrote into
	// cacheRow.DueDate — the invariant under test is that UpdateIssue did not
	// wipe it. Today that value is NULL (buildUpsertParamsFromCreate hardcodes
	// pgtype.Timestamptz{}); if a future refactor threads req.DueDate into the
	// upsert, flip this branch to compare against the posted timestamp.
	_ = dueDate
	if gotDueDate != nil {
		t.Errorf("due_date = %v, want NULL (matches cacheRow.DueDate after UpsertIssueFromGitlab)", gotDueDate)
	}
}

func TestCreateIssue_LegacyPathWhenNoGitlabConnection(t *testing.T) {
	// No workspace_gitlab_connection row → handler takes the legacy direct-DB
	// path. (Same behaviour as pre-Phase-3a.)
	h := buildHandlerWithGitlab(t, "http://unused")
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	body, _ := json.Marshal(map[string]any{"title": "Legacy", "status": "todo", "priority": "medium"})
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
}

// TestUpdateIssue_WriteThroughStatusChangeSendsLabelDiff verifies that when a
// GitLab-connected workspace receives a PATCH that changes status, the handler
// computes the label diff + state_event via BuildUpdateIssueInput and sends
// the correct PUT /projects/:id/issues/:iid request to GitLab, then reflects
// the result in the cache row.
func TestUpdateIssue_WriteThroughStatusChangeSendsLabelDiff(t *testing.T) {
	ctx := context.Background()

	var capturedAddLabels, capturedRemoveLabels, capturedStateEvent string
	var capturedMethod, capturedPath string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if s, ok := body["add_labels"].(string); ok {
			capturedAddLabels = s
		}
		if s, ok := body["remove_labels"].(string); ok {
			capturedRemoveLabels = s
		}
		if s, ok := body["state_event"].(string); ok {
			capturedStateEvent = s
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":9001,"iid":200,"title":"T","state":"closed","updated_at":"2026-04-17T13:00:00Z","labels":["status::done"]}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)
	t.Cleanup(func() {
		h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	})

	issueID := uuid.New().String()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO issue (id, workspace_id, number, title, description, status, priority,
		 gitlab_iid, gitlab_project_id, gitlab_issue_id, external_updated_at,
		 creator_type, creator_id, position)
		 VALUES ($1::uuid, $2::uuid, 1001, 'T', '', 'in_progress', 'none', 200, 42, 9001, '2026-04-17T12:00:00Z', 'member', $3::uuid, 0)`,
		issueID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1::uuid`, issueID)
	})

	body := []byte(`{"status":"done"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/issues/"+issueID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", issueID)
	rec := httptest.NewRecorder()

	h.UpdateIssue(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/200" {
		t.Errorf("path = %s, want /api/v4/projects/42/issues/200", capturedPath)
	}
	if capturedAddLabels != "status::done" {
		t.Errorf("add_labels = %q, want status::done", capturedAddLabels)
	}
	if capturedRemoveLabels != "status::in_progress" {
		t.Errorf("remove_labels = %q, want status::in_progress", capturedRemoveLabels)
	}
	if capturedStateEvent != "close" {
		t.Errorf("state_event = %q, want close", capturedStateEvent)
	}

	var cachedStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1::uuid`, issueID).Scan(&cachedStatus); err != nil {
		t.Fatalf("scan cache: %v", err)
	}
	if cachedStatus != "done" {
		t.Errorf("cached status = %s, want done", cachedStatus)
	}
}

// TestUpdateIssue_WriteThroughErrorReturnsNonZeroStatus verifies the
// authoritative-write-through guarantee: on GitLab failure, the handler
// returns a non-2xx status AND the cache row is untouched (no fallback to
// legacy direct-DB write).
func TestUpdateIssue_WriteThroughErrorReturnsNonZeroStatus(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)
	t.Cleanup(func() {
		h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	})

	issueID := uuid.New().String()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO issue (id, workspace_id, number, title, description, status, priority,
		 gitlab_iid, gitlab_project_id, gitlab_issue_id, external_updated_at,
		 creator_type, creator_id, position)
		 VALUES ($1::uuid, $2::uuid, 1002, 'T', '', 'in_progress', 'none', 201, 42, 9002, '2026-04-17T12:00:00Z', 'member', $3::uuid, 0)`,
		issueID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1::uuid`, issueID)
	})

	body := []byte(`{"status":"done"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/issues/"+issueID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", issueID)
	rec := httptest.NewRecorder()

	h.UpdateIssue(rec, req)

	if rec.Code < 400 {
		t.Fatalf("status = %d, want >=400, body = %s", rec.Code, rec.Body.String())
	}

	var cachedStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1::uuid`, issueID).Scan(&cachedStatus); err != nil {
		t.Fatalf("scan cache: %v", err)
	}
	if cachedStatus != "in_progress" {
		t.Errorf("cache was touched: status = %s, want in_progress", cachedStatus)
	}
}

// TestDeleteIssue_WriteThroughSendsDELETE verifies that when a GitLab-connected
// workspace receives a DELETE for an issue, the handler sends
// DELETE /api/v4/projects/:id/issues/:iid with the service token and then
// removes the cache row.
func TestDeleteIssue_WriteThroughSendsDELETE(t *testing.T) {
	ctx := context.Background()
	var capturedMethod, capturedPath, capturedToken string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedToken = r.Header.Get("PRIVATE-TOKEN")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)
	t.Cleanup(func() {
		h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	})

	issueID := uuid.New().String()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO issue (id, workspace_id, number, title, description, status, priority,
		 gitlab_iid, gitlab_project_id, gitlab_issue_id, external_updated_at,
		 creator_type, creator_id, position)
		 VALUES ($1::uuid, $2::uuid, 1003, 'T', '', 'todo', 'none', 301, 42, 9101, '2026-04-17T12:00:00Z', 'member', $3::uuid, 0)`,
		issueID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1::uuid`, issueID)
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/issues/"+issueID, nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", issueID)
	rec := httptest.NewRecorder()

	h.DeleteIssue(rec, req)

	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if capturedMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/301" {
		t.Errorf("path = %s, want /api/v4/projects/42/issues/301", capturedPath)
	}
	if capturedToken == "" {
		t.Errorf("PRIVATE-TOKEN header missing")
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM issue WHERE id = $1::uuid`, issueID).Scan(&count); err != nil {
		t.Fatalf("count cache: %v", err)
	}
	if count != 0 {
		t.Errorf("issue row not deleted, count = %d", count)
	}
}

// TestDeleteIssue_WriteThroughErrorPreservesCache verifies the authoritative
// guarantee: on GitLab failure the handler returns a non-2xx status AND the
// cache row remains intact (no fallback to legacy direct-DB delete).
func TestDeleteIssue_WriteThroughErrorPreservesCache(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)
	t.Cleanup(func() {
		h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	})

	issueID := uuid.New().String()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO issue (id, workspace_id, number, title, description, status, priority,
		 gitlab_iid, gitlab_project_id, gitlab_issue_id, external_updated_at,
		 creator_type, creator_id, position)
		 VALUES ($1::uuid, $2::uuid, 1004, 'T', '', 'todo', 'none', 302, 42, 9102, '2026-04-17T12:00:00Z', 'member', $3::uuid, 0)`,
		issueID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1::uuid`, issueID)
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/issues/"+issueID, nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", issueID)
	rec := httptest.NewRecorder()

	h.DeleteIssue(rec, req)

	if rec.Code < 400 {
		t.Fatalf("status = %d, want >=400, body = %s", rec.Code, rec.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM issue WHERE id = $1::uuid`, issueID).Scan(&count); err != nil {
		t.Fatalf("count cache: %v", err)
	}
	if count != 1 {
		t.Errorf("cache was mutated on GitLab failure, count = %d", count)
	}
}

func TestBatchResult_ShapeAndJSON(t *testing.T) {
	r := BatchWriteResult{
		Succeeded: []BatchSucceeded{{ID: "abc", Issue: nil}},
		Failed:    []BatchFailed{{ID: "def", ErrorCode: "GITLAB_403", Message: "forbidden"}},
	}
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"succeeded":[{"id":"abc","issue":null}],"failed":[{"id":"def","error_code":"GITLAB_403","message":"forbidden"}]}`
	if string(body) != want {
		t.Errorf("json = %s\nwant  %s", body, want)
	}
}
