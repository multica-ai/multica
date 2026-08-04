package daemon

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestParseA1CodeMRSnapshot(t *testing.T) {
	view := []byte(`{
  "mergeRequest": {
    "id": 28981841,
    "title": "Deliver model catalog",
    "state": "merged",
    "sourceBranch": "feature/DAT-77",
    "targetBranch": "develop/pre",
    "createdAt": "2026-08-01T21:38:22+08:00",
    "updatedAt": "2026-08-02T12:49:01+08:00",
    "author": {"username": "guanjing.pangj"}
  }
}`)
	status := []byte(`{"mrId":28981841,"readyToMerge":true,"checks":[]}`)
	comments := []byte(`[
  {"id":1,"note":"open","closed":0},
  {"id":2,"note":"resolved","closed":1},
  {"id":3,"note":"also open","closed":0}
]`)

	got, err := parseA1CodeMRSnapshot(view, status, comments)
	if err != nil {
		t.Fatalf("parseA1CodeMRSnapshot: %v", err)
	}
	if got.Title != "Deliver model catalog" || got.State != "merged" {
		t.Fatalf("identity fields = %+v", got)
	}
	if got.SourceBranch != "feature/DAT-77" || got.TargetBranch != "develop/pre" {
		t.Fatalf("branches = %+v", got)
	}
	if got.AuthorLogin != "guanjing.pangj" {
		t.Fatalf("author_login = %q", got.AuthorLogin)
	}
	if got.ReadyToMerge == nil || !*got.ReadyToMerge {
		t.Fatalf("ready_to_merge = %#v", got.ReadyToMerge)
	}
	if got.CommentCount != 3 || got.UnresolvedCommentCount != 2 {
		t.Fatalf("comment counts = %d/%d", got.CommentCount, got.UnresolvedCommentCount)
	}
	if got.CreatedAt != "2026-08-01T21:38:22+08:00" || got.UpdatedAt != "2026-08-02T12:49:01+08:00" {
		t.Fatalf("timestamps = %q/%q", got.CreatedAt, got.UpdatedAt)
	}
}

func TestParseA1CodeMRSnapshotRejectsMalformedOrMismatchedOutput(t *testing.T) {
	validStatus := []byte(`{"mrId":7,"readyToMerge":false}`)
	validComments := []byte(`[]`)
	tests := []struct {
		name   string
		view   string
		status []byte
	}{
		{name: "malformed view", view: `{`, status: validStatus},
		{name: "missing merge request", view: `{}`, status: validStatus},
		{name: "unsupported state", view: `{"mergeRequest":{"id":7,"title":"x","state":"deleted"}}`, status: validStatus},
		{name: "mismatched status", view: `{"mergeRequest":{"id":8,"title":"x","state":"opened"}}`, status: validStatus},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseA1CodeMRSnapshot([]byte(tc.view), tc.status, validComments)
			if err == nil {
				t.Fatal("expected parse failure")
			}
		})
	}
}

func TestValidateCodeMRSyncRequest(t *testing.T) {
	valid := protocol.CodeMRSyncPayload{
		RuntimeID:             "runtime-1",
		ExternalPullRequestID: "6b19bbf3-2451-442c-a23c-bc9efc948aca",
		RepositoryPath:        "base-biz/agentworks-python",
		ReviewNumber:          28981841,
	}
	if err := validateCodeMRSyncRequest(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	tests := []struct {
		name string
		edit func(*protocol.CodeMRSyncPayload)
	}{
		{name: "missing runtime", edit: func(p *protocol.CodeMRSyncPayload) { p.RuntimeID = "" }},
		{name: "bad external id", edit: func(p *protocol.CodeMRSyncPayload) { p.ExternalPullRequestID = "not-a-uuid" }},
		{name: "zero review", edit: func(p *protocol.CodeMRSyncPayload) { p.ReviewNumber = 0 }},
		{name: "shell separator", edit: func(p *protocol.CodeMRSyncPayload) { p.RepositoryPath = "base-biz/repo; touch /tmp/pwned" }},
		{name: "path traversal", edit: func(p *protocol.CodeMRSyncPayload) { p.RepositoryPath = "base-biz/../repo" }},
		{name: "leading flag", edit: func(p *protocol.CodeMRSyncPayload) { p.RepositoryPath = "--repo/evil" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := valid
			tc.edit(&got)
			if err := validateCodeMRSyncRequest(got); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func TestA1CommandErrorIsRedactedAndBounded(t *testing.T) {
	err := formatA1CommandError("comment list", strings.Repeat("x", 5000)+" Private-Token: secret")
	if strings.Contains(err, "secret") {
		t.Fatalf("error leaked token: %q", err)
	}
	if len(err) > 1200 {
		t.Fatalf("error was not bounded: %d bytes", len(err))
	}
}
