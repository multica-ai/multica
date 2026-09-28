package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type supportCommitUnknownStarter struct{ txStarter }

func (s supportCommitUnknownStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return supportCommitUnknownTx{Tx: tx}, nil
}

type supportCommitUnknownTx struct{ pgx.Tx }

func (tx supportCommitUnknownTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	return errors.New("response lost after successful commit")
}

type supportPauseIssueStarter struct {
	txStarter
	reached chan struct{}
	release chan struct{}
}

func (s supportPauseIssueStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return supportPauseIssueTx{Tx: tx, reached: s.reached, release: s.release}, nil
}

type supportPauseIssueTx struct {
	pgx.Tx
	reached chan struct{}
	release chan struct{}
}

func (tx supportPauseIssueTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "SELECT 1 FROM issue WHERE id =") {
		close(tx.reached)
		select {
		case <-tx.release:
		case <-ctx.Done():
		}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func supportTestRequest(method, issueID string, body any) *http.Request {
	return withURLParam(newRequest(method, "/api/issues/"+issueID+"/support-dispatch/claim", body), "id", issueID)
}

func supportReadbackRequest(workspaceID, issueID string) *http.Request {
	r := chi.NewRouteContext()
	r.URLParams.Add("id", workspaceID)
	r.URLParams.Add("issueID", issueID)
	req := newRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/support-dispatch/claims/"+issueID, nil)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, r))
}

func supportTestClaim(t *testing.T, issueID, commentID, content string) map[string]any {
	t.Helper()
	var revision int64
	if err := testPool.QueryRow(context.Background(), `SELECT revision FROM issue WHERE id = $1`, issueID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(content))
	return map[string]any{"expected_revision": revision, "staff_comment_id": commentID,
		"staff_comment_revision": 1, "attestation_digest": hex.EncodeToString(digest[:])}
}

func supportTestHandler() *Handler {
	h := *testHandler
	h.SupportDispatchClaimEnabled = true
	return &h
}

func TestSupportClaimDisabledAndMalformed(t *testing.T) {
	issueID := dbfx.Issue(t, "disabled support claim")
	testutil.Call(t, testHandler.ClaimSupportDispatch, supportTestRequest(http.MethodPut, issueID, nil)).Want(http.StatusNotFound)
	h := supportTestHandler()
	for _, body := range []any{`{`, map[string]any{"expected_revision": 1},
		map[string]any{"expected_revision": 1, "staff_comment_id": issueID,
			"staff_comment_revision": 1, "attestation_digest": "abc", "unexpected": true}} {
		testutil.Call(t, h.ClaimSupportDispatch, supportTestRequest(http.MethodPut, issueID, body)).Want(http.StatusBadRequest)
	}
}

func TestSupportClaimExactEvidenceAndOnceEver(t *testing.T) {
	issueID := dbfx.Issue(t, "support claim", testutil.Cols{"assignee_type": "member", "assignee_id": testUserID})
	comment := dbfx.Comment(t, issueID, `{"kind":"test","label":"Search"}`)
	content := `{"kind":"test","label":"Search"}`
	h := supportTestHandler()
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM support_dispatch_claim WHERE issue_id = $1`, issueID)
	})
	request := supportTestClaim(t, issueID, comment, content)
	var response struct {
		Claim    supportClaim    `json:"claim"`
		Evidence supportSnapshot `json:"evidence"`
	}
	testutil.Call(t, h.ClaimSupportDispatch, supportTestRequest(http.MethodPut, issueID, request)).Want(http.StatusCreated).JSON(&response)
	if response.Claim.IssueID != issueID || response.Claim.StaffCommentID != comment ||
		response.Evidence.Comments.Total != 1 || !response.Evidence.Comments.Complete ||
		response.Evidence.Comments.Truncated || len(response.Evidence.Members.Rows) == 0 {
		t.Fatalf("claim or evidence mismatch: %#v", response)
	}
	// An identical retry is NOT a new dispatch grant, including after issue revision changes.
	testutil.Call(t, h.ClaimSupportDispatch, supportTestRequest(http.MethodPut, issueID, request)).Want(http.StatusConflict)
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET revision = revision + 1 WHERE id = $1`, issueID); err != nil {
		t.Fatal(err)
	}
	testutil.Call(t, h.ClaimSupportDispatch, supportTestRequest(http.MethodPut, issueID, request)).Want(http.StatusConflict)
	var stored supportClaim
	testutil.Call(t, h.GetSupportDispatchClaim, supportReadbackRequest(testWorkspaceID, issueID)).Want(http.StatusOK).JSON(&stored)
	if stored.IssueRevision != response.Claim.IssueRevision || stored.AttestationDigest != response.Claim.AttestationDigest {
		t.Fatalf("readback changed the original claim: %#v", stored)
	}
}

func TestSupportClaimRejectsStaleAndForeignEvidence(t *testing.T) {
	issueID := dbfx.Issue(t, "support evidence rejection", testutil.Cols{"assignee_type": "member", "assignee_id": testUserID})
	otherIssueID := dbfx.Issue(t, "another issue", testutil.Cols{"assignee_type": "member", "assignee_id": testUserID})
	comment := dbfx.Comment(t, issueID, "original")
	foreign := dbfx.Comment(t, otherIssueID, "original")
	h := supportTestHandler()
	base := supportTestClaim(t, issueID, comment, "original")
	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"wrong digest", func(m map[string]any) { m["attestation_digest"] = hex.EncodeToString(sha256.New().Sum(nil)) }},
		{"foreign comment", func(m map[string]any) { m["staff_comment_id"] = foreign }},
		{"wrong revision", func(m map[string]any) { m["expected_revision"] = int64(7) }},
		{"wrong comment revision", func(m map[string]any) { m["staff_comment_revision"] = 2 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := make(map[string]any, len(base))
			for key, value := range base {
				request[key] = value
			}
			tc.change(request)
			testutil.Call(t, h.ClaimSupportDispatch, supportTestRequest(http.MethodPut, issueID, request)).Want(http.StatusConflict)
		})
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE comment SET content = 'altered', revision = revision + 1 WHERE id = $1`, comment); err != nil {
		t.Fatal(err)
	}
	testutil.Call(t, h.ClaimSupportDispatch, supportTestRequest(http.MethodPut, issueID, base)).Want(http.StatusConflict)
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM support_dispatch_claim WHERE issue_id = $1`, issueID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected claims wrote row: count %d, error %v", count, err)
	}
}

func TestSupportClaimRequiresCurrentMembershipAndAdmin(t *testing.T) {
	issueID := dbfx.Issue(t, "claim access", testutil.Cols{"assignee_type": "member", "assignee_id": testUserID})
	commentID := dbfx.Comment(t, issueID, "attestation")
	h := supportTestHandler()
	request := supportTestClaim(t, issueID, commentID, "attestation")
	otherID := dbfx.Insert(t, "user", testutil.Cols{"name": "Other member", "email": "other-support-test@example.invalid"})
	other := func(method string) *http.Request {
		req := supportTestRequest(method, issueID, request)
		req.Header.Set("X-User-ID", otherID)
		return req
	}
	testutil.Call(t, h.ClaimSupportDispatch, other(http.MethodPut)).Want(http.StatusNotFound)
	dbfx.Insert(t, "member", testutil.Cols{"workspace_id": testWorkspaceID, "user_id": otherID, "role": "member"})
	testutil.Call(t, h.GetSupportDispatchEvidence, other(http.MethodGet)).Want(http.StatusOK)
	testutil.Call(t, h.ClaimSupportDispatch, other(http.MethodPut)).Want(http.StatusForbidden)
	readback := supportReadbackRequest(testWorkspaceID, issueID)
	readback.Header.Set("X-User-ID", otherID)
	testutil.Call(t, h.GetSupportDispatchClaim, readback).Want(http.StatusForbidden)
	wrongWorkspace := supportTestRequest(http.MethodPut, issueID, request)
	wrongWorkspace.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000005")
	testutil.Call(t, h.ClaimSupportDispatch, wrongWorkspace).Want(http.StatusNotFound)
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM support_dispatch_claim WHERE issue_id = $1`, issueID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unauthorized claim wrote row: count %d, error %v", count, err)
	}
}

func TestSupportEvidenceCapacityFailsClosed(t *testing.T) {
	issueID := dbfx.Issue(t, "full evidence boundary", testutil.Cols{"assignee_type": "member", "assignee_id": testUserID})
	comment := dbfx.Comment(t, issueID, "attestation")
	h := supportTestHandler()
	ctx := context.Background()
	_, err := testPool.Exec(ctx, `INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		SELECT $1, $2, 'member', $3, 'unrelated', 'comment' FROM generate_series(1, $4)`,
		issueID, testWorkspaceID, testUserID, supportEvidenceLimit-1)
	if err != nil {
		t.Fatal(err)
	}
	var evidence supportSnapshot
	testutil.Call(t, h.GetSupportDispatchEvidence, supportTestRequest(http.MethodGet, issueID, nil)).Want(http.StatusOK).JSON(&evidence)
	if evidence.Comments.Total != supportEvidenceLimit || !evidence.Comments.Complete {
		t.Fatalf("exact-cap evidence incomplete: total %d", evidence.Comments.Total)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, 'overflow', 'comment')`, issueID, testWorkspaceID, testUserID); err != nil {
		t.Fatal(err)
	}
	testutil.Call(t, h.GetSupportDispatchEvidence, supportTestRequest(http.MethodGet, issueID, nil)).Want(http.StatusConflict)
	testutil.Call(t, h.ClaimSupportDispatch, supportTestRequest(http.MethodPut, issueID, supportTestClaim(t, issueID, comment, "attestation"))).Want(http.StatusConflict)
	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM support_dispatch_claim WHERE issue_id = $1`, issueID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("overflow reserved claim: count %d, error %v", count, err)
	}
}

func TestSupportClaimConcurrentSingleWinner(t *testing.T) {
	issueID := dbfx.Issue(t, "concurrent claim", testutil.Cols{"assignee_type": "member", "assignee_id": testUserID})
	comment := dbfx.Comment(t, issueID, "attestation")
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM support_dispatch_claim WHERE issue_id = $1`, issueID)
	})
	request := supportTestClaim(t, issueID, comment, "attestation")
	h := supportTestHandler()
	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := httptest.NewRecorder()
			h.ClaimSupportDispatch(w, supportTestRequest(http.MethodPut, issueID, request))
			statuses[i] = w.Code
		}(i)
	}
	wg.Wait()
	if !((statuses[0] == http.StatusCreated && statuses[1] == http.StatusConflict) ||
		(statuses[1] == http.StatusCreated && statuses[0] == http.StatusConflict)) {
		t.Fatalf("concurrent claim statuses = %v, want one 201 and one 409", statuses)
	}
}

func TestSupportClaimAmbiguousCommitRequiresReadback(t *testing.T) {
	issueID := dbfx.Issue(t, "unknown outcome", testutil.Cols{"assignee_type": "member", "assignee_id": testUserID})
	commentID := dbfx.Comment(t, issueID, "attestation")
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM support_dispatch_claim WHERE issue_id = $1`, issueID)
	})
	h := supportTestHandler()
	h.TxStarter = supportCommitUnknownStarter{h.TxStarter}
	request := supportTestClaim(t, issueID, commentID, "attestation")
	testutil.Call(t, h.ClaimSupportDispatch, supportTestRequest(http.MethodPut, issueID, request)).Want(http.StatusConflict)
	// Use the real transaction starter for read-back; neither the lost response
	// nor an identical PUT is allowed to imply a new invocation.
	h.TxStarter = testPool
	var claim supportClaim
	testutil.Call(t, h.GetSupportDispatchClaim, supportReadbackRequest(testWorkspaceID, issueID)).Want(http.StatusOK).JSON(&claim)
	if claim.IssueID != issueID || claim.StaffCommentID != commentID {
		t.Fatalf("ambiguous commit readback: %#v", claim)
	}
	testutil.Call(t, h.ClaimSupportDispatch, supportTestRequest(http.MethodPut, issueID, request)).Want(http.StatusConflict)
}

func TestSupportClaimReadbackSurvivesIssueDeletionAndEnforcesWorkspace(t *testing.T) {
	issueID := dbfx.Issue(t, "historical claim", testutil.Cols{"assignee_type": "member", "assignee_id": testUserID})
	commentID := dbfx.Comment(t, issueID, "attestation")
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM support_dispatch_claim WHERE issue_id = $1`, issueID)
	})
	h := supportTestHandler()
	request := supportTestClaim(t, issueID, commentID, "attestation")
	testutil.Call(t, h.ClaimSupportDispatch, supportTestRequest(http.MethodPut, issueID, request)).Want(http.StatusCreated)
	testutil.Call(t, h.GetSupportDispatchClaim, supportReadbackRequest(testWorkspaceID, uuid.NewString())).Want(http.StatusNotFound)
	testutil.Call(t, h.GetSupportDispatchClaim, supportReadbackRequest(uuid.NewString(), issueID)).Want(http.StatusNotFound)
	testutil.Call(t, h.GetSupportDispatchClaim, supportReadbackRequest(testWorkspaceID, "not-a-uuid")).Want(http.StatusBadRequest)

	// The real issue-delete path removes comments too. A committed claim must
	// remain readable without either row, including after a lost POST response.
	deleteRecorder := httptest.NewRecorder()
	h.DeleteIssue(deleteRecorder, supportTestRequest(http.MethodDelete, issueID, nil))
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete issue: %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	var stored supportClaim
	testutil.Call(t, h.GetSupportDispatchClaim, supportReadbackRequest(testWorkspaceID, issueID)).Want(http.StatusOK).JSON(&stored)
	if stored.IssueID != issueID || stored.StaffCommentID != commentID {
		t.Fatalf("historical claim changed: %#v", stored)
	}
	// No issue is left to retry against, and read-back is never a new grant.
	testutil.Call(t, h.ClaimSupportDispatch, supportTestRequest(http.MethodPut, issueID, request)).Want(http.StatusNotFound)

	userID := uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `INSERT INTO "user" (id, name, email) VALUES ($1, 'Support reader', $2)`,
		userID, userID+"@example.invalid"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })
	visitor := supportReadbackRequest(testWorkspaceID, issueID)
	visitor.Header.Set("X-User-ID", userID)
	testutil.Call(t, h.GetSupportDispatchClaim, visitor).Want(http.StatusNotFound)
	if _, err := testPool.Exec(context.Background(), `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, userID); err != nil {
		t.Fatal(err)
	}
	visitor = supportReadbackRequest(testWorkspaceID, issueID)
	visitor.Header.Set("X-User-ID", userID)
	testutil.Call(t, h.GetSupportDispatchClaim, visitor).Want(http.StatusForbidden)
}

func TestSupportClaimReadbackRouteRoleAndActor(t *testing.T) {
	h := supportTestHandler()
	issueID := uuid.NewString()
	router := chi.NewRouter()
	router.With(RequireHumanActor, middleware.RequireWorkspaceRoleFromURL(h.Queries, "id", "owner", "admin")).Get(
		"/api/workspaces/{id}/support-dispatch/claims/{issueID}", h.GetSupportDispatchClaim)
	path := "/api/workspaces/" + testWorkspaceID + "/support-dispatch/claims/" + issueID
	for _, tc := range []struct {
		name   string
		userID string
		actor  string
		want   int
	}{
		{"missing authentication", "", "", http.StatusUnauthorized},
		{"machine actor", testUserID, "task_token", http.StatusForbidden},
		{"cloud actor", testUserID, "cloud_pat", http.StatusForbidden},
		{"workspace owner sees absent claim", testUserID, "", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := newRequest(http.MethodGet, path, nil)
			req.Header.Set("X-User-ID", tc.userID)
			if tc.actor != "" {
				req.Header.Set("X-Actor-Source", tc.actor)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("route status %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestSupportClaimIssueChangesAfterSnapshotBeforeLock(t *testing.T) {
	issueID := dbfx.Issue(t, "snapshot race", testutil.Cols{"assignee_type": "member", "assignee_id": testUserID})
	commentID := dbfx.Comment(t, issueID, "attestation")
	h := supportTestHandler()
	reached, release := make(chan struct{}), make(chan struct{})
	h.TxStarter = supportPauseIssueStarter{txStarter: testPool, reached: reached, release: release}
	request := supportTestClaim(t, issueID, commentID, "attestation")
	status := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		h.ClaimSupportDispatch(w, supportTestRequest(http.MethodPut, issueID, request))
		status <- w.Code
	}()
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("claim did not reach issue lock")
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET revision = revision + 1 WHERE id = $1`, issueID); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	select {
	case got := <-status:
		if got != http.StatusConflict {
			t.Fatalf("revision drift returned %d, want 409", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("claim did not settle after revision change")
	}
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM support_dispatch_claim WHERE issue_id = $1`, issueID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("race reserved claim: count %d, error %v", count, err)
	}
}

func TestSupportClaimCommentChangesWithoutIssueBump(t *testing.T) {
	issueID := dbfx.Issue(t, "comment snapshot race", testutil.Cols{"assignee_type": "member", "assignee_id": testUserID})
	commentID := dbfx.Comment(t, issueID, "attestation")
	h := supportTestHandler()
	reached, release := make(chan struct{}), make(chan struct{})
	h.TxStarter = supportPauseIssueStarter{txStarter: testPool, reached: reached, release: release}
	request := supportTestClaim(t, issueID, commentID, "attestation")
	status := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		h.ClaimSupportDispatch(w, supportTestRequest(http.MethodPut, issueID, request))
		status <- w.Code
	}()
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("claim did not reach issue lock")
	}
	// Bypass the ordinary comment handler's parent revision bump to prove that
	// the attestation row lock itself detects a conflicting concurrent edit.
	if _, err := testPool.Exec(context.Background(), `UPDATE comment SET content = 'different', revision = revision + 1 WHERE id = $1`, commentID); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	select {
	case got := <-status:
		if got != http.StatusConflict {
			t.Fatalf("comment drift returned %d, want 409", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("claim did not settle after comment change")
	}
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM support_dispatch_claim WHERE issue_id = $1`, issueID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale comment reserved claim: count %d, error %v", count, err)
	}
}

func TestSupportClaimBlocksWorkspaceTeardown(t *testing.T) {
	ctx := context.Background()
	workspaceID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name": "Support claim workspace", "slug": "support-claim-" + uuid.NewString(), "issue_prefix": "SUP",
	})
	dbfx.Insert(t, "member", testutil.Cols{"workspace_id": workspaceID, "user_id": testUserID, "role": "owner"})
	issueID := dbfx.Insert(t, "issue", testutil.Cols{
		"workspace_id": workspaceID, "number": 1, "title": "claimed issue",
		"creator_type": "member", "creator_id": testUserID,
	})
	_, err := testPool.Exec(ctx, `INSERT INTO support_dispatch_claim (
		workspace_id, issue_id, issue_revision, staff_comment_id, staff_comment_revision,
		attestation_digest, claimed_by_user_id) VALUES ($1, $2, 1, $3, 1, $4, $5)`,
		workspaceID, issueID, uuid.NewString(), strings.Repeat("0", 64), testUserID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM support_dispatch_claim WHERE issue_id = $1`, issueID)
	})
	req := withURLParam(newRequest(http.MethodDelete, "/api/workspaces/"+workspaceID, nil), "id", workspaceID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusConflict)
	var remaining int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM support_dispatch_claim WHERE issue_id = $1`, issueID).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("teardown lost claim: remaining %d, error %v", remaining, err)
	}
}
