package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func executeEngineering(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	root := &cobra.Command{Use: "multica", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().String("profile", "", "")
	root.PersistentFlags().String("server-url", "", "")
	root.PersistentFlags().String("workspace-id", "", "")
	project := &cobra.Command{Use: "project"}
	project.AddCommand(newLabrastroProjectEngineeringCmd())
	root.AddCommand(project)
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"project", "engineering"}, args...))
	cmd, err := root.ExecuteC()
	if err != nil {
		fmt.Fprintln(&stderr, err)
	}
	return out.String(), stderr.String(), labrastroBaselineExitCode(cmd, err)
}

func TestLabrastroEngineeringCLIGetSet(t *testing.T) {
	for _, action := range []string{"get", "set"} {
		cmd, _, err := rootCmd.Find([]string{"project", "engineering", action})
		if err != nil || cmd.Name() != action {
			t.Fatalf("%s not registered", action)
		}
	}
	t.Chdir(t.TempDir())
	stages := []any{map[string]any{"stage": float64(9), "title": "Release:验收", "goal": "Verified"}}
	raw, _ := json.Marshal(stages)
	if err := os.WriteFile("stages.json", raw, 0600); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"enabled": true, "current_stage": 9, "mode_summary": "Iterate", "stages": stages, "future_field": "preserved"}
	_, calls := serveBaseline(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if !strings.HasSuffix(r.URL.Path, "/engineering") {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"engineering": config, "chapters": []any{}})
	})
	for _, action := range []string{"get", "set"} {
		args := []string{action, baselineTestProject}
		if action == "set" {
			args = append(args, "--stages-file", "stages.json", "--current-stage", "9", "--mode-summary", "")
		}
		out, stderr, code := executeEngineering(t, args...)
		if code != 0 || stderr != "" || !strings.Contains(out, "Current stage: 9") || !strings.Contains(out, "Release:验收") || !strings.Contains(out, "Verified") {
			t.Fatalf("%d %s %s", code, out, stderr)
		}
	}
	if got := calls()[1]; got.Method != "PATCH" || !reflect.DeepEqual(got.Body["stages"], stages) || got.Body["mode_summary"] != "" || got.Body["current_stage"] != float64(9) {
		t.Fatalf("patch=%+v", got)
	}
	out, stderr, code := executeEngineering(t, "get", baselineTestProject, "--output", "json")
	if code != 0 || stderr != "" || baselineJSON(t, out)["future_field"] != "preserved" || baselineJSON(t, out)["engineering"] != nil {
		t.Fatalf("json=%d %s %s", code, out, stderr)
	}
	_, _, code = executeEngineering(t, "set", baselineTestProject, "--clear-stages")
	if code != 0 || !reflect.DeepEqual(calls()[3].Body, map[string]any{"stages": []any{}}) {
		t.Fatalf("clear=%+v", calls())
	}
}

func TestLabrastroEngineeringCLIArgumentsAndFiles(t *testing.T) {
	outside := t.TempDir()
	inside := t.TempDir()
	t.Chdir(inside)
	if err := os.WriteFile(filepath.Join(outside, "outside.json"), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside.json"), "escape.json"); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, content string }{{"null.json", "null"}, {"object.json", "{}"}, {"invalid.json", "["}, {"invalid-utf8.json", string([]byte{0xff})}, {"large.json", strings.Repeat(" ", 1<<20) + "[]"}} {
		if err := os.WriteFile(tt.name, []byte(tt.content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cases := [][]string{
		{"get"}, {"set", baselineTestProject}, {"set", baselineTestProject, "--current-stage", "0"},
		{"set", baselineTestProject, "--current-stage", "1.5"}, {"get", baselineTestProject, "--output", "yaml"},
		{"set", baselineTestProject, "--clear-stages=false"},
		{"set", baselineTestProject, "--mainline-issue", "OL-1"},
	}
	for _, file := range []string{filepath.Join(outside, "outside.json"), "escape.json", "null.json", "object.json", "invalid.json", "invalid-utf8.json", "large.json", "missing.json"} {
		cases = append(cases, []string{"set", baselineTestProject, "--stages-file", file})
	}
	for _, args := range cases {
		out, stderr, code := executeEngineering(t, args...)
		if code != 2 || out != "" || stderr == "" {
			t.Fatalf("%v: %d %s %s", args, code, out, stderr)
		}
	}
}

func TestLabrastroEngineeringCLIStageFlagsMutuallyExclusive(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("ok.json", []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	_, calls := serveBaseline(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		json.NewEncoder(w).Encode(map[string]any{"engineering": map[string]any{
			"enabled": true, "current_stage": 1, "mode_summary": "", "stages": []any{},
		}})
	})
	out, stderr, code := executeEngineering(t, "set", baselineTestProject, "--stages-file", "ok.json", "--clear-stages")
	if code != 2 || out != "" || !strings.Contains(stderr, "mutually exclusive") {
		t.Errorf("expected mutually exclusive argument error: code=%d stdout=%q stderr=%q", code, out, stderr)
	}
	if got := calls(); len(got) != 0 {
		t.Errorf("mutually exclusive flags must not send requests: %+v", got)
	}
}

func TestLabrastroEngineeringCLIHumanRequired(t *testing.T) {
	_, calls := serveBaseline(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.WriteHeader(403)
		json.NewEncoder(w).Encode(map[string]any{"error": "SECRET_RESPONSE"})
	})
	out, stderr, code := executeEngineering(t, "set", baselineTestProject, "--current-stage", "9", "--output", "json")
	if code != 1 || out != "" || !strings.Contains(stderr, "工程配置只能由人类决定人修改，请在工程总览设置中修改") || strings.Contains(stderr, "SECRET_RESPONSE") || len(calls()) != 1 {
		t.Fatalf("refusal=%d %s %s %+v", code, out, stderr, calls())
	}
}
