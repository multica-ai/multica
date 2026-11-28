package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func postSignedGitHubEventForTest(t *testing.T, secret, event string, payload map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s payload: %v", event, err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-Hub-Signature-256", sig)
	testutil.Call(t, testHandler.HandleGitHubWebhook, req).Want(http.StatusAccepted)
}

func createPRReviewTestAgent(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	var agentID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, 'PR review test agent', 'cloud', '{}'::jsonb, $3, 'workspace', 'public_to', 2, $4)
		RETURNING id
	`, testWorkspaceID, name, testRuntimeID, testUserID).Scan(&agentID)
	if err != nil {
		t.Fatalf("insert test agent %s: %v", name, err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, agentID, testWorkspaceID); err != nil {
		t.Fatalf("seed agent invocation target %s: %v", name, err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM agent_invocation_target WHERE agent_id = $1`, agentID)
		testPool.Exec(bg, `DELETE FROM task_usage WHERE task_id IN (SELECT id FROM agent_task_queue WHERE agent_id = $1)`, agentID)
		testPool.Exec(bg, `DELETE FROM task_token WHERE agent_id = $1`, agentID)
		testPool.Exec(bg, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		testPool.Exec(bg, `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

type prReviewFixture struct {
	secret         string
	installationID int64
	repoOwner      string
	repoName       string
	repoPrivate    bool
	prNumber       int
	prID           string
	headSHA        string
}

func setupPRReviewFixture(t *testing.T, installationID int64, prNumber int, prAuthor string) prReviewFixture {
	t.Helper()
	ctx := context.Background()
	secret := fmt.Sprintf("pr-review-secret-%d", installationID)
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "acme",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	headSHA := "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	pr, err := testHandler.Queries.UpsertGitHubPullRequest(ctx, db.UpsertGitHubPullRequestParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		RepoOwner:      "acme",
		RepoName:       "web-app",
		PrNumber:       int32(prNumber),
		Title:          fmt.Sprintf("feat: PR #%d", prNumber),
		State:          "open",
		HtmlUrl:        fmt.Sprintf("https://github.com/Acme/web-app/pull/%d", prNumber),
		Branch:         pgtype.Text{String: "feat/mul-28", Valid: true},
		AuthorLogin:    pgtype.Text{String: prAuthor, Valid: prAuthor != ""},
		PrCreatedAt:    now,
		PrUpdatedAt:    now,
		HeadSha:        headSHA,
	})
	if err != nil {
		t.Fatalf("UpsertGitHubPullRequest: %v", err)
	}

	prID := uuidToString(pr.ID)
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM github_pr_review_delivery WHERE pull_request_id = $1`, prID)
		testPool.Exec(bg, `DELETE FROM github_pr_review_wakeup_state WHERE pull_request_id = $1`, prID)
		testPool.Exec(bg, `DELETE FROM issue_pull_request WHERE pull_request_id = $1`, prID)
		testPool.Exec(bg, `DELETE FROM github_pull_request WHERE id = $1`, prID)
		testPool.Exec(bg, `DELETE FROM github_installation WHERE workspace_id = $1 AND installation_id = $2`, testWorkspaceID, installationID)
	})

	return prReviewFixture{
		secret:         secret,
		installationID: installationID,
		repoOwner:      "Acme",
		repoName:       "web-app",
		repoPrivate:    true,
		prNumber:       prNumber,
		prID:           prID,
		headSHA:        headSHA,
	}
}

func createPRReviewTestIssue(t *testing.T, title, status string) IssueResponse {
	t.Helper()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": status,
	})
	w := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated)
	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created issue: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM github_pr_review_delivery WHERE issue_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM github_pr_review_wakeup_state WHERE issue_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM comment WHERE issue_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM agent_task_queue WHERE issue_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM issue_unread WHERE issue_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM issue_subscriber WHERE issue_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM issue WHERE id = $1`, created.ID)
	})
	return created
}

func makeReviewSubmittedPayload(fx prReviewFixture, reviewID int64, reviewerLogin, reviewerType, prAuthor, state, body string) map[string]any {
	return makeReviewSubmittedPayloadWithAssociation(fx, reviewID, reviewerLogin, reviewerType, "MEMBER", prAuthor, state, body)
}

func makeReviewSubmittedPayloadWithAssociation(fx prReviewFixture, reviewID int64, reviewerLogin, reviewerType, authorAssociation, prAuthor, state, body string) map[string]any {
	return map[string]any{
		"action": "submitted",
		"installation": map[string]any{
			"id": fx.installationID,
		},
		"repository": map[string]any{
			"name":    fx.repoName,
			"private": fx.repoPrivate,
			"owner": map[string]any{
				"login": fx.repoOwner,
				"type":  "Organization",
			},
		},
		"pull_request": map[string]any{
			"number":   fx.prNumber,
			"state":    "open",
			"draft":    false,
			"merged":   false,
			"html_url": fmt.Sprintf("https://github.com/%s/%s/pull/%d", fx.repoOwner, fx.repoName, fx.prNumber),
			"user": map[string]any{
				"login": prAuthor,
				"type":  "User",
			},
			"head": map[string]any{
				"sha": fx.headSHA,
			},
		},
		"review": map[string]any{
			"id":                 reviewID,
			"state":              state,
			"body":               body,
			"commit_id":          fx.headSHA,
			"author_association": authorAssociation,
			"html_url":           fmt.Sprintf("https://github.com/%s/%s/pull/%d#pullrequestreview-%d", fx.repoOwner, fx.repoName, fx.prNumber, reviewID),
			"user": map[string]any{
				"login": reviewerLogin,
				"type":  reviewerType,
			},
		},
	}
}

func makeReviewCommentPayload(fx prReviewFixture, action string, commentID int64, commenterLogin, commenterType, prAuthor, path string, startLine, line int, body string) map[string]any {
	return makeReviewCommentPayloadWithReviewID(fx, action, commentID, 0, commenterLogin, commenterType, "MEMBER", prAuthor, path, startLine, line, body)
}

func makeReviewCommentPayloadWithReviewID(fx prReviewFixture, action string, commentID, reviewID int64, commenterLogin, commenterType, authorAssociation, prAuthor, path string, startLine, line int, body string) map[string]any {
	commentObj := map[string]any{
		"id":                 commentID,
		"path":               path,
		"line":               line,
		"diff_hunk":          "@@ -40,6 +40,8 @@ func HandleOrder() {\n+\tif order == nil {\n+\t\treturn nil\n+\t}",
		"commit_id":          fx.headSHA,
		"body":               body,
		"author_association": authorAssociation,
		"html_url":           fmt.Sprintf("https://github.com/%s/%s/pull/%d#discussion_r%d", fx.repoOwner, fx.repoName, fx.prNumber, commentID),
		"user": map[string]any{
			"login": commenterLogin,
			"type":  commenterType,
		},
	}
	if reviewID > 0 {
		commentObj["pull_request_review_id"] = reviewID
	}
	if startLine > 0 {
		commentObj["start_line"] = startLine
	}
	return map[string]any{
		"action": action,
		"installation": map[string]any{
			"id": fx.installationID,
		},
		"repository": map[string]any{
			"name":    fx.repoName,
			"private": fx.repoPrivate,
			"owner": map[string]any{
				"login": fx.repoOwner,
				"type":  "Organization",
			},
		},
		"pull_request": map[string]any{
			"number":   fx.prNumber,
			"state":    "open",
			"draft":    false,
			"merged":   false,
			"html_url": fmt.Sprintf("https://github.com/%s/%s/pull/%d", fx.repoOwner, fx.repoName, fx.prNumber),
			"user": map[string]any{
				"login": prAuthor,
				"type":  "User",
			},
			"head": map[string]any{
				"sha": fx.headSHA,
			},
		},
		"comment": commentObj,
	}
}

func TestGitHubPRReview_UnlinkedBotSelfOutsiderAndNonActionableIgnored(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized")
	}
	ctx := context.Background()
	fx := setupPRReviewFixture(t, 41001001, 1955, "agent-author")
	devAgentID := createPRReviewTestAgent(t, "Atlas-Filter-Test")
	issue := createPRReviewTestIssue(t, "Unlinked and filter checks", "in_progress")

	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issue.ID, devAgentID); err != nil {
		t.Fatalf("assign issue: %v", err)
	}

	// 1. PR is mirrored in github_pull_request, NOT linked in issue_pull_request -> ignored.
	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review",
		makeReviewSubmittedPayload(fx, 1001, "alice-reviewer", "User", "agent-author", "changes_requested", "Please add nil guard"))
	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review_comment",
		makeReviewCommentPayload(fx, "created", 2001, "alice-reviewer", "User", "agent-author", "internal/order.go", 40, 42, "Missing check"))

	var commentCount, taskCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, issue.ID).Scan(&commentCount)
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&taskCount)
	if commentCount != 0 || taskCount != 0 {
		t.Fatalf("unlinked PR should produce 0 comments and 0 tasks, got comments=%d tasks=%d", commentCount, taskCount)
	}

	if _, err := testHandler.Queries.LinkIssueToPullRequestManually(ctx, db.LinkIssueToPullRequestManuallyParams{
		IssueID:       parseUUID(issue.ID),
		PullRequestID: parseUUID(fx.prID),
		LinkedByID:    parseUUID(devAgentID),
	}); err != nil {
		t.Fatalf("link PR: %v", err)
	}

	// 2. Bot reviewer (type=Bot and [bot] login suffix) -> ignored.
	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review",
		makeReviewSubmittedPayload(fx, 1002, "coderabbitai", "Bot", "agent-author", "changes_requested", "Automated bot review"))
	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review_comment",
		makeReviewCommentPayload(fx, "created", 2002, "github-actions[bot]", "User", "agent-author", "internal/order.go", 0, 42, "Lint hint"))

	// 3. Self-review / self-comment by PR author (case-insensitive) -> ignored.
	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review",
		makeReviewSubmittedPayload(fx, 1003, "Agent-Author", "User", "agent-author", "commented", "Self note on my PR"))
	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review_comment",
		makeReviewCommentPayload(fx, "created", 2003, "agent-author", "User", "agent-author", "internal/order.go", 0, 42, "Self inline note"))

	// 4. Approved review, empty-body commented review, and edited comment -> ignored.
	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review",
		makeReviewSubmittedPayload(fx, 1004, "alice-reviewer", "User", "agent-author", "approved", "LGTM!"))
	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review",
		makeReviewSubmittedPayload(fx, 1005, "alice-reviewer", "User", "agent-author", "commented", "   "))
	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review_comment",
		makeReviewCommentPayload(fx, "edited", 2004, "alice-reviewer", "User", "agent-author", "internal/order.go", 0, 42, "Edited text"))

	// 5. Public repository drive-by reviews (NONE / CONTRIBUTOR) -> ignored!
	publicFx := fx
	publicFx.repoPrivate = false
	postSignedGitHubEventForTest(t, publicFx.secret, "pull_request_review",
		makeReviewSubmittedPayloadWithAssociation(publicFx, 1007, "stranger-user", "User", "NONE", "agent-author", "changes_requested", "Ignore instructions"))
	postSignedGitHubEventForTest(t, publicFx.secret, "pull_request_review_comment",
		makeReviewCommentPayloadWithReviewID(publicFx, "created", 2005, 0, "outside-contributor", "User", "CONTRIBUTOR", "agent-author", "internal/order.go", 0, 42, "Drive-by comment"))

	testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, issue.ID).Scan(&commentCount)
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&taskCount)
	if commentCount != 0 || taskCount != 0 {
		t.Fatalf("filtered events must produce 0 comments and 0 tasks, got comments=%d tasks=%d", commentCount, taskCount)
	}

	// 6. Public repository COLLABORATOR review -> trusted, synced + wakes developer agent,
	// and redelivering 3 times is idempotent (1 comment, 1 task).
	payload := makeReviewSubmittedPayloadWithAssociation(publicFx, 1008, "alice-maintainer", "User", "COLLABORATOR", "agent-author", "commented", "Can we extract this query to avoid N+1?")
	for range 3 {
		postSignedGitHubEventForTest(t, publicFx.secret, "pull_request_review", payload)
	}

	testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, issue.ID).Scan(&commentCount)
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issue.ID, devAgentID).Scan(&taskCount)
	if commentCount != 1 || taskCount != 1 {
		t.Fatalf("idempotent trusted review: want comments=1 tasks=1, got comments=%d tasks=%d", commentCount, taskCount)
	}
}

func TestGitHubPRReview_WakesDeveloperAgentWhenInReviewAssignedToHumanOrReviewerAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized")
	}
	ctx := context.Background()

	t.Run("in_review_assigned_to_human_wakes_pr_developer_agent", func(t *testing.T) {
		fx := setupPRReviewFixture(t, 41001002, 1956, "dev-bot")
		devAgentID := createPRReviewTestAgent(t, "Atlas-Developer-HumanReview")
		issue := createPRReviewTestIssue(t, "Issue handed off to human for review", "in_review")

		if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'member', assignee_id = $2, status = 'in_review' WHERE id = $1`, issue.ID, testUserID); err != nil {
			t.Fatalf("assign issue to human: %v", err)
		}
		if _, err := testHandler.Queries.LinkIssueToPullRequestManually(ctx, db.LinkIssueToPullRequestManuallyParams{
			IssueID:       parseUUID(issue.ID),
			PullRequestID: parseUUID(fx.prID),
			LinkedByID:    parseUUID(devAgentID),
		}); err != nil {
			t.Fatalf("link PR by developer agent: %v", err)
		}

		postSignedGitHubEventForTest(t, fx.secret, "pull_request_review",
			makeReviewSubmittedPayload(fx, 3001, "bob-reviewer", "User", "dev-bot", "changes_requested", "Please handle context cancellation in worker loop."))

		var taskAgentID string
		if err := testPool.QueryRow(ctx, `SELECT agent_id FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&taskAgentID); err != nil {
			t.Fatalf("expected queued task for developer agent: %v", err)
		}
		if taskAgentID != devAgentID {
			t.Fatalf("woke agent %s, want developer agent %s", taskAgentID, devAgentID)
		}
	})

	t.Run("in_review_workflow_handoff_to_reviewer_agent_wakes_prior_developer_agent", func(t *testing.T) {
		fx := setupPRReviewFixture(t, 41001003, 1957, "dev-bot")
		devAgentID := createPRReviewTestAgent(t, "Atlas-Developer")
		reviewerAgentID := createPRReviewTestAgent(t, "Sage-Reviewer")

		var projectID, workflowID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO project (workspace_id, title, status, priority, visibility, creator_type, creator_id)
			VALUES ($1, 'Workflow Project', 'in_progress', 'medium', 'workspace', 'member', $2)
			RETURNING id
		`, testWorkspaceID, testUserID).Scan(&projectID); err != nil {
			t.Fatalf("insert project: %v", err)
		}
		stepsJSON := fmt.Sprintf(`[
			{"status_key":"todo"},
			{"status_key":"in_progress"},
			{"status_key":"in_review","handler":{"type":"agent","id":%q}},
			{"status_key":"done"},
			{"status_key":"cancelled"}
		]`, reviewerAgentID)
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue_workflow (workspace_id, name, initial_status_key, steps, is_default)
			VALUES ($1, 'Dev -> Sage Review', 'todo', $2::jsonb, false)
			RETURNING id
		`, testWorkspaceID, stepsJSON).Scan(&workflowID); err != nil {
			t.Fatalf("insert workflow: %v", err)
		}
		if _, err := testPool.Exec(ctx, `UPDATE project SET workflow_id = $2 WHERE id = $1`, projectID, workflowID); err != nil {
			t.Fatalf("bind project workflow: %v", err)
		}
		t.Cleanup(func() {
			bg := context.Background()
			testPool.Exec(bg, `DELETE FROM project WHERE id = $1`, projectID)
			testPool.Exec(bg, `DELETE FROM issue_workflow WHERE id = $1`, workflowID)
		})

		issue := createPRReviewTestIssue(t, "Workflow review routing", "in_review")
		if _, err := testPool.Exec(ctx, `
			UPDATE issue
			SET project_id = $2, status = 'in_review', assignee_type = 'agent', assignee_id = $3
			WHERE id = $1
		`, issue.ID, projectID, reviewerAgentID); err != nil {
			t.Fatalf("set issue project & reviewer assignee: %v", err)
		}

		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, issue_id, status, priority, workflow_step,
				originator_user_id, accountable_user_id, originator_source, created_at, completed_at
			) VALUES
			($1, $3, $4, 'completed', 2, 'in_progress', $5, $5, 'direct_human', now() - interval '10 minutes', now() - interval '6 minutes'),
			($2, $3, $4, 'completed', 2, 'in_review',   $5, $5, 'direct_human', now() - interval '5 minutes',  now() - interval '2 minutes')
		`, devAgentID, reviewerAgentID, testRuntimeID, issue.ID, testUserID); err != nil {
			t.Fatalf("seed historical tasks: %v", err)
		}
		if _, err := testHandler.Queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{
			IssueID:       parseUUID(issue.ID),
			PullRequestID: parseUUID(fx.prID),
		}); err != nil {
			t.Fatalf("auto-link PR: %v", err)
		}

		postSignedGitHubEventForTest(t, fx.secret, "pull_request_review_comment",
			makeReviewCommentPayload(fx, "created", 4001, "colleague-dev", "User", "dev-bot", "server/internal/handler/order.go", 40, 42, "Nil pointer dereference when items slice is empty"))

		var commentContent string
		if err := testPool.QueryRow(ctx, `SELECT content FROM comment WHERE issue_id = $1`, issue.ID).Scan(&commentContent); err != nil {
			t.Fatalf("load synced comment: %v", err)
		}
		for _, want := range []string{
			"GitHub PR Review Comment",
			"Acme/web-app#1957",
			"@colleague-dev",
			"`server/internal/handler/order.go` (lines 40-42)",
			"Nil pointer dereference when items slice is empty",
		} {
			if !strings.Contains(commentContent, want) {
				t.Errorf("synced comment missing %q:\n%s", want, commentContent)
			}
		}

		var queuedAgentID string
		if err := testPool.QueryRow(ctx, `SELECT agent_id FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'`, issue.ID).Scan(&queuedAgentID); err != nil {
			t.Fatalf("expected a queued developer task: %v", err)
		}
		if queuedAgentID != devAgentID {
			t.Fatalf("queued task targeted %s (reviewer=%s), want developer agent %s", queuedAgentID, reviewerAgentID, devAgentID)
		}
	})

	t.Run("squad_leader_role_preserved_and_terminal_issues_skipped", func(t *testing.T) {
		fx := setupPRReviewFixture(t, 41001005, 1959, "dev-bot")
		leaderAgentID := createPRReviewTestAgent(t, "Squad-Leader-Dev")

		var squadID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
			VALUES ($1, 'Delivery Squad', 'Test squad', $2, $3)
			RETURNING id
		`, testWorkspaceID, leaderAgentID, testUserID).Scan(&squadID); err != nil {
			t.Fatalf("insert squad: %v", err)
		}
		if _, err := testPool.Exec(ctx, `INSERT INTO squad_member (squad_id, member_type, member_id) VALUES ($1, 'agent', $2)`, squadID, leaderAgentID); err != nil {
			t.Fatalf("insert squad member: %v", err)
		}
		t.Cleanup(func() {
			bg := context.Background()
			testPool.Exec(bg, `DELETE FROM squad_member WHERE squad_id = $1`, squadID)
			testPool.Exec(bg, `DELETE FROM squad WHERE id = $1`, squadID)
		})

		issue := createPRReviewTestIssue(t, "Squad leader PR review", "in_review")
		if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'squad', assignee_id = $2 WHERE id = $1`, issue.ID, squadID); err != nil {
			t.Fatalf("assign issue to squad: %v", err)
		}
		if _, err := testHandler.Queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{
			IssueID:       parseUUID(issue.ID),
			PullRequestID: parseUUID(fx.prID),
		}); err != nil {
			t.Fatalf("link PR: %v", err)
		}

		postSignedGitHubEventForTest(t, fx.secret, "pull_request_review",
			makeReviewSubmittedPayload(fx, 6001, "colleague-dev", "User", "dev-bot", "changes_requested", "Update squad implementation"))

		var taskAgentID, taskSquadID string
		var isLeader bool
		if err := testPool.QueryRow(ctx, `SELECT agent_id, squad_id, is_leader_task FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&taskAgentID, &taskSquadID, &isLeader); err != nil {
			t.Fatalf("scan squad leader task: %v", err)
		}
		if taskAgentID != leaderAgentID || taskSquadID != squadID || !isLeader {
			t.Fatalf("squad task = (agent=%s, squad=%s, leader=%v), want (%s, %s, true)", taskAgentID, taskSquadID, isLeader, leaderAgentID, squadID)
		}

		if _, err := testPool.Exec(ctx, `UPDATE issue SET status = 'done' WHERE id = $1`, issue.ID); err != nil {
			t.Fatalf("mark issue done: %v", err)
		}
		postSignedGitHubEventForTest(t, fx.secret, "pull_request_review_comment",
			makeReviewCommentPayload(fx, "created", 6002, "colleague-dev", "User", "dev-bot", "pkg/done.go", 0, 10, "Late comment after done"))

		var commentCount int
		testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, issue.ID).Scan(&commentCount)
		if commentCount != 1 {
			t.Fatalf("done issue must ignore late PR review comments; commentCount=%d, want 1", commentCount)
		}
	})

	t.Run("human_pr_never_wakes_workflow_reviewer_agent", func(t *testing.T) {
		fx := setupPRReviewFixture(t, 41001007, 1961, "human-engineer")
		reviewerAgentID := createPRReviewTestAgent(t, "Sage-NeverWake")

		var projectID, workflowID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO project (workspace_id, title, status, priority, visibility, creator_type, creator_id)
			VALUES ($1, 'Human PR Project', 'in_progress', 'medium', 'workspace', 'member', $2)
			RETURNING id
		`, testWorkspaceID, testUserID).Scan(&projectID); err != nil {
			t.Fatalf("insert project: %v", err)
		}
		stepsJSON := fmt.Sprintf(`[
			{"status_key":"todo"},
			{"status_key":"in_progress"},
			{"status_key":"in_review","handler":{"type":"agent","id":%q}},
			{"status_key":"done"},
			{"status_key":"cancelled"}
		]`, reviewerAgentID)
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue_workflow (workspace_id, name, initial_status_key, steps, is_default)
			VALUES ($1, 'Human -> Sage Review', 'todo', $2::jsonb, false)
			RETURNING id
		`, testWorkspaceID, stepsJSON).Scan(&workflowID); err != nil {
			t.Fatalf("insert workflow: %v", err)
		}
		if _, err := testPool.Exec(ctx, `UPDATE project SET workflow_id = $2 WHERE id = $1`, projectID, workflowID); err != nil {
			t.Fatalf("bind workflow: %v", err)
		}
		t.Cleanup(func() {
			bg := context.Background()
			testPool.Exec(bg, `DELETE FROM project WHERE id = $1`, projectID)
			testPool.Exec(bg, `DELETE FROM issue_workflow WHERE id = $1`, workflowID)
		})

		issue := createPRReviewTestIssue(t, "Human authored PR in review", "in_review")
		if _, err := testPool.Exec(ctx, `
			UPDATE issue SET project_id = $2, status = 'in_review', assignee_type = 'agent', assignee_id = $3 WHERE id = $1
		`, issue.ID, projectID, reviewerAgentID); err != nil {
			t.Fatalf("assign reviewer: %v", err)
		}
		if _, err := testHandler.Queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{
			IssueID: parseUUID(issue.ID), PullRequestID: parseUUID(fx.prID),
		}); err != nil {
			t.Fatalf("link PR: %v", err)
		}

		postSignedGitHubEventForTest(t, fx.secret, "pull_request_review",
			makeReviewSubmittedPayload(fx, 6101, "colleague-reviewer", "User", "human-engineer", "changes_requested", "Please rename this helper"))

		var commentCount, taskCount int
		testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, issue.ID).Scan(&commentCount)
		testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&taskCount)
		if commentCount != 1 || taskCount != 0 {
			t.Fatalf("human PR in_review should sync comment (1) without waking reviewer agent (0): got comments=%d tasks=%d", commentCount, taskCount)
		}
	})

	t.Run("squad_worker_pr_linker_routes_to_squad_leader_and_reassignment_wins", func(t *testing.T) {
		fx := setupPRReviewFixture(t, 41001008, 1962, "worker-bot")
		leaderID := createPRReviewTestAgent(t, "Squad-Leader-Router")
		workerID := createPRReviewTestAgent(t, "Squad-Worker-Coder")

		var squadID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
			VALUES ($1, 'Worker Routing Squad', 'Test squad', $2, $3)
			RETURNING id
		`, testWorkspaceID, leaderID, testUserID).Scan(&squadID); err != nil {
			t.Fatalf("insert squad: %v", err)
		}
		t.Cleanup(func() {
			bg := context.Background()
			testPool.Exec(bg, `DELETE FROM squad_member WHERE squad_id = $1`, squadID)
			testPool.Exec(bg, `DELETE FROM squad WHERE id = $1`, squadID)
		})

		issue := createPRReviewTestIssue(t, "Worker linked PR, issue in human review", "in_review")
		if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'member', assignee_id = $2 WHERE id = $1`, issue.ID, testUserID); err != nil {
			t.Fatalf("assign to human: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, issue_id, status, priority, is_leader_task, squad_id, usage_squad_id,
				workflow_step, originator_user_id, accountable_user_id, originator_source, created_at, completed_at
			) VALUES
			($1, $3, $4, 'completed', 2, true,  $5, $5, 'in_progress', $6, $6, 'direct_human', now() - interval '10 minutes', now() - interval '8 minutes'),
			($2, $3, $4, 'completed', 2, false, NULL, $5, 'in_progress', $6, $6, 'delegation', now() - interval '7 minutes', now() - interval '3 minutes')
		`, leaderID, workerID, testRuntimeID, issue.ID, squadID, testUserID); err != nil {
			t.Fatalf("seed squad tasks: %v", err)
		}
		if _, err := testHandler.Queries.LinkIssueToPullRequestManually(ctx, db.LinkIssueToPullRequestManuallyParams{
			IssueID: parseUUID(issue.ID), PullRequestID: parseUUID(fx.prID), LinkedByID: parseUUID(workerID),
		}); err != nil {
			t.Fatalf("worker link PR: %v", err)
		}

		postSignedGitHubEventForTest(t, fx.secret, "pull_request_review",
			makeReviewSubmittedPayload(fx, 6201, "colleague-reviewer", "User", "worker-bot", "changes_requested", "Address worker PR feedback"))

		var gotAgentID, gotSquadID string
		var gotLeader bool
		if err := testPool.QueryRow(ctx, `SELECT agent_id, squad_id, is_leader_task FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'`, issue.ID).Scan(&gotAgentID, &gotSquadID, &gotLeader); err != nil {
			t.Fatalf("scan queued squad task: %v", err)
		}
		if gotAgentID != leaderID || gotSquadID != squadID || !gotLeader {
			t.Fatalf("expected squad leader (%s, squad=%s, leader=true), got (agent=%s, squad=%s, leader=%v)", leaderID, squadID, gotAgentID, gotSquadID, gotLeader)
		}

		agentA2 := createPRReviewTestAgent(t, "Reassigned-Developer-A2")
		testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1 AND status = 'queued'`, issue.ID)
		if _, err := testPool.Exec(ctx, `UPDATE issue SET status = 'in_progress', assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issue.ID, agentA2); err != nil {
			t.Fatalf("reassign issue to A2: %v", err)
		}

		postSignedGitHubEventForTest(t, fx.secret, "pull_request_review_comment",
			makeReviewCommentPayload(fx, "created", 6202, "colleague-reviewer", "User", "worker-bot", "pkg/a2.go", 0, 18, "Follow-up for A2"))

		if err := testPool.QueryRow(ctx, `SELECT agent_id FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'`, issue.ID).Scan(&gotAgentID); err != nil {
			t.Fatalf("scan A2 queued task: %v", err)
		}
		if gotAgentID != agentA2 {
			t.Fatalf("reassigned in_progress issue woke %s, want current assignee A2 (%s)", gotAgentID, agentA2)
		}
	})
}

func TestGitHubPRReview_BatchCoalescingTwoRoundCircuitBreakerAndHumanReset(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized")
	}
	ctx := context.Background()
	fx := setupPRReviewFixture(t, 41001004, 1958, "dev-bot")
	devAgentID := createPRReviewTestAgent(t, "Nova-CircuitBreaker")
	issue := createPRReviewTestIssue(t, "2-round circuit breaker test", "in_review")

	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'member', assignee_id = $2 WHERE id = $1`, issue.ID, testUserID); err != nil {
		t.Fatalf("assign issue to human: %v", err)
	}
	if _, err := testHandler.Queries.LinkIssueToPullRequestManually(ctx, db.LinkIssueToPullRequestManuallyParams{
		IssueID:       parseUUID(issue.ID),
		PullRequestID: parseUUID(fx.prID),
		LinkedByID:    parseUUID(devAgentID),
	}); err != nil {
		t.Fatalf("link PR: %v", err)
	}

	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review_comment",
		makeReviewCommentPayload(fx, "created", 5001, "reviewer-a", "User", "dev-bot", "pkg/a.go", 0, 12, "Inline comment 1"))
	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review_comment",
		makeReviewCommentPayload(fx, "created", 5002, "reviewer-a", "User", "dev-bot", "pkg/b.go", 20, 25, "Inline comment 2"))
	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review",
		makeReviewSubmittedPayload(fx, 5003, "reviewer-a", "User", "dev-bot", "changes_requested", "Round 1 summary"))

	var round1TaskID string
	var coalescedIDs []pgtype.UUID
	var queuedCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&queuedCount)
	if queuedCount != 1 {
		t.Fatalf("Round 1 burst should coalesce into 1 queued task, got %d", queuedCount)
	}
	if err := testPool.QueryRow(ctx, `SELECT id, coalesced_comment_ids FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&round1TaskID, &coalescedIDs); err != nil {
		t.Fatalf("scan round 1 task: %v", err)
	}
	if len(coalescedIDs) != 2 {
		t.Fatalf("expected 2 coalesced prior comments on Round 1 task, got %d", len(coalescedIDs))
	}

	var rounds int32
	if err := testPool.QueryRow(ctx, `SELECT consecutive_rounds FROM github_pr_review_wakeup_state WHERE issue_id = $1 AND pull_request_id = $2`, issue.ID, fx.prID).Scan(&rounds); err != nil {
		t.Fatalf("scan wakeup state after round 1: %v", err)
	}
	if rounds != 1 {
		t.Fatalf("consecutive_rounds after coalesced Round 1 = %d, want 1", rounds)
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, round1TaskID); err != nil {
		t.Fatalf("mark round 1 running: %v", err)
	}
	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review_comment",
		makeReviewCommentPayloadWithReviewID(fx, "created", 50031, 5003, "reviewer-a", "User", "MEMBER", "dev-bot", "pkg/c.go", 0, 30, "Late-arriving inline comment from same review 5003"))
	testPool.QueryRow(ctx, `SELECT consecutive_rounds FROM github_pr_review_wakeup_state WHERE issue_id = $1 AND pull_request_id = $2`, issue.ID, fx.prID).Scan(&rounds)
	if rounds != 1 {
		t.Fatalf("same pull_request_review_id comment while round 1 is running must stay Round 1; got %d", rounds)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1 AND status IN ('running', 'queued')`, issue.ID); err != nil {
		t.Fatalf("complete round 1 tasks: %v", err)
	}

	var tasksBeforeRound2 int
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&tasksBeforeRound2)
	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review",
		makeReviewSubmittedPayload(fx, 5004, "reviewer-a", "User", "dev-bot", "changes_requested", "Round 2 feedback"))

	var totalTasks int
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&totalTasks)
	if totalTasks != tasksBeforeRound2+1 {
		t.Fatalf("expected %d total tasks after Round 2, got %d", tasksBeforeRound2+1, totalTasks)
	}
	testPool.QueryRow(ctx, `SELECT consecutive_rounds FROM github_pr_review_wakeup_state WHERE issue_id = $1 AND pull_request_id = $2`, issue.ID, fx.prID).Scan(&rounds)
	if rounds != 2 {
		t.Fatalf("consecutive_rounds after Round 2 = %d, want 2", rounds)
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1 AND status = 'queued'`, issue.ID); err != nil {
		t.Fatalf("complete round 2 task: %v", err)
	}

	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review",
		makeReviewSubmittedPayload(fx, 5005, "reviewer-a", "User", "dev-bot", "changes_requested", "Round 3 P2 nitpick"))

	var tasksAfterRound3 int
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&tasksAfterRound3)
	if tasksAfterRound3 != totalTasks {
		t.Fatalf("Round 3 must NOT start another automated task; got %d, want %d", tasksAfterRound3, totalTasks)
	}

	var round3Comment string
	if err := testPool.QueryRow(ctx, `SELECT content FROM comment WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1`, issue.ID).Scan(&round3Comment); err != nil {
		t.Fatalf("read round 3 comment: %v", err)
	}
	if !strings.Contains(round3Comment, "Round 3 P2 nitpick") || !strings.Contains(round3Comment, "Auto-wakeup paused") {
		t.Fatalf("Round 3 comment should include both review body and pause banner, got:\n%s", round3Comment)
	}

	time.Sleep(10 * time.Millisecond)
	commentReq := withURLParam(newRequest("POST", "/api/issues/"+issue.ID+"/comments", map[string]any{
		"content": "Agreed with reviewer-a on this point, please go ahead and adjust.",
	}), "id", issue.ID)
	testutil.Call(t, testHandler.CreateComment, commentReq).Want(http.StatusCreated)

	testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1 AND status = 'queued'`, issue.ID)
	var beforePostResetTasks int
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&beforePostResetTasks)

	postSignedGitHubEventForTest(t, fx.secret, "pull_request_review_comment",
		makeReviewCommentPayload(fx, "created", 5006, "reviewer-a", "User", "dev-bot", "pkg/a.go", 0, 15, "Post-human-reset inline comment"))

	var afterPostResetTasks int
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&afterPostResetTasks)
	if afterPostResetTasks != beforePostResetTasks+1 {
		t.Fatalf("human intervention in Multica should reset circuit breaker and enqueue a new task: before=%d after=%d", beforePostResetTasks, afterPostResetTasks)
	}
	testPool.QueryRow(ctx, `SELECT consecutive_rounds FROM github_pr_review_wakeup_state WHERE issue_id = $1 AND pull_request_id = $2`, issue.ID, fx.prID).Scan(&rounds)
	if rounds != 1 {
		t.Fatalf("consecutive_rounds after human reset + new review = %d, want 1", rounds)
	}
}

func TestGitHubPRReview_ConcurrentBurstAndWorkspaceTeardown(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized")
	}
	ctx := context.Background()
	fx := setupPRReviewFixture(t, 41001006, 1960, "dev-bot")
	devAgentID := createPRReviewTestAgent(t, "Concurrent-Burst-Agent")
	issue := createPRReviewTestIssue(t, "Concurrent inline review burst", "in_review")

	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'member', assignee_id = $2 WHERE id = $1`, issue.ID, testUserID); err != nil {
		t.Fatalf("assign issue: %v", err)
	}
	if _, err := testHandler.Queries.LinkIssueToPullRequestManually(ctx, db.LinkIssueToPullRequestManuallyParams{
		IssueID:       parseUUID(issue.ID),
		PullRequestID: parseUUID(fx.prID),
		LinkedByID:    parseUUID(devAgentID),
	}); err != nil {
		t.Fatalf("link PR: %v", err)
	}

	const burstSize = 5
	var wg sync.WaitGroup
	for i := range burstSize {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			payload := makeReviewCommentPayload(
				fx,
				"created",
				int64(7000+idx),
				"reviewer-concurrent",
				"User",
				"dev-bot",
				fmt.Sprintf("internal/file_%d.go", idx),
				0,
				10+idx,
				fmt.Sprintf("Concurrent inline comment #%d", idx),
			)
			postSignedGitHubEventForTest(t, fx.secret, "pull_request_review_comment", payload)
		}(i)
	}
	wg.Wait()

	var commentCount, taskCount int
	var coalescedIDs []pgtype.UUID
	var rounds int32
	testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, issue.ID).Scan(&commentCount)
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&taskCount)
	testPool.QueryRow(ctx, `SELECT coalesced_comment_ids FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&coalescedIDs)
	testPool.QueryRow(ctx, `SELECT consecutive_rounds FROM github_pr_review_wakeup_state WHERE issue_id = $1 AND pull_request_id = $2`, issue.ID, fx.prID).Scan(&rounds)

	if commentCount != burstSize {
		t.Fatalf("expected %d synced comments, got %d", burstSize, commentCount)
	}
	if taskCount != 1 {
		t.Fatalf("expected 5 concurrent inline comments to coalesce into 1 queued task, got %d", taskCount)
	}
	if len(coalescedIDs) != burstSize-1 {
		t.Fatalf("expected %d coalesced comment IDs, got %d", burstSize-1, len(coalescedIDs))
	}
	if rounds != 1 {
		t.Fatalf("expected consecutive_rounds = 1 after concurrent burst, got %d", rounds)
	}

	var tempWSID string
	slug := fmt.Sprintf("gh-pr-review-del-%d", time.Now().UnixNano())
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Temp Delete WS', $1, 'Teardown test', 'TDW')
		RETURNING id
	`, slug).Scan(&tempWSID); err != nil {
		t.Fatalf("insert temp workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, tempWSID, testUserID); err != nil {
		t.Fatalf("insert temp member: %v", err)
	}
	var dummyID string
	testPool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&dummyID)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO github_pr_review_delivery (workspace_id, issue_id, pull_request_id, event_type, github_event_id, comment_id)
		VALUES ($1, $2, $2, 'pull_request_review', 8801, $2)
	`, tempWSID, dummyID); err != nil {
		t.Fatalf("seed delivery in temp ws: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO github_pr_review_wakeup_state (issue_id, pull_request_id, workspace_id, consecutive_rounds)
		VALUES ($2, $2, $1, 1)
	`, tempWSID, dummyID); err != nil {
		t.Fatalf("seed wakeup state in temp ws: %v", err)
	}

	delReq := withURLParam(newRequest("DELETE", "/api/workspaces/"+tempWSID, nil), "id", tempWSID)
	testutil.Call(t, testHandler.DeleteWorkspace, delReq).Want(http.StatusNoContent)

	var leftDeliveries, leftStates int
	testPool.QueryRow(ctx, `SELECT count(*) FROM github_pr_review_delivery WHERE workspace_id = $1`, tempWSID).Scan(&leftDeliveries)
	testPool.QueryRow(ctx, `SELECT count(*) FROM github_pr_review_wakeup_state WHERE workspace_id = $1`, tempWSID).Scan(&leftStates)
	if leftDeliveries != 0 || leftStates != 0 {
		t.Fatalf("DeleteWorkspace leaked rows: deliveries=%d states=%d", leftDeliveries, leftStates)
	}
}
