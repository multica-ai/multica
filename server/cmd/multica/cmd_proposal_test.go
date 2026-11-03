package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	testProposalSquadID   = "11111111-1111-1111-1111-111111111111"
	testProposalHandoffID = "22222222-2222-2222-2222-222222222222"
)

func newProposalCreateTestCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "create"}
	addProposalCreateFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return cmd
}

func newProposalListTestCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "list"}
	addProposalListFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return cmd
}

// validProposalArgs is a complete set of create flags; tests drop or add one.
func validProposalArgs(extra ...string) []string {
	args := []string{
		"--squad", testProposalSquadID,
		"--title", "Pilot usage pricing",
		"--summary", "Competitors moved to usage-based pricing.",
		"--evidence", "https://example.com/a",
		"--evidence", "https://example.com/b",
		"--recommendation", "Pilot it for new accounts.",
		"--option", "Keep seat pricing",
		"--option", "Pilot usage pricing",
		"--cost-risk", "Two weeks of work; churn risk.",
		"--ask", "Approve the pilot.",
	}
	return append(args, extra...)
}

func withoutFlag(args []string, flag string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == flag {
			i++ // drop its value too
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func TestProposalCreateValidatesClientSide(t *testing.T) {
	// No server is configured, so any request attempt would fail differently
	// from the validation error asserted here.
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing squad", withoutFlag(validProposalArgs(), "--squad"), "--squad is required"},
		{"missing title", withoutFlag(validProposalArgs(), "--title"), "--title is required"},
		{"missing summary", withoutFlag(validProposalArgs(), "--summary"), "--summary is required"},
		{"missing evidence", withoutFlag(validProposalArgs(), "--evidence"), "--evidence is required"},
		{"blank evidence", append(withoutFlag(validProposalArgs(), "--evidence"), "--evidence", "  "), "--evidence is required"},
		{"missing recommendation", withoutFlag(validProposalArgs(), "--recommendation"), "--recommendation is required"},
		{"missing option", withoutFlag(validProposalArgs(), "--option"), "--option is required"},
		{"missing cost-risk", withoutFlag(validProposalArgs(), "--cost-risk"), "--cost-risk is required"},
		{"missing ask", withoutFlag(validProposalArgs(), "--ask"), "--ask is required"},
		{"urgent without reason", validProposalArgs("--urgent"), "--urgent-reason is required"},
		{"reason without urgent", validProposalArgs("--urgent-reason", "now"), "--urgent-reason only applies"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runProposalCreate(newProposalCreateTestCmd(t, tc.args...), nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestProposalCreateSendsTheExpectedBody(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/proposals" {
			t.Errorf("request = %s %s, want POST /api/proposals", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "p-1", "issue_identifier": "MUL-7", "urgent": got["urgent"] == true})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runProposalCreate(newProposalCreateTestCmd(t, validProposalArgs(
			"--handoff-squad", testProposalHandoffID,
			"--urgent", "--urgent-reason", "The contract lapses tomorrow.")...), nil)
	})
	if err != nil {
		t.Fatalf("runProposalCreate: %v", err)
	}

	want := map[string]any{
		"squad_id":         testProposalSquadID,
		"title":            "Pilot usage pricing",
		"summary":          "Competitors moved to usage-based pricing.",
		"evidence":         []any{"https://example.com/a", "https://example.com/b"},
		"recommendation":   "Pilot it for new accounts.",
		"options":          []any{"Keep seat pricing", "Pilot usage pricing"},
		"cost_risk":        "Two weeks of work; churn risk.",
		"ask":              "Approve the pilot.",
		"handoff_squad_id": testProposalHandoffID,
		"urgent":           true,
		"urgent_reason":    "The contract lapses tomorrow.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %#v\nwant  %#v", got, want)
	}
	if !strings.Contains(out, "p-1") || !strings.Contains(out, "MUL-7") || !strings.Contains(out, "urgent") {
		t.Fatalf("output should name the proposal and say it is urgent:\n%s", out)
	}
}

func TestProposalCreateOmitsOptionalFields(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "p-1", "issue_identifier": "MUL-7"})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runProposalCreate(newProposalCreateTestCmd(t, validProposalArgs()...), nil)
	})
	if err != nil {
		t.Fatalf("runProposalCreate: %v", err)
	}
	for _, key := range []string{"handoff_squad_id", "urgent", "urgent_reason"} {
		if _, ok := got[key]; ok {
			t.Errorf("body should not carry %q: %#v", key, got)
		}
	}
	if !strings.Contains(out, "queued for the next brief") {
		t.Errorf("output should say the proposal is queued:\n%s", out)
	}
}

func TestProposalCreateResolvesSquadNames(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/squads":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": testProposalSquadID, "name": "Research"},
				{"id": testProposalHandoffID, "name": "Product Discovery"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/proposals":
			_ = json.NewDecoder(r.Body).Decode(&got)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "p-1"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	args := append(withoutFlag(validProposalArgs(), "--squad"), "--squad", "research", "--handoff-squad", "Product Discovery")
	if _, err := captureStdout(t, func() error {
		return runProposalCreate(newProposalCreateTestCmd(t, args...), nil)
	}); err != nil {
		t.Fatalf("runProposalCreate: %v", err)
	}
	if got["squad_id"] != testProposalSquadID || got["handoff_squad_id"] != testProposalHandoffID {
		t.Fatalf("squad names were not resolved to ids: %#v", got)
	}

	// An unknown name stops before anything is filed.
	args = append(withoutFlag(validProposalArgs(), "--squad"), "--squad", "Nope")
	err := runProposalCreate(newProposalCreateTestCmd(t, args...), nil)
	if err == nil || !strings.Contains(err.Error(), `no squad named "Nope"`) {
		t.Fatalf("error = %v, want unknown squad", err)
	}
}

func TestProposalCreatePrintsTheLimitMessageVerbatim(t *testing.T) {
	const message = "this squad already has 3 proposals waiting for the boss's decision (the limit); wait until some are decided before filing another."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "proposal_limit", "error": message})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	err := runProposalCreate(newProposalCreateTestCmd(t, validProposalArgs()...), nil)
	if err == nil {
		t.Fatal("expected an error when the server refuses")
	}
	if got := cli.FormatError(err, false); got != message {
		t.Fatalf("printed %q, want the server message verbatim %q", got, message)
	}
}

func TestProposalCreateOtherConflictsKeepTheirUsualFormatting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "something_else", "error": "other conflict"})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	err := runProposalCreate(newProposalCreateTestCmd(t, validProposalArgs()...), nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := cli.FormatError(err, false); got == "other conflict" || !strings.Contains(got, "other conflict") {
		t.Fatalf("a non-limit conflict should go through the generic formatting, got %q", got)
	}
}

func TestProposalListSendsFiltersAndPrintsJSON(t *testing.T) {
	var query map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/proposals" {
			t.Errorf("request = %s %s, want GET /api/proposals", r.Method, r.URL.Path)
		}
		query = r.URL.Query()
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "p-1", "issue_identifier": "MUL-7", "state": "pending", "urgent": true, "title": "Pilot usage pricing"},
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runProposalList(newProposalListTestCmd(t, "--state", "pending", "--squad", testProposalSquadID, "--json"), nil)
	})
	if err != nil {
		t.Fatalf("runProposalList: %v", err)
	}
	if query["state"][0] != "pending" || query["squad_id"][0] != testProposalSquadID {
		t.Fatalf("query = %v", query)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 || rows[0]["id"] != "p-1" {
		t.Fatalf("--json output = %q (%v)", out, err)
	}
}

func TestProposalListTable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("unfiltered list should send no query, got %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "p-1", "issue_identifier": "MUL-7", "state": "approved", "urgent": false, "title": "Pilot usage pricing"},
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runProposalList(newProposalListTestCmd(t), nil)
	})
	if err != nil {
		t.Fatalf("runProposalList: %v", err)
	}
	for _, want := range []string{"ID", "STATE", "p-1", "MUL-7", "approved", "Pilot usage pricing"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestProposalListRejectsUnknownState(t *testing.T) {
	err := runProposalList(newProposalListTestCmd(t, "--state", "done"), nil)
	if err == nil || !strings.Contains(err.Error(), "invalid --state") {
		t.Fatalf("error = %v, want invalid --state", err)
	}
}

func TestProposalCommandIsRegistered(t *testing.T) {
	found, _, err := rootCmd.Find([]string{"proposal", "create"})
	if err != nil || found != proposalCreateCmd {
		t.Fatalf("proposal create is not reachable from the root command: %v", err)
	}
	found, _, err = rootCmd.Find([]string{"proposal", "revise"})
	if err != nil || found != proposalReviseCmd {
		t.Fatalf("proposal revise is not reachable from the root command: %v", err)
	}
	found, _, err = rootCmd.Find([]string{"proposal", "list"})
	if err != nil || found != proposalListCmd {
		t.Fatalf("proposal list is not reachable from the root command: %v", err)
	}
}

const testProposalRevisionID = "33333333-3333-3333-3333-333333333333"

func newProposalReviseTestCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "revise"}
	addProposalReviseFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return cmd
}

// validReviseArgs is a complete set of revise flags: the body and no squad.
func validReviseArgs(extra ...string) []string {
	args := []string{
		"--title", "Pilot usage pricing, new accounts",
		"--summary", "Pilot usage pricing on new accounts only.",
		"--evidence", "https://example.com/a",
		"--recommendation", "Pilot it for new accounts.",
		"--option", "Keep seat pricing",
		"--cost-risk", "Two weeks of work; churn risk.",
		"--ask", "Approve a six-week pilot.",
	}
	return append(args, extra...)
}

func TestProposalReviseSendsTheWholeBody(t *testing.T) {
	var got map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": testProposalRevisionID, "revision": 2, "state": "pending"})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runProposalRevise(newProposalReviseTestCmd(t, validReviseArgs()...), []string{testProposalRevisionID})
	})
	if err != nil {
		t.Fatalf("runProposalRevise: %v", err)
	}
	if path != "POST /api/proposals/"+testProposalRevisionID+"/revision" {
		t.Fatalf("request = %s", path)
	}
	want := map[string]any{
		"title":          "Pilot usage pricing, new accounts",
		"summary":        "Pilot usage pricing on new accounts only.",
		"evidence":       []any{"https://example.com/a"},
		"recommendation": "Pilot it for new accounts.",
		"options":        []any{"Keep seat pricing"},
		"cost_risk":      "Two weeks of work; churn risk.",
		"ask":            "Approve a six-week pilot.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %#v\nwant  %#v", got, want)
	}
	if !strings.Contains(out, "revision 2") || !strings.Contains(out, testProposalRevisionID) {
		t.Errorf("output should name the proposal and its revision:\n%s", out)
	}
}

func TestProposalReviseValidatesClientSide(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing title", withoutFlag(validReviseArgs(), "--title"), "--title is required"},
		{"missing ask", withoutFlag(validReviseArgs(), "--ask"), "--ask is required"},
		{"missing evidence", withoutFlag(validReviseArgs(), "--evidence"), "--evidence is required"},
		{"missing option", withoutFlag(validReviseArgs(), "--option"), "--option is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runProposalRevise(newProposalReviseTestCmd(t, tc.args...), []string{testProposalRevisionID})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestProposalReviseRejectsANonUUIDId(t *testing.T) {
	err := runProposalRevise(newProposalReviseTestCmd(t, validReviseArgs()...), []string{"pilot-pricing"})
	if err == nil || !strings.Contains(err.Error(), "proposal id must be a UUID") {
		t.Fatalf("error = %v, want a UUID complaint", err)
	}
}

func TestProposalReviseRefusalsKeepTheirMessages(t *testing.T) {
	t.Run("a limit is printed as the server wrote it", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "proposal_limit", "error": "the boss already has 3 proposals waiting"})
		}))
		defer srv.Close()
		setCLITestServerEnv(t, srv.URL)
		err := runProposalRevise(newProposalReviseTestCmd(t, validReviseArgs()...), []string{testProposalRevisionID})
		if err == nil || cli.FormatError(err, false) != "the boss already has 3 proposals waiting" {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("another conflict keeps the usual formatting", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "proposal_not_revising", "error": "not waiting for changes"})
		}))
		defer srv.Close()
		setCLITestServerEnv(t, srv.URL)
		err := runProposalRevise(newProposalReviseTestCmd(t, validReviseArgs()...), []string{testProposalRevisionID})
		if err == nil || !strings.Contains(cli.FormatError(err, false), "not waiting for changes") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestProposalCreatePrintsRelatedWork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "p-1", "issue_identifier": "MUL-7", "urgent": false,
			"related": []any{
				map[string]any{"kind": "proposal", "issue_identifier": "MUL-3", "state": "approved", "title": "Pilot usage pricing"},
				map[string]any{"kind": "decision", "title": "Decision: usage pricing pilot"},
			},
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runProposalCreate(newProposalCreateTestCmd(t, validProposalArgs()...), nil)
	})
	if err != nil {
		t.Fatalf("runProposalCreate: %v", err)
	}
	for _, want := range []string{"proposal MUL-3 (approved): Pilot usage pricing", "decision: Decision: usage pricing pilot"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should list the related work %q:\n%s", want, out)
		}
	}
}

func TestProposalListAcceptsTheLifecycleStates(t *testing.T) {
	var query map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	for _, state := range []string{"revising", "expired"} {
		if _, err := captureStdout(t, func() error {
			return runProposalList(newProposalListTestCmd(t, "--state", state), nil)
		}); err != nil {
			t.Fatalf("--state %s: %v", state, err)
		}
		if query["state"][0] != state {
			t.Fatalf("--state %s sent %v", state, query)
		}
	}
	err := runProposalList(newProposalListTestCmd(t, "--state", "snoozed"), nil)
	if err == nil || !strings.Contains(err.Error(), "revising, expired") {
		t.Fatalf("error = %v, want the valid states listed", err)
	}
}

func TestProposalDecideRefusesAMalformedOutcomeBeforeCallingTheServer(t *testing.T) {
	cmd := proposalDecideCmd
	if err := cmd.Flags().Set("decision", "approve"); err != nil {
		t.Fatal(err)
	}
	defer cmd.Flags().Set("decision", "")
	if err := cmd.Flags().Set("outcome", "delete"); err != nil {
		t.Fatal(err)
	}
	defer cmd.Flags().Set("outcome", "")

	err := runProposalDecide(cmd, []string{"11111111-1111-1111-1111-111111111111"})
	if err == nil || !strings.Contains(err.Error(), "invalid --outcome") {
		t.Fatalf("a malformed outcome must be refused client side, got %v", err)
	}
}
