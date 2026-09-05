package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const (
	testOwnersDefID   = "b0b0b0b0-1111-4111-8111-111111111111"
	testStaleOptionID = "99999999-9999-4999-8999-999999999999"
	testMemberAdaID   = "abababab-2222-4222-8222-222222222222"
	testMemberGoneID  = "abababab-3333-4333-8333-333333333333"
)

// resolvePropertiesTestCatalog is the filter catalog plus a multi_actor
// definition, so both multi-value shapes are covered.
func resolvePropertiesTestCatalog() []propertyDTO {
	owners := propertyDTO{ID: testOwnersDefID, Name: "Owners", Type: "multi_actor"}
	return append(propertyFilterTestCatalog(), owners)
}

// alphaBag sets one value of every type, including an archived definition
// and a type this build does not know.
func alphaBag() map[string]any {
	return map[string]any{
		testImpactDefID:    testImpactHighID,
		testBlockedDefID:   true,
		testNotesDefID:     "needs a second look",
		testArchivedDefID:  testImpactLowID,
		testPlatformsDefID: []any{testPlatformsIOSID},
		testReviewerDefID:  "member:" + testMemberAdaID,
		testScoreDefID:     float64(42),
		testShipDateDefID:  "2026-08-28",
		testSpecDefID:      "https://example.com/spec",
		testFutureDefID:    "meh",
		testOwnersDefID:    []any{"member:" + testMemberAdaID, "member:" + testMemberGoneID},
	}
}

// alphaRows is alphaBag resolved against the catalog: catalog order, names
// beside ids, the archived definition still resolved, the unknown type and
// the removed member rendered raw.
func alphaRows() []issuePropertyValueRow {
	return []issuePropertyValueRow{
		{PropertyID: testImpactDefID, Name: "Impact", Type: "select", Value: testImpactHighID, Display: "High"},
		{PropertyID: testBlockedDefID, Name: "Blocked", Type: "checkbox", Value: true, Display: "✓"},
		{PropertyID: testNotesDefID, Name: "Notes", Type: "text", Value: "needs a second look", Display: "needs a second look"},
		{PropertyID: testArchivedDefID, Name: "Old Impact", Type: "select", Value: testImpactLowID, Display: "Legacy", Archived: true},
		{PropertyID: testPlatformsDefID, Name: "Platforms", Type: "multi_select", Value: []any{testPlatformsIOSID}, Display: "iOS", DisplayValues: []string{"iOS"}},
		{PropertyID: testReviewerDefID, Name: "Reviewer", Type: "actor", Value: "member:" + testMemberAdaID, Display: "Ada"},
		{PropertyID: testScoreDefID, Name: "Score", Type: "number", Value: float64(42), Display: "42"},
		{PropertyID: testShipDateDefID, Name: "Ship Date", Type: "date", Value: "2026-08-28", Display: "2026-08-28"},
		{PropertyID: testSpecDefID, Name: "Spec", Type: "url", Value: "https://example.com/spec", Display: "https://example.com/spec"},
		{PropertyID: testFutureDefID, Name: "Sentiment", Type: "mood", Value: "meh", Display: "meh"},
		{PropertyID: testOwnersDefID, Name: "Owners", Type: "multi_actor",
			Value:         []any{"member:" + testMemberAdaID, "member:" + testMemberGoneID},
			Display:       "Ada, member:" + testMemberGoneID,
			DisplayValues: []string{"Ada", "member:" + testMemberGoneID}},
	}
}

func testIssue(id, key string, bag any) map[string]any {
	issue := map[string]any{"id": id, "identifier": key, "title": key + " title", "status": "todo"}
	if bag != nil {
		issue["properties"] = bag
	}
	return issue
}

// resolveTestServer serves a fixed page, the catalog and the member list,
// counting the catalog and member requests so tests can pin when each is
// (not) made. Status overrides fail an endpoint.
type resolveTestServer struct {
	page          []map[string]any
	catalog       []propertyDTO
	catalogStatus int
	membersStatus int

	propertiesCalls int
	membersCalls    int
	issueQueries    []url.Values
}

func newResolveTestServer(t *testing.T, page ...map[string]any) *resolveTestServer {
	t.Helper()
	s := &resolveTestServer{page: page, catalog: resolvePropertiesTestCatalog()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/properties":
			s.propertiesCalls++
			if s.catalogStatus != 0 {
				http.Error(w, "catalog unavailable", s.catalogStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"properties": s.catalog})
		case "/api/issues":
			s.issueQueries = append(s.issueQueries, r.URL.Query())
			_ = json.NewEncoder(w).Encode(map[string]any{"issues": s.page, "total": len(s.page)})
		case "/api/issues/MUL-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "issue-1", "identifier": "MUL-1"})
		case "/api/issues/issue-1":
			_ = json.NewEncoder(w).Encode(s.page[0])
		case "/api/issues/issue-1/properties/" + testReviewerDefID:
			var body struct {
				Value any `json:"value"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{"properties": map[string]any{testReviewerDefID: body.Value}})
		case "/api/workspaces/ws-1/members":
			s.membersCalls++
			if s.membersStatus != 0 {
				http.Error(w, "members unavailable", s.membersStatus)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"user_id": testMemberAdaID, "name": "Ada"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	return s
}

// jsonValue round-trips v through encoding/json so fixtures written with Go
// types compare against what the CLI decoded and printed.
func jsonValue(t *testing.T, v any) any {
	t.Helper()
	buf, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(buf, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func runIssueListJSON(t *testing.T, flags map[string]string) (string, map[string]any, error) {
	t.Helper()
	cmd := newIssueListTestCmd()
	_ = cmd.Flags().Set("output", "json")
	for name, value := range flags {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set --%s %q: %v", name, value, err)
		}
	}
	out, err := captureStdout(t, func() error { return runIssueList(cmd, nil) })
	if err != nil {
		return out, nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}
	return out, resp, nil
}

func issuesOf(t *testing.T, resp map[string]any) []map[string]any {
	t.Helper()
	raw, _ := resp["issues"].([]any)
	issues := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		issue, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("issue entry is %T", item)
		}
		issues = append(issues, issue)
	}
	return issues
}

func TestRunIssueListResolvesProperties(t *testing.T) {
	srv := newResolveTestServer(t,
		testIssue("issue-1", "MUL-1", alphaBag()),
		testIssue("issue-2", "MUL-2", map[string]any{}),
		testIssue("issue-3", "MUL-3", map[string]any{testImpactDefID: testStaleOptionID}),
		testIssue("issue-4", "MUL-4", nil),
	)

	_, resp, err := runIssueListJSON(t, map[string]string{"resolve-properties": "true"})
	if err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	issues := issuesOf(t, resp)
	if len(issues) != 4 {
		t.Fatalf("issues = %d, want 4", len(issues))
	}

	if got, want := issues[0]["properties"], jsonValue(t, alphaRows()); !reflect.DeepEqual(got, want) {
		t.Errorf("resolved rows differ\n got: %#v\nwant: %#v", got, want)
	}
	// Pin the wire keys independently of the struct tags: display_values
	// only on multi-value rows, archived only on the archived definition.
	rows, _ := issues[0]["properties"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		_, hasValues := row["display_values"]
		multi := row["type"] == "multi_select" || row["type"] == "multi_actor"
		if hasValues != multi {
			t.Errorf("row %v: display_values present = %v, want %v", row["name"], hasValues, multi)
		}
		_, hasArchived := row["archived"]
		if hasArchived != (row["property_id"] == testArchivedDefID) {
			t.Errorf("row %v: archived present = %v", row["name"], hasArchived)
		}
	}
	if got := issues[0]["title"]; got != "MUL-1 title" {
		t.Errorf("other issue fields must survive; title = %v", got)
	}

	if got := issues[1]["properties"]; !reflect.DeepEqual(got, []any{}) {
		t.Errorf("empty bag = %#v, want []", got)
	}

	// A stored option id that is no longer in the definition keeps its key
	// and surfaces the raw id, the way the server sorts it as NULL rather
	// than failing.
	stale := []issuePropertyValueRow{{PropertyID: testImpactDefID, Name: "Impact", Type: "select", Value: testStaleOptionID, Display: testStaleOptionID}}
	if got, want := issues[2]["properties"], jsonValue(t, stale); !reflect.DeepEqual(got, want) {
		t.Errorf("stale option rows = %#v, want %#v", got, want)
	}

	if _, present := issues[3]["properties"]; present {
		t.Errorf("an issue without a properties key must stay without one: %#v", issues[3]["properties"])
	}

	for _, key := range []string{"total", "limit", "offset", "has_more"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("wrapper key %q missing from %v", key, resp)
		}
	}
	if srv.propertiesCalls != 1 || srv.membersCalls != 1 {
		t.Errorf("catalog calls = %d, members calls = %d; want one each", srv.propertiesCalls, srv.membersCalls)
	}
}

func TestRunIssueListWithoutResolveKeepsRawBag(t *testing.T) {
	srv := newResolveTestServer(t, testIssue("issue-1", "MUL-1", alphaBag()))

	plain, resp, err := runIssueListJSON(t, nil)
	if err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	if got, want := issuesOf(t, resp)[0]["properties"], jsonValue(t, alphaBag()); !reflect.DeepEqual(got, want) {
		t.Errorf("properties changed without the flag\n got: %#v\nwant: %#v", got, want)
	}
	if srv.propertiesCalls != 0 || srv.membersCalls != 0 {
		t.Errorf("catalog calls = %d, members calls = %d; want none without the flag", srv.propertiesCalls, srv.membersCalls)
	}

	explicit, _, err := runIssueListJSON(t, map[string]string{"resolve-properties": "false"})
	if err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	if explicit != plain {
		t.Errorf("--resolve-properties=false output differs from the default:\n%s\n---\n%s", explicit, plain)
	}
}

func TestRunIssueListResolvePropertiesTableUnaffected(t *testing.T) {
	srv := newResolveTestServer(t, testIssue("issue-1", "MUL-1", alphaBag()))
	srv.catalogStatus = http.StatusInternalServerError

	cmd := newIssueListTestCmd()
	_ = cmd.Flags().Set("resolve-properties", "true")
	if _, err := captureStdout(t, func() error { return runIssueList(cmd, nil) }); err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	if srv.propertiesCalls != 0 || srv.membersCalls != 0 {
		t.Errorf("table output must not fetch the catalog (%d) or members (%d)", srv.propertiesCalls, srv.membersCalls)
	}
}

func TestRunIssueListResolvePropertiesCatalogError(t *testing.T) {
	srv := newResolveTestServer(t, testIssue("issue-1", "MUL-1", alphaBag()))
	srv.catalogStatus = http.StatusInternalServerError

	_, _, err := runIssueListJSON(t, map[string]string{"resolve-properties": "true"})
	if err == nil || !strings.Contains(err.Error(), "list properties") {
		t.Fatalf("err = %v, want the catalog failure", err)
	}
}

func TestRunIssueListResolvePropertiesMembersError(t *testing.T) {
	srv := newResolveTestServer(t, testIssue("issue-1", "MUL-1", alphaBag()))
	srv.membersStatus = http.StatusInternalServerError

	// The flag promises names; a failed member lookup must not print raw
	// references at exit 0.
	_, _, err := runIssueListJSON(t, map[string]string{"resolve-properties": "true"})
	if err == nil || !strings.Contains(err.Error(), "list members") {
		t.Fatalf("err = %v, want the members failure", err)
	}
}

func TestRunIssueListResolvePropertiesSharesCatalogFetch(t *testing.T) {
	srv := newResolveTestServer(t, testIssue("issue-1", "MUL-1", alphaBag()))

	_, resp, err := runIssueListJSON(t, map[string]string{"resolve-properties": "true", "property": "Impact=High"})
	if err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	if srv.propertiesCalls != 1 {
		t.Errorf("catalog calls = %d, want the filter and the resolution to share one", srv.propertiesCalls)
	}
	if got := srv.issueQueries[0].Get("properties"); got == "" {
		t.Errorf("filter was not sent: %v", srv.issueQueries[0])
	}
	if got, want := issuesOf(t, resp)[0]["properties"], jsonValue(t, alphaRows()); !reflect.DeepEqual(got, want) {
		t.Errorf("resolved rows differ\n got: %#v\nwant: %#v", got, want)
	}
}

func TestRunIssueListResolvePropertiesSkipsCatalogWhenNothingToResolve(t *testing.T) {
	srv := newResolveTestServer(t,
		testIssue("issue-2", "MUL-2", map[string]any{}),
		testIssue("issue-4", "MUL-4", nil),
	)
	// A server old enough to omit properties has no catalog route either.
	srv.catalogStatus = http.StatusNotFound

	_, resp, err := runIssueListJSON(t, map[string]string{"resolve-properties": "true"})
	if err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	issues := issuesOf(t, resp)
	if got := issues[0]["properties"]; !reflect.DeepEqual(got, []any{}) {
		t.Errorf("empty bag = %#v, want []", got)
	}
	if _, present := issues[1]["properties"]; present {
		t.Errorf("absent bag was invented: %#v", issues[1]["properties"])
	}
	if srv.propertiesCalls != 0 {
		t.Errorf("catalog calls = %d, want none when no bag holds a value", srv.propertiesCalls)
	}
}

func TestRunIssueListResolvePropertiesUnknownDefinition(t *testing.T) {
	const orphan = "0d0d0d0d-1111-4111-8111-111111111111"
	bag := alphaBag()
	bag[orphan] = "stray"
	newResolveTestServer(t, testIssue("issue-1", "MUL-1", bag))

	_, _, err := runIssueListJSON(t, map[string]string{"resolve-properties": "true"})
	if err == nil || !strings.Contains(err.Error(), "MUL-1") || !strings.Contains(err.Error(), orphan) {
		t.Fatalf("err = %v, want the issue key and the unknown definition id", err)
	}
}

func TestRunIssueListResolvePropertiesRejectsNonObjectBag(t *testing.T) {
	newResolveTestServer(t, testIssue("issue-1", "MUL-1", "oops"))

	_, _, err := runIssueListJSON(t, map[string]string{"resolve-properties": "true"})
	if err == nil || !strings.Contains(err.Error(), "expected an object") {
		t.Fatalf("err = %v, want a shape error", err)
	}
}

func TestRunIssueListResolvePropertiesOneMembersRequestPerPage(t *testing.T) {
	reviewer := map[string]any{testReviewerDefID: "member:" + testMemberAdaID}
	srv := newResolveTestServer(t,
		testIssue("issue-1", "MUL-1", reviewer),
		testIssue("issue-2", "MUL-2", reviewer),
	)
	if _, _, err := runIssueListJSON(t, map[string]string{"resolve-properties": "true"}); err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	if srv.membersCalls != 1 {
		t.Errorf("members calls = %d, want one for the whole page", srv.membersCalls)
	}

	srv = newResolveTestServer(t, testIssue("issue-1", "MUL-1", map[string]any{testImpactDefID: testImpactHighID}))
	if _, _, err := runIssueListJSON(t, map[string]string{"resolve-properties": "true"}); err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	if srv.membersCalls != 0 {
		t.Errorf("members calls = %d, want none without an actor value", srv.membersCalls)
	}
}

// An actor --property turns a member name into an id and --resolve-properties
// turns ids back into names. Both read the member list, and the command
// fetches it once however many actor filters it carries.
func TestRunIssueListActorFilterSharesMembersRequest(t *testing.T) {
	srv := newResolveTestServer(t, testIssue("issue-1", "MUL-1", map[string]any{
		testReviewerDefID: "member:" + testMemberAdaID,
		testOwnersDefID:   []any{"member:" + testMemberAdaID},
	}))
	cmd := newIssueListTestCmd()
	_ = cmd.Flags().Set("output", "json")
	_ = cmd.Flags().Set("resolve-properties", "true")
	for _, flag := range []string{"Reviewer=Ada", "Owners=Ada"} {
		_ = cmd.Flags().Set("property", flag)
	}
	out, err := captureStdout(t, func() error { return runIssueList(cmd, nil) })
	if err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	if srv.membersCalls != 1 {
		t.Errorf("members calls = %d, want one shared by the filter and the resolution", srv.membersCalls)
	}
	want := map[string][]string{
		testReviewerDefID: {"member:" + testMemberAdaID},
		testOwnersDefID:   {"member:" + testMemberAdaID},
	}
	if got := decodePropertiesParam(t, srv.issueQueries); !reflect.DeepEqual(got, want) {
		t.Errorf("filter sent = %v, want %v", got, want)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}
	rows := jsonValue(t, []issuePropertyValueRow{
		{PropertyID: testReviewerDefID, Name: "Reviewer", Type: "actor", Value: "member:" + testMemberAdaID, Display: "Ada"},
		{PropertyID: testOwnersDefID, Name: "Owners", Type: "multi_actor", Value: []any{"member:" + testMemberAdaID}, Display: "Ada", DisplayValues: []string{"Ada"}},
	})
	if got := issuesOf(t, resp)[0]["properties"]; !reflect.DeepEqual(got, rows) {
		t.Errorf("resolved rows differ\n got: %#v\nwant: %#v", got, rows)
	}
}

func newIssueGetTestCmd(output string, resolve bool) *cobra.Command {
	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", output, "")
	cmd.Flags().Bool("resolve-properties", resolve, "")
	return cmd
}

func TestRunIssueGetResolvesProperties(t *testing.T) {
	srv := newResolveTestServer(t, testIssue("issue-1", "MUL-1", alphaBag()))

	out, err := captureStdout(t, func() error { return runIssueGet(newIssueGetTestCmd("json", true), []string{"MUL-1"}) })
	if err != nil {
		t.Fatalf("runIssueGet: %v", err)
	}
	var issue map[string]any
	if err := json.Unmarshal([]byte(out), &issue); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}
	if got, want := issue["properties"], jsonValue(t, alphaRows()); !reflect.DeepEqual(got, want) {
		t.Errorf("resolved rows differ\n got: %#v\nwant: %#v", got, want)
	}
	if srv.propertiesCalls != 1 || srv.membersCalls != 1 {
		t.Errorf("catalog calls = %d, members calls = %d; want one each", srv.propertiesCalls, srv.membersCalls)
	}

	srv = newResolveTestServer(t, testIssue("issue-1", "MUL-1", alphaBag()))
	out, err = captureStdout(t, func() error { return runIssueGet(newIssueGetTestCmd("json", false), []string{"MUL-1"}) })
	if err != nil {
		t.Fatalf("runIssueGet: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &issue); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}
	if got, want := issue["properties"], jsonValue(t, alphaBag()); !reflect.DeepEqual(got, want) {
		t.Errorf("properties changed without the flag\n got: %#v\nwant: %#v", got, want)
	}
	if srv.propertiesCalls != 0 {
		t.Errorf("catalog calls = %d, want none without the flag", srv.propertiesCalls)
	}

	srv = newResolveTestServer(t, testIssue("issue-1", "MUL-1", alphaBag()))
	srv.catalogStatus = http.StatusInternalServerError
	if _, err := captureStdout(t, func() error { return runIssueGet(newIssueGetTestCmd("table", true), []string{"MUL-1"}) }); err != nil {
		t.Fatalf("runIssueGet table: %v", err)
	}
	if srv.propertiesCalls != 0 {
		t.Errorf("table output must not fetch the catalog (%d)", srv.propertiesCalls)
	}
}
