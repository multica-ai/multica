package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// storageLimitWorkspace is a workspace of its own whose attachments already
// hold usedBytes, so its storage use belongs to the test alone.
func storageLimitWorkspace(t *testing.T, usedBytes int64) (workspaceID, attachmentID string) {
	t.Helper()
	workspaceID = memberLimitWorkspace(t, 0)
	dbfx.Cleanup(t, `DELETE FROM attachment WHERE workspace_id = $1`, workspaceID)
	dbfx.Cleanup(t, `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
	attachmentID = dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": workspaceID, "uploader_type": "member", "uploader_id": testUserID,
		"filename": "existing.bin", "url": "https://cdn.example.com/workspaces/" + workspaceID + "/existing.bin",
		"content_type": "application/octet-stream", "size_bytes": usedBytes,
	})
	return workspaceID, attachmentID
}

func useStorageLimit(t *testing.T, workspaceID string, action entitlement.Action, limit int) *entitlementtest.Stub {
	t.Helper()
	stub := entitlementtest.New()
	stub.Set(uuid.MustParse(workspaceID), entitlement.GateStorageBytes, memberLimitDecision(action, limit))
	prior := testHandler.Entitlements
	testHandler.Entitlements = stub
	t.Cleanup(func() { testHandler.Entitlements = prior })
	return stub
}

func useMockStorage(t *testing.T) *mockStorage {
	t.Helper()
	store := &mockStorage{}
	prior := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = prior })
	return store
}

func uploadBytes(t *testing.T, workspaceID, filename string, size int) *testutil.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(bytes.Repeat([]byte("x"), size))
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	return testutil.Call(t, testHandler.UploadFile, req)
}

func storageOverageOf(t *testing.T, workspaceID string) memberLimitOverage {
	t.Helper()
	var o memberLimitOverage
	dbfx.QueryRow(t, `
		SELECT COALESCE(MAX(limit_value), 0), COALESCE(MAX(peak_usage), 0),
		       COALESCE(SUM(observed_count), 0), COALESCE(SUM(enforced_count), 0)
		  FROM entitlement_limit_overage
		 WHERE workspace_id = $1 AND gate = 'storage_bytes'`, workspaceID).
		Scan(&o.limit, &o.peak, &o.observed, &o.enforced)
	return o
}

type storageLimitBody struct {
	Code       string `json:"code"`
	UsedBytes  int64  `json:"used_bytes"`
	LimitBytes int64  `json:"limit_bytes"`
}

func wantStorageLimitReached(t *testing.T, resp *testutil.Response, used, limit int64) {
	t.Helper()
	var body storageLimitBody
	resp.Want(http.StatusForbidden).JSON(&body)
	if body.Code != storageLimitReachedCode || body.UsedBytes != used || body.LimitBytes != limit {
		t.Fatalf("body = %+v, want %s with used %d of %d", body, storageLimitReachedCode, used, limit)
	}
}

func attachmentCount(t *testing.T, workspaceID string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM attachment WHERE workspace_id = $1`, workspaceID)
}

// A workspace holding 900 of 1000 bytes uploads 200 more: off admits without
// reading anything, observe admits and records, enforce refuses before the
// object is written and records.
func TestUploadFileUnderEachStorageLimitAction(t *testing.T) {
	for _, tc := range []struct {
		action entitlement.Action
		record memberLimitOverage
	}{
		{entitlement.ActionOff, memberLimitOverage{}},
		{entitlement.ActionObserve, memberLimitOverage{limit: 1000, peak: 1100, observed: 1}},
		{entitlement.ActionEnforce, memberLimitOverage{limit: 1000, peak: 1100, enforced: 1}},
	} {
		t.Run(string(tc.action), func(t *testing.T) {
			workspaceID, _ := storageLimitWorkspace(t, 900)
			useStorageLimit(t, workspaceID, tc.action, 1000)
			store := useMockStorage(t)

			resp := uploadBytes(t, workspaceID, "design-v3.fig", 200)
			if tc.action == entitlement.ActionEnforce {
				wantStorageLimitReached(t, resp, 900, 1000)
				if attachmentCount(t, workspaceID) != 1 || len(store.files) != 0 {
					t.Fatalf("refused upload stored something: rows=%d objects=%d", attachmentCount(t, workspaceID), len(store.files))
				}
			} else {
				resp.Want(http.StatusOK)
				if attachmentCount(t, workspaceID) != 2 {
					t.Fatalf("admitted upload rows = %d, want 2", attachmentCount(t, workspaceID))
				}
			}
			if got := storageOverageOf(t, workspaceID); got != tc.record {
				t.Fatalf("overage = %+v, want %+v", got, tc.record)
			}
		})
	}
}

func TestUploadFileWithinStorageLimitRecordsNothing(t *testing.T) {
	workspaceID, _ := storageLimitWorkspace(t, 900)
	useStorageLimit(t, workspaceID, entitlement.ActionEnforce, 1000)
	useMockStorage(t)

	uploadBytes(t, workspaceID, "small.txt", 100).Want(http.StatusOK)
	if got := storageOverageOf(t, workspaceID); got != (memberLimitOverage{}) {
		t.Fatalf("overage = %+v, want none", got)
	}
	// Now exactly at the limit: the next byte is refused.
	wantStorageLimitReached(t, uploadBytes(t, workspaceID, "one.txt", 1), 1000, 1000)
}

// A full workspace keeps every attachment it has: reading one is never checked.
func TestFullWorkspaceStillDownloadsExistingAttachments(t *testing.T) {
	workspaceID, attachmentID := storageLimitWorkspace(t, 5000)
	stub := useStorageLimit(t, workspaceID, entitlement.ActionEnforce, 1000)
	origStorage, origCfg, origSigner := testHandler.Storage, testHandler.cfg, testHandler.CFSigner
	testHandler.Storage = &mockStorageNoCdn{}
	testHandler.cfg.AttachmentDownloadMode = "proxy"
	testHandler.CFSigner = nil
	t.Cleanup(func() {
		testHandler.Storage, testHandler.cfg, testHandler.CFSigner = origStorage, origCfg, origSigner
	})

	req := httptest.NewRequest(http.MethodGet, "/api/attachments/"+attachmentID+"/download-metadata", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	var resp AttachmentResponse
	testutil.Call(t, testHandler.GetAttachmentForDownload, withURLParam(req, "id", attachmentID)).Want(http.StatusOK).JSON(&resp)
	if resp.ID != attachmentID {
		t.Fatalf("download metadata = %+v", resp)
	}
	if calls := stub.Calls(); len(calls) != 0 {
		t.Fatalf("reading an attachment consulted the storage gate: %+v", calls)
	}
}

// Right after an upgrade the cached policy is still the old one. The upload
// re-reads it before refusing and goes through on the new limit.
func TestUploadFileRefreshesPolicyBeforeRefusing(t *testing.T) {
	workspaceID, _ := storageLimitWorkspace(t, 900)
	stub := useStorageLimit(t, workspaceID, entitlement.ActionEnforce, 1000)
	stub.SetRefreshed(uuid.MustParse(workspaceID), entitlement.GateStorageBytes, memberLimitDecision(entitlement.ActionEnforce, 10_000))
	useMockStorage(t)

	uploadBytes(t, workspaceID, "after-upgrade.png", 200).Want(http.StatusOK)
	if len(stub.Refreshes()) != 1 {
		t.Fatalf("refreshes = %d, want 1", len(stub.Refreshes()))
	}
	if got := storageOverageOf(t, workspaceID); got != (memberLimitOverage{}) {
		t.Fatalf("overage = %+v, want none after the upgrade", got)
	}
}

func getStorageUsage(t *testing.T, workspaceID string) StorageUsageResponse {
	t.Helper()
	req := newRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/storage-usage", nil)
	req.Header.Set("X-Workspace-ID", workspaceID)
	var resp StorageUsageResponse
	testutil.Call(t, testHandler.GetStorageUsage, withURLParam(req, "id", workspaceID)).Want(http.StatusOK).JSON(&resp)
	return resp
}

func TestGetStorageUsage(t *testing.T) {
	workspaceID, _ := storageLimitWorkspace(t, 1000)

	if off := getStorageUsage(t, workspaceID); off.Action != "off" || off.UsedBytes != nil || off.LimitBytes != nil || off.Reached != nil {
		t.Fatalf("without a provider = %+v, want off without figures", off)
	}
	useStorageLimit(t, workspaceID, entitlement.ActionObserve, 1000)
	if got := getStorageUsage(t, workspaceID); got.Action != "observe" || got.UsedBytes == nil || *got.UsedBytes != 1000 ||
		got.LimitBytes == nil || *got.LimitBytes != 1000 || got.Reached != nil {
		t.Fatalf("observed = %+v, want 1000 of 1000 without reached", got)
	}
	useStorageLimit(t, workspaceID, entitlement.ActionEnforce, 1000)
	if got := getStorageUsage(t, workspaceID); got.Action != "enforce" || got.Reached == nil || !*got.Reached {
		t.Fatalf("enforced = %+v, want reached", got)
	}
	useStorageLimit(t, workspaceID, entitlement.ActionEnforce, 1001)
	if got := getStorageUsage(t, workspaceID); got.Reached == nil || *got.Reached {
		t.Fatalf("enforced below the limit = %+v, want reached false", got)
	}
}

func TestGetStorageUsageRefreshesBeforeReportingFull(t *testing.T) {
	workspaceID, _ := storageLimitWorkspace(t, 1000)
	stub := useStorageLimit(t, workspaceID, entitlement.ActionEnforce, 1000)
	stub.SetRefreshed(uuid.MustParse(workspaceID), entitlement.GateStorageBytes, memberLimitDecision(entitlement.ActionEnforce, 100_000))

	got := getStorageUsage(t, workspaceID)
	if got.Reached == nil || *got.Reached || got.LimitBytes == nil || *got.LimitBytes != 100_000 || len(stub.Refreshes()) != 1 {
		t.Fatalf("usage = %+v, refreshes = %d; want the upgraded limit", got, len(stub.Refreshes()))
	}
}

// Usage is summed from the rows, so attachments removed by deleting their
// issue (a database cascade the application never sees) stop counting at once.
func TestStorageUsageDropsWhenAnIssueIsDeleted(t *testing.T) {
	workspaceID, _ := storageLimitWorkspace(t, 100)
	useStorageLimit(t, workspaceID, entitlement.ActionObserve, 1000)
	fx := testutil.New(testPool, workspaceID, testUserID)
	issueID := fx.Issue(t, "Holds a large file")
	fx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": workspaceID, "issue_id": issueID, "uploader_type": "member", "uploader_id": testUserID,
		"filename": "large.bin", "url": "https://cdn.example.com/large.bin",
		"content_type": "application/octet-stream", "size_bytes": 800,
	})
	if got := getStorageUsage(t, workspaceID); got.UsedBytes == nil || *got.UsedBytes != 900 {
		t.Fatalf("before delete = %+v, want 900", got)
	}
	fx.Exec(t, `DELETE FROM issue WHERE id = $1`, issueID)
	if got := getStorageUsage(t, workspaceID); got.UsedBytes == nil || *got.UsedBytes != 100 {
		t.Fatalf("after delete = %+v, want 100", got)
	}
}

// Creating a sub-issue from a comment copies the source attachments. On a
// full workspace it is refused before the sub-issue or any copy exists, and
// the source attachments stay as they were.
func TestCommentSubIssueRefusedWhenCopyWouldExceedStorage(t *testing.T) {
	workspaceID, _ := storageLimitWorkspace(t, 900)
	store := useMockStorage(t)
	fx := testutil.New(testPool, workspaceID, testUserID)
	sourceIssueID := fx.Issue(t, "Source issue")
	commentID := fx.Comment(t, sourceIssueID, "selected context")
	key := "workspaces/" + workspaceID + "/source.txt"
	store.put(key, []byte(strings.Repeat("s", 50)))
	fx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": workspaceID, "issue_id": sourceIssueID, "comment_id": commentID,
		"uploader_type": "member", "uploader_id": testUserID,
		"filename": "source.txt", "url": store.ObjectURL(key), "content_type": "text/plain", "size_bytes": 50,
	})
	dbfx.Cleanup(t, `DELETE FROM issue_source_context WHERE workspace_id = $1`, workspaceID)
	dbfx.Cleanup(t, `DELETE FROM issue_source_context_object_intent WHERE workspace_id = $1`, workspaceID)

	previewReq := newRequest(http.MethodGet, "/api/comments/"+commentID+"/sub-issue-preview", nil)
	previewReq.Header.Set("X-Workspace-ID", workspaceID)
	var preview sourceContextPreviewResponse
	testutil.Call(t, testHandler.PreviewCommentSubIssue, withURLParam(previewReq, "commentId", commentID)).Want(http.StatusOK).JSON(&preview)

	// 950 used; copying 50 more passes a limit of 1000 but not 990.
	useStorageLimit(t, workspaceID, entitlement.ActionEnforce, 990)
	create := func() *testutil.Response {
		req := newRequest(http.MethodPost, "/api/comments/"+commentID+"/sub-issues", map[string]any{
			"mode": "manual", "capture_token": preview.CaptureToken,
			"issue": map[string]any{"title": "Follow-up", "status": "todo", "priority": "none"},
		})
		req.Header.Set("X-Workspace-ID", workspaceID)
		return testutil.Call(t, testHandler.CreateCommentSubIssue, withURLParam(req, "commentId", commentID))
	}
	wantStorageLimitReached(t, create(), 950, 990)
	if issues := dbfx.Count(t, `SELECT count(*) FROM issue WHERE workspace_id = $1`, workspaceID); issues != 1 {
		t.Fatalf("issues = %d, want only the source", issues)
	}
	if copies := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE workspace_id = $1 AND source_context_id IS NOT NULL`, workspaceID); copies != 0 {
		t.Fatalf("copied attachments = %d, want none", copies)
	}
	if _, uploads := store.streamCopyCalls(); uploads != 0 {
		t.Fatalf("stream uploads = %d, want none", uploads)
	}
	if got := storageOverageOf(t, workspaceID); got.enforced != 1 || got.peak != 1000 {
		t.Fatalf("overage = %+v, want one refusal at 1000", got)
	}

	useStorageLimit(t, workspaceID, entitlement.ActionEnforce, 1000)
	var created IssueResponse
	create().Want(http.StatusCreated).JSON(&created)
	if created.ID == "" {
		t.Fatal("sub-issue was not created within the limit")
	}
}

func TestStorageLimitResponseShape(t *testing.T) {
	raw, err := json.Marshal(StorageUsageResponse{Action: "off"})
	if err != nil || string(raw) != `{"action":"off"}` {
		t.Fatalf("off usage = %s, err = %v", raw, err)
	}
}

// A guest cannot read the workspace's storage usage, so a refusal tells it the
// storage is full without the figures.
func TestUploadFileRefusesAGuestWithoutTheFigures(t *testing.T) {
	workspaceID, _ := storageLimitWorkspace(t, 900)
	useStorageLimit(t, workspaceID, entitlement.ActionEnforce, 1000)
	useMockStorage(t)
	guestID := dbfx.User(t, "Storage guest "+uuid.NewString()[:8], "storage-guest-"+uuid.NewString()[:8]+"@multica.ai")
	dbfx.Member(t, workspaceID, guestID, "guest")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "guest.png")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(bytes.Repeat([]byte("x"), 200))
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", guestID)
	req.Header.Set("X-Workspace-ID", workspaceID)

	got := testutil.Call(t, testHandler.UploadFile, req).Want(http.StatusForbidden).Map()
	if got["code"] != storageLimitReachedCode {
		t.Fatalf("body = %v, want %s", got, storageLimitReachedCode)
	}
	if _, ok := got["used_bytes"]; ok {
		t.Fatalf("guest refusal carries used_bytes: %v", got)
	}
	if _, ok := got["limit_bytes"]; ok {
		t.Fatalf("guest refusal carries limit_bytes: %v", got)
	}
}
