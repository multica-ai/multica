package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func TestIssueSearchErrorGuidance(t *testing.T) {
	t.Setenv("LC_ALL", "C")
	t.Setenv("MULTICA_DEBUG", "")
	const timeoutBody = `{"error":"search timed out; please refine your query or try again"}`
	const guidance = "Issue search timed out on the server. Refine your query before searching again."
	generic := cli.FormatError(&cli.HTTPError{StatusCode: 503}, false)
	for _, tc := range []struct {
		name               string
		status             int
		body, locale, want string
		exit               int
	}{
		{"timeout", 503, timeoutBody, "C", guidance, 1},
		{"formatted timeout", 503, "  {\n \"error\" : \"search timed out; please refine your query or try again\"\n }\n", "C", guidance, 1},
		{"Chinese", 503, timeoutBody, "zh_CN.UTF-8", "服务端 issue 搜索超时。请缩小查询范围后再搜索。", 1},
		{"success", 200, `{"issues":[],"total":0}`, "C", "", 0},
		{"general 503", 503, `{"error":"SECRET upstream details"}`, "C", generic, 1},
		{"HTML", 503, `<html>SECRET upstream</html>`, "C", generic, 1},
		{"malformed", 503, `{"error":`, "C", generic, 1},
		{"wrong type", 503, `{"error":["search timed out; please refine your query or try again"]}`, "C", generic, 1},
		{"prefix injection", 503, `{"error":"search timed out; please refine your query or try again SECRET"}`, "C", generic, 1},
		{"extra field", 503, `{"error":"search timed out; please refine your query or try again","detail":"SECRET"}`, "C", generic, 1},
		{"forged code", 503, `{"code":"search_timeout","error":"SECRET"}`, "C", generic, 1},
		{"message alias", 503, `{"message":"search timed out; please refine your query or try again"}`, "C", generic, 1},
		{"duplicate key", 503, `{"error":"SECRET","error":"search timed out; please refine your query or try again"}`, "C", generic, 1},
		{"trailing JSON", 503, timeoutBody + ` {"error":"SECRET"}`, "C", generic, 1},
		{"other 5xx", 500, timeoutBody, "C", generic, 1},
		{"auth", 401, timeoutBody, "C", cli.FormatError(&cli.HTTPError{StatusCode: 401}, false), 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				if r.Method != "GET" || r.URL.Path != "/api/issues/search" || r.URL.Query().Get("q") != "PRIVATE_QUERY" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
			t.Setenv("MULTICA_TOKEN", "test-token")
			t.Setenv("LC_ALL", tc.locale)
			cmd := &cobra.Command{}
			cmd.Flags().Int("limit", 20, "")
			cmd.Flags().Bool("include-closed", false, "")
			cmd.Flags().String("output", "json", "")
			out, err := captureStdout(t, func() error { return runIssueSearch(cmd, []string{"PRIVATE_QUERY"}) })
			if hits != 1 {
				t.Fatalf("requests = %d, want 1", hits)
			}
			if tc.status == 200 {
				if err != nil || !strings.Contains(out, `"issues"`) {
					t.Fatalf("success = %q, %v", out, err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if got := cli.FormatError(err, false); got != tc.want {
				t.Errorf("message = %q, want %q", got, tc.want)
			}
			if got := cli.ExitCodeFor(err); got != tc.exit {
				t.Errorf("exit = %d, want %d", got, tc.exit)
			}
			var httpErr *cli.HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != tc.status || httpErr.Body != strings.TrimSpace(tc.body) {
				t.Fatalf("lost HTTP error: %v", err)
			}
			if tc.status >= 500 && httpErr.Kind() != cli.KindServerError {
				t.Errorf("kind = %v", httpErr.Kind())
			}
			if !strings.Contains(cli.FormatError(err, true), httpErr.Body) {
				t.Error("debug lost original body")
			}
		})
	}
}

func TestIssueSearchTransportError(t *testing.T) {
	t.Setenv("MULTICA_DEBUG", "")
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("LC_ALL", "C")
	cmd := &cobra.Command{}
	cmd.Flags().Int("limit", 20, "")
	cmd.Flags().Bool("include-closed", false, "")
	cmd.Flags().String("output", "json", "")
	err := runIssueSearch(cmd, []string{"PRIVATE_QUERY"})
	var networkErr *cli.NetworkError
	if !errors.As(err, &networkErr) || networkErr.Kind != cli.KindNetworkRefused {
		t.Fatalf("error = %v, want refused NetworkError", err)
	}
	if got := cli.ExitCodeFor(err); got != cli.ExitNetwork {
		t.Errorf("exit = %d, want %d", got, cli.ExitNetwork)
	}
	if got := cli.FormatError(err, false); strings.Contains(got, "PRIVATE_QUERY") || strings.Contains(got, "search timed out") {
		t.Errorf("unexpected guidance: %q", got)
	}
}
