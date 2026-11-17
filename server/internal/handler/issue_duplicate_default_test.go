package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestCreateIssueDuplicateDefault(t *testing.T) {
	for _, setting := range []string{"", "false", "true", "invalid"} {
		for _, override := range []string{"omitted", "null", "false", "true"} {
			t.Run(setting+"/"+override, func(t *testing.T) {
				t.Setenv("MULTICA_ALLOW_DUPLICATE_ISSUES", setting)
				h := *testHandler
				s := testHandler.IssueService
				h.IssueService = service.NewIssueService(s.Queries, s.TxStarter, s.Bus, s.Analytics, s.TaskService)
				title := fmt.Sprintf("Duplicate default %s %s %d", setting, override, time.Now().UnixNano())
				dbfx.Issue(t, title)
				body := map[string]any{"title": title}
				switch override {
				case "null":
					body["allow_duplicate"] = nil
				case "false":
					body["allow_duplicate"] = false
				case "true":
					body["allow_duplicate"] = true
				}
				want := http.StatusConflict
				if override == "true" || (setting == "true" && (override == "omitted" || override == "null")) {
					want = http.StatusCreated
				}
				req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body)
				w := testutil.Call(t, h.CreateIssue, req)
				if w.Code == http.StatusCreated {
					var issue IssueResponse
					w.JSON(&issue)
					dbfx.Cleanup(t, "DELETE FROM issue WHERE id = $1", issue.ID)
				}
				w.Want(want)
			})
		}
	}
}

func TestCommentSubIssueDuplicateDefault(t *testing.T) {
	t.Setenv("MULTICA_ALLOW_DUPLICATE_ISSUES", "true")
	h := *testHandler
	s := testHandler.IssueService
	h.IssueService = service.NewIssueService(s.Queries, s.TxStarter, s.Bus, s.Analytics, s.TaskService)
	for _, explicitFalse := range []bool{false, true} {
		t.Run(fmt.Sprint(explicitFalse), func(t *testing.T) {
			source := dbfx.Issue(t, fmt.Sprintf("Duplicate source %d", time.Now().UnixNano()))
			comment := dbfx.Comment(t, source, "Capture this comment")
			title := fmt.Sprintf("Duplicate captured title %d", time.Now().UnixNano())
			dbfx.Issue(t, title, testutil.Cols{"parent_issue_id": source})
			previewReq := withURLParam(newRequest(http.MethodGet, "/api/comments/"+comment+"/sub-issue-preview", nil), "commentId", comment)
			var preview sourceContextPreviewResponse
			testutil.Call(t, h.PreviewCommentSubIssue, previewReq).Want(http.StatusOK).JSON(&preview)
			issue := map[string]any{"title": title, "status": "todo", "priority": "none"}
			if explicitFalse {
				issue["allow_duplicate"] = false
			}
			req := withURLParam(newRequest(http.MethodPost, "/api/comments/"+comment+"/sub-issues", map[string]any{"mode": "manual", "capture_token": preview.CaptureToken, "issue": issue}), "commentId", comment)
			w := testutil.Call(t, h.CreateCommentSubIssue, req)
			if explicitFalse {
				w.Want(http.StatusConflict)
				return
			}
			w.Want(http.StatusCreated)
			var created IssueResponse
			w.JSON(&created)
			dbfx.Cleanup(t, "DELETE FROM issue WHERE id = $1", created.ID)
			dbfx.Cleanup(t, "DELETE FROM issue_source_context WHERE issue_id = $1", created.ID)
		})
	}
}

func TestIssueServiceDuplicateDefaultWithoutHTTP(t *testing.T) {
	t.Setenv("MULTICA_ALLOW_DUPLICATE_ISSUES", "true")
	s := testHandler.IssueService
	svc := service.NewIssueService(s.Queries, s.TxStarter, nil, nil, nil)
	title := fmt.Sprintf("Channel service duplicate %d", time.Now().UnixNano())
	original := dbfx.Issue(t, title)
	result, err := svc.Create(context.Background(), service.IssueCreateParams{
		WorkspaceID: parseUUID(testWorkspaceID), Title: title, Status: "todo", Priority: "none", CreatorType: "member", CreatorID: parseUUID(testUserID),
	}, service.IssueCreateOpts{})
	if err != nil {
		t.Fatalf("service create without request override: %v", err)
	}
	id := util.UUIDToString(result.Issue.ID)
	dbfx.Cleanup(t, "DELETE FROM issue WHERE id = $1", id)
	if id == original {
		t.Fatal("service reused the existing issue")
	}
}
