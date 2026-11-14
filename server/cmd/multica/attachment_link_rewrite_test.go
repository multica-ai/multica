package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rewriteURL = "https://cdn.example/a"

func TestRewriteAttachmentLinks(t *testing.T) {
	workdir := withWorkdir(t)
	for _, name := range []string{"a.png", "b.png", "my shot.png", "shots/a.png"} {
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("png"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		name  string
		paths []string
		body  string
		want  string
	}{
		{
			name:  "dot-relative image",
			paths: []string{"./a.png"},
			body:  "Option A\n\n![Option A](./a.png)\n\nOption B",
			want:  "Option A\n\n![Option A](" + rewriteURL + ")\n\nOption B",
		},
		{
			name:  "bare name matches a dot-relative attachment",
			paths: []string{"./a.png"},
			body:  "![](a.png)",
			want:  "![](" + rewriteURL + ")",
		},
		{
			name:  "bare name matches an attachment in a subdirectory",
			paths: []string{"shots/a.png"},
			body:  "![](./a.png)",
			want:  "![](" + rewriteURL + ")",
		},
		{
			name:  "relative path",
			paths: []string{"./shots/a.png"},
			body:  "![](shots/a.png)",
			want:  "![](" + rewriteURL + ")",
		},
		{
			name:  "absolute path in the workdir",
			paths: []string{"a.png"},
			body:  "![](" + filepath.ToSlash(filepath.Join(workdir, "a.png")) + ")",
			want:  "![](" + rewriteURL + ")",
		},
		{
			name:  "plain link, angle brackets and title",
			paths: []string{"my shot.png"},
			body:  "[open](<./my shot.png> \"title\")",
			want:  "[open](<" + rewriteURL + "> \"title\")",
		},
		{
			name:  "percent-encoded name",
			paths: []string{"my shot.png"},
			body:  "![](./my%20shot.png)",
			want:  "![](" + rewriteURL + ")",
		},
		{
			name:  "every occurrence",
			paths: []string{"a.png"},
			body:  "![1](./a.png) and ![2](a.png)",
			want:  "![1](" + rewriteURL + ") and ![2](" + rewriteURL + ")",
		},
		{
			name:  "label text naming the same path is untouched",
			paths: []string{"a.png"},
			body:  "[see ./a.png](./a.png)",
			want:  "[see ./a.png](" + rewriteURL + ")",
		},
		{
			name:  "image nested in a link to the same file",
			paths: []string{"a.png"},
			body:  "[![](./a.png)](./a.png)",
			want:  "[![](" + rewriteURL + ")](" + rewriteURL + ")",
		},
		{
			name:  "nested destination sharing the attachment path as a prefix is untouched",
			paths: []string{"a.png"},
			body:  "[![preview](./a.png.thumb.png)](./a.png)",
			want:  "[![preview](./a.png.thumb.png)](" + rewriteURL + ")",
		},
		{
			name:  "nested angle destination sharing a prefix is untouched",
			paths: []string{"a.png"},
			body:  "[![preview](<./a.png thumb.png>)](<./a.png>)",
			want:  "[![preview](<./a.png thumb.png>)](<" + rewriteURL + ">)",
		},
		{
			name:  "code span is untouched",
			paths: []string{"a.png"},
			body:  "write `![](./a.png)` like this: ![](./a.png)",
			want:  "write `![](./a.png)` like this: ![](" + rewriteURL + ")",
		},
		{
			name:  "fenced block is untouched",
			paths: []string{"a.png"},
			body:  "```md\n![](./a.png)\n```\n",
			want:  "```md\n![](./a.png)\n```\n",
		},
		{
			name:  "indented block is untouched",
			paths: []string{"a.png"},
			body:  "text\n\n    ![](./a.png)\n",
			want:  "text\n\n    ![](./a.png)\n",
		},
		{
			name:  "unmatched local path is untouched",
			paths: []string{"a.png"},
			body:  "![](./b.png)",
			want:  "![](./b.png)",
		},
		{
			name:  "different directory is not a bare-name match",
			paths: []string{"a.png"},
			body:  "![](other/a.png)",
			want:  "![](other/a.png)",
		},
		{
			name:  "ambiguous bare name is untouched",
			paths: []string{"shots/a.png", "other/a.png"},
			body:  "![](./a.png)",
			want:  "![](./a.png)",
		},
		{
			name:  "URL with the same name is untouched",
			paths: []string{"a.png"},
			body:  "![](https://example.com/a.png)",
			want:  "![](https://example.com/a.png)",
		},
		{
			name:  "reference-style link is untouched",
			paths: []string{"a.png"},
			body:  "![shot][s]\n\n[s]: ./a.png\n",
			want:  "![shot][s]\n\n[s]: ./a.png\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			urls := make([]string, len(tc.paths))
			for i := range urls {
				urls[i] = rewriteURL
			}
			got, _ := rewriteAttachmentLinks(tc.body, tc.paths, urls)
			if got != tc.want {
				t.Errorf("rewrite =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestRewriteAttachmentLinksMapsEachAttachment(t *testing.T) {
	withWorkdir(t)
	got, rewrites := rewriteAttachmentLinks(
		"A\n\n![A](./a.png)\n\nB\n\n![B](./b.png)\n\nagain ![A](a.png)",
		[]string{"a.png", "b.png"},
		[]string{"/api/attachments/1/download", "/api/attachments/2/download"},
	)
	want := "A\n\n![A](/api/attachments/1/download)\n\nB\n\n![B](/api/attachments/2/download)\n\nagain ![A](/api/attachments/1/download)"
	if got != want {
		t.Fatalf("rewrite =\n%q\nwant\n%q", got, want)
	}
	if len(rewrites) != 3 || rewrites[0].Target != "./a.png" || rewrites[1].Target != "./b.png" || rewrites[2].Target != "a.png" {
		t.Errorf("rewrites = %#v", rewrites)
	}
}

// A link to a file attached in the same call is not a dead local link, so the
// lint must not reject it — the preview body it checks has it rewritten.
func TestPreviewAttachmentLinksSatisfiesLocalPathLint(t *testing.T) {
	workdir := withWorkdir(t)
	withAgentContext(t)
	shot := filepath.Join(workdir, "shot.png")
	if err := os.WriteFile(shot, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := "![shot](" + shot + ")"
	if err := guardLocalPathLinks(previewAttachmentLinks(body, []string{"shot.png"}), "comment body", "hint"); err != nil {
		t.Errorf("attached path should pass the lint, got: %v", err)
	}
	if err := guardLocalPathLinks(previewAttachmentLinks(body, nil), "comment body", "hint"); err == nil {
		t.Error("unattached workdir path should still fail the lint")
	}
}

func TestRunIssueCommentAddRewritesInlineAttachmentLinks(t *testing.T) {
	withWorkdir(t)
	withAgentContext(t)
	if err := os.WriteFile("a.png", []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := "Option A\n\n![Option A](./a.png)\n\n```\n![](./a.png)\n```\n\nOption B ![](./b.png)\n"
	if err := os.WriteFile("reply.md", []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	const markdownURL = "https://app.example/api/attachments/att-1/download"
	var posted map[string]any
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/MUL-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "issue-1", "identifier": "MUL-1"})
		case isPresignUploadRequest(r):
			// Models a server from before the presigned-upload endpoint.
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/api/upload-file":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "att-1", "filename": "a.png", "content_type": "image/png", "markdown_url": markdownURL,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/issue-1/comments":
			posts++
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatalf("decode comment: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "comment-1", "revision": 1})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := newIssueCommentAddTestCmd()
	_ = cmd.Flags().Set("content-file", "reply.md")
	_ = cmd.Flags().Set("attachment", "./a.png")
	_ = cmd.Flags().Set("output", "table")
	stderr := captureStderr(t)
	err := runIssueCommentAdd(cmd, []string{"MUL-1"})
	report := stderr.read()
	if err != nil {
		t.Fatalf("runIssueCommentAdd: %v", err)
	}

	if posts != 1 {
		t.Fatalf("comment posts = %d, want exactly 1", posts)
	}
	want := "Option A\n\n![Option A](" + markdownURL + ")\n\n```\n![](./a.png)\n```\n\nOption B ![](./b.png)"
	if posted["content"] != want {
		t.Errorf("content =\n%q\nwant\n%q", posted["content"], want)
	}
	if ids, _ := posted["attachment_ids"].([]any); len(ids) != 1 || ids[0] != "att-1" {
		t.Errorf("attachment_ids = %#v, want [att-1]", posted["attachment_ids"])
	}
	if !strings.Contains(report, "Linked ./a.png in the comment body to the uploaded attachment "+markdownURL) {
		t.Errorf("stderr should report the rewrite, got %q", report)
	}
}

func TestRunIssueCreateKeepsInlineAttachmentInPlace(t *testing.T) {
	withWorkdir(t)
	if err := os.WriteFile("a.png", []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}

	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case isPresignUploadRequest(r):
			// Models a server from before the presigned-upload endpoint.
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/api/upload-file":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "att-1", "filename": "a.png", "content_type": "image/png"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues":
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatalf("decode issue: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "issue-1", "identifier": "MUL-1"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newIssueCreateTestCmd()
	_ = cmd.Flags().Set("title", "inline")
	_ = cmd.Flags().Set("description", "Before\n\n![shot](a.png)\n\nAfter")
	_ = cmd.Flags().Set("attachment", "a.png")
	_ = cmd.Flags().Set("output", "json")
	stderr := captureStderr(t)
	err := runIssueCreate(cmd, nil)
	stderr.read()
	if err != nil {
		t.Fatalf("runIssueCreate: %v", err)
	}
	// No markdown_url from this server: the download path is the fallback, and
	// the inline reference means nothing is appended at the end.
	want := "Before\n\n![shot](/api/attachments/att-1/download)\n\nAfter"
	if posted["description"] != want {
		t.Errorf("description =\n%q\nwant\n%q", posted["description"], want)
	}
}
