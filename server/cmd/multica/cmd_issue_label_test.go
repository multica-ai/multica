package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestRunIssueLabelAddOutput(t *testing.T) {
	t.Chdir(t.TempDir())
	clearDaemonTaskEnv(t)
	const labelID = "22222222-2222-2222-2222-222222222222"
	const labelsJSON = `[{"id":"` + labelID + `","name":"Bug","color":"#ef4444"}]`
	for _, tc := range []struct {
		name     string
		response string
		status   int
		wantJSON string
		wantText string
	}{
		{name: "unavailable list", response: `{}`, wantJSON: `{"attached":true}`, wantText: "Label attached."},
		{name: "null list", response: `{"labels":null}`, wantJSON: `{"attached":true}`, wantText: "Label attached."},
		{name: "invalid list type", response: `{"labels":{}}`, wantJSON: `{"attached":true}`, wantText: "Label attached."},
		{name: "empty list", response: `{"labels":[]}`, wantJSON: `[]`, wantText: "ID NAME COLOR"},
		{name: "nonempty list", response: `{"labels":` + labelsJSON + `}`, wantJSON: labelsJSON},
		{name: "bad request", response: `{"error":"attachment rejected"}`, status: http.StatusBadRequest},
		{name: "forbidden", response: `{"error":"attachment rejected"}`, status: http.StatusForbidden},
		{name: "server error", response: `{"error":"attachment rejected"}`, status: http.StatusInternalServerError},
	} {
		for _, output := range []string{"json", "table"} {
			for _, fullID := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/full-id=%t", tc.name, output, fullID), func(t *testing.T) {
					var requests atomic.Int32
					var posts atomic.Int32
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests.Add(1)
						if r.Method != http.MethodPost || r.URL.Path != "/api/issues/"+testIssueUUID+"/labels" {
							t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
							http.NotFound(w, r)
							return
						}
						posts.Add(1)
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Errorf("decode request: %v", err)
						}
						if !reflect.DeepEqual(body, map[string]any{"label_id": labelID}) {
							t.Errorf("request body = %#v, want label_id only", body)
						}
						w.Header().Set("Content-Type", "application/json")
						if tc.status != 0 {
							w.WriteHeader(tc.status)
						}
						fmt.Fprint(w, tc.response)
					}))
					defer srv.Close()
					setCLITestServerEnv(t, srv.URL)

					cmd := &cobra.Command{Use: "add <issue-id> <label-id>", Args: exactArgs(2), RunE: runIssueLabelAdd, SilenceErrors: true, SilenceUsage: true}
					cmd.Flags().String("output", "table", "")
					cmd.Flags().Bool("full-id", false, "")
					cmd.SetArgs([]string{testIssueUUID, labelID, "--output", output, fmt.Sprintf("--full-id=%t", fullID)})
					out, err := captureStdout(t, cmd.Execute)
					if requests.Load() != 1 || posts.Load() != 1 {
						t.Errorf("requests = %d, POSTs = %d; want exactly one POST and no follow-up requests", requests.Load(), posts.Load())
					}
					if tc.status != 0 {
						var httpErr *cli.HTTPError
						if !errors.As(err, &httpErr) || httpErr.StatusCode != tc.status || !strings.Contains(err.Error(), "attach label:") {
							t.Fatalf("error = %v, want attach label HTTP %d", err, tc.status)
						}
						if out != "" {
							t.Errorf("stdout = %q, want no success output", out)
						}
						return
					}
					if err != nil {
						t.Fatalf("execute add: %v", err)
					}
					if output == "json" {
						var got, want any
						if err := json.Unmarshal([]byte(out), &got); err != nil {
							t.Fatalf("decode stdout: %v; output = %q", err, out)
						}
						if err := json.Unmarshal([]byte(tc.wantJSON), &want); err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(got, want) {
							t.Errorf("stdout = %s, want %s", out, tc.wantJSON)
						}
						return
					}
					wantText := tc.wantText
					if tc.name == "nonempty list" {
						id := "22222222"
						if fullID {
							id = labelID
						}
						wantText = "ID NAME COLOR " + id + " Bug #ef4444"
					}
					if got := strings.Join(strings.Fields(out), " "); got != wantText {
						t.Errorf("stdout = %q, want fields %q", out, wantText)
					}
				})
			}
		}
	}
}
