package github

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/webhooks"
)

// makeReq builds a *http.Request with the GitHub-canonical headers
// the adapter requires. Tests override specific headers/body via the
// returned request before passing to Normalize.
func makeReq(eventType, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	r.Header.Set("X-GitHub-Event", eventType)
	r.Header.Set("X-GitHub-Delivery", "test-delivery-id-0001")
	return r
}

func TestSource_Interface(t *testing.T) {
	s := New(Config{SecretCurrent: "secret"})
	if s.Name() != "github" {
		t.Fatalf("Name() = %q, want github", s.Name())
	}
	if s.SignatureHeader() != "X-Hub-Signature-256" {
		t.Fatalf("SignatureHeader() = %q, want X-Hub-Signature-256", s.SignatureHeader())
	}
	cur, prev := s.Secrets()
	if cur != "secret" || prev != "" {
		t.Fatalf("Secrets() = (%q, %q), want (secret, \"\")", cur, prev)
	}
}

func TestNormalize_MissingHeaders(t *testing.T) {
	s := New(Config{SecretCurrent: "secret"})

	r := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader("{}"))
	if _, err := s.Normalize(r); !errors.Is(err, webhooks.ErrSchemaMismatch) {
		t.Fatalf("missing event header: err = %v, want ErrSchemaMismatch", err)
	}

	r = httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader("{}"))
	r.Header.Set("X-GitHub-Event", "workflow_run")
	if _, err := s.Normalize(r); !errors.Is(err, webhooks.ErrSchemaMismatch) {
		t.Fatalf("missing delivery header: err = %v, want ErrSchemaMismatch", err)
	}
}

func TestNormalize_WorkflowRun_FailureWithPR(t *testing.T) {
	body := `{
        "action": "completed",
        "workflow_run": {
            "conclusion": "failure",
            "head_sha": "abc123",
            "head_branch": "agent-1/pul-42-foo",
            "pull_requests": [
                {"number": 42, "html_url": "https://github.com/owner/repo/pull/42"}
            ]
        },
        "repository": {"full_name": "owner/repo"}
    }`
	s := New(Config{SecretCurrent: "x"})
	evt, err := s.Normalize(makeReq("workflow_run", body))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if evt.EventType != webhooks.EventTypeCIFailure {
		t.Errorf("EventType = %q, want ci_failure", evt.EventType)
	}
	if evt.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", evt.PRNumber)
	}
	if evt.HeadSHA != "abc123" {
		t.Errorf("HeadSHA = %q, want abc123", evt.HeadSHA)
	}
	if evt.Branch != "agent-1/pul-42-foo" {
		t.Errorf("Branch = %q, want agent-1/pul-42-foo", evt.Branch)
	}
}

func TestNormalize_WorkflowRun_SuccessSkips(t *testing.T) {
	body := `{
        "action": "completed",
        "workflow_run": {"conclusion": "success", "head_sha": "x", "pull_requests": []},
        "repository": {"full_name": "owner/repo"}
    }`
	s := New(Config{SecretCurrent: "x"})
	if _, err := s.Normalize(makeReq("workflow_run", body)); !errors.Is(err, webhooks.ErrUnsupportedEvent) {
		t.Fatalf("success run: err = %v, want ErrUnsupportedEvent", err)
	}
}

func TestNormalize_WorkflowRun_MultiplePRsSkips(t *testing.T) {
	// Fork PRs / merge-queue runs can carry multiple PRs. We deliberately
	// skip these — the cascade only handles single-PR workflows.
	body := `{
        "action": "completed",
        "workflow_run": {
            "conclusion": "failure",
            "head_sha": "x",
            "pull_requests": [
                {"number": 1, "html_url": "u1"},
                {"number": 2, "html_url": "u2"}
            ]
        },
        "repository": {"full_name": "owner/repo"}
    }`
	s := New(Config{SecretCurrent: "x"})
	if _, err := s.Normalize(makeReq("workflow_run", body)); !errors.Is(err, webhooks.ErrUnsupportedEvent) {
		t.Fatalf("multi-PR run: err = %v, want ErrUnsupportedEvent", err)
	}
}

func TestNormalize_CheckRun_FailureWithPR(t *testing.T) {
	body := `{
        "action": "completed",
        "check_run": {
            "conclusion": "failure",
            "head_sha": "def456",
            "html_url": "https://github.com/owner/repo/runs/42",
            "pull_requests": [{"number": 7, "html_url": "https://github.com/owner/repo/pull/7"}]
        },
        "repository": {"full_name": "owner/repo"}
    }`
	s := New(Config{SecretCurrent: "x"})
	evt, err := s.Normalize(makeReq("check_run", body))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if evt.EventType != webhooks.EventTypeCIFailure {
		t.Errorf("EventType = %q, want ci_failure", evt.EventType)
	}
	if evt.PRNumber != 7 {
		t.Errorf("PRNumber = %d, want 7", evt.PRNumber)
	}
}

func TestNormalize_PullRequest_MergedFiresPRMerged(t *testing.T) {
	body := `{
        "action": "closed",
        "number": 99,
        "pull_request": {
            "html_url": "https://github.com/owner/repo/pull/99",
            "title": "[PUL-99] feat(x): y",
            "merged": true,
            "head": {"sha": "merged-sha", "ref": "agent-2/pul-99-x"}
        }
    }`
	s := New(Config{SecretCurrent: "x"})
	evt, err := s.Normalize(makeReq("pull_request", body))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if evt.EventType != webhooks.EventTypePRMerged {
		t.Errorf("EventType = %q, want pr_merged", evt.EventType)
	}
	if evt.PRTitle != "[PUL-99] feat(x): y" {
		t.Errorf("PRTitle = %q", evt.PRTitle)
	}
	if evt.Branch != "agent-2/pul-99-x" {
		t.Errorf("Branch = %q", evt.Branch)
	}
}

func TestNormalize_PullRequest_ClosedUnmergedSkips(t *testing.T) {
	body := `{
        "action": "closed",
        "number": 1,
        "pull_request": {
            "html_url": "https://github.com/owner/repo/pull/1",
            "title": "[PUL-1] test",
            "merged": false,
            "head": {"sha": "x", "ref": "feat/y"}
        }
    }`
	s := New(Config{SecretCurrent: "x"})
	if _, err := s.Normalize(makeReq("pull_request", body)); !errors.Is(err, webhooks.ErrUnsupportedEvent) {
		t.Fatalf("closed-unmerged: err = %v, want ErrUnsupportedEvent", err)
	}
}

func TestNormalize_PullRequest_EditedTitleFiresEdit(t *testing.T) {
	body := `{
        "action": "edited",
        "number": 5,
        "pull_request": {
            "html_url": "https://github.com/owner/repo/pull/5",
            "title": "now without prefix",
            "merged": false,
            "head": {"sha": "x", "ref": "agent-1/pul-5-x"}
        },
        "changes": {"title": {"from": "[PUL-5] original"}}
    }`
	s := New(Config{SecretCurrent: "x"})
	evt, err := s.Normalize(makeReq("pull_request", body))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if evt.EventType != webhooks.EventTypePRTitleEdit {
		t.Errorf("EventType = %q, want pr_title_edit", evt.EventType)
	}
}

func TestNormalize_PullRequest_EditedNonTitleSkips(t *testing.T) {
	body := `{
        "action": "edited",
        "number": 5,
        "pull_request": {
            "html_url": "u",
            "title": "[PUL-5] x",
            "merged": false,
            "head": {"sha": "x", "ref": "agent-1/pul-5-x"}
        }
    }`
	s := New(Config{SecretCurrent: "x"})
	if _, err := s.Normalize(makeReq("pull_request", body)); !errors.Is(err, webhooks.ErrUnsupportedEvent) {
		t.Fatalf("edited body-only: err = %v, want ErrUnsupportedEvent", err)
	}
}

func TestNormalize_PullRequestReview_ChangesRequested(t *testing.T) {
	body := `{
        "action": "submitted",
        "review": {"state": "changes_requested"},
        "pull_request": {
            "number": 12,
            "html_url": "https://github.com/owner/repo/pull/12",
            "title": "[PUL-12] r",
            "head": {"sha": "rev-sha", "ref": "agent-1/pul-12-r"}
        }
    }`
	s := New(Config{SecretCurrent: "x"})
	evt, err := s.Normalize(makeReq("pull_request_review", body))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if evt.EventType != webhooks.EventTypePRReviewChange {
		t.Errorf("EventType = %q, want pr_review_change", evt.EventType)
	}
}

func TestNormalize_PullRequestReview_ApprovedSkips(t *testing.T) {
	body := `{
        "action": "submitted",
        "review": {"state": "approved"},
        "pull_request": {"number": 1, "html_url": "u", "title": "t", "head": {"sha": "x", "ref": "b"}}
    }`
	s := New(Config{SecretCurrent: "x"})
	if _, err := s.Normalize(makeReq("pull_request_review", body)); !errors.Is(err, webhooks.ErrUnsupportedEvent) {
		t.Fatalf("approved: err = %v, want ErrUnsupportedEvent", err)
	}
}

func TestNormalize_PingSkips(t *testing.T) {
	s := New(Config{SecretCurrent: "x"})
	if _, err := s.Normalize(makeReq("ping", "{}")); !errors.Is(err, webhooks.ErrUnsupportedEvent) {
		t.Fatalf("ping: err = %v, want ErrUnsupportedEvent", err)
	}
}

func TestNormalize_UnknownEventSkips(t *testing.T) {
	s := New(Config{SecretCurrent: "x"})
	if _, err := s.Normalize(makeReq("star", "{}")); !errors.Is(err, webhooks.ErrUnsupportedEvent) {
		t.Fatalf("star: err = %v, want ErrUnsupportedEvent", err)
	}
}

func TestNormalize_MalformedJSON(t *testing.T) {
	s := New(Config{SecretCurrent: "x"})
	if _, err := s.Normalize(makeReq("workflow_run", `{not json`)); !errors.Is(err, webhooks.ErrSchemaMismatch) {
		t.Fatalf("malformed: err = %v, want ErrSchemaMismatch", err)
	}
}

func TestEventID_Deterministic(t *testing.T) {
	// Same delivery ID must produce the same event_id across calls —
	// the dedup contract on cascade_retrigger.event_id depends on it.
	a := EventID("some-delivery")
	b := EventID("some-delivery")
	if a != b {
		t.Fatalf("EventID not deterministic: %v vs %v", a, b)
	}

	c := EventID("other-delivery")
	if a == c {
		t.Fatalf("EventID collision across distinct deliveries")
	}
}

func TestFromEnv_PresentAndAbsent(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		got := FromEnv(func(string) string { return "" })
		if got != nil {
			t.Fatalf("FromEnv with no env should return nil, got %v", got)
		}
	})

	t.Run("current only", func(t *testing.T) {
		got := FromEnv(func(k string) string {
			if k == "MULTICA_GITHUB_WEBHOOK_SECRET_CURRENT" {
				return "current"
			}
			return ""
		})
		if got == nil {
			t.Fatal("FromEnv with current should return Source")
		}
		c, p := got.Secrets()
		if c != "current" || p != "" {
			t.Fatalf("Secrets = (%q, %q), want (current, \"\")", c, p)
		}
	})

	t.Run("current + previous", func(t *testing.T) {
		got := FromEnv(func(k string) string {
			switch k {
			case "MULTICA_GITHUB_WEBHOOK_SECRET_CURRENT":
				return "new"
			case "MULTICA_GITHUB_WEBHOOK_SECRET_PREVIOUS":
				return "old"
			}
			return ""
		})
		c, p := got.Secrets()
		if c != "new" || p != "old" {
			t.Fatalf("Secrets = (%q, %q), want (new, old)", c, p)
		}
	})
}
