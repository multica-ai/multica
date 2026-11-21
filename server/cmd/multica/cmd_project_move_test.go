package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestProjectMoveRegistered(t *testing.T) {
	command, _, err := projectCmd.Find([]string{"move"})
	if err != nil || command != projectMoveCmd {
		t.Fatalf("move subcommand not registered: %v", err)
	}
	for _, name := range []string{"to", "preview", "yes", "output"} {
		if command.Flags().Lookup(name) == nil {
			t.Errorf("missing flag %s", name)
		}
	}
}

func TestProjectMoveCommand(t *testing.T) {
	const project = "11111111-1111-4111-8111-111111111111"
	const target = "22222222-2222-4222-8222-222222222222"
	for _, tc := range []struct {
		name                              string
		preview, yes, blockers, malformed bool
		to, wantError                     string
		wantMove                          int
	}{
		{name: "preview", preview: true, to: "target", wantMove: 0},
		{name: "confirmation required", to: target, wantError: "--yes", wantMove: 0},
		{name: "confirmed slug", yes: true, to: "target", wantMove: 1},
		{name: "confirmed id", yes: true, to: target, wantMove: 1},
		{name: "blocked", yes: true, blockers: true, to: target, wantError: "blockers", wantMove: 0},
		{name: "malformed", yes: true, malformed: true, to: target, wantError: "plan", wantMove: 0},
		{name: "missing target", wantError: "--to", wantMove: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			moved := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/workspaces":
					json.NewEncoder(w).Encode([]map[string]any{{"id": target, "slug": "target", "name": "Target"}})
				case "/api/projects/" + project + "/move/preview":
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if body["target_workspace_id"] != target || r.Header.Get("X-Workspace-ID") != "ws-1" {
						t.Errorf("wrong preview workspace: %#v %v", body, r.Header)
					}
					plan := map[string]any{"plan_hash": strings.Repeat("a", 64), "blockers": []any{}, "counts": map[string]any{"issues": 2}}
					if tc.blockers {
						plan["blockers"] = []any{map[string]any{"kind": "active_task"}}
					}
					if tc.malformed {
						delete(plan, "plan_hash")
					}
					json.NewEncoder(w).Encode(plan)
				case "/api/projects/" + project + "/move":
					moved++
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if body["target_workspace_id"] != target || body["plan_hash"] != strings.Repeat("a", 64) {
						t.Errorf("move did not quote preview: %#v", body)
					}
					json.NewEncoder(w).Encode(map[string]any{"moved": true})
				default:
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			setCopyTestEnv(t, srv.URL)
			cmd := &cobra.Command{Use: "move"}
			registerProjectMoveFlags(cmd)
			cmd.Flags().String("profile", "", "")
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.Flags().Set("to", tc.to)
			if tc.preview {
				cmd.Flags().Set("preview", "true")
			}
			if tc.yes {
				cmd.Flags().Set("yes", "true")
			}
			err := runProjectMove(cmd, []string{project})
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("move: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("want error containing %q got %v", tc.wantError, err)
			}
			if moved != tc.wantMove {
				t.Errorf("execute calls: want %d got %d", tc.wantMove, moved)
			}
			if tc.preview || tc.wantError == "--yes" {
				var plan map[string]any
				if err := json.Unmarshal(out.Bytes(), &plan); err != nil || plan["plan_hash"] == nil {
					t.Errorf("preview not printed: %s", out.String())
				}
			}
		})
	}
}
