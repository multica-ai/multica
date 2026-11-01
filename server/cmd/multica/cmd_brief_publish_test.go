package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func newBriefPublishTestCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "publish"}
	addBriefPublishFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return cmd
}

func writeBriefFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBriefPublishSendsTheBody(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/briefs" {
			t.Errorf("request = %s %s, want POST /api/briefs", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "b-1"})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	file := writeBriefFile(t, "## Headline\nQuiet.\n")
	out, err := captureStdout(t, func() error {
		return runBriefPublish(newBriefPublishTestCmd(t,
			"--kind", "weekly", "--title", "Weekly review", "--file", file,
			"--proposal", "p-1", "--proposal", "p-2"), nil)
	})
	if err != nil {
		t.Fatalf("runBriefPublish: %v", err)
	}
	want := map[string]any{
		"kind": "weekly", "title": "Weekly review", "body": "## Headline\nQuiet.\n",
		"proposal_ids": []any{"p-1", "p-2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %#v\nwant  %#v", got, want)
	}
	if !strings.Contains(out, "b-1") {
		t.Fatalf("output should name the brief:\n%s", out)
	}
}

func TestBriefPublishValidatesFlagsLocally(t *testing.T) {
	file := writeBriefFile(t, "x")
	cases := map[string][]string{
		"--kind":  {"--title", "t", "--file", file},
		"invalid": {"--kind", "hourly", "--title", "t", "--file", file},
		"--title": {"--kind", "daily", "--file", file},
		"--file":  {"--kind", "daily", "--title", "t"},
	}
	for want, args := range cases {
		err := runBriefPublish(newBriefPublishTestCmd(t, args...), nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("args %v: err = %v, want it to mention %q", args, err, want)
		}
	}
}

func TestBriefPublishShowsTheServersRefusalUnchanged(t *testing.T) {
	const message = `body is missing the required heading(s) ## Watch; a brief needs these H2 headings, in this order: ...`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": message})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	file := writeBriefFile(t, "## Headline\n")
	err := runBriefPublish(newBriefPublishTestCmd(t, "--kind", "daily", "--title", "t", "--file", file), nil)
	if err == nil || !strings.Contains(cli.FormatError(err, false), "## Watch") {
		t.Fatalf("the server's message should reach the agent, got %v", err)
	}
}

func TestBriefListAndShow(t *testing.T) {
	const briefID = "11111111-1111-1111-1111-111111111111"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/briefs":
			if r.URL.Query().Get("kind") != "daily" {
				t.Errorf("kind filter not sent: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "b-1", "kind": "daily", "title": "Daily brief", "created_at": "2026-05-10T08:00:00Z"}})
		case "/api/briefs/" + briefID:
			_ = json.NewEncoder(w).Encode(map[string]any{"title": "Daily brief", "kind": "daily", "body": "## Headline\nQuiet."})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	listCmd := &cobra.Command{Use: "list"}
	addBriefListFlags(listCmd)
	_ = listCmd.ParseFlags([]string{"--kind", "daily"})
	out, err := captureStdout(t, func() error { return runBriefList(listCmd, nil) })
	if err != nil || !strings.Contains(out, "Daily brief") {
		t.Fatalf("list: %v\n%s", err, out)
	}

	showCmd := &cobra.Command{Use: "show"}
	addBriefShowFlags(showCmd)
	out, err = captureStdout(t, func() error { return runBriefShow(showCmd, []string{briefID}) })
	if err != nil || !strings.Contains(out, "## Headline") {
		t.Fatalf("show: %v\n%s", err, out)
	}
	if err := runBriefShow(showCmd, []string{"not-an-id"}); err == nil {
		t.Fatalf("show should reject a non-id before calling the server")
	}
}
