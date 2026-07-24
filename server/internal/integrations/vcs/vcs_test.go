package vcs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRegistry(t *testing.T) {
	for _, k := range []string{"forgejo", "gitea", "gitlab"} {
		if _, ok := For(k); !ok {
			t.Errorf("registry missing provider %q", k)
		}
	}
	if _, ok := For("bitbucket"); ok {
		t.Error("unknown provider should not resolve")
	}
}

func TestKindValid(t *testing.T) {
	if !KindForgejo.Valid() || !KindGitea.Valid() || !KindGitLab.Valid() {
		t.Error("known kinds must be valid")
	}
	if Kind("svn").Valid() {
		t.Error("unknown kind must be invalid")
	}
}

func TestNormalizeInstanceURL(t *testing.T) {
	cases := map[string]string{
		"https://forgejo.example.com/": "https://forgejo.example.com",
		"  https://forge.test  ":       "https://forge.test",
		"https://forge.test/sub/":      "https://forge.test/sub",
	}
	for in, want := range cases {
		if got := NormalizeInstanceURL(in); got != want {
			t.Errorf("NormalizeInstanceURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestForgejoVerifySignature(t *testing.T) {
	p, _ := For("forgejo")
	secret, body := "s3cr3t", []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	valid := hex.EncodeToString(mac.Sum(nil))

	h := http.Header{}
	h.Set("X-Gitea-Signature", valid)
	if !p.VerifySignature(secret, h, body) {
		t.Error("valid signature rejected")
	}
	h.Set("X-Gitea-Signature", "sha256="+valid)
	if !p.VerifySignature(secret, h, body) {
		t.Error("sha256-prefixed signature rejected")
	}
	if p.VerifySignature("wrong", h, body) {
		t.Error("wrong secret accepted")
	}
	if p.VerifySignature(secret, h, []byte("tampered")) {
		t.Error("tampered body accepted")
	}
	// An empty secret must be rejected outright — HMAC with an empty key is
	// forgeable. Sign the body with the empty key and confirm it's still refused.
	emptyMac := hmac.New(sha256.New, []byte(""))
	emptyMac.Write(body)
	h.Set("X-Gitea-Signature", hex.EncodeToString(emptyMac.Sum(nil)))
	if p.VerifySignature("", h, body) {
		t.Error("empty secret accepted")
	}
}

func TestGitlabVerifySignature(t *testing.T) {
	p, _ := For("gitlab")
	h := http.Header{}
	h.Set("X-Gitlab-Token", "tok123")
	if !p.VerifySignature("tok123", h, nil) {
		t.Error("matching token rejected")
	}
	if p.VerifySignature("other", h, nil) {
		t.Error("mismatched token accepted")
	}
	if p.VerifySignature("", h, nil) {
		t.Error("empty stored secret must never validate")
	}
}

func TestForgejoEventKindAndParse(t *testing.T) {
	p, _ := For("gitea")
	h := http.Header{}
	h.Set("X-Gitea-Event", "pull_request")
	if p.EventKind(h) != EventPullRequest {
		t.Error("pull_request not classified")
	}
	h.Set("X-Gitea-Event", "status")
	if p.EventKind(h) != EventCIStatus {
		t.Error("status not classified")
	}

	pr, err := p.ParsePullRequest([]byte(`{
		"action":"closed",
		"pull_request":{"number":7,"title":"Fix MUL-1","state":"closed","merged":true,
			"html_url":"https://g/acme/widget/pulls/7","head":{"ref":"fix","sha":"abc"},
			"user":{"username":"octo"},"additions":3},
		"repository":{"name":"widget","owner":{"username":"acme"}}}`))
	if err != nil {
		t.Fatalf("ParsePullRequest: %v", err)
	}
	if pr.RepoOwner != "acme" || pr.RepoName != "widget" || pr.Number != 7 {
		t.Errorf("bad identity: %+v", pr)
	}
	if pr.State != "merged" {
		t.Errorf("state = %q, want merged", pr.State)
	}
	if pr.AuthorLogin != "octo" || pr.HeadSHA != "abc" {
		t.Errorf("bad author/sha: %+v", pr)
	}
	if !pr.Terminal() {
		t.Error("closed action must be terminal")
	}

	st, err := p.ParseCIStatus([]byte(`{"sha":"abc","context":"ci","state":"success"}`))
	if err != nil {
		t.Fatalf("ParseCIStatus: %v", err)
	}
	if st.State != "passed" {
		t.Errorf("state = %q, want passed", st.State)
	}
}

func TestGitlabParse(t *testing.T) {
	p, _ := For("gitlab")
	h := http.Header{}
	h.Set("X-Gitlab-Event", "Merge Request Hook")
	if p.EventKind(h) != EventPullRequest {
		t.Error("MR hook not classified")
	}

	pr, err := p.ParsePullRequest([]byte(`{
		"object_kind":"merge_request",
		"user":{"username":"alice","avatar_url":"a"},
		"project":{"path_with_namespace":"group/sub/widget"},
		"object_attributes":{"iid":42,"title":"Add MUL-9","state":"merged","action":"merge",
			"source_branch":"feat","url":"https://gl/group/sub/widget/-/merge_requests/42",
			"last_commit":{"id":"deadbeef"}}}`))
	if err != nil {
		t.Fatalf("ParsePullRequest: %v", err)
	}
	if pr.RepoOwner != "group/sub" || pr.RepoName != "widget" {
		t.Errorf("namespace split wrong: owner=%q name=%q", pr.RepoOwner, pr.RepoName)
	}
	if pr.Number != 42 || pr.State != "merged" || pr.HeadSHA != "deadbeef" {
		t.Errorf("bad MR: %+v", pr)
	}
	if !pr.Terminal() {
		t.Error("merge action must be terminal")
	}

	h.Set("X-Gitlab-Event", "Pipeline Hook")
	if p.EventKind(h) != EventCIStatus {
		t.Error("pipeline hook not classified")
	}
	st, err := p.ParseCIStatus([]byte(`{"object_kind":"pipeline","object_attributes":{"sha":"deadbeef","status":"failed"}}`))
	if err != nil {
		t.Fatalf("ParseCIStatus: %v", err)
	}
	if st.SHA != "deadbeef" || st.State != "failed" || st.Context != "gitlab/pipeline" {
		t.Errorf("bad status: %+v", st)
	}
}

func TestGitlabDraftDetection(t *testing.T) {
	p, _ := For("gitlab")
	pr, _ := p.ParsePullRequest([]byte(`{"object_kind":"merge_request",
		"project":{"path_with_namespace":"g/r"},
		"object_attributes":{"iid":1,"title":"Draft: wip","state":"opened","work_in_progress":true}}`))
	if pr.State != "draft" {
		t.Errorf("state = %q, want draft", pr.State)
	}
}

func TestValidateToken(t *testing.T) {
	cases := []struct {
		kind, path, authHeader, authValue, bodyJSON, wantLogin string
	}{
		{"forgejo", "/api/v1/user", "Authorization", "token tok", `{"login":"octo"}`, "octo"},
		{"gitlab", "/api/v4/user", "PRIVATE-TOKEN", "tok", `{"username":"alice"}`, "alice"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != c.path {
					t.Errorf("path = %q, want %q", r.URL.Path, c.path)
				}
				if r.Header.Get(c.authHeader) != c.authValue {
					t.Errorf("%s = %q, want %q", c.authHeader, r.Header.Get(c.authHeader), c.authValue)
				}
				_, _ = w.Write([]byte(c.bodyJSON))
			}))
			defer srv.Close()
			p, _ := For(c.kind)
			acct, err := p.ValidateToken(context.Background(), srv.URL, "tok")
			if err != nil {
				t.Fatalf("ValidateToken: %v", err)
			}
			if acct.Login != c.wantLogin {
				t.Errorf("login = %q, want %q", acct.Login, c.wantLogin)
			}
		})
	}
}

// GitLab sends timestamps as "2017-09-20 08:31:45 UTC" (not RFC3339). The
// shared handler parser is RFC3339-only, so without normalization every GitLab
// timestamp was dropped and replaced with ingestion time — defeating the
// monotonic guards on PR upsert and commit status for GitLab specifically, and
// skewing PR list ordering. These assert the adapter normalizes to RFC3339.
func TestNormalizeGitLabTime(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"2017-09-20 08:31:45 UTC", "2017-09-20T08:31:45Z"},
		{"2017-09-20T08:31:45Z", "2017-09-20T08:31:45Z"},
		// Sub-second precision is preserved (RFC3339Nano output) so two events in
		// the same wall-clock second still order correctly under the monotonic
		// guards; a zero fraction still formats without a decimal part.
		{"2017-09-20T08:31:45.123Z", "2017-09-20T08:31:45.123Z"},
		{"2017-09-20 08:31:45.123456 UTC", "2017-09-20T08:31:45.123456Z"},
		{"not a time", ""},
	}
	for _, c := range cases {
		if got := normalizeGitLabTime(c.in); got != c.want {
			t.Errorf("normalizeGitLabTime(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGitlabParseTimestamps(t *testing.T) {
	p, _ := For("gitlab")
	// A merge_request payload with GitLab-format created_at/updated_at.
	pr, err := p.ParsePullRequest([]byte(`{
		"object_kind":"merge_request",
		"project":{"path_with_namespace":"g/r"},
		"object_attributes":{"iid":1,"title":"MUL-1","state":"opened","action":"open",
			"created_at":"2017-09-20 08:31:45 UTC","updated_at":"2017-09-21 09:00:00 UTC",
			"last_commit":{"id":"abc"}}}`))
	if err != nil {
		t.Fatalf("ParsePullRequest: %v", err)
	}
	if pr.CreatedAt != "2017-09-20T08:31:45Z" || pr.UpdatedAt != "2017-09-21T09:00:00Z" {
		t.Errorf("timestamps not normalized: created=%q updated=%q", pr.CreatedAt, pr.UpdatedAt)
	}
	if _, err := time.Parse(time.RFC3339, pr.UpdatedAt); err != nil {
		t.Errorf("UpdatedAt not RFC3339: %v", err)
	}

	// A pipeline payload's finished_at feeds the commit-status guard.
	st, err := p.ParseCIStatus([]byte(`{"object_kind":"pipeline",
		"object_attributes":{"sha":"abc","status":"success",
			"created_at":"2017-09-20 08:00:00 UTC","finished_at":"2017-09-20 08:05:00 UTC"}}`))
	if err != nil {
		t.Fatalf("ParseCIStatus: %v", err)
	}
	if st.UpdatedAt != "2017-09-20T08:05:00Z" {
		t.Errorf("CI UpdatedAt = %q, want finished_at normalized", st.UpdatedAt)
	}
}

func TestForgejoStatusTimestamp(t *testing.T) {
	p, _ := For("forgejo")
	// Forgejo/Gitea status payloads carry RFC3339 created_at/updated_at.
	st, err := p.ParseCIStatus([]byte(`{"sha":"abc","context":"ci","state":"success",
		"created_at":"2017-09-20T08:00:00Z","updated_at":"2017-09-20T08:05:00Z"}`))
	if err != nil {
		t.Fatalf("ParseCIStatus: %v", err)
	}
	if st.UpdatedAt != "2017-09-20T08:05:00Z" {
		t.Errorf("CI UpdatedAt = %q, want updated_at", st.UpdatedAt)
	}
}

func TestValidateTokenUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	p, _ := For("forgejo")
	_, err := p.ValidateToken(context.Background(), srv.URL, "bad")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}
