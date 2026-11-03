package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

const baselineTestProject = "10000000-0000-4000-8000-000000000001"
const baselineTestBase = "20000000-0000-4000-8000-000000000001"
const baselineTestCandidate = "30000000-0000-4000-8000-000000000001"
const baselineTestIssue = "40000000-0000-4000-8000-000000000001"

func baselineTestRevision(chapter string, n int, status string) map[string]any {
	id := baselineTestCandidate
	if n == 1 {
		id = baselineTestBase
	}
	var base any
	if n > 1 {
		base = baselineTestBase
	}
	if chapter == "architecture" {
		id = "30000000-0000-4000-8000-000000000002"
		base = "20000000-0000-4000-8000-000000000002"
	}
	return map[string]any{
		"id": id, "chapter": chapter, "revision_number": n, "status": status, "base_revision_id": base,
		"content_markdown": "### [Rule-中文] 保留规则\nMUST keep body\n", "change_summary": "中文摘要", "decided_at": "2026-10-30T00:00:00Z",
		"decision_credential": "agent_task", "future_audit_field": "preserved",
		"anchors_diff": map[string]any{"chapter": chapter, "base_revision_number": 1, "added": []any{}, "modified": []any{},
			"removed": []any{map[string]any{"anchor": "Rule-中文", "title": "保留规则", "before": "MUST keep body"}}, "unchanged_count": 0,
			"warnings": []any{map[string]any{"code": "rule_removed", "anchor": "Rule-中文"}, map[string]any{"code": "rule_shrunk", "anchor": "Rule-2"}, map[string]any{"code": "rule_deferred", "anchor": "Rule-3"}, map[string]any{"code": "defer_missing_pool", "anchor": "Defer-4"}}},
	}
}
func baselineTestState(adopted, candidate any) map[string]any {
	chapters := []any{}
	for _, chapter := range []string{"overview", "domain_rules", "architecture", "exclusions_and_risks"} {
		a, c := any(nil), any(nil)
		if chapter == "domain_rules" {
			a, c = adopted, candidate
		}
		chapters = append(chapters, map[string]any{"chapter": chapter, "adopted": a, "candidate": c})
	}
	return map[string]any{"engineering": map[string]any{"enabled": true}, "chapters": chapters}
}

type baselineTestCall struct {
	Method, Path string
	Query        url.Values
	Body         map[string]any
}

func serveBaseline(t *testing.T, respond func(http.ResponseWriter, *http.Request, map[string]any)) (*httptest.Server, func() []baselineTestCall) {
	t.Helper()
	var mu sync.Mutex
	calls := []baselineTestCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mat_baseline_test" || r.Header.Get("X-Workspace-ID") != "offline-workspace" {
			t.Error("request lost task credentials or workspace")
		}
		var body map[string]any
		if r.Method == "POST" || r.Method == "PATCH" {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
		}
		mu.Lock()
		calls = append(calls, baselineTestCall{r.Method, r.URL.Path, r.URL.Query(), body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		respond(w, r, body)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_baseline_test")
	t.Setenv("MULTICA_WORKSPACE_ID", "offline-workspace")
	return srv, func() []baselineTestCall { mu.Lock(); defer mu.Unlock(); return append([]baselineTestCall{}, calls...) }
}
func executeBaseline(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	root := &cobra.Command{Use: "multica", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().String("profile", "", "")
	root.PersistentFlags().String("server-url", "", "")
	root.PersistentFlags().String("workspace-id", "", "")
	project := &cobra.Command{Use: "project"}
	project.AddCommand(newLabrastroProjectBaselineCmd())
	root.AddCommand(project)
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"project", "baseline"}, args...))
	cmd, err := root.ExecuteC()
	if err != nil {
		fmt.Fprintln(&stderr, cli.FormatError(err, false))
	}
	return out.String(), stderr.String(), labrastroBaselineExitCode(cmd, err)
}
func baselineJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, s)
	}
	return m
}
func writeBaselineInput(t *testing.T) string {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.WriteFile("proposal.md", []byte("### [Rule-中文] 保留规则\nSHOULD keep body\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return "proposal.md"
}

func TestLabrastroBaselineRegistration(t *testing.T) {
	for _, action := range []string{"get", "propose", "diff", "adopt", "export"} {
		cmd, _, err := rootCmd.Find([]string{"project", "baseline", action})
		if err != nil || cmd.Name() != action {
			t.Fatalf("%s is not registered", action)
		}
	}
}
func TestLabrastroBaselineReadAndDiff(t *testing.T) {
	a, c := baselineTestRevision("domain_rules", 1, "adopted"), baselineTestRevision("domain_rules", 2, "candidate")
	_, calls := serveBaseline(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		if strings.HasSuffix(r.URL.Path, "/engineering") {
			json.NewEncoder(w).Encode(baselineTestState(a, c))
			return
		}
		if r.URL.Query().Get("diff_from") != "" {
			if r.URL.Query().Get("diff_from") != "1" || r.URL.Query().Get("diff_to") != "2" {
				t.Error("wrong historical Diff range")
			}
			json.NewEncoder(w).Encode(c["anchors_diff"])
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"revisions": []any{a}})
	})
	out, stderr, code := executeBaseline(t, "get", baselineTestProject)
	if code != 0 || stderr != "" || !strings.Contains(out, "domain_rules: revision 1") || strings.Contains(out, "MUST keep body") {
		t.Fatalf("summary: %d %q %q", code, out, stderr)
	}
	out, _, code = executeBaseline(t, "get", baselineTestProject, "--chapter", "domain_rules", "--candidate", "--output", "json")
	rev := baselineJSON(t, out)["chapters"].([]any)[0].(map[string]any)["revision"]
	if code != 0 || !reflect.DeepEqual(rev, baselineJSONRoundtrip(t, c)) {
		t.Fatalf("candidate JSON lost revision/audit fields: %s", out)
	}
	out, _, code = executeBaseline(t, "get", baselineTestProject, "--revision", "domain_rules:1", "--output", "markdown")
	if code != 0 || !strings.Contains(out, "MUST keep body") || !strings.Contains(out, "Revision: 1") {
		t.Fatalf("historical Markdown: %d %s", code, out)
	}
	last := calls()[len(calls())-1]
	if last.Query.Get("before") != "2" || last.Query.Get("limit") != "1" {
		t.Fatalf("history lookup was not bounded: %+v", last)
	}
	out, stderr, code = executeBaseline(t, "diff", baselineTestProject, "--chapter", "domain_rules", "--output", "json")
	if code != 0 || len(baselineJSON(t, out)["diffs"].([]any)) != 1 || strings.Count(stderr, "[BASELINE WARNING]") != 4 || !strings.Contains(stderr, "[BASELINE WARNING]") {
		t.Fatalf("warnings: %d %s %s", code, out, stderr)
	}
	out, _, code = executeBaseline(t, "diff", baselineTestProject, "--chapter", "domain_rules", "--from", "1", "--to", "2")
	if code != 0 || !strings.Contains(out, "-MUST keep body") || !strings.Contains(out, "[Rule-中文]") {
		t.Fatalf("server Diff not rendered: %d %s", code, out)
	}
}
func baselineJSONRoundtrip(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var result any
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestLabrastroBaselineProposeReviewLink(t *testing.T) {
	for _, mode := range []string{"text", "json", "metadata unavailable"} {
		t.Run(mode, func(t *testing.T) {
			file := writeBaselineInput(t)
			t.Setenv("MULTICA_APP_URL", "https://workspace.example")
			_, calls := serveBaseline(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/engineering"):
					json.NewEncoder(w).Encode(baselineTestState(nil, nil))
				case strings.HasSuffix(r.URL.Path, "/candidates"):
					w.WriteHeader(201)
					json.NewEncoder(w).Encode(baselineTestRevision("domain_rules", 2, "candidate"))
				case r.URL.Path == "/api/workspaces/offline-workspace":
					if mode == "metadata unavailable" {
						w.WriteHeader(503)
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"slug": "chosen-workspace"})
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(404)
				}
			})
			format := "text"
			if mode == "json" {
				format = "json"
			}
			out, stderr, code := executeBaseline(t, "propose", baselineTestProject, "--chapter", "domain_rules", "--summary", "change", "--content-file", file, "--output", format)
			if code != 0 {
				t.Fatalf("proposal failed: %s", stderr)
			}
			if !strings.Contains(stderr, "[BASELINE WARNING]") {
				t.Fatalf("missing warning: %s", stderr)
			}
			link := "https://workspace.example/chosen-workspace/projects/" + baselineTestProject + "/engineering?candidate=" + baselineTestCandidate
			switch mode {
			case "text":
				if !strings.Contains(stderr, link) || strings.Contains(out, link) {
					t.Fatalf("wrong review link streams: %s / %s", out, stderr)
				}
			case "json":
				baselineJSON(t, out)
				if strings.Contains(stderr, "Review candidate") {
					t.Fatalf("unexpected JSON guidance: %s", stderr)
				}
				for _, call := range calls() {
					if strings.HasPrefix(call.Path, "/api/workspaces/") {
						t.Fatal("JSON proposal looked up review link")
					}
				}
			default:
				if !strings.Contains(stderr, "engineering overview on the dashboard (approval link unavailable)") {
					t.Fatalf("missing dashboard fallback: %s", stderr)
				}
			}
		})
	}
}

func TestLabrastroBaselineProposeCAS(t *testing.T) {
	for _, mode := range []string{"first", "explicit base", "current base", "replace", "existing candidate", "missing base", "racing base", "racing candidate"} {
		t.Run(mode, func(t *testing.T) {
			file := writeBaselineInput(t)
			a, c := baselineTestRevision("domain_rules", 1, "adopted"), baselineTestRevision("domain_rules", 2, "candidate")
			var reads atomic.Int32
			_, calls := serveBaseline(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/engineering"):
					read := reads.Add(1)
					var adopted, candidate any
					if mode != "first" {
						adopted = a
					}
					if mode == "replace" || mode == "existing candidate" || mode == "racing candidate" && read > 1 {
						candidate = c
					}
					json.NewEncoder(w).Encode(baselineTestState(adopted, candidate))
				case strings.HasSuffix(r.URL.Path, "/revisions"):
					rev := a
					if r.URL.Query().Get("before") == "3" {
						rev = c
					}
					json.NewEncoder(w).Encode(map[string]any{"revisions": []any{rev}})
				case r.URL.Path == "/api/issues/OL-93":
					json.NewEncoder(w).Encode(map[string]any{"id": baselineTestIssue, "identifier": "OL-93"})
				case strings.HasSuffix(r.URL.Path, "/candidates"):
					if mode == "racing base" || mode == "racing candidate" {
						w.WriteHeader(409)
						code := "baseline_base_stale"
						if mode == "racing candidate" {
							code = "baseline_candidate_open"
						}
						json.NewEncoder(w).Encode(map[string]any{"code": code, "error": "SECRET_BASELINE_CONTENT"})
						return
					}
					expected := any(baselineTestBase)
					if mode == "first" {
						expected = nil
					}
					if body["base_revision_id"] != expected || body["source_issue_id"] != baselineTestIssue || body["content_markdown"] != "### [Rule-中文] 保留规则\nSHOULD keep body\n" {
						t.Errorf("wrong candidate payload: %+v", body)
					}
					if mode == "replace" && body["replaces_candidate_id"] != baselineTestCandidate {
						t.Error("explicit replacement lost")
					}
					json.NewEncoder(w).Encode(c)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			})
			args := []string{"propose", baselineTestProject, "--chapter", "domain_rules", "--summary", "change", "--content-file", file, "--source-issue", "OL-93", "--output", "json"}
			switch mode {
			case "explicit base":
				args = append(args, "--base", "1")
			case "replace":
				args = append(args, "--base-current", "--replaces", "2")
			case "first", "missing base":
			default:
				args = append(args, "--base-current")
			}
			out, stderr, code := executeBaseline(t, args...)
			if mode == "racing candidate" && !strings.Contains(stderr, baselineTestCandidate) {
				t.Error("concurrent candidate ID missing from conflict guidance")
			}
			want, writeCount := 0, 1
			switch mode {
			case "existing candidate":
				want, writeCount = 1, 0
				if !strings.Contains(stderr, baselineTestCandidate) {
					t.Error("existing candidate ID missing")
				}
			case "missing base":
				want, writeCount = 2, 0
			case "racing base", "racing candidate":
				want = 1
			}
			writes := 0
			for _, c := range calls() {
				if c.Method == "POST" {
					writes++
				}
			}
			if code != want || writes != writeCount || strings.Contains(stderr, "SECRET_BASELINE_CONTENT") {
				t.Fatalf("%d writes=%d; stdout=%s stderr=%s", code, writes, out, stderr)
			}
			if code == 0 {
				if !reflect.DeepEqual(baselineJSON(t, out), baselineJSONRoundtrip(t, c)) {
					t.Error("proposal response lost fields")
				}
				if !strings.Contains(stderr, "[BASELINE WARNING]") {
					t.Error("proposal red card absent")
				}
			} else if out != "" {
				t.Error("failure polluted stdout")
			}
		})
	}
}

func TestLabrastroBaselineDecisionVersionAndCredentials(t *testing.T) {
	for _, mode := range []string{"adopt", "reject", "multi", "closed", "stale", "denied", "forbidden", "human required", "invalid", "server failure", "missing"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("MULTICA_AGENT_ID", baselineTestIssue)
			t.Setenv("MULTICA_TASK_ID", baselineTestCandidate)
			t.Setenv("MULTICA_TASK_CONFIG_ROOT", t.TempDir())
			t.Setenv("MULTICA_APP_URL", "https://workspace.example")
			_, calls := serveBaseline(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
				if r.URL.Path == "/api/workspaces/offline-workspace" {
					json.NewEncoder(w).Encode(map[string]any{"slug": "chosen-workspace"})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/revisions") {
					chapter := r.URL.Query().Get("chapter")
					rev := baselineTestRevision(chapter, 2, "candidate")
					if mode == "closed" {
						rev["status"] = "withdrawn"
					}
					list := []any{rev}
					if mode == "missing" {
						list = []any{}
					}
					json.NewEncoder(w).Encode(map[string]any{"revisions": list})
					return
				}
				if !strings.HasSuffix(r.URL.Path, "/decisions") {
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				revisions := body["revisions"].([]any)
				want := 1
				if mode == "multi" {
					want = 2
				}
				if len(revisions) != want {
					t.Errorf("expected %d revisions in one transaction", want)
				}
				for i, raw := range revisions {
					item := raw.(map[string]any)
					wantID, wantBase := baselineTestCandidate, baselineTestBase
					if i == 1 {
						wantID, wantBase = "30000000-0000-4000-8000-000000000002", "20000000-0000-4000-8000-000000000002"
					}
					if item["revision_id"] != wantID || item["expected_adopted_revision_id"] != wantBase {
						t.Errorf("must use selected version's original base: %+v", item)
					}
				}
				action := "adopt"
				if mode == "reject" {
					action = "reject"
				}
				if body["action"] != action {
					t.Error("wrong decision action")
				}
				status, code := 0, ""
				switch mode {
				case "closed":
					status, code = 409, "baseline_candidate_closed"
				case "stale":
					status, code = 409, "baseline_base_stale"
				case "denied":
					status, code = 403, "baseline_credential_denied"
				case "forbidden":
					status, code = 403, "baseline_not_decider"
				case "human required":
					status, code = 403, "baseline_human_required"
				case "invalid":
					status, code = 422, "baseline_content_invalid"
				case "server failure":
					status, code = 500, "engineering_failed"
				}
				if status > 0 {
					w.WriteHeader(status)
					json.NewEncoder(w).Encode(map[string]any{"code": code, "error": "SECRET_BASELINE_CONTENT"})
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"chapters": baselineTestState(nil, nil)["chapters"], "open_issues": map[string]any{"in_flight": []any{map[string]any{"identifier": "OL-93", "title": "active", "status": "custom_started"}}, "queued": []any{map[string]any{"identifier": "OL-94", "title": "queued", "status": "custom_backlog"}}, "truncated": true}, "future_field": "preserved"})
			})
			args := []string{"adopt", baselineTestProject, "--revision", "domain_rules:2"}
			if mode == "reject" {
				args = append(args, "--reject")
			}
			if mode == "multi" {
				args = append(args, "--revision", "architecture:2", "--output", "json")
			}
			out, stderr, code := executeBaseline(t, args...)
			want, writes := 1, 1
			if mode == "adopt" || mode == "reject" || mode == "multi" {
				want = 0
			}
			if mode == "missing" {
				want, writes = 2, 0
			}
			actualWrites := 0
			for _, c := range calls() {
				if c.Method == "POST" {
					actualWrites++
				}
			}
			if code != want || actualWrites != writes || strings.Contains(stderr, "SECRET_BASELINE_CONTENT") {
				t.Fatalf("code=%d writes=%d\n%s\n%s", code, actualWrites, out, stderr)
			}
			if mode == "human required" {
				if !strings.Contains(stderr, "基线只能由决定人采用或驳回") || !strings.Contains(stderr, "https://workspace.example/chosen-workspace/projects/"+baselineTestProject+"/engineering?candidate="+baselineTestCandidate) {
					t.Fatalf("missing human decision link: %s", stderr)
				}
			}
			if mode == "multi" {
				m := baselineJSON(t, out)
				if m["open_issues"] == nil || m["future_field"] != "preserved" {
					t.Error("decision response lost fields")
				}
			}
			if mode == "adopt" || mode == "reject" {
				if !strings.Contains(out, "OL-93") || !strings.Contains(out, "OL-94") || !strings.Contains(out, "@@ removed [Rule-中文] 保留规则 @@") || strings.Index(out, "base 1") > strings.Index(out, "Open issues:") || !strings.Contains(stderr, "truncated") {
					t.Errorf("missing ordered Diff/issues/truncation: %s %s", out, stderr)
				}
			}
			if mode == "denied" || mode == "forbidden" {
				if strings.Contains(stderr, "engineering?candidate=") || strings.Contains(stderr, "human decider") || strings.Contains(stderr, "PAT") {
					t.Errorf("incorrect refusal guidance: %s", stderr)
				}
				for _, call := range calls() {
					if call.Path == "/api/config" || strings.HasPrefix(call.Path, "/api/workspaces/") {
						t.Error("denied decision must not look up approval links")
					}
				}
			}
			if want == 0 {
				for _, warning := range []string{"rule_removed", "rule_shrunk", "rule_deferred", "defer_missing_pool", "[BASELINE WARNING]"} {
					if !strings.Contains(stderr, warning) {
						t.Errorf("successful decision lost warning %s: %s", warning, stderr)
					}
				}
			}
		})
	}
}

func TestLabrastroBaselineArgumentsAndContentBoundary(t *testing.T) {
	file := writeBaselineInput(t)
	_, calls := serveBaseline(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		t.Error("invalid arguments reached server")
		w.WriteHeader(500)
	})
	cases := [][]string{
		{"get"}, {"get", baselineTestProject, "--chapter", "risks"}, {"get", baselineTestProject, "--output", "csv"}, {"get", baselineTestProject, "--revision", "rules:1"},
		{"get", baselineTestProject, "--revision", ""},
		{"get", baselineTestProject, "--revision", "domain_rules:1", "--candidate"}, {"get", baselineTestProject, "--revision", "domain_rules:1", "--chapter", "overview"},
		{"propose", baselineTestProject}, {"propose", baselineTestProject, "--chapter", "domain_rules", "--summary", "ok", "--content-file", file, "--base", "1", "--base-current"},
		{"diff", baselineTestProject, "--from", "1", "--to", "2"}, {"diff", baselineTestProject, "--chapter", "overview", "--from", "0", "--to", "2"},
		{"adopt", baselineTestProject}, {"adopt", baselineTestProject, "--revision", "overview:1", "--revision", "overview:2"}, {"adopt", baselineTestProject, "--revision", "overview:2147483648"},
		{"export", baselineTestProject, "--dir", "a", "--single-file", "b"}, {"get", baselineTestProject, "--unknown"},
	}
	for _, args := range cases {
		out, stderr, code := executeBaseline(t, args...)
		if code != 2 || out != "" || stderr == "" {
			t.Errorf("%v: %d %q %q", args, code, out, stderr)
		}
	}
	if len(calls()) != 0 {
		t.Error("parameter failures made requests")
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	os.WriteFile(outside, []byte("PRIVATE"), 0600)
	if err := os.Symlink(outside, "escape.md"); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"large.md": bytes.Repeat([]byte("x"), 65537), "invalid.md": {0xff}, "nul.md": {0}} {
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{outside, "escape.md", "..", "missing.md", "large.md", "invalid.md", "nul.md"} {
		_, err := baselineReadContent(path)
		if err == nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Errorf("unsafe content path %q: %v", path, err)
		}
	}
	if _, err := baselineReadContent(file); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(file)
	if _, err := baselineReadContent(abs); err != nil {
		t.Fatal(err)
	}
}

type baselineRoundTripper func(*http.Request) (*http.Response, error)

func (f baselineRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLabrastroBaselineApprovalURL(t *testing.T) {
	for _, tc := range []struct {
		name, appEnv, frontendEnv, profileApp, advertised, apiURL, wantOrigin string
		configStatus, workspaceStatus                                         int
		missingSlug                                                           bool
	}{
		{name: "task defaults", advertised: "https://public.example/", wantOrigin: "https://public.example"},
		{name: "app env wins", appEnv: "https://env.example", frontendEnv: "https://frontend.example", profileApp: "https://profile.example", advertised: "https://server.example", wantOrigin: "https://env.example"},
		{name: "frontend env wins", frontendEnv: "https://frontend.example", profileApp: "https://profile.example", wantOrigin: "https://frontend.example"},
		{name: "selected profile", profileApp: "https://profile.example", advertised: "https://server.example", wantOrigin: "https://profile.example"},
		{name: "official cloud", apiURL: "https://api.multica.ai", wantOrigin: "https://multica.ai"},
		{name: "fork cloud", apiURL: defaultCloudServerURL, wantOrigin: defaultCloudAppURL},
		{name: "cloud config unavailable", apiURL: "https://api.multica.ai", configStatus: 503, wantOrigin: "https://multica.ai"},
		{name: "internal API with no app", apiURL: "http://multica-backend:8080"},
		{name: "config unavailable", configStatus: 503},
		{name: "invalid app", advertised: "://invalid"},
		{name: "non web app", advertised: "javascript:alert(1)"},
		{name: "workspace unavailable", advertised: "https://public.example", workspaceStatus: 403},
		{name: "workspace slug absent", advertised: "https://public.example", missingSlug: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MULTICA_APP_URL", tc.appEnv)
			t.Setenv("FRONTEND_ORIGIN", tc.frontendEnv)
			t.Setenv("MULTICA_TASK_CONFIG_ROOT", t.TempDir())
			if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{AppURL: tc.profileApp}, "approval"); err != nil {
				t.Fatal(err)
			}
			cmd := &cobra.Command{}
			cmd.Flags().String("profile", "approval", "")
			srv, calls := serveBaseline(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				switch r.URL.Path {
				case "/api/config":
					if tc.appEnv != "" || tc.frontendEnv != "" || tc.profileApp != "" {
						t.Error("explicit app setting should not query server config")
					}
					if tc.configStatus != 0 {
						w.WriteHeader(tc.configStatus)
					}
					json.NewEncoder(w).Encode(map[string]any{"daemon_app_url": tc.advertised, "error": "SECRET_METADATA"})
				case "/api/workspaces/offline-workspace":
					if tc.workspaceStatus != 0 {
						w.WriteHeader(tc.workspaceStatus)
					}
					slug := "chosen-workspace"
					if tc.missingSlug {
						slug = ""
					}
					json.NewEncoder(w).Encode(map[string]any{"slug": slug, "error": "SECRET_METADATA"})
				default:
					t.Errorf("unexpected metadata request: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			})
			client := cli.NewAPIClient(srv.URL, "offline-workspace", "mat_baseline_test")
			if tc.apiURL != "" {
				client.BaseURL = tc.apiURL
				local, _ := url.Parse(srv.URL)
				client.HTTPClient.Transport = baselineRoundTripper(func(r *http.Request) (*http.Response, error) {
					clone := r.Clone(r.Context())
					clone.URL.Scheme, clone.URL.Host = local.Scheme, local.Host
					return srv.Client().Transport.RoundTrip(clone)
				})
			}
			link := baselineApprovalURL(cmd, client, baselineTestProject, baselineTestCandidate)
			if tc.wantOrigin != "" {
				want := tc.wantOrigin + "/chosen-workspace/projects/" + baselineTestProject + "/engineering?candidate=" + baselineTestCandidate
				if link != want {
					t.Fatalf("approval URL: want %s; got %s", want, link)
				}
			} else if link != "" {
				t.Fatalf("expected no guessed URL: %s", link)
			}
			for _, c := range calls() {
				if c.Method != "GET" {
					t.Error("approval lookup must not retry a decision")
				}
			}
		})
	}
}

func TestLabrastroBaselineProjectErrors(t *testing.T) {
	for _, tc := range []struct {
		name, project, response, want string
		status, exit                  int
	}{
		{"HTTP 400", baselineTestProject, `{"error":"SECRET_BASELINE_CONTENT"}`, "(HTTP 400)", 400, 2},
		{"HTTP 404", baselineTestProject, `{"error":"SECRET_BASELINE_CONTENT"}`, "(HTTP 404)", 404, 2},
		{"invalid prefix", "not-hex", "", "expected a UUID prefix containing only hex characters", 0, 2},
		{"missing prefix", "1000", `{"projects":[]}`, `no project found matching id prefix "1000"`, 200, 2},
		{"ambiguous prefix", "1000", `{"projects":[{"id":"10000000-0000-4000-8000-000000000001"},{"id":"10000000-0000-4000-8000-000000000002"}]}`, "ambiguous project id prefix", 200, 2},
		{"prefix unauthorized", "1000", `{"error":"SECRET_BASELINE_CONTENT"}`, "Stop here: do not retry", 401, 1},
		{"prefix service failure", "1000", `{"error":"SECRET_BASELINE_CONTENT"}`, "(HTTP 503)", 503, 1},
		{"prefix malformed response", "1000", `SECRET_BASELINE_CONTENT`, "baseline read failed", 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, calls := serveBaseline(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				if tc.status == 0 {
					t.Error("invalid prefix reached server")
					return
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.response)
			})
			out, stderr, code := executeBaseline(t, "get", tc.project, "--output", "json")
			if code != tc.exit || out != "" || !strings.Contains(stderr, tc.want) || strings.Contains(stderr, "SECRET_BASELINE_CONTENT") {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out, stderr)
			}
			if tc.name == "ambiguous prefix" {
				for _, id := range []string{baselineTestProject, "10000000-0000-4000-8000-000000000002"} {
					if !strings.Contains(stderr, id) {
						t.Error("ambiguity guidance lost matching project IDs")
					}
				}
			}
			if len(calls()) > 1 {
				t.Error("project failure must not proceed to baseline requests or retry")
			}
		})
	}
}

func TestLabrastroBaselineExport(t *testing.T) {
	t.Chdir(t.TempDir())
	adopted := baselineTestRevision("domain_rules", 1, "adopted")
	_, calls := serveBaseline(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		json.NewEncoder(w).Encode(baselineTestState(adopted, baselineTestRevision("domain_rules", 2, "candidate")))
	})
	for attempt := 0; attempt < 2; attempt++ {
		out, stderr, code := executeBaseline(t, "export", baselineTestProject, "--output", "json")
		if code != 0 || !strings.Contains(stderr, "Exported 4") {
			t.Fatalf("export failed: %d %s %s", code, out, stderr)
		}
		files := baselineJSON(t, out)["files"].([]any)
		if len(files) != 4 {
			t.Fatal("not four files")
		}
		for _, v := range files {
			path := v.(string)
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm()&0222 != 0 || bytes.Contains(b, []byte(baselineTestBase)) || bytes.Contains(b, []byte(baselineTestCandidate)) {
				t.Fatalf("invalid read-only snapshot: %s", path)
			}
			if strings.Contains(path, "domain-rules") && !bytes.Contains(b, []byte("Revision: 1")) {
				t.Fatal("exported candidate instead of adopted")
			}
		}
	}
	out, _, code := executeBaseline(t, "export", baselineTestProject, "--single-file", "./PROJECT_BASELINE.md", "--output", "json")
	if code != 0 || len(baselineJSON(t, out)["files"].([]any)) != 1 {
		t.Fatal("single file export failed")
	}
	b, _ := os.ReadFile("PROJECT_BASELINE.md")
	if strings.Count(string(b), "# ") < 4 {
		t.Fatal("missing chapters")
	}
	for _, call := range calls() {
		if call.Method != "GET" {
			t.Fatal("export mutated server")
		}
	}
	if err := os.Symlink("PROJECT_BASELINE.md", "link.md"); err != nil {
		t.Fatal(err)
	}
	if err := baselineWriteSnapshot("link.md", "overwrite"); err == nil {
		t.Fatal("snapshot followed symlink")
	}
}

func TestLabrastroBaselineMalformedResponses(t *testing.T) {
	for _, response := range []any{map[string]any{}, map[string]any{"chapters": []any{}}, map[string]any{"chapters": []any{map[string]any{"chapter": "domain_rules", "adopted": map[string]any{"id": "bogus"}}}}} {
		t.Run(fmt.Sprint(response), func(t *testing.T) {
			_, _ = serveBaseline(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) { json.NewEncoder(w).Encode(response) })
			out, _, code := executeBaseline(t, "export", baselineTestProject, "--dir", filepath.Join(t.TempDir(), "snapshot"))
			if code != 1 || out != "" {
				t.Fatal("malformed response reported successful export")
			}
		})
	}
}

func TestLabrastroBaselineCLIProcess(t *testing.T) {
	if os.Getenv("BASELINE_CLI_PROCESS") != "1" {
		return
	}
	t.Setenv("MULTICA_SERVER_URL", os.Getenv("BASELINE_CLI_URL"))
	t.Setenv("MULTICA_TOKEN", "mat_baseline_test")
	t.Setenv("MULTICA_WORKSPACE_ID", "offline-workspace")
	t.Setenv("MULTICA_TASK_CONFIG_ROOT", t.TempDir())
	t.Setenv("MULTICA_AGENT_ID", baselineTestIssue)
	t.Setenv("MULTICA_TASK_ID", baselineTestCandidate)
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "")
	if err := os.Chdir(os.Getenv("BASELINE_CLI_DIR")); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs(os.Args[3:])
	main()
	os.Exit(0)
}
func TestLabrastroBaselineProcessExitAndStreams(t *testing.T) {
	srv, _ := serveBaseline(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		switch r.URL.Path {
		case "/api/config":
			json.NewEncoder(w).Encode(map[string]any{"daemon_app_url": "https://workspace.example"})
			return
		case "/api/workspaces/offline-workspace":
			json.NewEncoder(w).Encode(map[string]any{"slug": "chosen-workspace"})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/revisions") {
			json.NewEncoder(w).Encode(map[string]any{"revisions": []any{baselineTestRevision("domain_rules", 2, "candidate")}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/decisions") {
			w.WriteHeader(403)
			json.NewEncoder(w).Encode(map[string]any{"code": "baseline_credential_denied", "error": "SECRET_BASELINE_CONTENT"})
			return
		}
		json.NewEncoder(w).Encode(baselineTestState(baselineTestRevision("domain_rules", 1, "adopted"), nil))
	})
	for _, tc := range []struct {
		args []string
		exit int
		hint string
	}{
		{[]string{"get", baselineTestProject, "--output", "json"}, 0, ""},
		{[]string{"get", baselineTestProject, "--revision", "domain_rules:999"}, 2, "404"},
		{[]string{"adopt", baselineTestProject, "--revision", "domain_rules:2", "--output", "json", "--debug"}, 1, "baseline_credential_denied"},
		{[]string{"get", baselineTestProject, "--unknown"}, 2, "unknown flag"},
	} {
		args := append([]string{"-test.run=^TestLabrastroBaselineCLIProcess$", "--", "project", "baseline"}, tc.args...)
		cmd := exec.Command(os.Args[0], args...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "BASELINE_CLI_PROCESS=1", "BASELINE_CLI_URL=" + srv.URL, "BASELINE_CLI_DIR=" + t.TempDir()}
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		err := cmd.Run()
		exit := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				exit = e.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if exit != tc.exit || !strings.Contains(stderr.String(), tc.hint) || strings.Contains(stderr.String(), "SECRET_BASELINE_CONTENT") {
			t.Fatalf("%v: exit=%d out=%s err=%s", tc.args, exit, out.String(), stderr.String())
		}
		if exit == 0 {
			baselineJSON(t, out.String())
		} else if out.Len() != 0 {
			t.Fatal("error leaked to stdout")
		}
	}
}

func TestLabrastroBaselineTaskAuthErrorRedaction(t *testing.T) {
	t.Setenv("MULTICA_DEBUG", "1")
	t.Setenv("LANG", "en_US.UTF-8")
	_, _ = serveBaseline(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]any{"error": "SECRET_BASELINE_CONTENT"})
	})
	out, stderr, code := executeBaseline(t, "get", baselineTestProject, "--output", "json")
	if code != 1 || out != "" || strings.Contains(stderr, "SECRET_BASELINE_CONTENT") || !strings.Contains(stderr, "task token") {
		t.Fatalf("task refusal must remain actionable and redacted: %d %q %q", code, out, stderr)
	}
}

func TestLabrastroBaselineMissingRevisionPayload(t *testing.T) {
	for _, field := range []string{"content_markdown", "anchors_diff"} {
		t.Run(field, func(t *testing.T) {
			revision := baselineTestRevision("domain_rules", 1, "adopted")
			delete(revision, field)
			_, _ = serveBaseline(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
				json.NewEncoder(w).Encode(baselineTestState(revision, nil))
			})
			out, _, code := executeBaseline(t, "get", baselineTestProject, "--chapter", "domain_rules")
			if code != 1 || out != "" {
				t.Fatal("incomplete revision silently rendered as empty content")
			}
		})
	}
}
