package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// readJSON is a tiny test helper that drains the request body and
// unmarshals it. The Phase 3 write methods all send JSON bodies; pulling
// the assertion into one place keeps each test focused on the
// per-endpoint contract.
func readJSON(t *testing.T, r *http.Request, into any) {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) == 0 {
		return
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("decode body: %v (%s)", err, string(body))
	}
}

// TestMergePullRequest_HappyPath verifies the path, method, payload, and
// response decode for the merge endpoint.
func TestMergePullRequest_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method: got %s", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/pulls/42/merge" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		var got mergePullRequestRequest
		readJSON(t, r, &got)
		if got.MergeMethod != "squash" || got.SHA != "abc123" {
			t.Errorf("payload: got %+v", got)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"sha":"deadbeef","merged":true,"message":"PR merged"}`))
	}))
	defer srv.Close()

	c := NewClient("t")
	c.BaseURL = srv.URL
	res, err := c.MergePullRequest(context.Background(), "owner", "repo", 42, "squash", "abc123")
	if err != nil {
		t.Fatalf("MergePullRequest: %v", err)
	}
	if !res.Merged || res.SHA != "deadbeef" {
		t.Errorf("result: %+v", res)
	}
}

// TestMergePullRequest_NotMergeable maps GitHub's 422 to ErrUnprocessable
// so the handler can render a 422 to the chip without parsing JSON.
func TestMergePullRequest_NotMergeable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message":"Pull Request is not mergeable"}`))
	}))
	defer srv.Close()

	c := NewClient("t")
	c.BaseURL = srv.URL
	_, err := c.MergePullRequest(context.Background(), "o", "r", 1, "merge", "")
	if !errors.Is(err, ErrUnprocessable) {
		t.Fatalf("expected ErrUnprocessable, got %v", err)
	}
}

// TestMergePullRequest_MethodNotAllowed — GitHub historically returned 405
// for non-mergeable PRs. We bucket it with 422 so the chip behavior is
// consistent regardless of which API era we're talking to.
func TestMergePullRequest_MethodNotAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(`{"message":"Pull Request is not mergeable"}`))
	}))
	defer srv.Close()

	c := NewClient("t")
	c.BaseURL = srv.URL
	_, err := c.MergePullRequest(context.Background(), "o", "r", 1, "merge", "")
	if !errors.Is(err, ErrUnprocessable) {
		t.Fatalf("expected ErrUnprocessable, got %v", err)
	}
}

func TestMergePullRequest_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient("t")
	c.BaseURL = srv.URL
	_, err := c.MergePullRequest(context.Background(), "o", "r", 1, "merge", "")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestMergePullRequest_InvalidMethod(t *testing.T) {
	c := NewClient("t")
	c.BaseURL = "http://invalid.example"
	_, err := c.MergePullRequest(context.Background(), "o", "r", 1, "force-push", "")
	if err == nil || !strings.Contains(err.Error(), "invalid merge method") {
		t.Fatalf("expected invalid method error, got %v", err)
	}
}

func TestUpdatePullRequestBranch_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method: got %s", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/pulls/7/update-branch" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		var got updatePullRequestBranchRequest
		readJSON(t, r, &got)
		if got.ExpectedHeadSHA != "abc" {
			t.Errorf("expected_head_sha: got %q", got.ExpectedHeadSHA)
		}
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"message":"Updating pull request branch."}`))
	}))
	defer srv.Close()

	c := NewClient("t")
	c.BaseURL = srv.URL
	if err := c.UpdatePullRequestBranch(context.Background(), "owner", "repo", 7, "abc"); err != nil {
		t.Fatalf("UpdatePullRequestBranch: %v", err)
	}
}

// TestUpdatePullRequestBranch_Conflict — already up-to-date branches
// produce a 409 from GitHub; we surface that as ErrConflict so the chip
// can render "already up to date" instead of a generic failure.
func TestUpdatePullRequestBranch_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()

	c := NewClient("t")
	c.BaseURL = srv.URL
	err := c.UpdatePullRequestBranch(context.Background(), "owner", "repo", 7, "abc")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestCreatePullRequestComment_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/issues/3/comments" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		var got createCommentRequest
		readJSON(t, r, &got)
		if got.Body != "hello" {
			t.Errorf("body: got %q", got.Body)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":111,"html_url":"https://github.com/owner/repo/issues/3#issuecomment-111","body":"hello","user":{"login":"alice"}}`))
	}))
	defer srv.Close()

	c := NewClient("t")
	c.BaseURL = srv.URL
	cm, err := c.CreatePullRequestComment(context.Background(), "owner", "repo", 3, "hello")
	if err != nil {
		t.Fatalf("CreatePullRequestComment: %v", err)
	}
	if cm.ID != 111 || cm.Body != "hello" {
		t.Errorf("comment: %+v", cm)
	}
}

func TestCreatePullRequestComment_EmptyBodyRejected(t *testing.T) {
	c := NewClient("t")
	c.BaseURL = "http://invalid.example"
	if _, err := c.CreatePullRequestComment(context.Background(), "o", "r", 1, "  "); err == nil {
		t.Fatalf("expected empty-body error")
	}
}

func TestDismissPullRequestReview_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method: got %s", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/pulls/9/reviews/123/dismissals" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		var got dismissReviewRequest
		readJSON(t, r, &got)
		if got.Message != "stale" || got.Event != "DISMISS" {
			t.Errorf("body: %+v", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient("t")
	c.BaseURL = srv.URL
	if err := c.DismissPullRequestReview(context.Background(), "owner", "repo", 9, 123, "stale"); err != nil {
		t.Fatalf("DismissPullRequestReview: %v", err)
	}
}

// TestDismissPullRequestReview_Forbidden — admin permission is required;
// the typed forbidden error lets the handler return 403.
func TestDismissPullRequestReview_Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
	}))
	defer srv.Close()

	c := NewClient("t")
	c.BaseURL = srv.URL
	err := c.DismissPullRequestReview(context.Background(), "owner", "repo", 9, 123, "stale")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestClosePullRequest_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method: got %s", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/pulls/12" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		var got updatePullRequestRequest
		readJSON(t, r, &got)
		if got.State != "closed" {
			t.Errorf("body: %+v", got)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"state":"closed"}`))
	}))
	defer srv.Close()

	c := NewClient("t")
	c.BaseURL = srv.URL
	if err := c.ClosePullRequest(context.Background(), "owner", "repo", 12); err != nil {
		t.Fatalf("ClosePullRequest: %v", err)
	}
}

func TestDispatchWorkflow_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/actions/workflows/smoke.yml/dispatches" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		var got dispatchWorkflowRequest
		readJSON(t, r, &got)
		if got.Ref != "main" || got.Inputs["environment_id"] != "abc" {
			t.Errorf("body: %+v", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient("t")
	c.BaseURL = srv.URL
	if err := c.DispatchWorkflow(context.Background(), "owner", "repo", "smoke.yml", "main", map[string]string{"environment_id": "abc"}); err != nil {
		t.Fatalf("DispatchWorkflow: %v", err)
	}
}

func TestDispatchWorkflow_RejectsEmpty(t *testing.T) {
	c := NewClient("t")
	c.BaseURL = "http://invalid.example"
	if err := c.DispatchWorkflow(context.Background(), "o", "r", "", "main", nil); err == nil {
		t.Fatalf("expected error on empty workflow file")
	}
	if err := c.DispatchWorkflow(context.Background(), "o", "r", "smoke.yml", "", nil); err == nil {
		t.Fatalf("expected error on empty ref")
	}
}
