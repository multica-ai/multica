package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Tests for `multica issue metadata list` 404-degradation behavior, plus
// regression coverage that get / set / delete keep real error semantics so
// we don't lose signal when the user actually depends on the metadata
// endpoint working.
//
// Background: GitHub issue multica-ai/multica#3711 — on self-hosted
// backends that pre-date the per-issue metadata route, agent runtime
// bootstrap calls `multica issue metadata list <issue> --output json`
// best-effort and any non-zero exit was being escalated by the Hermes
// provider into a failed agent run. The fix is to treat a 404 from
// /api/issues/{id}/metadata as "this server has no metadata yet" and
// emit `{}` with exit 0 — but only for `list`, since get/set/delete on
// a missing endpoint really are operational failures the caller asked
// for.

const testIssueUUID = "11111111-1111-1111-1111-111111111111"

func newIssueMetadataListTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "list"}
	c.Flags().String("output", "json", "")
	return c
}

func newIssueMetadataGetTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "get"}
	c.Flags().String("output", "json", "")
	c.Flags().String("key", "", "")
	return c
}

func newIssueMetadataSetTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "set"}
	c.Flags().String("output", "json", "")
	c.Flags().String("key", "", "")
	c.Flags().String("value", "", "")
	c.Flags().String("type", "", "")
	return c
}

func newIssueMetadataDeleteTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "delete"}
	c.Flags().String("output", "json", "")
	c.Flags().String("key", "", "")
	return c
}

func TestParseMetadataValue(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		forcedType string
		want       string
		wantErr    bool
	}{
		{name: "nested array", raw: `["frontend",{"release":{"ready":true}}]`, want: `["frontend",{"release":{"ready":true}}]`},
		{name: "nested object", raw: `{"workstreams":[],"qa":{"passed":true}}`, want: `{"workstreams":[],"qa":{"passed":true}}`},
		{name: "empty array", raw: `[]`, want: `[]`},
		{name: "empty object", raw: `{}`, want: `{}`},
		{name: "invalid JSON remains string", raw: `{"broken":`, want: `"{\"broken\":"`},
		{name: "bare string remains string", raw: `waiting`, want: `"waiting"`},
		{name: "bool", raw: `true`, want: `true`},
		{name: "number preserves spelling", raw: `9007199254740993`, want: `9007199254740993`},
		{name: "forced JSON-looking string", raw: `{"nested":true}`, forcedType: "string", want: `"{\"nested\":true}"`},
		{name: "forced null-looking string", raw: `null`, forcedType: "string", want: `"null"`},
		{name: "null rejected", raw: `null`, wantErr: true},
		{name: "whitespace null rejected", raw: " \n null\t", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMetadataValue(tt.raw, tt.forcedType)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseMetadataValue(%q, %q) returned %s, want error", tt.raw, tt.forcedType, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMetadataValue(%q, %q): %v", tt.raw, tt.forcedType, err)
			}
			if string(got) != tt.want {
				t.Fatalf("parseMetadataValue(%q, %q) = %s, want %s", tt.raw, tt.forcedType, got, tt.want)
			}
		})
	}
}

func TestBuildMetadataFilterQueryParamPreservesStructuredValuesAndNumbers(t *testing.T) {
	got, err := buildMetadataFilterQueryParam([]string{
		`workstreams=["frontend",{"release":{"ready":true}}]`,
		`sequence=9007199254740993`,
	})
	if err != nil {
		t.Fatalf("buildMetadataFilterQueryParam: %v", err)
	}
	want := `{"sequence":9007199254740993,"workstreams":["frontend",{"release":{"ready":true}}]}`
	if got != want {
		t.Fatalf("filter = %s, want %s", got, want)
	}
}

func TestRunIssueMetadataRoundTripsStructuredAndForcedStringValues(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		forcedType string
		wantRaw    string
		wantValue  any
	}{
		{
			name:      "structured",
			value:     `{"checks":[],"summary":{"passed":true}}`,
			wantRaw:   `{"checks":[],"summary":{"passed":true}}`,
			wantValue: map[string]any{"checks": []any{}, "summary": map[string]any{"passed": true}},
		},
		{
			name:       "forced string",
			value:      `{"checks":[]}`,
			forcedType: "string",
			wantRaw:    `"{\"checks\":[]}"`,
			wantValue:  `{"checks":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var received json.RawMessage
			_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodPut:
					var body struct {
						Value json.RawMessage `json:"value"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatalf("decode metadata request: %v", err)
					}
					received = append(received[:0], body.Value...)
				case http.MethodGet:
					// Return the value captured from PUT so list and get exercise the
					// same JSON decoding path used against a real API.
				default:
					t.Fatalf("metadata method = %s, want PUT or GET", r.Method)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"metadata": map[string]json.RawMessage{"payload": received},
				})
			})

			cmd := newIssueMetadataSetTestCmd()
			_ = cmd.Flags().Set("key", "payload")
			_ = cmd.Flags().Set("value", tt.value)
			_ = cmd.Flags().Set("output", "json")
			if tt.forcedType != "" {
				_ = cmd.Flags().Set("type", tt.forcedType)
			}

			out, err := captureStdout(t, func() error {
				return runIssueMetadataSet(cmd, []string{testIssueUUID})
			})
			if err != nil {
				t.Fatalf("runIssueMetadataSet: %v", err)
			}
			if string(received) != tt.wantRaw {
				t.Fatalf("request value = %s, want %s", received, tt.wantRaw)
			}
			var response map[string]any
			if err := json.Unmarshal([]byte(out), &response); err != nil {
				t.Fatalf("decode command output: %v\n%s", err, out)
			}
			if !reflect.DeepEqual(response["payload"], tt.wantValue) {
				t.Fatalf("output payload = %#v, want %#v", response["payload"], tt.wantValue)
			}

			listCmd := newIssueMetadataListTestCmd()
			listOut, err := captureStdout(t, func() error {
				return runIssueMetadataList(listCmd, []string{testIssueUUID})
			})
			if err != nil {
				t.Fatalf("runIssueMetadataList: %v", err)
			}
			var listed map[string]any
			if err := json.Unmarshal([]byte(listOut), &listed); err != nil {
				t.Fatalf("decode list output: %v\n%s", err, listOut)
			}
			if !reflect.DeepEqual(listed["payload"], tt.wantValue) {
				t.Fatalf("listed payload = %#v, want %#v", listed["payload"], tt.wantValue)
			}

			getCmd := newIssueMetadataGetTestCmd()
			_ = getCmd.Flags().Set("key", "payload")
			getOut, err := captureStdout(t, func() error {
				return runIssueMetadataGet(getCmd, []string{testIssueUUID})
			})
			if err != nil {
				t.Fatalf("runIssueMetadataGet: %v", err)
			}
			var gotValue any
			if err := json.Unmarshal([]byte(getOut), &gotValue); err != nil {
				t.Fatalf("decode get output: %v\n%s", err, getOut)
			}
			if !reflect.DeepEqual(gotValue, tt.wantValue) {
				t.Fatalf("get payload = %#v, want %#v", gotValue, tt.wantValue)
			}
		})
	}
}

// captureStdout used in this file is the (string, error) helper defined
// in cmd_skill_test.go.

// metadataTestServer wires a minimal fake backend that answers the
// resolveIssueRef GET on /api/issues/<id> and forwards every metadata
// request to the supplied handler. It returns the captured request paths
// in order so callers can assert routing.
func metadataTestServer(t *testing.T, metadataHandler http.HandlerFunc) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+testIssueUUID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         testIssueUUID,
				"identifier": "MUL-1",
				"title":      "test issue",
			})
		case strings.HasPrefix(r.URL.Path, "/api/issues/"+testIssueUUID+"/metadata"):
			metadataHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	// Use the task-token shape so these tests also run inside a daemon-managed
	// agent worktree, where newAPIClient enforces the scoped-token contract.
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	return srv, &paths
}

func TestRunIssueMetadataListDegradesOn404JSON(t *testing.T) {
	var hits int32
	_, paths := metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})

	cmd := newIssueMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "json")

	out, runErr := captureStdout(t, func() error {
		return runIssueMetadataList(cmd, []string{testIssueUUID})
	})
	if runErr != nil {
		t.Fatalf("runIssueMetadataList returned error on 404, want nil: %v", runErr)
	}
	if got := strings.TrimSpace(out); got != "{}" {
		t.Fatalf("stdout = %q, want %q (empty JSON object on 404 degradation)", got, "{}")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("metadata endpoint hits = %d, want 1; routing: %v", got, *paths)
	}
}

func TestRunIssueMetadataListDegradesOn404Table(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such route", http.StatusNotFound)
	})

	cmd := newIssueMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "table")

	out, runErr := captureStdout(t, func() error {
		return runIssueMetadataList(cmd, []string{testIssueUUID})
	})
	if runErr != nil {
		t.Fatalf("runIssueMetadataList returned error on 404 table mode: %v", runErr)
	}
	// Table mode prints headers even with zero rows; the important
	// invariant is just that exit is clean and the row table doesn't
	// surface a stack trace or error blob.
	if !strings.Contains(out, "KEY") {
		t.Fatalf("table output missing KEY header, got %q", out)
	}
	if strings.Contains(strings.ToLower(out), "error") || strings.Contains(out, "404") {
		t.Fatalf("table output unexpectedly leaked error text: %q", out)
	}
}

func TestRunIssueMetadataListSuccessReturnsServerMetadata(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metadata": map[string]any{
				"pr_url":          "https://example.com/pr/1",
				"pipeline_status": "waiting_review",
			},
		})
	})

	cmd := newIssueMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "json")

	out, runErr := captureStdout(t, func() error {
		return runIssueMetadataList(cmd, []string{testIssueUUID})
	})
	if runErr != nil {
		t.Fatalf("runIssueMetadataList: %v", runErr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out)
	}
	if got["pr_url"] != "https://example.com/pr/1" || got["pipeline_status"] != "waiting_review" {
		t.Fatalf("stdout = %#v, missing expected keys", got)
	}
}

// 5xx and other non-404 errors must keep real error semantics — we only
// want to mask "this server has no metadata endpoint", not "the server
// is broken".
func TestRunIssueMetadataListPropagatesNon404Error(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	cmd := newIssueMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "json")

	// Drop stdout to keep test output clean even if the implementation
	// regresses and prints something.
	_, err := captureStdout(t, func() error {
		return runIssueMetadataList(cmd, []string{testIssueUUID})
	})
	if err == nil {
		t.Fatal("runIssueMetadataList returned nil on 500, want error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v, want it to mention status 500", err)
	}
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error chain missing *cli.HTTPError: %v", err)
	}
	if httpErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("HTTPError.StatusCode = %d, want 500", httpErr.StatusCode)
	}
}

func TestRunIssueMetadataListPropagates401Error(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})

	cmd := newIssueMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdout(t, func() error {
		return runIssueMetadataList(cmd, []string{testIssueUUID})
	})
	if err == nil {
		t.Fatal("runIssueMetadataList returned nil on 401, want error")
	}
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 *cli.HTTPError, got %v", err)
	}
}

// get/set/delete must NOT degrade on 404 — those calls represent real
// caller intent and the user needs to see the failure.
func TestRunIssueMetadataGetReturnsErrorOn404(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	cmd := newIssueMetadataGetTestCmd()
	_ = cmd.Flags().Set("key", "pr_url")
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdout(t, func() error {
		return runIssueMetadataGet(cmd, []string{testIssueUUID})
	})
	if err == nil {
		t.Fatal("runIssueMetadataGet returned nil on 404, want error")
	}
}

func TestRunIssueMetadataSetReturnsErrorOn404(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// PUT /metadata/<key> on an old server — must surface the failure.
		http.Error(w, "not found", http.StatusNotFound)
	})

	cmd := newIssueMetadataSetTestCmd()
	_ = cmd.Flags().Set("key", "pr_url")
	_ = cmd.Flags().Set("value", "https://example.com/pr/1")
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdout(t, func() error {
		return runIssueMetadataSet(cmd, []string{testIssueUUID})
	})
	if err == nil {
		t.Fatal("runIssueMetadataSet returned nil on 404, want error")
	}
	if !strings.Contains(err.Error(), "set metadata") {
		t.Fatalf("error = %v, want it wrapped with 'set metadata' prefix", err)
	}
}

func TestRunIssueMetadataDeleteReturnsErrorOn404(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	cmd := newIssueMetadataDeleteTestCmd()
	_ = cmd.Flags().Set("key", "pr_url")
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdout(t, func() error {
		return runIssueMetadataDelete(cmd, []string{testIssueUUID})
	})
	if err == nil {
		t.Fatal("runIssueMetadataDelete returned nil on 404, want error")
	}
	if !strings.Contains(err.Error(), "delete metadata") {
		t.Fatalf("error = %v, want it wrapped with 'delete metadata' prefix", err)
	}
}
