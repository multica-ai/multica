package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunIssueStagesSetsAndClearsTheSquad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	const issueID = "11111111-1111-4111-8111-111111111111"
	var puts []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/squads":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "squad-1", "name": "Разработка"}, {"id": "squad-2", "name": "Ops"}})
		case strings.HasSuffix(r.URL.Path, "/stage-dispatch") && r.Method == http.MethodPut:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			puts = append(puts, body)
			_ = json.NewEncoder(w).Encode(map[string]any{"issue_id": issueID, "squad_id": body["squad_id"]})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	newCmd := func(flags map[string]string) *cobra.Command {
		cmd := &cobra.Command{Use: "stages"}
		for _, f := range []string{"server-url", "workspace-id", "profile", "squad"} {
			cmd.Flags().String(f, "", "")
		}
		cmd.Flags().Bool("off", false, "")
		cmd.Flags().String("output", "json", "")
		for k, v := range flags {
			_ = cmd.Flags().Set(k, v)
		}
		return cmd
	}
	if err := runIssueStages(newCmd(map[string]string{"squad": "разработка"}), []string{issueID}); err != nil {
		t.Fatal(err)
	}
	if err := runIssueStages(newCmd(map[string]string{"off": "true"}), []string{issueID}); err != nil {
		t.Fatal(err)
	}
	if len(puts) != 2 || puts[0]["squad_id"] != "squad-1" || puts[1]["squad_id"] != nil {
		t.Fatalf("puts = %#v", puts)
	}
	if err := runIssueStages(newCmd(map[string]string{"squad": "nobody"}), []string{issueID}); err == nil {
		t.Fatal("an unknown squad was accepted")
	}
}
