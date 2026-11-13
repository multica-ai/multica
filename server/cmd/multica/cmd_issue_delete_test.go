package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	issueDeleteTestID1 = "11111111-1111-4111-8111-111111111111"
	issueDeleteTestID2 = "22222222-2222-4222-8222-222222222222"
)

// setIssueDeleteFlags drives the command the CLI actually registers, then
// restores the previous flag values so tests do not leak into each other.
// Building a throwaway command here would leave the real --yes/--output
// registration untested.
func setIssueDeleteFlags(t *testing.T, yes bool, output string) {
	t.Helper()
	prevYes, err := issueDeleteCmd.Flags().GetBool("yes")
	if err != nil {
		t.Fatalf("read --yes: %v", err)
	}
	prevOutput, err := issueDeleteCmd.Flags().GetString("output")
	if err != nil {
		t.Fatalf("read --output: %v", err)
	}
	t.Cleanup(func() {
		_ = issueDeleteCmd.Flags().Set("yes", strconv.FormatBool(prevYes))
		_ = issueDeleteCmd.Flags().Set("output", prevOutput)
	})
	if err := issueDeleteCmd.Flags().Set("yes", strconv.FormatBool(yes)); err != nil {
		t.Fatalf("set --yes: %v", err)
	}
	if err := issueDeleteCmd.Flags().Set("output", output); err != nil {
		t.Fatalf("set --output: %v", err)
	}
}

func setIssueDeleteTestEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", serverURL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
}

type issueDeleteRequest struct {
	method string
	path   string
}

// TestIssueDeleteCommandRegistration covers the pieces only the real command
// carries: the zero-argument guard and the --yes/-y, --output registration.
func TestIssueDeleteCommandRegistration(t *testing.T) {
	if got := issueDeleteCmd.Flags().Lookup("yes"); got == nil || got.DefValue != "false" {
		t.Fatalf("--yes flag = %+v, want a bool defaulting to false", got)
	}
	if got := issueDeleteCmd.Flags().ShorthandLookup("y"); got == nil || got.Name != "yes" {
		t.Fatalf("-y shorthand = %+v, want it bound to --yes", got)
	}
	if got := issueDeleteCmd.Flags().Lookup("output"); got == nil || got.DefValue != "table" {
		t.Fatalf("--output flag = %+v, want table by default", got)
	}
	if issueDeleteCmd.Args == nil {
		t.Fatal("delete command accepts no positional arguments")
	}

	stderr := captureStderr(t)
	_, err := captureStdout(t, func() error {
		return issueDeleteCmd.Args(issueDeleteCmd, nil)
	})
	msg := stderr.read()
	if err == nil {
		t.Fatal("zero arguments accepted, want the minimum-args error")
	}
	if !strings.Contains(msg, "accepts at least 1 arg, received 0") {
		t.Fatalf("stderr = %q, want the minimum-args message", msg)
	}
}

func TestRunIssueDeleteSingleByKey(t *testing.T) {
	var requests []issueDeleteRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, issueDeleteRequest{r.Method, r.URL.Path})
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/MUL-1":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"id": issueDeleteTestID1, "identifier": "MUL-1"})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/issues/"+issueDeleteTestID1:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, true, "json")

	out, err := captureStdout(t, func() error {
		return runIssueDelete(issueDeleteCmd, []string{"MUL-1"})
	})
	if err != nil {
		t.Fatalf("run delete: %v", err)
	}

	want := []issueDeleteRequest{
		{http.MethodGet, "/api/issues/MUL-1"},
		{http.MethodDelete, "/api/issues/" + issueDeleteTestID1},
	}
	if !issueDeleteRequestsEqual(requests, want) {
		t.Fatalf("requests = %v, want %v", requests, want)
	}

	// A single delete keeps the sibling delete shape, with the canonical UUID
	// rather than whatever spelling the caller used.
	var result struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	if result.ID != issueDeleteTestID1 || !result.Deleted {
		t.Fatalf("output = %q, want {id:%s deleted:true}", out, issueDeleteTestID1)
	}
}

// A full UUID is a self-identifying reference elsewhere in the CLI, but the
// batch route silently drops a UUID that no longer resolves and deletes the
// rest; delete therefore reads every argument, UUID included, before writing.
func TestRunIssueDeleteBatchResolvesStaleUUIDBeforeWrite(t *testing.T) {
	var requests []issueDeleteRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, issueDeleteRequest{r.Method, r.URL.Path})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/MUL-1":
			json.NewEncoder(w).Encode(map[string]any{"id": issueDeleteTestID1, "identifier": "MUL-1"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+issueDeleteTestID2:
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "issue not found"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, true, "json")

	_, err := captureStdout(t, func() error {
		return runIssueDelete(issueDeleteCmd, []string{"MUL-1", issueDeleteTestID2})
	})
	if err == nil {
		t.Fatal("delete succeeded, want the stale UUID to abort the batch")
	}
	if !strings.Contains(err.Error(), `resolve issue "`+issueDeleteTestID2+`"`) {
		t.Fatalf("error = %v, want it to name the stale UUID", err)
	}
	for _, req := range requests {
		if req.method != http.MethodGet {
			t.Fatalf("requests = %v, want reads only before an unresolved id aborts", requests)
		}
	}
}

// The batch route accepts UUIDs only and skips anything it cannot parse, so
// the CLI must resolve keys first. A key sent unchanged would produce
// {"deleted": 0} instead of failing.
func TestRunIssueDeleteBatchResolvesKeysToUUIDs(t *testing.T) {
	keys := map[string]string{"MUL-1": issueDeleteTestID1, "MUL-2": issueDeleteTestID2}
	var batchIDs []string
	var deleteCalls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/issues/"):
			key := strings.TrimPrefix(r.URL.Path, "/api/issues/")
			id, ok := keys[key]
			if !ok {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"id": id, "identifier": key})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/batch-delete":
			var body struct {
				IssueIDs []string `json:"issue_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode batch body: %v", err)
			}
			batchIDs = body.IssueIDs
			json.NewEncoder(w).Encode(map[string]any{"deleted": len(body.IssueIDs)})
		case r.Method == http.MethodDelete:
			deleteCalls++
			http.Error(w, "unexpected single delete", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, true, "json")

	out, err := captureStdout(t, func() error {
		return runIssueDelete(issueDeleteCmd, []string{"MUL-1", "MUL-2"})
	})
	if err != nil {
		t.Fatalf("run delete: %v", err)
	}

	if deleteCalls != 0 {
		t.Fatalf("single delete calls = %d, want 0", deleteCalls)
	}
	if len(batchIDs) != 2 || batchIDs[0] != issueDeleteTestID1 || batchIDs[1] != issueDeleteTestID2 {
		t.Fatalf("batch issue_ids = %v, want [%s %s]", batchIDs, issueDeleteTestID1, issueDeleteTestID2)
	}

	var result struct {
		IDs     []string `json:"ids"`
		Deleted bool     `json:"deleted"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	if !result.Deleted || len(result.IDs) != 2 || result.IDs[0] != issueDeleteTestID1 || result.IDs[1] != issueDeleteTestID2 {
		t.Fatalf("output = %q, want canonical UUIDs %v and deleted:true", out, []string{issueDeleteTestID1, issueDeleteTestID2})
	}
}

func TestRunIssueDeleteDuplicateKeysDeleteOnce(t *testing.T) {
	deleteCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"id": issueDeleteTestID1, "identifier": "MUL-1"})
		case http.MethodDelete:
			deleteCalls++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, true, "json")

	_, err := captureStdout(t, func() error {
		return runIssueDelete(issueDeleteCmd, []string{"MUL-1", "MUL-1"})
	})
	if err != nil {
		t.Fatalf("run delete: %v", err)
	}
	if deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want 1", deleteCalls)
	}
}

func TestRunIssueDeletePromptAborts(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": issueDeleteTestID1, "identifier": "MUL-1"})
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, false, "table")

	stderr := captureStderr(t)
	out, err := captureStdout(t, func() error {
		var runErr error
		pipeStdin(t, "n\n", func() {
			runErr = runIssueDelete(issueDeleteCmd, []string{issueDeleteTestID1})
		})
		return runErr
	})
	if err != nil {
		t.Fatalf("run delete: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want only the resolving read", requests)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
	msg := stderr.read()
	if !strings.Contains(msg, "Delete issue MUL-1") {
		t.Fatalf("prompt = %q, want it to name the issue", msg)
	}
	if !strings.Contains(msg, "cannot be undone") {
		t.Fatalf("prompt = %q, want it to state permanence", msg)
	}
	if !strings.Contains(msg, "Aborted.") {
		t.Fatalf("stderr = %q, want Aborted.", msg)
	}
}

func TestRunIssueDeletePromptAcceptsYes(t *testing.T) {
	deleteCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"id": issueDeleteTestID1, "identifier": "MUL-1"})
			return
		}
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		deleteCalls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, false, "table")

	_, err := captureStdout(t, func() error {
		var runErr error
		pipeStdin(t, "y\n", func() {
			runErr = runIssueDelete(issueDeleteCmd, []string{issueDeleteTestID1})
		})
		return runErr
	})
	if err != nil {
		t.Fatalf("run delete: %v", err)
	}
	if deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want 1", deleteCalls)
	}
}

func TestRunIssueDeletePromptWaitDoesNotConsumeDeleteTimeout(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"single", []string{"MUL-1"}},
		{"batch", []string{"MUL-1", "MUL-2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MULTICA_HTTP_TIMEOUT", "1s")
			resolved := make(chan struct{})
			var reads, writes atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method {
				case http.MethodGet:
					id := issueDeleteTestID1
					if r.URL.Path == "/api/issues/MUL-2" {
						id = issueDeleteTestID2
					}
					json.NewEncoder(w).Encode(map[string]any{"id": id, "identifier": strings.TrimPrefix(r.URL.Path, "/api/issues/")})
					if int(reads.Add(1)) == len(tc.args) {
						close(resolved)
					}
				case http.MethodDelete:
					writes.Add(1)
					w.WriteHeader(http.StatusNoContent)
				case http.MethodPost:
					writes.Add(1)
					json.NewEncoder(w).Encode(map[string]any{"deleted": len(tc.args)})
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			setIssueDeleteTestEnv(t, srv.URL)
			setIssueDeleteFlags(t, false, "json")

			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			defer w.Close()
			originalStdin := os.Stdin
			os.Stdin = r
			defer func() { os.Stdin = originalStdin }()
			answered := make(chan struct{})
			go func() {
				defer close(answered)
				<-resolved
				// Let the entire resolution budget expire while the user reads the prompt.
				time.Sleep(cli.APITimeout() + 100*time.Millisecond)
				if _, err := w.WriteString("y\n"); err != nil {
					t.Errorf("confirm deletion: %v", err)
				}
			}()

			stderr := captureStderr(t)
			out, err := captureStdout(t, func() error {
				return runIssueDelete(issueDeleteCmd, tc.args)
			})
			<-answered
			stderr.read()
			if err != nil {
				t.Fatalf("delete after delayed confirmation: %v", err)
			}
			if got := writes.Load(); got != 1 {
				t.Fatalf("delete requests = %d, want 1", got)
			}
			var result struct {
				Deleted bool `json:"deleted"`
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil || !result.Deleted {
				t.Fatalf("output = %q, error = %v; want deleted:true", out, err)
			}
		})
	}
}

// A batch prompt has to name the count and the issues, not just "these".
func TestIssueDeleteConfirmationNamesCountAndConsequences(t *testing.T) {
	got := issueDeleteConfirmation([]resolvedID{
		{ID: issueDeleteTestID1, Display: "MUL-1"},
		{ID: issueDeleteTestID2, Display: "MUL-2"},
	})
	for _, want := range []string{"2 issues", "MUL-1", "MUL-2", "comments and attachments", "cannot be undone"} {
		if !strings.Contains(got, want) {
			t.Fatalf("confirmation = %q, want it to contain %q", got, want)
		}
	}
}

// A non-interactive run has no answer to give, so it must fail instead of
// printing "Aborted." and exiting 0. A deliberate "n" stays a quiet decline.
func TestRunIssueDeleteNonInteractiveFails(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": issueDeleteTestID1, "identifier": "MUL-1"})
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, false, "table")

	stderr := captureStderr(t)
	_, err := captureStdout(t, func() error {
		var runErr error
		pipeStdin(t, "", func() {
			runErr = runIssueDelete(issueDeleteCmd, []string{issueDeleteTestID1})
		})
		return runErr
	})
	msg := stderr.read()
	if err == nil {
		t.Fatal("delete with closed stdin succeeded, want a non-zero failure")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("error = %v, want it to point at --yes", err)
	}
	if strings.Contains(msg, "Aborted.") {
		t.Fatalf("stderr = %q, want a failure before any Aborted. path", msg)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want only the resolving read", requests)
	}
}

// Resolution happens before the prompt and before the batch request, so one
// unknown key aborts the whole batch instead of deleting the resolvable ones.
func TestRunIssueDeleteBatchAbortsOnUnknownKey(t *testing.T) {
	var wrote bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/MUL-1":
			json.NewEncoder(w).Encode(map[string]any{"id": issueDeleteTestID1, "identifier": "MUL-1"})
		case r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "issue not found"})
		default:
			wrote = true
			t.Errorf("unexpected write %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, true, "json")

	err := runIssueDelete(issueDeleteCmd, []string{"MUL-1", "MUL-404"})
	if err == nil || !strings.Contains(err.Error(), `resolve issue "MUL-404"`) {
		t.Fatalf("error = %v, want a resolve failure for MUL-404", err)
	}
	if wrote {
		t.Fatal("wrote to the server, want no writes after a failed resolution")
	}
}

// The batch route reports only how many ids it deleted, so a partial batch is
// an error with JSON output that says so instead of a silent success or a
// success-shaped document.
func TestRunIssueDeletePartialBatchReportsJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet:
			id := strings.TrimPrefix(r.URL.Path, "/api/issues/")
			json.NewEncoder(w).Encode(map[string]any{"id": id, "identifier": "MUL-" + id[:1]})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/batch-delete":
			json.NewEncoder(w).Encode(map[string]any{"deleted": 1})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, true, "json")

	out, err := captureStdout(t, func() error {
		return runIssueDelete(issueDeleteCmd, []string{issueDeleteTestID1, issueDeleteTestID2})
	})
	if err == nil || !strings.Contains(err.Error(), "deleted 1 of 2 issues") {
		t.Fatalf("error = %v, want a partial-batch error", err)
	}

	var result struct {
		IDs          []string `json:"ids"`
		Deleted      bool     `json:"deleted"`
		DeletedCount int      `json:"deleted_count"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	if result.Deleted {
		t.Fatalf("output = %q, want deleted:false for a partial batch", out)
	}
	if result.DeletedCount != 1 {
		t.Fatalf("deleted_count = %d, want 1", result.DeletedCount)
	}
	if len(result.IDs) != 2 || result.IDs[0] != issueDeleteTestID1 || result.IDs[1] != issueDeleteTestID2 {
		t.Fatalf("ids = %v, want the requested UUIDs", result.IDs)
	}
}

func TestRunIssueDeleteTableOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"id": issueDeleteTestID1, "identifier": "MUL-1"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, true, "table")

	out, err := captureStdout(t, func() error {
		return runIssueDelete(issueDeleteCmd, []string{"MUL-1"})
	})
	if err != nil {
		t.Fatalf("run delete: %v", err)
	}
	if out != "Deleted issue MUL-1\n" {
		t.Fatalf("stdout = %q, want the human-facing line", out)
	}
}

// With --output json the prompt still goes to stderr, so stdout stays one
// parseable JSON document for scripts.
func TestRunIssueDeletePromptKeepsStdoutJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"id": issueDeleteTestID1, "identifier": "MUL-1"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, false, "json")

	stderr := captureStderr(t)
	out, err := captureStdout(t, func() error {
		var runErr error
		pipeStdin(t, "y\n", func() {
			runErr = runIssueDelete(issueDeleteCmd, []string{"MUL-1"})
		})
		return runErr
	})
	if err != nil {
		t.Fatalf("run delete: %v", err)
	}
	var result struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout %q is not pure JSON: %v", out, err)
	}
	if result.ID != issueDeleteTestID1 || !result.Deleted {
		t.Fatalf("output = %q, want the deleted issue", out)
	}
	if !strings.Contains(stderr.read(), "Delete issue MUL-1") {
		t.Fatal("prompt missing from stderr")
	}
}

func TestRunIssueDeletePermissionErrorIsSurfaced(t *testing.T) {
	const serverMessage = "issue deletion is restricted to its creator, the creating agent's owner, and workspace owners or admins"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"id": issueDeleteTestID1, "identifier": "MUL-1"})
			return
		}
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": serverMessage})
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, true, "table")

	err := runIssueDelete(issueDeleteCmd, []string{issueDeleteTestID1})
	if err == nil {
		t.Fatal("delete succeeded, want the server's refusal")
	}
	// FormatError is what the user sees; the 403 copy must be the server's,
	// not the generic kind message.
	if got := cli.FormatError(err, false); !strings.Contains(got, serverMessage) {
		t.Fatalf("formatted error = %q, want the server's %q", got, serverMessage)
	}
}

func TestRunIssueDeleteBatchPermissionErrorIsSurfaced(t *testing.T) {
	const serverMessage = "batch deletion is restricted to workspace owners or admins"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			id := strings.TrimPrefix(r.URL.Path, "/api/issues/")
			json.NewEncoder(w).Encode(map[string]any{"id": id, "identifier": "MUL-1"})
			return
		}
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": serverMessage})
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, true, "json")

	err := runIssueDelete(issueDeleteCmd, []string{issueDeleteTestID1, issueDeleteTestID2})
	if err == nil {
		t.Fatal("delete succeeded, want the server's refusal")
	}
	if got := cli.FormatError(err, false); !strings.Contains(got, serverMessage) {
		t.Fatalf("formatted error = %q, want the server's %q", got, serverMessage)
	}
}

func TestRunIssueDeleteNotFoundTextIsSurfaced(t *testing.T) {
	const serverMessage = "issue not found"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": serverMessage})
	}))
	defer srv.Close()
	setIssueDeleteTestEnv(t, srv.URL)
	setIssueDeleteFlags(t, true, "table")

	err := runIssueDelete(issueDeleteCmd, []string{issueDeleteTestID1})
	if err == nil {
		t.Fatal("delete succeeded, want a 404")
	}
	if got := cli.FormatError(err, false); !strings.Contains(got, serverMessage) {
		t.Fatalf("formatted error = %q, want the server's %q", got, serverMessage)
	}
}

func issueDeleteRequestsEqual(got, want []issueDeleteRequest) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
