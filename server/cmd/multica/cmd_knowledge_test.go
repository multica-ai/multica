package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newKnowledgeTestCmd(t *testing.T, addFlags func(*cobra.Command), args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "knowledge-test"}
	addFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return cmd
}

func TestKnowledgeAddInputNeedsExactlyOneSource(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(file, []byte("# Notes\n\nAcme sells anvils.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.md")
	if err := os.WriteFile(big, make([]byte, knowledgeFileLimit+1), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "bin")
	if err := os.WriteFile(binary, []byte{0xff, 0xfe, 0x00}, 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		in      knowledgeAddInput
		want    string
		wantErr string
	}{
		{"file", knowledgeAddInput{KB: "market", Title: "t", File: file}, "# Notes\n\nAcme sells anvils.\n", ""},
		{"text", knowledgeAddInput{KB: "market", Title: "t", Text: "hello"}, "hello", ""},
		{"url", knowledgeAddInput{KB: "market", Title: "t", URL: "https://example.test"}, "", ""},
		{"no kb", knowledgeAddInput{Title: "t", Text: "x"}, "", "--kb is required"},
		{"no title", knowledgeAddInput{KB: "market", Text: "x"}, "", "--title is required"},
		{"none", knowledgeAddInput{KB: "market", Title: "t"}, "", "exactly one of"},
		{"two", knowledgeAddInput{KB: "market", Title: "t", Text: "x", URL: "https://example.test"}, "", "exactly one of"},
		{"missing file", knowledgeAddInput{KB: "market", Title: "t", File: filepath.Join(dir, "none")}, "", "--file"},
		{"big file", knowledgeAddInput{KB: "market", Title: "t", File: big}, "", "larger than"},
		{"binary file", knowledgeAddInput{KB: "market", Title: "t", File: binary}, "", "UTF-8"},
	}
	for _, c := range cases {
		got, err := c.in.content()
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s: content = %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}

func TestKnowledgeRetryKeyIsStableAndSpecific(t *testing.T) {
	in := knowledgeAddInput{KB: "market", Title: "t"}
	a := in.retryKey("body", "")
	if a != in.retryKey("body", "") || !strings.HasPrefix(a, "cli:") {
		t.Fatalf("key = %q", a)
	}
	if a == in.retryKey("other body", "") || a == in.retryKey("body", "issue-1") {
		t.Fatal("different content or source must give a different key")
	}
	in.IdempotencyKey = "mine"
	if in.retryKey("body", "") != "mine" {
		t.Fatal("an explicit key wins")
	}
}

func TestKnowledgeAddSendsTheExpectedBody(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/issues/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "33333333-3333-3333-3333-333333333333", "identifier": "MUL-9"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/knowledge/documents":
			_ = json.NewDecoder(r.Body).Decode(&got)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "doc-1", "state": "draft", "knowledge_base": "market", "title": "Acme"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runKnowledgeAdd(newKnowledgeTestCmd(t, addKnowledgeAddFlags,
			"--kb", "market", "--title", "Acme", "--text", "Acme sells anvils.", "--source-issue", "33333333-3333-3333-3333-333333333333"), nil)
	})
	if err != nil {
		t.Fatalf("runKnowledgeAdd: %v", err)
	}
	if got["kb"] != "market" || got["title"] != "Acme" || got["content"] != "Acme sells anvils." ||
		got["source_type"] != "issue" || got["source_id"] != "33333333-3333-3333-3333-333333333333" {
		t.Fatalf("body = %#v", got)
	}
	if key, _ := got["idempotency_key"].(string); !strings.HasPrefix(key, "cli:") {
		t.Fatalf("body must carry a retry key: %#v", got)
	}
	if _, ok := got["url"]; ok {
		t.Fatalf("a text draft must not send a url: %#v", got)
	}
	if _, ok := got["publish"]; ok {
		t.Fatalf("the CLI never asks to publish: %#v", got)
	}
	if !strings.Contains(out, "Saved as draft: doc-1") || !strings.Contains(out, "A person must publish it") {
		t.Fatalf("output = %q", out)
	}
}

func TestKnowledgeAddURLSendsOnlyTheURL(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "doc-2", "state": "draft"})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	if _, err := captureStdout(t, func() error {
		return runKnowledgeAdd(newKnowledgeTestCmd(t, addKnowledgeAddFlags, "--kb", "market", "--title", "Site", "--url", "https://example.test/p"), nil)
	}); err != nil {
		t.Fatal(err)
	}
	if got["url"] != "https://example.test/p" {
		t.Fatalf("body = %#v", got)
	}
	if _, ok := got["content"]; ok {
		t.Fatalf("a URL draft must not send content: %#v", got)
	}
}

func TestKnowledgeOfflineAndNotReadyHaveTheirOwnMessages(t *testing.T) {
	for _, c := range []struct {
		status int
		body   map[string]any
		want   string
	}{
		{http.StatusServiceUnavailable, map[string]any{"error": "the knowledge service is offline", "state": "offline"}, "knowledge service is offline; nothing was saved"},
		{http.StatusConflict, map[string]any{"error": "x", "state": "disabled"}, "not available in this workspace (state: disabled)"},
		{http.StatusBadRequest, map[string]any{"error": "title is required"}, "title is required"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_ = json.NewEncoder(w).Encode(c.body)
		}))
		setCLITestServerEnv(t, srv.URL)
		_, err := captureStdout(t, func() error {
			return runKnowledgeAdd(newKnowledgeTestCmd(t, addKnowledgeAddFlags, "--kb", "market", "--title", "t", "--text", "x"), nil)
		})
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("status %d: err = %v, want %q", c.status, err, c.want)
		}
	}
}

func TestKnowledgeListSearchPublishDiscard(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.URL.Path == "/api/knowledge/documents" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "offline", "documents": []map[string]any{
				{"id": "d1", "knowledge_base": "market", "state": "draft", "title": "Acme"},
			}})
		case r.URL.Path == "/api/knowledge/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "ready", "hits": []map[string]any{
				{"knowledge_base": "company", "title": "Charter", "score": 0.81, "content": "We ship\nweekly."},
			}})
		case strings.HasSuffix(r.URL.Path, "/publish"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "d1", "title": "Acme", "state": "published"})
		case strings.HasSuffix(r.URL.Path, "/discard"):
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runKnowledgeList(newKnowledgeTestCmd(t, addKnowledgeListFlags, "--kb", "market", "--state", "draft"), nil)
	})
	if err != nil || !strings.Contains(out, "d1") || !strings.Contains(out, "draft") || !strings.Contains(out, "Acme") {
		t.Fatalf("list = %q, %v", out, err)
	}
	if err := runKnowledgeList(newKnowledgeTestCmd(t, addKnowledgeListFlags, "--state", "weird"), nil); err == nil {
		t.Fatal("an unknown --state must fail before any request")
	}

	out, err = captureStdout(t, func() error {
		return runKnowledgeSearch(newKnowledgeTestCmd(t, addKnowledgeSearchFlags, "--kb", "company", "--limit", "3"), []string{"ship cadence"})
	})
	if err != nil || !strings.Contains(out, "[company] Charter (score 0.81)") || !strings.Contains(out, "We ship weekly.") {
		t.Fatalf("search = %q, %v", out, err)
	}

	out, err = captureStdout(t, func() error {
		return runKnowledgePublish(newKnowledgeTestCmd(t, addKnowledgeActionFlags), []string{"d1"})
	})
	if err != nil || !strings.Contains(out, "Published: d1") {
		t.Fatalf("publish = %q, %v", out, err)
	}
	out, err = captureStdout(t, func() error {
		return runKnowledgeDiscard(newKnowledgeTestCmd(t, addKnowledgeActionFlags), []string{"d1"})
	})
	if err != nil || !strings.Contains(out, "Discarded: d1") {
		t.Fatalf("discard = %q, %v", out, err)
	}

	want := []string{
		"GET /api/knowledge/documents?kb=market&state=draft",
		"GET /api/knowledge/search?kb=company&limit=3&q=ship+cadence",
		"POST /api/knowledge/documents/d1/publish",
		"POST /api/knowledge/documents/d1/discard",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests =\n%s\nwant\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}
