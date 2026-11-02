package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestMediaCLIReceiptAndExplicitDownload(t *testing.T) {
	id, attachment := uuid.NewString(), uuid.NewString()
	var downloads, posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/media/jobs":
			posts.Add(1)
			var request protocol.MediaRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if request.Prompt != "fixture prompt" || request.Provider != "auto" {
				t.Errorf("request: %+v", request)
			}
			w.WriteHeader(202)
			json.NewEncoder(w).Encode(protocol.MediaJob{ID: id, State: "queued"})
		case "/api/media/jobs/" + id:
			json.NewEncoder(w).Encode(protocol.MediaJob{ID: id, State: "completed", AttachmentID: attachment, MarkdownURL: "https://example.test/image", Markdown: "![image](https://example.test/image)"})
		case "/api/attachments/" + attachment + "/download":
			downloads.Add(1)
			w.Write([]byte("image bytes"))
		case "/api/media/jobs/" + id + "/cancel":
			json.NewEncoder(w).Encode(protocol.MediaJob{ID: id, State: "cancelled"})
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	setCLITestServerEnv(t, server.URL)
	t.Setenv("MULTICA_TOKEN", "mat_fixture")
	run := func(args ...string) string {
		t.Helper()
		cmd := newMediaCmd()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		return output.String()
	}
	prompt := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(prompt, []byte("fixture prompt"), 0600); err != nil {
		t.Fatal(err)
	}
	result := run("generate", "--prompt-file", prompt, "--output", "json")
	var job protocol.MediaJob
	if err := json.Unmarshal([]byte(result), &job); err != nil {
		t.Fatal(err)
	}
	if job.AttachmentID != attachment || job.Markdown == "" || downloads.Load() != 0 {
		t.Fatal("no-out must return a receipt without binary downloads")
	}
	out := filepath.Join(t.TempDir(), "image.png")
	run("job", "wait", id, "--out", out, "--output", "json")
	data, err := os.ReadFile(out)
	if err != nil || string(data) != "image bytes" || downloads.Load() != 1 {
		t.Fatal("--out did not write bytes")
	}
	run("job", "get", id)
	run("job", "cancel", id)
	run("generate", "--prompt", "fixture prompt", "--async")
	if posts.Load() != 2 {
		t.Fatal("unexpected generate count")
	}
	for _, args := range [][]string{{"generate", "--prompt", "x", "--prompt-file", prompt}, {"generate", "--prompt", "x", "--async", "--out", out}, {"generate", "--prompt", "x", "--capability", "bad"}, {"job", "get", "bad-id"}} {
		cmd := newMediaCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		if cmd.Execute() == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
	if posts.Load() != 2 {
		t.Fatal("invalid arguments submitted work")
	}
}

func TestMediaCLIRetiredOptionsDoNotUploadOrSubmit(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	setCLITestServerEnv(t, server.URL)
	t.Setenv("MULTICA_TOKEN", "mat_fixture")
	input := filepath.Join(t.TempDir(), "reference.png")
	if err := os.WriteFile(input, []byte("reference"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][]string{{"--provider", "flow"}, {"--provider", "whisk"}, {"--capability", "video-generate"}, {"--capability", "image-remix"}, {"--duration", "8"}} {
		cmd := newMediaCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(append([]string{"generate", "--prompt", "image", "--input", input}, flags...))
		if cmd.Execute() == nil {
			t.Fatalf("retired option accepted: %v", flags)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("retired options uploaded or submitted work")
	}
}
