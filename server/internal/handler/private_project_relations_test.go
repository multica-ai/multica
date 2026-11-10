package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Relations and indirect ids (MUL-8472): an object a request reaches through
// another issue, a comment or an attachment id must answer for a private
// project exactly as the direct issue route does.

func relationIssueNumber() int {
	return 700000 + int(time.Now().UnixNano()%50000)*2
}

func putIssueAs(t *testing.T, userID, issueID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := withURLParam(asUser(newRequest(http.MethodPut, "/api/issues/"+issueID, body), userID), "id", issueID)
	return testutil.Call(t, testHandler.UpdateIssue, req)
}

func getIssueAs(t *testing.T, userID, issueID string) *testutil.Response {
	t.Helper()
	req := withURLParam(asUser(newRequest(http.MethodGet, "/api/issues/"+issueID, nil), userID), "id", issueID)
	return testutil.Call(t, testHandler.GetIssue, req)
}

func TestPrivateProjectDuplicateMarkNeedsVisibleTarget(t *testing.T) {
	f := newPrivateProjectFixture(t)
	secretTitle := f.marker + " secret issue"

	hidden := putIssueAs(t, f.outsider, f.publicIssue, map[string]any{"duplicate_of_issue_id": f.secretIssue})
	missing := putIssueAs(t, f.outsider, f.publicIssue, map[string]any{"duplicate_of_issue_id": uuid.NewString()})
	hidden.Want(http.StatusBadRequest)
	if hidden.Code != missing.Code || hidden.Text() != missing.Text() {
		t.Errorf("hidden target answers %d %s, missing target %d %s", hidden.Code, hidden.Text(), missing.Code, missing.Text())
	}
	if strings.Contains(hidden.Text(), secretTitle) {
		t.Errorf("refused mark leaks the hidden original: %s", hidden.Text())
	}
	var status string
	var duplicateOf *string
	dbfx.QueryRow(t, `SELECT status, duplicate_of_issue_id::text FROM issue WHERE id = $1`, f.publicIssue).Scan(&status, &duplicateOf)
	if status != "todo" || duplicateOf != nil {
		t.Errorf("refused mark wrote the issue: status=%s duplicate_of=%v", status, duplicateOf)
	}

	// The insider can mark, and sees the original.
	own := dbfx.Issue(t, f.marker+" insider duplicate", testutil.Cols{"number": relationIssueNumber()})
	var marked IssueResponse
	putIssueAs(t, f.insider, own, map[string]any{"duplicate_of_issue_id": f.secretIssue}).Want(http.StatusOK).JSON(&marked)
	if marked.DuplicateOf == nil || marked.DuplicateOf.ID != f.secretIssue {
		t.Fatalf("insider mark response duplicate_of = %+v, want the secret issue", marked.DuplicateOf)
	}

	// After losing access the same mark no longer names the original.
	removeProjectMemberAsOwner(t, f, f.insider)
	var detail IssueResponse
	body := getIssueAs(t, f.insider, own).Want(http.StatusOK).JSON(&detail).Text()
	if detail.DuplicateOf != nil || strings.Contains(body, secretTitle) {
		t.Errorf("revoked member's issue detail still names the hidden original: %s", body)
	}
	var edited IssueResponse
	body = putIssueAs(t, f.outsider, own, map[string]any{"title": f.marker + " retitled duplicate"}).Want(http.StatusOK).JSON(&edited).Text()
	if edited.DuplicateOf != nil || strings.Contains(body, secretTitle) {
		t.Errorf("outsider update response names the hidden original: %s", body)
	}
	getIssueAs(t, testUserID, own).Want(http.StatusOK).JSON(&detail)
	if detail.DuplicateOf == nil || detail.DuplicateOf.ID != f.secretIssue {
		t.Errorf("workspace owner lost the duplicate_of on %s: %+v", own, detail.DuplicateOf)
	}
}

func uploadAs(t *testing.T, userID string, fields map[string]string) *testutil.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "relation.txt")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("relation upload"))
	for k, v := range fields {
		writer.WriteField(k, v)
	}
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	return testutil.Call(t, testHandler.UploadFile, req)
}

func TestPrivateProjectUploadToHiddenComment(t *testing.T) {
	f := newPrivateProjectFixture(t)
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	t.Cleanup(func() { testHandler.Storage = origStorage })
	dbfx.Cleanup(t, `DELETE FROM attachment WHERE comment_id = $1`, f.secretComment)
	count := func() int {
		return dbfx.Count(t, `SELECT count(*) FROM attachment WHERE comment_id = $1`, f.secretComment)
	}

	uploadAs(t, f.outsider, map[string]string{"comment_id": f.secretComment}).Want(http.StatusForbidden)
	uploadAs(t, f.outsider, map[string]string{"issue_id": f.publicIssue, "comment_id": f.secretComment}).Want(http.StatusForbidden)
	if n := count(); n != 0 {
		t.Fatalf("outsider uploads attached %d files to the hidden comment", n)
	}
	uploadAs(t, f.insider, map[string]string{"comment_id": f.secretComment}).Want(http.StatusOK)
	if n := count(); n != 1 {
		t.Fatalf("insider upload attached %d files, want 1", n)
	}
}

func insertCommentOnlyAttachment(t *testing.T, commentID, filename string) string {
	t.Helper()
	return dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"comment_id":    commentID,
		"uploader_type": "member",
		"uploader_id":   testUserID,
		"filename":      filename,
		"url":           "https://example.invalid/" + filename,
		"content_type":  "text/plain",
		"size_bytes":    1,
	})
}

func attachmentIssueID(t *testing.T, attachmentID string) *string {
	t.Helper()
	var issueID *string
	dbfx.QueryRow(t, `SELECT issue_id::text FROM attachment WHERE id = $1`, attachmentID).Scan(&issueID)
	return issueID
}

// An attachment that carries only a comment id belongs to that comment's
// issue; an issue write must not adopt it past the comment's project.
func TestPrivateProjectAttachmentAdoptionSkipsHidden(t *testing.T) {
	f := newPrivateProjectFixture(t)
	secretFile := f.marker + "-secret-file.txt"
	hiddenAtt := insertCommentOnlyAttachment(t, f.secretComment, secretFile)

	putIssueAs(t, f.outsider, f.publicIssue, map[string]any{"attachment_ids": []string{hiddenAtt}}).Want(http.StatusOK)
	if got := attachmentIssueID(t, hiddenAtt); got != nil {
		t.Fatalf("outsider update adopted the hidden attachment into %s", *got)
	}
	if body := getIssueAs(t, f.outsider, f.publicIssue).Want(http.StatusOK).Text(); strings.Contains(body, secretFile) {
		t.Fatalf("public issue exposes the hidden attachment: %s", body)
	}

	var created IssueResponse
	testutil.Call(t, testHandler.CreateIssue, asUser(newRequest(http.MethodPost, "/api/issues", map[string]any{
		"title": f.marker + " adopting create", "attachment_ids": []string{hiddenAtt},
	}), f.outsider)).Want(http.StatusCreated).JSON(&created)
	dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, created.ID)
	if got := attachmentIssueID(t, hiddenAtt); got != nil {
		t.Fatalf("outsider create adopted the hidden attachment into %s", *got)
	}

	// A caller who can see the comment keeps the existing adoption behavior.
	visibleAtt := insertCommentOnlyAttachment(t, f.secretComment, f.marker+"-insider-file.txt")
	own := dbfx.Issue(t, f.marker+" insider adopter", testutil.Cols{"number": relationIssueNumber()})
	putIssueAs(t, f.insider, own, map[string]any{"attachment_ids": []string{visibleAtt}}).Want(http.StatusOK)
	if got := attachmentIssueID(t, visibleAtt); got == nil || *got != own {
		t.Fatalf("insider update did not adopt the visible attachment: %v", got)
	}
}

// A sub-issue in a workspace project under a private parent: the parent group
// must not name the parent to a reader outside its project.
func TestPrivateProjectTableParentGroupHidden(t *testing.T) {
	f := newPrivateProjectFixture(t)
	publicProject := dbfx.Project(t, f.marker+" public project")
	dbfx.Issue(t, f.marker+" public child", testutil.Cols{
		"project_id": publicProject, "parent_issue_id": f.secretIssue, "number": relationIssueNumber(),
	})
	secretTitle := f.marker + " secret issue"
	groups := func(userID string) string {
		t.Helper()
		req := asUser(newRequest(http.MethodPost, "/api/issues/table/groups", issueTableGroupsRequest{
			Query: issueTableQuerySpec{
				Scope: issueTableScope{Kind: "project", ProjectID: publicProject},
				Sort:  issueTableSortRequest{Field: "title", Direction: "asc"},
			},
			Group: issueTableGroupSpec{Kind: "parent"},
			Page:  issueTablePageRequest{Limit: 100},
		}), userID)
		return testutil.Call(t, testHandler.ListIssueTableGroups, req).Want(http.StatusOK).Text()
	}
	outsiderBody := groups(f.outsider)
	if strings.Contains(outsiderBody, secretTitle) {
		t.Errorf("outsider parent group names the hidden parent: %s", outsiderBody)
	}
	var outsider issueTableGroupsResponse
	if err := json.Unmarshal([]byte(outsiderBody), &outsider); err != nil {
		t.Fatal(err)
	}
	for _, g := range outsider.Groups {
		if g.Value.ParentID != nil && *g.Value.ParentID == f.secretIssue && (g.Value.Parent != nil || g.Value.ValueState != "unavailable") {
			t.Errorf("outsider hidden parent group = %+v, want unavailable with no parent ref", g.Value)
		}
	}
	for _, userID := range []string{f.insider, testUserID} {
		if body := groups(userID); !strings.Contains(body, secretTitle) {
			t.Errorf("authorized reader %s lost the parent group title: %s", userID, body)
		}
	}
}

// A sub-issue created from a private thread into a workspace project keeps its
// frozen snapshot, but the live source is not compared against for a reader
// who cannot see it: nothing posted there after capture reaches them.
func TestPrivateProjectSourceContextLiveThreadHidden(t *testing.T) {
	f := newPrivateProjectFixture(t)
	ctx := context.Background()
	// Same lock as TestCommentSourceContextLifecycle: another package's intent
	// sweeper must not claim this test's capture.
	lockConn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(0x53434f4e54455854)); err != nil {
		lockConn.Release()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, int64(0x53434f4e54455854))
		lockConn.Release()
	})
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	t.Cleanup(func() { testHandler.Storage = origStorage })

	publicProject := dbfx.Project(t, f.marker+" source target project")
	anchor := dbfx.Comment(t, f.secretIssue, f.marker+" anchor reply", testutil.Cols{"parent_id": f.secretComment})

	var preview sourceContextPreviewResponse
	previewReq := withURLParam(asUser(newRequest(http.MethodGet, "/api/comments/"+anchor+"/sub-issue-preview", nil), f.insider), "commentId", anchor)
	testutil.Call(t, testHandler.PreviewCommentSubIssue, previewReq).Want(http.StatusOK).JSON(&preview)

	var target IssueResponse
	createReq := withURLParam(asUser(newRequest(http.MethodPost, "/api/comments/"+anchor+"/sub-issues", map[string]any{
		"mode": "manual", "capture_token": preview.CaptureToken,
		"issue": map[string]any{"title": f.marker + " public target", "status": "todo", "priority": "none", "project_id": publicProject},
	}), f.insider), "commentId", anchor)
	testutil.Call(t, testHandler.CreateCommentSubIssue, createReq).Want(http.StatusCreated).JSON(&target)
	dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, target.ID)
	dbfx.Cleanup(t, `DELETE FROM issue_source_context WHERE issue_id = $1`, target.ID)

	// After capture, a file lands in the private description, and a comment
	// with an earlier timestamp (as a backfill or import writes it) joins the
	// captured part of the thread.
	liveFile := f.marker + "-live-plan.pdf"
	liveAtt := dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": f.secretIssue, "uploader_type": "member", "uploader_id": testUserID,
		"filename": liveFile, "url": "https://example.invalid/" + liveFile, "content_type": "application/pdf", "size_bytes": 1,
	})
	dbfx.Exec(t, `UPDATE issue SET description = $1 WHERE id = $2`, "!file["+liveFile+"](/api/attachments/"+liveAtt+"/download)", f.secretIssue)
	live := f.marker + " live reply after capture"
	dbfx.Comment(t, f.secretIssue, live, testutil.Cols{
		"parent_id":  f.secretComment,
		"created_at": testutil.Raw("(SELECT created_at + interval '1 microsecond' FROM comment WHERE id = '" + f.secretComment + "')"),
	})

	type detail struct {
		SourceContext *struct {
			DisplayState         string `json:"display_state"`
			CanOpenCurrentSource bool   `json:"can_open_current_source"`
			CurrentSource        any    `json:"current_source"`
			Snapshot             struct {
				AnchorCommentID string `json:"anchor_comment_id"`
			} `json:"snapshot"`
		} `json:"source_context"`
	}
	var out detail
	body := getIssueAs(t, f.outsider, target.ID).Want(http.StatusOK).JSON(&out).Text()
	if strings.Contains(body, live) || strings.Contains(body, liveFile) {
		t.Errorf("outsider sees content added to the hidden source after capture: %s", body)
	}
	if out.SourceContext == nil || out.SourceContext.Snapshot.AnchorCommentID != anchor {
		t.Fatalf("outsider lost the frozen snapshot: %s", body)
	}
	if out.SourceContext.DisplayState != "unavailable" || out.SourceContext.CanOpenCurrentSource || out.SourceContext.CurrentSource != nil {
		t.Errorf("outsider source state = %+v, want unavailable with no current source", out.SourceContext)
	}
	for _, userID := range []string{f.insider, testUserID} {
		if body := getIssueAs(t, userID, target.ID).Want(http.StatusOK).Text(); !strings.Contains(body, live) || !strings.Contains(body, liveFile) {
			t.Errorf("authorized reader %s no longer sees the live source change: %s", userID, body)
		}
	}
}

// Validations that run before the visibility check would tell a hidden id
// from a missing one; each pair below must answer identically.
func TestPrivateProjectRelationExistenceOracles(t *testing.T) {
	f := newPrivateProjectFixture(t)
	same := func(name string, hidden, missing *testutil.Response) {
		t.Helper()
		if hidden.Code != missing.Code || hidden.Text() != missing.Text() {
			t.Errorf("%s: hidden answers %d %s, missing answers %d %s", name, hidden.Code, hidden.Text(), missing.Code, missing.Text())
		}
	}

	move := func(userID string, body map[string]any) *testutil.Response {
		req := withURLParam(asUser(newRequest(http.MethodPost, "/api/issues/"+f.publicIssue+"/move", body), userID), "id", f.publicIssue)
		return testutil.Call(t, testHandler.MoveIssue, req)
	}
	same("move anchor",
		move(f.outsider, map[string]any{"before_id": f.secretIssue, "after_id": nil}),
		move(f.outsider, map[string]any{"before_id": uuid.NewString(), "after_id": nil}))
	same("move project",
		move(f.outsider, map[string]any{"project_id": f.projectID, "before_id": uuid.NewString(), "after_id": nil}),
		move(f.outsider, map[string]any{"project_id": uuid.NewString(), "before_id": uuid.NewString(), "after_id": nil}))
	if body := move(testUserID, map[string]any{"before_id": f.secretIssue, "after_id": nil}).Text(); strings.Contains(body, "move anchor not found") {
		t.Errorf("workspace owner cannot anchor on the private issue: %s", body)
	}

	title := f.marker + " oracle create"
	create := func(parentID string) *testutil.Response {
		return testutil.Call(t, testHandler.CreateIssue, asUser(newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title": title, "parent_issue_id": parentID, "assignee_type": "agent", "assignee_id": uuid.NewString(),
		}), f.outsider))
	}
	same("create parent with invalid assignee", create(f.secretIssue), create(uuid.NewString()))
	if n := dbfx.Count(t, `SELECT count(*) FROM issue WHERE title = $1`, title); n != 0 {
		t.Errorf("refused creates wrote %d issues", n)
	}

	subIssue := func(tokenIssueID string) *testutil.Response {
		anchor := uuid.NewString()
		token := "sha256:" + strings.Repeat("0", 64) + ":" + tokenIssueID
		req := withURLParam(asUser(newRequest(http.MethodPost, "/api/comments/"+anchor+"/sub-issues", map[string]any{
			"mode": "manual", "capture_token": token, "issue": map[string]any{"title": title, "status": "todo", "priority": "none"},
		}), f.outsider), "commentId", anchor)
		return testutil.Call(t, testHandler.CreateCommentSubIssue, req)
	}
	same("comment sub-issue token issue", subIssue(f.secretIssue), subIssue(uuid.NewString()))

	agentID := seededReadyAgentID(t)
	preview := func(userID string) IssueTriggerPreviewResponse {
		var resp IssueTriggerPreviewResponse
		testutil.Call(t, testHandler.PreviewIssueTrigger, asUser(newRequest(http.MethodPost, "/api/issues/preview-trigger", map[string]any{
			"issue_ids": []string{f.secretIssue}, "assignee_type": "agent", "assignee_id": agentID,
		}), userID)).Want(http.StatusOK).JSON(&resp)
		return resp
	}
	if got := preview(f.outsider); got.TotalCount != 0 {
		t.Errorf("outsider preview evaluates the hidden issue: %+v", got)
	}
	if got := preview(testUserID); got.TotalCount != 1 || got.Triggers[0].IssueID != f.secretIssue {
		t.Errorf("workspace owner preview = %+v, want one run on the private issue", got)
	}
}
