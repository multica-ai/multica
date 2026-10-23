package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

func newUsageExportTestCmd(serverURL string) *cobra.Command {
	cmd := &cobra.Command{Use: "export"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("from", "2025-03-09", "")
	cmd.Flags().String("to", "2025-03-10", "")
	cmd.Flags().String("timezone", "America/New_York", "")
	cmd.Flags().StringArray("agent", []string{"agent-1", "Agent Two"}, "")
	cmd.Flags().String("runtime", "runtime-1", "")
	cmd.Flags().String("project", "project-1", "")
	cmd.Flags().String("group-by", "agent,model,day", "")
	cmd.Flags().String("output", "json", "")
	_ = cmd.Flags().Set("server-url", serverURL)
	_ = cmd.Flags().Set("workspace-id", "ws-1")
	return cmd
}

func TestRunUsageExportFetchesEveryPageWithoutTruncation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/usage/export" || r.Header.Get("X-Workspace-ID") != "ws-1" {
			t.Fatalf("request = %s, workspace = %q", r.URL.String(), r.Header.Get("X-Workspace-ID"))
		}
		q := r.URL.Query()
		if q.Get("from") != "2025-03-09" || q.Get("to") != "2025-03-10" || q.Get("timezone") != "America/New_York" || q.Get("runtime_id") != "runtime-1" || q.Get("project_id") != "project-1" || q.Get("group_by") != "agent,model,day" || q.Get("page_size") != "1000" {
			t.Fatalf("query = %v", q)
		}
		if got := q["agent"]; len(got) != 2 || got[0] != "agent-1" || got[1] != "Agent Two" {
			t.Fatalf("agent filters = %#v", got)
		}
		resp := usageExportPage{
			From: "2025-03-09T00:00:00-05:00", To: "2025-03-10T00:00:00-04:00",
			Timezone: "America/New_York", GroupBy: []string{"day", "agent", "model"},
			Items: []usageExportItem{{Model: "first", InputTokens: 10}},
		}
		if q.Get("cursor") == "" {
			next := "next-page"
			resp.NextCursor = &next
		} else if q.Get("cursor") == "next-page" {
			resp.Items = []usageExportItem{{Model: "second", InputTokens: 20}}
		} else {
			t.Fatalf("unexpected cursor %q", q.Get("cursor"))
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	out, err := captureRuntimeStdout(t, func() error {
		return runUsageExport(newUsageExportTestCmd(srv.URL), nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	var got usageExportPage
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if len(got.Items) != 2 || got.Items[0].Model != "first" || got.Items[1].Model != "second" || got.NextCursor != nil {
		t.Fatalf("output = %+v", got)
	}
}
