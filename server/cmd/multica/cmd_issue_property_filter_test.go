package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

const (
	testImpactDefID    = "aaaaaaaa-1111-4111-8111-111111111111"
	testImpactLowID    = "bbbbbbbb-1111-4111-8111-111111111111"
	testImpactMediumID = "bbbbbbbb-2222-4222-8222-222222222222"
	testImpactHighID   = "bbbbbbbb-3333-4333-8333-333333333333"
	testBlockedDefID   = "cccccccc-1111-4111-8111-111111111111"
	testNotesDefID     = "dddddddd-1111-4111-8111-111111111111"
	testArchivedDefID  = "eeeeeeee-1111-4111-8111-111111111111"
	testPlatformsDefID = "ffffffff-1111-4111-8111-111111111111"
	testPlatformsIOSID = "ffffffff-2222-4222-8222-222222222222"
	testReviewerDefID  = "abababab-1111-4111-8111-111111111111"
	testScoreDefID     = "acacacac-1111-4111-8111-111111111111"
	testShipDateDefID  = "adadadad-1111-4111-8111-111111111111"
	testSpecDefID      = "aeaeaeae-1111-4111-8111-111111111111"
	testFutureDefID    = "afafafaf-1111-4111-8111-111111111111"
)

func propertyFilterTestCatalog() []propertyDTO {
	impact := propertyDTO{ID: testImpactDefID, Name: "Impact", Type: "select"}
	impact.Config.Options = []propertyOptionDTO{
		{ID: testImpactLowID, Name: "Low"},
		{ID: testImpactMediumID, Name: "Medium"},
		{ID: testImpactHighID, Name: "High"},
	}
	blocked := propertyDTO{ID: testBlockedDefID, Name: "Blocked", Type: "checkbox"}
	notes := propertyDTO{ID: testNotesDefID, Name: "Notes", Type: "text"}
	archived := propertyDTO{ID: testArchivedDefID, Name: "Old Impact", Type: "select", Archived: true}
	archived.Config.Options = []propertyOptionDTO{{ID: testImpactLowID, Name: "Legacy"}}
	platforms := propertyDTO{ID: testPlatformsDefID, Name: "Platforms", Type: "multi_select"}
	platforms.Config.Options = []propertyOptionDTO{{ID: testPlatformsIOSID, Name: "iOS"}}
	reviewer := propertyDTO{ID: testReviewerDefID, Name: "Reviewer", Type: "actor"}
	score := propertyDTO{ID: testScoreDefID, Name: "Score", Type: "number"}
	shipDate := propertyDTO{ID: testShipDateDefID, Name: "Ship Date", Type: "date"}
	spec := propertyDTO{ID: testSpecDefID, Name: "Spec", Type: "url"}
	// A type this build has never heard of, the way a newer backend would report one.
	future := propertyDTO{ID: testFutureDefID, Name: "Sentiment", Type: "mood"}
	return []propertyDTO{impact, blocked, notes, archived, platforms, reviewer, score, shipDate, spec, future}
}

// newPropertyFilterTestServer serves the fixed property catalog and captures
// each /api/issues query. Counters expose how often each endpoint was hit so
// tests can pin the single-catalog-fetch behavior and error paths that must
// never reach /api/issues.
func newPropertyFilterTestServer(t *testing.T) (issueQueries *[]url.Values, propertiesCalls *int) {
	t.Helper()
	queries := &[]url.Values{}
	calls := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/properties":
			*calls++
			json.NewEncoder(w).Encode(map[string]any{"properties": propertyFilterTestCatalog()})
		case "/api/issues":
			*queries = append(*queries, r.URL.Query())
			json.NewEncoder(w).Encode(map[string]any{"issues": []any{}, "total": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	return queries, calls
}

func listIssuesWithFlags(t *testing.T, flags map[string][]string) error {
	t.Helper()
	cmd := newIssueListTestCmd()
	_ = cmd.Flags().Set("output", "json")
	for name, values := range flags {
		for _, value := range values {
			if err := cmd.Flags().Set(name, value); err != nil {
				t.Fatalf("set --%s %q: %v", name, value, err)
			}
		}
	}
	_, err := captureStdout(t, func() error { return runIssueList(cmd, nil) })
	return err
}

func decodePropertiesParam(t *testing.T, queries []url.Values) map[string][]string {
	t.Helper()
	if len(queries) != 1 {
		t.Fatalf("expected exactly one /api/issues request, got %d", len(queries))
	}
	raw := queries[0].Get("properties")
	if raw == "" {
		t.Fatalf("no properties query param sent; query = %v", queries[0])
	}
	var filter map[string][]string
	if err := json.Unmarshal([]byte(raw), &filter); err != nil {
		t.Fatalf("properties param %q is not valid JSON: %v", raw, err)
	}
	return filter
}

func TestRunIssueListSendsPropertyFilter(t *testing.T) {
	cases := []struct {
		name  string
		flags []string
		want  map[string][]string
	}{
		{
			name:  "select option by name",
			flags: []string{"Impact=High"},
			want:  map[string][]string{testImpactDefID: {testImpactHighID}},
		},
		{
			name:  "case-insensitive property and option names",
			flags: []string{"impact=high"},
			want:  map[string][]string{testImpactDefID: {testImpactHighID}},
		},
		{
			name:  "property and option addressed by UUID",
			flags: []string{testImpactDefID + "=" + testImpactHighID},
			want:  map[string][]string{testImpactDefID: {testImpactHighID}},
		},
		{
			name:  "repeated property flags OR into one definition",
			flags: []string{"Impact=High", "impact=Medium"},
			want:  map[string][]string{testImpactDefID: {testImpactHighID, testImpactMediumID}},
		},
		{
			name:  "duplicate resolved values collapse",
			flags: []string{"Impact=High", "Impact=high"},
			want:  map[string][]string{testImpactDefID: {testImpactHighID}},
		},
		{
			name:  "distinct definitions AND together",
			flags: []string{"Impact=High", "Blocked=true"},
			want: map[string][]string{
				testImpactDefID:  {testImpactHighID},
				testBlockedDefID: {"true"},
			},
		},
		{
			name:  "unset sentinel passes through for a checkbox",
			flags: []string{"Blocked=__none__"},
			want:  map[string][]string{testBlockedDefID: {"__none__"}},
		},
		{
			name:  "unset sentinel passes through for a text property",
			flags: []string{"Notes=__none__"},
			want:  map[string][]string{testNotesDefID: {"__none__"}},
		},
		{
			name:  "multi_select option by name",
			flags: []string{"Platforms=iOS"},
			want:  map[string][]string{testPlatformsDefID: {testPlatformsIOSID}},
		},
		{
			// Stored actor values are canonical lowercase-hyphenated and the
			// filter is exact containment, so an explicit member:<uuid> value
			// must be re-serialized rather than passed through as typed.
			name:  "actor member reference canonicalized",
			flags: []string{"Reviewer=member:ABABABAB-2222-4222-8222-222222222222"},
			want:  map[string][]string{testReviewerDefID: {"member:abababab-2222-4222-8222-222222222222"}},
		},
		{
			name:  "text value forwarded verbatim",
			flags: []string{"Notes=needs a second look"},
			want:  map[string][]string{testNotesDefID: {"needs a second look"}},
		},
		{
			// Text is stored exactly as written, so the filter has to pass
			// through what the user typed.
			name:  "text value keeps surrounding whitespace",
			flags: []string{"Notes= padded "},
			want:  map[string][]string{testNotesDefID: {" padded "}},
		},
		{
			name:  "number value forwarded",
			flags: []string{"Score=42"},
			want:  map[string][]string{testScoreDefID: {"42"}},
		},
		{
			name:  "negative and fractional numbers are accepted",
			flags: []string{"Score=-1.5"},
			want:  map[string][]string{testScoreDefID: {"-1.5"}},
		},
		{
			name:  "date value forwarded",
			flags: []string{"Ship Date=2026-08-28"},
			want:  map[string][]string{testShipDateDefID: {"2026-08-28"}},
		},
		{
			// A url is stored trimmed, so the filter trims to match it.
			name:  "url value trimmed to the stored spelling",
			flags: []string{"Spec= https://example.com/spec "},
			want:  map[string][]string{testSpecDefID: {"https://example.com/spec"}},
		},
		{
			// The store cap is 2000 runes, not bytes.
			name:  "text value at the store length cap is sent",
			flags: []string{"Notes=" + strings.Repeat("é", 2000)},
			want:  map[string][]string{testNotesDefID: {strings.Repeat("é", 2000)}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queries, _ := newPropertyFilterTestServer(t)
			if err := listIssuesWithFlags(t, map[string][]string{"property": tc.flags}); err != nil {
				t.Fatalf("runIssueList: %v", err)
			}
			if got := decodePropertiesParam(t, *queries); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("properties param = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRunIssueListPropertyFilterErrors(t *testing.T) {
	cases := []struct {
		name    string
		flags   []string
		wantErr string
	}{
		{"missing equals", []string{"Impact"}, "Name=Value"},
		{"empty value", []string{"Impact="}, "__none__"},
		{"unknown property lists names", []string{"Nope=High"}, "Impact"},
		{"unknown option lists options", []string{"Impact=Critical"}, "Low"},
		{"archived property", []string{"Old Impact=Legacy"}, "archived"},
		{"checkbox value must be a bool", []string{"Blocked=maybe"}, "true or false"},
		{"whitespace-only value", []string{"Notes=   "}, "__none__"},
		{"number value must be numeric", []string{"Score=high"}, "not a valid number"},
		{"number value must be finite", []string{"Score=NaN"}, "not a finite number"},
		{"date value must be YYYY-MM-DD", []string{"Ship Date=28/08/2026"}, "YYYY-MM-DD"},
		{"unknown property type is rejected", []string{"Sentiment=happy"}, "does not know how to filter"},
		{"url must have a scheme", []string{"Spec=example.com/foo"}, "http(s) URL"},
		{"url must be http or https", []string{"Spec=ftp://example.com/x"}, "http(s) URL"},
		{"url must have a host", []string{"Spec=http://"}, "http(s) URL"},
		{"url over the store length cap", []string{"Spec=https://example.com/" + strings.Repeat("a", 2048)}, "2048 characters or fewer"},
		{"text over the store length cap", []string{"Notes=" + strings.Repeat("a", 2001)}, "2000 characters or fewer"},
		{"comparison operators are reserved", []string{"Impact>=Medium"}, "comparison operators"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queries, _ := newPropertyFilterTestServer(t)
			err := listIssuesWithFlags(t, map[string][]string{"property": tc.flags})
			if err == nil {
				t.Fatalf("expected error for --property %q", tc.flags)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to mention %q", err, tc.wantErr)
			}
			if len(*queries) != 0 {
				t.Fatalf("a failing --property still hit /api/issues: %v", *queries)
			}
		})
	}
}

func TestRunIssueListSendsPropertySort(t *testing.T) {
	cases := []struct {
		name  string
		flags map[string][]string
	}{
		{"by name", map[string][]string{"sort": {"property:Impact"}, "direction": {"desc"}}},
		{"case-insensitive name", map[string][]string{"sort": {"property:impact"}, "direction": {"desc"}}},
		{"by UUID", map[string][]string{"sort": {"property:" + testImpactDefID}, "direction": {"desc"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queries, _ := newPropertyFilterTestServer(t)
			if err := listIssuesWithFlags(t, tc.flags); err != nil {
				t.Fatalf("runIssueList: %v", err)
			}
			if len(*queries) != 1 {
				t.Fatalf("expected one /api/issues request, got %d", len(*queries))
			}
			got := (*queries)[0]
			if got.Get("sort") != "property:"+testImpactDefID {
				t.Fatalf("sort query = %q, want property:%s", got.Get("sort"), testImpactDefID)
			}
			if got.Get("direction") != "desc" {
				t.Fatalf("direction query = %q, want desc", got.Get("direction"))
			}
		})
	}
}

func TestRunIssueListPropertySortErrors(t *testing.T) {
	cases := []struct {
		name    string
		sort    string
		wantErr string
	}{
		{"unknown property", "property:Nope", "not found"},
		{"archived property", "property:Old Impact", "archived"},
		{"type without a sort order", "property:Platforms", "no server-side sort order"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queries, _ := newPropertyFilterTestServer(t)
			err := listIssuesWithFlags(t, map[string][]string{"sort": {tc.sort}})
			if err == nil {
				t.Fatalf("expected error for --sort %q", tc.sort)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to mention %q", err, tc.wantErr)
			}
			if len(*queries) != 0 {
				t.Fatalf("a failing --sort still hit /api/issues: %v", *queries)
			}
		})
	}
}

// TestRunIssueListInvalidSortMentionsPropertyForm guards that the static
// column whitelist still rejects unknown plain columns, and that the error now
// tells the user the property:<name-or-id> form exists.
func TestRunIssueListInvalidSortMentionsPropertyForm(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueListTestCmd()
	_ = cmd.Flags().Set("sort", "bogus")
	err := runIssueList(cmd, nil)
	if err == nil {
		t.Fatal("expected error for invalid --sort")
	}
	if !strings.Contains(err.Error(), "invalid --sort") || !strings.Contains(err.Error(), "property:<name-or-id>") {
		t.Fatalf("error = %q, want it to reject the column and mention property:<name-or-id>", err)
	}
}

// TestRunIssueListFetchesCatalogOnce pins that combining --property and a
// property sort costs a single /api/properties round trip.
func TestRunIssueListFetchesCatalogOnce(t *testing.T) {
	queries, propertiesCalls := newPropertyFilterTestServer(t)
	err := listIssuesWithFlags(t, map[string][]string{
		"property":  {"Impact=High"},
		"sort":      {"property:Impact"},
		"direction": {"desc"},
	})
	if err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	if *propertiesCalls != 1 {
		t.Fatalf("property catalog fetched %d times, want exactly once", *propertiesCalls)
	}
	if len(*queries) != 1 {
		t.Fatalf("expected one /api/issues request, got %d", len(*queries))
	}
}
