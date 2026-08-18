package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func listIssueFiles(t *testing.T, issueID string) (files []AttachmentResponse, total int) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/files", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListIssueFiles(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssueFiles: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Files []AttachmentResponse `json:"files"`
		Total int                  `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode ListIssueFiles: %v", err)
	}
	return resp.Files, resp.Total
}

// TestIssueFilesAggregatesIssueAndCommentAttachments pins the "files in this
// task" contract: a file surfaces when it is attached to the issue directly or
// to one of its comments. Attachments on unrelated issues are excluded.
func TestIssueFilesAggregatesIssueAndCommentAttachments(t *testing.T) {
	project := createProjectFileTestProject(t, "issue files scoping")
	issue := createProjectFileTestIssue(t, project.ID)

	// A foreign issue in the same workspace must not leak in.
	otherProject := createProjectFileTestProject(t, "issue files other")
	otherIssue := createProjectFileTestIssue(t, otherProject.ID)
	insertTestAttachment(t, otherIssue.ID, "", "", "member", testUserID, "foreign.txt")

	// Issue-scoped file.
	insertTestAttachment(t, issue.ID, "", "", "agent", testUserID, "issue.txt")

	// Comment-scoped file: a comment on the issue.
	var commentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, 'produced a file')
		RETURNING id
	`, testWorkspaceID, issue.ID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
	})
	insertTestAttachment(t, "", commentID, "", "agent", testUserID, "comment.txt")

	files, total := listIssueFiles(t, issue.ID)
	if total != 2 || len(files) != 2 {
		t.Fatalf("issue files: total=%d files=%d, want 2/2 (got %+v)", total, len(files), files)
	}
	seen := map[string]bool{}
	for _, f := range files {
		seen[f.Filename] = true
	}
	for _, name := range []string{"issue.txt", "comment.txt"} {
		if !seen[name] {
			t.Errorf("missing %s in issue files: %+v", name, files)
		}
	}
	if seen["foreign.txt"] {
		t.Errorf("foreign attachment leaked into issue files: %+v", files)
	}
}

// TestIssueFilesEmpty asserts the endpoint returns an empty list (not an error)
// for an issue with no artifacts.
func TestIssueFilesEmpty(t *testing.T) {
	project := createProjectFileTestProject(t, "issue files empty")
	issue := createProjectFileTestIssue(t, project.ID)

	files, total := listIssueFiles(t, issue.ID)
	if total != 0 || len(files) != 0 {
		t.Fatalf("empty issue files: total=%d files=%d, want 0/0", total, len(files))
	}
}

// TestFilesDeduplicateSameFileTwice pins the artifact-dedup contract: an agent
// can attach one produced file to BOTH the issue and a comment (two upload
// rows, same filename + size) — the files list must surface it once, in both
// the issue-level and project-level surfaces.
func TestFilesDeduplicateSameFileTwice(t *testing.T) {
	project := createProjectFileTestProject(t, "files dedup")
	issue := createProjectFileTestIssue(t, project.ID)

	// First upload: issue-scoped.
	insertTestAttachment(t, issue.ID, "", "", "agent", testUserID, "dup.md")

	// Second upload: comment-scoped (the "attached again with this comment"
	// agent pattern).
	var commentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, 'same file again')
		RETURNING id
	`, testWorkspaceID, issue.ID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
	})
	insertTestAttachment(t, "", commentID, "", "agent", testUserID, "dup.md")

	files, total := listIssueFiles(t, issue.ID)
	if total != 1 || len(files) != 1 {
		t.Fatalf("issue files dedup: total=%d files=%d, want 1/1", total, len(files))
	}

	pfiles, ptotal := listProjectFiles(t, project.ID)
	if ptotal != 1 || len(pfiles) != 1 {
		t.Fatalf("project files dedup: total=%d files=%d, want 1/1", ptotal, len(pfiles))
	}
}
