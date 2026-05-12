package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ----------------------------- HMAC -----------------------------

func TestVerifySignatureValid(t *testing.T) {
	secret := "topsecret"
	body := []byte(`{"hello":"world"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	header := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !verifySignature(header, secret, body) {
		t.Fatalf("expected valid signature to verify")
	}
}

func TestVerifySignatureWrongSecret(t *testing.T) {
	body := []byte(`{}`)
	mac := hmac.New(sha256.New, []byte("a"))
	mac.Write(body)
	header := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if verifySignature(header, "b", body) {
		t.Fatalf("expected wrong secret to fail")
	}
}

func TestVerifySignatureMissingPrefix(t *testing.T) {
	if verifySignature("deadbeef", "x", []byte("a")) {
		t.Fatalf("expected missing prefix to fail")
	}
}

func TestVerifySignatureBadHex(t *testing.T) {
	if verifySignature("sha256=zzz", "x", []byte("a")) {
		t.Fatalf("expected bad hex to fail")
	}
}

// ----------------------------- HTTP early-exit paths -----------------------------

func TestServeHTTPIgnoresUnknownEvent(t *testing.T) {
	h := &WebhookHandler{Secret: ""}
	body := `{"action":"created"}`
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-GitHub-Event", "ping")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"action":"ignored"`) {
		t.Fatalf("body = %s, want ignored action", w.Body.String())
	}
}

func TestServeHTTPRejectsBadSignature(t *testing.T) {
	h := &WebhookHandler{Secret: "topsecret"}
	body := `{"action":"opened"}`
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Delivery", "d2")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestServeHTTPRejectsMissingHeaders(t *testing.T) {
	h := &WebhookHandler{Secret: ""}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestServeHTTPRejectsBadPayload(t *testing.T) {
	h := &WebhookHandler{Secret: ""}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", strings.NewReader(`not json`))
	req.Header.Set("X-GitHub-Delivery", "d3")
	req.Header.Set("X-GitHub-Event", "pull_request")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// ----------------------------- splitRepo -----------------------------

func TestSplitRepoOK(t *testing.T) {
	o, r, ok := splitRepo("zeyad-farrag/multica")
	if !ok || o != "zeyad-farrag" || r != "multica" {
		t.Fatalf("split = %s, %s, %v", o, r, ok)
	}
}

func TestSplitRepoInvalid(t *testing.T) {
	if _, _, ok := splitRepo("noslash"); ok {
		t.Fatalf("expected invalid")
	}
	if _, _, ok := splitRepo("/onlyright"); ok {
		t.Fatalf("expected invalid")
	}
	if _, _, ok := splitRepo("onlyleft/"); ok {
		t.Fatalf("expected invalid")
	}
}

// ----------------------------- Predicate -----------------------------

type fakePredicateClient struct {
	reviews               []Review
	threads               []ReviewThread
	reviewComments        map[int64][]ReviewComment // keyed by review ID
	prs                   []PullRequest
	listReviewsCalls      int
	listReviewThreadCalls int
	listCommentsCalls     int
	listPRsForCommitCalls int
	listReviewsErr        error
	listCommentsErr       error
}

func (f *fakePredicateClient) ListReviews(_ context.Context, _, _ string, _ int) ([]Review, error) {
	f.listReviewsCalls++
	if f.listReviewsErr != nil {
		return nil, f.listReviewsErr
	}
	return f.reviews, nil
}
func (f *fakePredicateClient) ListReviewThreads(_ context.Context, _, _ string, _ int) ([]ReviewThread, error) {
	f.listReviewThreadCalls++
	return f.threads, nil
}
func (f *fakePredicateClient) ListReviewComments(_ context.Context, _, _ string, _ int, reviewID int64) ([]ReviewComment, error) {
	f.listCommentsCalls++
	if f.listCommentsErr != nil {
		return nil, f.listCommentsErr
	}
	return f.reviewComments[reviewID], nil
}
func (f *fakePredicateClient) ListPRsForCommit(_ context.Context, _, _, _ string) ([]PullRequest, error) {
	f.listPRsForCommitCalls++
	return f.prs, nil
}

func newReview(login, state string) Review {
	r := Review{State: state}
	r.User.Login = login
	return r
}

func TestPredicateAllClean(t *testing.T) {
	c := fakePredicateClient{
		reviews: []Review{newReview("coderabbitai[bot]", "APPROVED")},
		threads: []ReviewThread{{IsResolved: true, Author: "coderabbitai[bot]"}},
	}
	noOpen, noUnresolved, err := EvaluatePredicate(context.Background(), &c, "o", "r", 1, "coderabbitai[bot]")
	if err != nil {
		t.Fatal(err)
	}
	if !noOpen || !noUnresolved {
		t.Fatalf("expected both true, got %v %v", noOpen, noUnresolved)
	}
}

func TestPredicateOpenChangesRequest(t *testing.T) {
	c := fakePredicateClient{
		reviews: []Review{newReview("coderabbitai[bot]", "CHANGES_REQUESTED")},
	}
	noOpen, _, _ := EvaluatePredicate(context.Background(), &c, "o", "r", 1, "coderabbitai[bot]")
	if noOpen {
		t.Fatalf("expected NoOpenCRChangesRequest=false")
	}
}

func TestPredicateChangesThenDismissed(t *testing.T) {
	// Latest CR-bot review wins. CHANGES_REQUESTED then DISMISSED → no open.
	c := fakePredicateClient{
		reviews: []Review{
			newReview("coderabbitai[bot]", "CHANGES_REQUESTED"),
			newReview("coderabbitai[bot]", "DISMISSED"),
		},
	}
	noOpen, _, _ := EvaluatePredicate(context.Background(), &c, "o", "r", 1, "coderabbitai[bot]")
	if !noOpen {
		t.Fatalf("expected NoOpenCRChangesRequest=true (latest is DISMISSED)")
	}
}

func TestPredicateUnresolvedThread(t *testing.T) {
	c := fakePredicateClient{
		threads: []ReviewThread{{IsResolved: false, Author: "coderabbitai[bot]"}},
	}
	_, noUnresolved, _ := EvaluatePredicate(context.Background(), &c, "o", "r", 1, "coderabbitai[bot]")
	if noUnresolved {
		t.Fatalf("expected NoUnresolvedCRThreads=false")
	}
}

func TestPredicateIgnoresHumanReviews(t *testing.T) {
	c := fakePredicateClient{
		reviews: []Review{newReview("alice", "CHANGES_REQUESTED")},
		threads: []ReviewThread{{IsResolved: false, Author: "alice"}},
	}
	noOpen, noUnresolved, _ := EvaluatePredicate(context.Background(), &c, "o", "r", 1, "coderabbitai[bot]")
	if !noOpen || !noUnresolved {
		t.Fatalf("expected both true (human-only), got %v %v", noOpen, noUnresolved)
	}
}

func TestHandleReviewComment_NoStatusFlip_v2(t *testing.T) {
	fixture := newWebhookHandlerFixture(StatusCoderabbit)
	client := &fakePredicateClient{reviews: []Review{newReview("coderabbitai[bot]", "CHANGES_REQUESTED")}}
	fixture.handler.NewClient = func(int64) PRReviewClient { return client }

	res, err := fixture.handler.handleReviewComment(context.Background(), reviewCommentPayloadMap("created", 501, "coderabbitai[bot]"), fixture.binding)
	if err != nil {
		t.Fatal(err)
	}
	if res.label != "review_comment_recorded" {
		t.Fatalf("label = %q, want review_comment_recorded", res.label)
	}
	if fixture.store.updatedStatus != "" {
		t.Fatalf("status updated to %q; v2 review comments must be silent mirrors", fixture.store.updatedStatus)
	}
	if fixture.store.upsertReviewThreadCalls != 1 {
		t.Fatalf("upsert review thread calls = %d, want 1", fixture.store.upsertReviewThreadCalls)
	}
	if client.listReviewsCalls != 0 || client.listReviewThreadCalls != 0 {
		t.Fatalf("predicate was called under v2: reviews=%d threads=%d", client.listReviewsCalls, client.listReviewThreadCalls)
	}
}

func TestHandleReviewComment_NoStatusFlip(t *testing.T) {
	fixture := newWebhookHandlerFixture(StatusCoderabbit)
	fixture.store.unresolvedCount = 1
	client := &fakePredicateClient{reviews: []Review{newReview("coderabbitai[bot]", "COMMENTED")}}
	fixture.handler.NewClient = func(int64) PRReviewClient { return client }

	res, err := fixture.handler.handleReviewComment(context.Background(), reviewCommentPayloadMap("created", 501, "coderabbitai[bot]"), fixture.binding)
	if err != nil {
		t.Fatal(err)
	}
	if res.label != "review_comment_recorded" {
		t.Fatalf("label = %q, want review_comment_recorded", res.label)
	}
	if fixture.store.updatedStatus != "" {
		t.Fatalf("status updated to %q; review comments must be silent mirrors", fixture.store.updatedStatus)
	}
	if client.listReviewsCalls != 0 || client.listReviewThreadCalls != 0 {
		t.Fatalf("predicate was called: reviews=%d threads=%d", client.listReviewsCalls, client.listReviewThreadCalls)
	}
}

func TestHandleReview_FromInReview_NoOp_v2(t *testing.T) {
	fixture := newWebhookHandlerFixture(StatusInReview)
	client := &fakePredicateClient{listCommentsErr: errors.New("github unavailable")}
	fixture.handler.NewClient = func(int64) PRReviewClient { return client }

	res, err := fixture.handler.handleReview(context.Background(), reviewPayloadMap("submitted", "CHANGES_REQUESTED", 77, "coderabbitai[bot]"), fixture.binding)
	if err != nil {
		t.Fatal(err)
	}
	if res.label != "v2_silent_mirror_review" {
		t.Fatalf("label = %q, want v2_silent_mirror_review", res.label)
	}
	if fixture.store.updatedStatus != "" {
		t.Fatalf("status updated to %q; v2 in_review wrapping reviews must noop", fixture.store.updatedStatus)
	}
	if client.listCommentsCalls != 0 || client.listReviewsCalls != 0 {
		t.Fatalf("v2 in_review review should not bulk mirror or predicate; comments=%d reviews=%d", client.listCommentsCalls, client.listReviewsCalls)
	}
}

func TestHandleReview_BulkMirrorFails_v2_FailsClosed(t *testing.T) {
	fixture := newWebhookHandlerFixture(StatusCoderabbit)
	client := &fakePredicateClient{listCommentsErr: errors.New("github unavailable")}
	fixture.handler.NewClient = func(int64) PRReviewClient { return client }

	res, err := fixture.handler.handleReview(context.Background(), reviewPayloadMap("submitted", "CHANGES_REQUESTED", 77, "coderabbitai[bot]"), fixture.binding)
	if err != nil {
		t.Fatal(err)
	}
	if res.label != "failed_closed_to_blocked" {
		t.Fatalf("label = %q, want failed_closed_to_blocked", res.label)
	}
	if fixture.store.updatedStatus != StatusBlocked {
		t.Fatalf("status = %q, want %q", fixture.store.updatedStatus, StatusBlocked)
	}
	if fixture.store.closedAttemptOutcome != "failed" {
		t.Fatalf("closed attempt outcome = %q, want failed", fixture.store.closedAttemptOutcome)
	}
	if !strings.Contains(fixture.store.createdCommentContent, "<!-- sidecar-block -->") {
		t.Fatalf("fail-closed comment missing sidecar marker: %q", fixture.store.createdCommentContent)
	}
	if fixture.store.createdActivityAction != "review_blocked" {
		t.Fatalf("activity action = %q, want review_blocked", fixture.store.createdActivityAction)
	}
}

func TestHandleReview_BulkMirrorFails_v2_AttemptAlreadyClosedNoops(t *testing.T) {
	fixture := newWebhookHandlerFixture(StatusCoderabbit)
	fixture.store.closeAttemptErr = pgx.ErrNoRows
	client := &fakePredicateClient{listCommentsErr: errors.New("github unavailable")}
	fixture.handler.NewClient = func(int64) PRReviewClient { return client }

	res, err := fixture.handler.handleReview(context.Background(), reviewPayloadMap("submitted", "CHANGES_REQUESTED", 77, "coderabbitai[bot]"), fixture.binding)
	if err != nil {
		t.Fatal(err)
	}
	if res.label != "fail_closed_attempt_already_closed" {
		t.Fatalf("label = %q, want fail_closed_attempt_already_closed", res.label)
	}
	if fixture.store.updatedStatus != "" {
		t.Fatalf("status updated to %q, want no update", fixture.store.updatedStatus)
	}
	if fixture.store.createdCommentContent != "" {
		t.Fatalf("comment content = %q, want none", fixture.store.createdCommentContent)
	}
	if fixture.store.createdActivityAction != "" {
		t.Fatalf("activity = %q, want none", fixture.store.createdActivityAction)
	}
}

func TestHandleCheckRun_Failure_RoutesBlocked_v2(t *testing.T) {
	fixture := newWebhookHandlerFixture(StatusCoderabbit)
	res, err := fixture.handler.handleCheckRun(context.Background(), checkRunPayloadMap("completed", "failure", "lint failed", []int32{7}, "abc123", "coderabbitai"), fixture.binding)
	if err != nil {
		t.Fatal(err)
	}
	if res.label != "failed_closed_to_blocked" {
		t.Fatalf("label = %q, want failed_closed_to_blocked", res.label)
	}
	if fixture.store.updatedStatus != StatusBlocked {
		t.Fatalf("status = %q, want %q", fixture.store.updatedStatus, StatusBlocked)
	}
	if fixture.store.closedAttemptOutcome != "failed" {
		t.Fatalf("closed outcome = %q, want failed", fixture.store.closedAttemptOutcome)
	}
	if got := fixture.store.signalKinds; !reflect.DeepEqual(got, []string{"check_run"}) {
		t.Fatalf("signals = %#v, want one check_run", got)
	}
	if !strings.Contains(fixture.store.createdCommentContent, "<!-- sidecar-block -->") {
		t.Fatalf("missing sidecar-block comment: %q", fixture.store.createdCommentContent)
	}
}

func TestHandleCheckRun_Skipped_RoutesStaged_v2(t *testing.T) {
	fixture := newWebhookHandlerFixture(StatusCoderabbit)
	res, err := fixture.handler.handleCheckRun(context.Background(), checkRunPayloadMap("completed", "skipped", "", []int32{7}, "abc123", "coderabbitai"), fixture.binding)
	if err != nil {
		t.Fatal(err)
	}
	if res.label != "check_run_skipped_to_staged" {
		t.Fatalf("label = %q, want check_run_skipped_to_staged", res.label)
	}
	if fixture.store.updatedStatus != StatusStaged {
		t.Fatalf("status = %q, want %q", fixture.store.updatedStatus, StatusStaged)
	}
	if fixture.store.closedAttemptOutcome != "skipped" {
		t.Fatalf("closed outcome = %q, want skipped", fixture.store.closedAttemptOutcome)
	}
	if !strings.Contains(fixture.store.createdCommentContent, "<!-- sidecar-cr-attempt -->") {
		t.Fatalf("missing cr attempt audit comment: %q", fixture.store.createdCommentContent)
	}
}

func TestHandleCheckRun_NonCRNoSignal_v2(t *testing.T) {
	fixture := newWebhookHandlerFixture(StatusCoderabbit)
	res, err := fixture.handler.handleCheckRun(context.Background(), checkRunPayloadMap("in_progress", "", "", []int32{7}, "abc123", "github-actions"), fixture.binding)
	if err != nil {
		t.Fatal(err)
	}
	if res.label != "noop" || res.fields["reason"] != "non-CR check_run" {
		t.Fatalf("result = %#v, want non-CR noop", res)
	}
	if len(fixture.store.signalKinds) != 0 {
		t.Fatalf("signals = %#v, want none", fixture.store.signalKinds)
	}
}

func TestHandleCheckRun_EmptyPullRequests_MapsOpenPRForCommit_v2(t *testing.T) {
	fixture := newWebhookHandlerFixture(StatusCoderabbit)
	client := &fakePredicateClient{prs: []PullRequest{
		{Number: 6, State: "closed"},
		{Number: 7, State: "open"},
	}}
	fixture.handler.NewClient = func(int64) PRReviewClient { return client }

	res, err := fixture.handler.handleCheckRun(context.Background(), checkRunPayloadMap("in_progress", "", "", nil, "abc123", "coderabbitai"), fixture.binding)
	if err != nil {
		t.Fatal(err)
	}
	if res.label != "check_run_recorded" {
		t.Fatalf("label = %q, want check_run_recorded", res.label)
	}
	if client.listPRsForCommitCalls != 1 {
		t.Fatalf("ListPRsForCommit calls = %d, want 1", client.listPRsForCommitCalls)
	}
	if got := fixture.store.signalKinds; !reflect.DeepEqual(got, []string{"check_run"}) {
		t.Fatalf("signals = %#v, want one check_run", got)
	}
}

func TestHandleIssueComment_CRPRCommentRecordsSignalWithEmptyHeadSHA_v2(t *testing.T) {
	fixture := newWebhookHandlerFixture(StatusCoderabbit)
	fixture.store.existingAttemptHeadSHA = "existing-sha"
	res, err := fixture.handler.handleIssueComment(context.Background(), issueCommentPayloadMap("created", true, 7, "coderabbitai[bot]"), fixture.binding)
	if err != nil {
		t.Fatal(err)
	}
	if res.label != "issue_comment_recorded" {
		t.Fatalf("label = %q, want issue_comment_recorded", res.label)
	}
	if got := fixture.store.signalKinds; !reflect.DeepEqual(got, []string{"issue_comment"}) {
		t.Fatalf("signals = %#v, want one issue_comment", got)
	}
	if fixture.store.upsertAttemptHeadSHA != "" {
		t.Fatalf("upsert head_sha = %q, want empty issue_comment signal head", fixture.store.upsertAttemptHeadSHA)
	}
}

func TestHandleIssueComment_IgnoresTopLevelAndNonCR_v2(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload map[string]json.RawMessage
		reason  string
	}{
		{"top-level", issueCommentPayloadMap("created", false, 7, "coderabbitai[bot]"), "not a PR comment"},
		{"non-cr", issueCommentPayloadMap("created", true, 7, "alice"), "non-CR issue_comment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newWebhookHandlerFixture(StatusCoderabbit)
			res, err := fixture.handler.handleIssueComment(context.Background(), tc.payload, fixture.binding)
			if err != nil {
				t.Fatal(err)
			}
			if res.label != "noop" || res.fields["reason"] != tc.reason {
				t.Fatalf("result = %#v, want noop reason %q", res, tc.reason)
			}
			if len(fixture.store.signalKinds) != 0 {
				t.Fatalf("signals = %#v, want none", fixture.store.signalKinds)
			}
		})
	}
}

func TestReviewThreadAuthoredByCR_UsesLocalMirror_v2(t *testing.T) {
	fixture := newWebhookHandlerFixture(StatusCoderabbit)
	fixture.store.reviewThreadAuthors = map[int64]string{501: "coderabbitai[bot]", 502: "alice"}
	if !reviewThreadAuthoredByCR(context.Background(), fixture.handler.Queries, reviewThreadInfo{Comments: []struct {
		ID int64 `json:"id"`
	}{{ID: 501}}}, "coderabbitai[bot]") {
		t.Fatalf("expected CR-authored local thread to be recognized")
	}
	if reviewThreadAuthoredByCR(context.Background(), fixture.handler.Queries, reviewThreadInfo{Comments: []struct {
		ID int64 `json:"id"`
	}{{ID: 502}}}, "coderabbitai[bot]") {
		t.Fatalf("expected human-authored local thread to be ignored")
	}
}

type webhookHandlerFixture struct {
	handler *WebhookHandler
	store   *fakeWebhookDB
	binding db.WorkspaceRepoBinding
}

func newWebhookHandlerFixture(status string) webhookHandlerFixture {
	workspaceID := testUUID(1)
	issueID := testUUID(2)
	issue := db.Issue{
		ID:          issueID,
		WorkspaceID: workspaceID,
		Title:       "CR Loop",
		Status:      status,
		Priority:    "medium",
		CreatorType: "system",
		Number:      42,
		PrUrl:       pgtype.Text{String: "https://github.com/acme/repo/pull/7", Valid: true},
		PrNumber:    pgtype.Int4{Int32: 7, Valid: true},
		PrRepo:      pgtype.Text{String: "acme/repo", Valid: true},
		PhaseState:  []byte(`{"cr_round":1}`),
	}
	store := &fakeWebhookDB{issue: issue, unresolvedCount: 1}
	handler := &WebhookHandler{
		Queries:   db.New(store),
		TxStarter: fakeTxStarter{store: store},
		Bus:       events.New(),
	}
	return webhookHandlerFixture{
		handler: handler,
		store:   store,
		binding: db.WorkspaceRepoBinding{
			WorkspaceID:    workspaceID,
			RepoFullName:   "acme/repo",
			InstallationID: 123,
			CrBotUsername:  "coderabbitai[bot]",
		},
	}
}

type fakeWebhookDB struct {
	issue                   db.Issue
	unresolvedCount         int64
	upsertReviewThreadCalls int
	updatedStatus           string
	closedAttemptOutcome    string
	createdCommentContent   string
	createdActivityAction   string
	closeAttemptErr         error
	signalKinds             []string
	firstSignalKind         string
	upsertAttemptHeadSHA    string
	existingAttemptHeadSHA  string
	reviewThreadAuthors     map[int64]string
}

func (f *fakeWebhookDB) Exec(_ context.Context, query string, args ...interface{}) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(query, "-- name: RecordCRFirstSignal"):
		if f.firstSignalKind == "" {
			f.firstSignalKind = args[2].(pgtype.Text).String
		}
	case strings.Contains(query, "-- name: InsertCRReviewSignal"):
		f.signalKinds = append(f.signalKinds, args[1].(string))
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *fakeWebhookDB) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (f *fakeWebhookDB) QueryRow(_ context.Context, query string, args ...interface{}) pgx.Row {
	switch {
	case strings.Contains(query, "-- name: GetIssueByPR"):
		return fakeRow(issueValues(f.issue))
	case strings.Contains(query, "-- name: GetIssue\n"):
		return fakeRow(issueValues(f.issue))
	case strings.Contains(query, "-- name: CountUnresolvedReviewThreadsByIssue"):
		return fakeRow([]any{f.unresolvedCount})
	case strings.Contains(query, "-- name: UpsertReviewThread"):
		f.upsertReviewThreadCalls++
		thread := db.IssueReviewThread{
			ID:             testUUID(3),
			WorkspaceID:    f.issue.WorkspaceID,
			IssueID:        f.issue.ID,
			PrRepo:         "acme/repo",
			PrNumber:       7,
			GhCommentID:    args[4].(int64),
			GhThreadNodeID: pgtype.Text{Valid: false},
			FilePath:       args[5].(string),
			Line:           args[15].(pgtype.Int4),
			Side:           args[16].(pgtype.Text),
			Severity:       args[6].(string),
			Title:          args[7].(string),
			Body:           args[8].(string),
			Url:            args[9].(string),
			AuthorLogin:    args[10].(string),
			State:          "unresolved",
			SeverityBadge:  args[11].(string),
			EffortBadge:    args[12].(string),
			AiPrompt:       args[13].(string),
		}
		return fakeRow(reviewThreadValues(thread))
	case strings.Contains(query, "-- name: UpsertCRReviewComment"):
		comment := db.Comment{
			ID:             testUUID(4),
			IssueID:        f.issue.ID,
			AuthorType:     "system",
			Content:        args[2].(string),
			Type:           "cr_review_comment",
			WorkspaceID:    f.issue.WorkspaceID,
			ReviewThreadID: args[3].(pgtype.UUID),
		}
		return fakeRow(commentValues(comment))
	case strings.Contains(query, "-- name: UpsertCRReviewAttempt"):
		f.upsertAttemptHeadSHA = args[4].(string)
		headSHA := args[4].(string)
		if headSHA == "" {
			headSHA = f.existingAttemptHeadSHA
		}
		return fakeRow(attemptValues(db.CrReviewAttempt{
			ID:          testUUID(5),
			IssueID:     f.issue.ID,
			WorkspaceID: f.issue.WorkspaceID,
			CrRound:     args[2].(int32),
			PrUrl:       args[3].(string),
			HeadSha:     headSHA,
		}))
	case strings.Contains(query, "-- name: GetCRReviewAttempt"):
		return fakeRow(attemptValues(db.CrReviewAttempt{
			ID:          testUUID(5),
			IssueID:     f.issue.ID,
			WorkspaceID: f.issue.WorkspaceID,
			CrRound:     args[1].(int32),
			HeadSha:     f.existingAttemptHeadSHA,
		}))
	case strings.Contains(query, "-- name: GetReviewThreadByCommentID"):
		author, ok := f.reviewThreadAuthors[args[0].(int64)]
		if !ok {
			return fakeErrRow{err: pgx.ErrNoRows}
		}
		return fakeRow(reviewThreadValues(db.IssueReviewThread{
			ID:          testUUID(8),
			WorkspaceID: f.issue.WorkspaceID,
			IssueID:     f.issue.ID,
			PrRepo:      "acme/repo",
			PrNumber:    7,
			GhCommentID: args[0].(int64),
			FilePath:    "main.go",
			Severity:    "suggestion",
			Title:       "finding",
			Body:        "body",
			Url:         "https://github.com/acme/repo/pull/7#discussion",
			AuthorLogin: author,
			State:       "unresolved",
		}))
	case strings.Contains(query, "-- name: CloseCRReviewAttempt"):
		if f.closeAttemptErr != nil {
			return fakeErrRow{err: f.closeAttemptErr}
		}
		outcome := args[2].(pgtype.Text)
		f.closedAttemptOutcome = outcome.String
		return fakeRow(attemptValues(db.CrReviewAttempt{
			ID:            testUUID(5),
			IssueID:       f.issue.ID,
			WorkspaceID:   f.issue.WorkspaceID,
			CrRound:       args[1].(int32),
			Outcome:       outcome,
			OutcomeReason: args[3].(pgtype.Text),
		}))
	case strings.Contains(query, "-- name: UpdateIssueStatus"):
		f.updatedStatus = args[1].(string)
		updated := f.issue
		updated.Status = f.updatedStatus
		return fakeRow(issueValues(updated))
	case strings.Contains(query, "-- name: CreateComment"):
		f.createdCommentContent = args[4].(string)
		return fakeRow(commentValues(db.Comment{
			ID:          testUUID(6),
			IssueID:     f.issue.ID,
			WorkspaceID: f.issue.WorkspaceID,
			AuthorType:  args[2].(string),
			Content:     args[4].(string),
			Type:        args[5].(string),
		}))
	case strings.Contains(query, "-- name: CreateActivity"):
		f.createdActivityAction = args[4].(string)
		return fakeRow(activityValues(f.issue.WorkspaceID, f.issue.ID, args[2].(pgtype.Text), args[4].(string), args[5].([]byte)))
	default:
		return fakeRowErr(errors.New("unexpected query: " + firstLine(query)))
	}
}

type fakeTxStarter struct{ store *fakeWebhookDB }

func (f fakeTxStarter) Begin(context.Context) (pgx.Tx, error) { return fakeTx{store: f.store}, nil }

type fakeTx struct{ store *fakeWebhookDB }

func (f fakeTx) Begin(context.Context) (pgx.Tx, error) { return f, nil }
func (f fakeTx) Commit(context.Context) error          { return nil }
func (f fakeTx) Rollback(context.Context) error        { return nil }
func (f fakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected CopyFrom")
}
func (f fakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}
func (f fakeTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (f fakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected Prepare")
}
func (f fakeTx) Exec(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error) {
	return f.store.Exec(ctx, query, args...)
}
func (f fakeTx) Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error) {
	return f.store.Query(ctx, query, args...)
}
func (f fakeTx) QueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row {
	return f.store.QueryRow(ctx, query, args...)
}
func (f fakeTx) Conn() *pgx.Conn { return nil }

type fakeRow []any

func fakeRowErr(err error) pgx.Row { return fakeErrRow{err: err} }

func (r fakeRow) Scan(dest ...interface{}) error {
	if len(dest) != len(r) {
		return errors.New("scan destination count mismatch")
	}
	for i := range dest {
		if r[i] == nil {
			continue
		}
		dv := reflect.ValueOf(dest[i])
		if dv.Kind() != reflect.Pointer || dv.IsNil() {
			return errors.New("scan destination is not a pointer")
		}
		v := reflect.ValueOf(r[i])
		if !v.Type().AssignableTo(dv.Elem().Type()) {
			return errors.New("scan value type mismatch")
		}
		dv.Elem().Set(v)
	}
	return nil
}

type fakeErrRow struct{ err error }

func (r fakeErrRow) Scan(...interface{}) error { return r.err }

func issueValues(i db.Issue) []any {
	return []any{
		i.ID, i.WorkspaceID, i.Title, i.Description, i.Status, i.Priority,
		i.AssigneeType, i.AssigneeID, i.CreatorType, i.CreatorID,
		i.ParentIssueID, i.AcceptanceCriteria, i.ContextRefs, i.Position,
		i.DueDate, i.CreatedAt, i.UpdatedAt, i.Number, i.ProjectID,
		i.OriginType, i.OriginID, i.FirstExecutedAt, i.PrUrl, i.PrNumber,
		i.PrRepo, i.EstimateMinutes, i.PhaseState,
	}
}

func reviewThreadValues(i db.IssueReviewThread) []any {
	return []any{
		i.ID, i.WorkspaceID, i.IssueID, i.PrRepo, i.PrNumber, i.GhCommentID,
		i.GhThreadNodeID, i.FilePath, i.Line, i.Side, i.Severity, i.Title,
		i.Body, i.Url, i.AuthorLogin, i.State, i.ResolvedByAgent, i.ResolvedAt,
		i.CreatedAt, i.UpdatedAt, i.SeverityBadge, i.EffortBadge, i.AiPrompt,
		i.ProcessedByResolverAt, i.ProcessedByAgent, i.ClaimedByAgent, i.ClaimExpiresAt,
	}
}

func commentValues(i db.Comment) []any {
	return []any{
		i.ID, i.IssueID, i.AuthorType, i.AuthorID, i.Content, i.Type,
		i.CreatedAt, i.UpdatedAt, i.ParentID, i.WorkspaceID, i.ReviewThreadID,
		i.PostedToGithubAt,
	}
}

func attemptValues(i db.CrReviewAttempt) []any {
	return []any{
		i.ID, i.IssueID, i.WorkspaceID, i.CrRound, i.PrUrl, i.HeadSha,
		i.StartedAt, i.ReviewSubmittedAt, i.ReviewState, i.FindingsCount,
		i.Outcome, i.OutcomeReason, i.ClosedAt, i.FirstSignalAt, i.FirstSignalKind,
	}
}

func activityValues(workspaceID, issueID pgtype.UUID, actorType pgtype.Text, action string, details []byte) []any {
	return []any{
		testUUID(7), workspaceID, issueID, actorType, pgtype.UUID{}, action, details, pgtype.Timestamptz{},
	}
}

func reviewPayloadMap(action, state string, reviewID int64, author string) map[string]json.RawMessage {
	return rawPayload(map[string]any{
		"action": action,
		"review": map[string]any{
			"id":    reviewID,
			"state": state,
			"user":  map[string]any{"login": author},
		},
		"pull_request": map[string]any{
			"number":   7,
			"html_url": "https://github.com/acme/repo/pull/7",
			"head":     map[string]any{"ref": "feature", "sha": "abc123"},
		},
	})
}

func reviewCommentPayloadMap(action string, commentID int64, author string) map[string]json.RawMessage {
	return rawPayload(map[string]any{
		"action": action,
		"comment": map[string]any{
			"id":       commentID,
			"path":     "main.go",
			"line":     10,
			"side":     "RIGHT",
			"body":     "**Issue:** fix this\n\n**AI Prompt:** patch it",
			"html_url": "https://github.com/acme/repo/pull/7#discussion_r501",
			"user":     map[string]any{"login": author},
		},
		"pull_request": map[string]any{
			"number":   7,
			"html_url": "https://github.com/acme/repo/pull/7",
			"head":     map[string]any{"ref": "feature", "sha": "abc123"},
		},
	})
}

func checkRunPayloadMap(action, conclusion, title string, prs []int32, headSHA, appSlug string) map[string]json.RawMessage {
	pullRequests := make([]map[string]any, 0, len(prs))
	for _, n := range prs {
		pullRequests = append(pullRequests, map[string]any{"number": n})
	}
	return rawPayload(map[string]any{
		"action": action,
		"check_run": map[string]any{
			"name":          "CodeRabbit",
			"status":        map[bool]string{true: "completed", false: "in_progress"}[action == "completed"],
			"conclusion":    conclusion,
			"head_sha":      headSHA,
			"html_url":      "https://github.com/acme/repo/runs/1",
			"app":           map[string]any{"slug": appSlug},
			"output":        map[string]any{"title": title},
			"pull_requests": pullRequests,
		},
	})
}

func issueCommentPayloadMap(action string, isPR bool, number int32, author string) map[string]json.RawMessage {
	issue := map[string]any{"number": number}
	if isPR {
		issue["pull_request"] = map[string]any{"html_url": "https://github.com/acme/repo/pull/7"}
	}
	return rawPayload(map[string]any{
		"action": action,
		"issue":  issue,
		"comment": map[string]any{
			"html_url": "https://github.com/acme/repo/pull/7#issuecomment-1",
			"user":     map[string]any{"login": author},
		},
	})
}

func rawPayload(v map[string]any) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(v))
	for k, val := range v {
		b, _ := json.Marshal(val)
		out[k] = b
	}
	return out
}

func testUUID(seed byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed}, Valid: true}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
