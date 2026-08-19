package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newSlackListsTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "lists"}
	cmd.Flags().String("json", "", "")
	return cmd
}

func TestRunSlackListsSchemaAndCreate(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "xoxb-") || strings.Contains(r.Header.Get("Authorization"), "xoxb-") {
			t.Fatal("slack bot token must not appear on the Multica request")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/slack/lists/F0BR8PBUAQH/schema":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"list_id": "F0BR8PBUAQH",
				"title":   "Daykee 灵感池",
				"columns": []map[string]any{{"id": "ColT", "name": "Title"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/slack/lists/F0BR8PBUAQH/items":
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatalf("decode: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "Rec1", "list_id": "F0BR8PBUAQH", "title": "lamp"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	out, err := captureStdout(t, func() error {
		return runSlackListsSchema(newSlackListsTestCmd(), []string{"F0BR8PBUAQH"})
	})
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	if !strings.Contains(out, "F0BR8PBUAQH") || strings.Contains(out, "xoxb-") {
		t.Fatalf("schema stdout = %s", out)
	}

	cmd := newSlackListsTestCmd()
	if err := cmd.Flags().Set("json", `{"灵感标题":"lamp"}`); err != nil {
		t.Fatal(err)
	}
	out, err = captureStdout(t, func() error { return runSlackListsCreate(cmd, []string{"F0BR8PBUAQH"}) })
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(out, "Rec1") {
		t.Fatalf("create stdout = %s", out)
	}
	fields, _ := createBody["fields"].(map[string]any)
	if fields["灵感标题"] != "lamp" {
		t.Fatalf("posted fields = %#v", createBody)
	}
}

func TestRunSlackListsCreateSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"List F0BPZZZZZZZ is not an allowed Slack Lists target for this agent's Slack app."}`)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	cmd := newSlackListsTestCmd()
	_ = cmd.Flags().Set("json", `{"Title":"x"}`)
	err := runSlackListsCreate(cmd, []string{"F0BPZZZZZZZ"})
	if err == nil || !strings.Contains(err.Error(), "F0BPZZZZZZZ") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "xoxb-") {
		t.Fatalf("token leaked: %v", err)
	}
}
