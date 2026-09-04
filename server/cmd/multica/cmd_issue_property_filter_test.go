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
	testScoreGtDefID   = "bcbcbcbc-1111-4111-8111-111111111111"
	testRiskDefID      = "bdbdbdbd-1111-4111-8111-111111111111"
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
	// Names that collide with the comparison spellings: a trailing > is only
	// reachable by UUID, an interior > still works under =.
	scoreGt := propertyDTO{ID: testScoreGtDefID, Name: "Score>", Type: "number"}
	risk := propertyDTO{ID: testRiskDefID, Name: "Risk>Reward", Type: "text"}
	return []propertyDTO{impact, blocked, notes, archived, platforms, reviewer, score, shipDate, spec, future, scoreGt, risk}
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

// decodePropertiesParam returns the sent filter with equality members as
// strings and comparison members as {"op", "value"} maps, the two shapes
// parsePropertiesFilterParam accepts.
func decodePropertiesParam(t *testing.T, queries []url.Values) map[string][]any {
	t.Helper()
	if len(queries) != 1 {
		t.Fatalf("expected exactly one /api/issues request, got %d", len(queries))
	}
	raw := queries[0].Get("properties")
	if raw == "" {
		t.Fatalf("no properties query param sent; query = %v", queries[0])
	}
	var filter map[string][]any
	if err := json.Unmarshal([]byte(raw), &filter); err != nil {
		t.Fatalf("properties param %q is not valid JSON: %v", raw, err)
	}
	return filter
}

func opMember(op, value string) map[string]any {
	return map[string]any{"op": op, "value": value}
}

func TestRunIssueListSendsPropertyFilter(t *testing.T) {
	cases := []struct {
		name  string
		flags []string
		want  map[string][]any
	}{
		{
			name:  "select option by name",
			flags: []string{"Impact=High"},
			want:  map[string][]any{testImpactDefID: {testImpactHighID}},
		},
		{
			name:  "case-insensitive property and option names",
			flags: []string{"impact=high"},
			want:  map[string][]any{testImpactDefID: {testImpactHighID}},
		},
		{
			name:  "property and option addressed by UUID",
			flags: []string{testImpactDefID + "=" + testImpactHighID},
			want:  map[string][]any{testImpactDefID: {testImpactHighID}},
		},
		{
			name:  "repeated property flags OR into one definition",
			flags: []string{"Impact=High", "impact=Medium"},
			want:  map[string][]any{testImpactDefID: {testImpactHighID, testImpactMediumID}},
		},
		{
			name:  "duplicate resolved values collapse",
			flags: []string{"Impact=High", "Impact=high"},
			want:  map[string][]any{testImpactDefID: {testImpactHighID}},
		},
		{
			name:  "distinct definitions AND together",
			flags: []string{"Impact=High", "Blocked=true"},
			want: map[string][]any{
				testImpactDefID:  {testImpactHighID},
				testBlockedDefID: {"true"},
			},
		},
		{
			name:  "unset sentinel passes through for a checkbox",
			flags: []string{"Blocked=__none__"},
			want:  map[string][]any{testBlockedDefID: {"__none__"}},
		},
		{
			name:  "unset sentinel passes through for a text property",
			flags: []string{"Notes=__none__"},
			want:  map[string][]any{testNotesDefID: {"__none__"}},
		},
		{
			name:  "multi_select option by name",
			flags: []string{"Platforms=iOS"},
			want:  map[string][]any{testPlatformsDefID: {testPlatformsIOSID}},
		},
		{
			// Stored actor values are canonical lowercase-hyphenated and the
			// filter is exact containment, so an explicit member:<uuid> value
			// must be re-serialized rather than passed through as typed.
			name:  "actor member reference canonicalized",
			flags: []string{"Reviewer=member:ABABABAB-2222-4222-8222-222222222222"},
			want:  map[string][]any{testReviewerDefID: {"member:abababab-2222-4222-8222-222222222222"}},
		},
		{
			name:  "text value forwarded verbatim",
			flags: []string{"Notes=needs a second look"},
			want:  map[string][]any{testNotesDefID: {"needs a second look"}},
		},
		{
			// Text is stored exactly as written, so the filter has to pass
			// through what the user typed.
			name:  "text value keeps surrounding whitespace",
			flags: []string{"Notes= padded "},
			want:  map[string][]any{testNotesDefID: {" padded "}},
		},
		{
			name:  "number value forwarded",
			flags: []string{"Score=42"},
			want:  map[string][]any{testScoreDefID: {"42"}},
		},
		{
			name:  "negative and fractional numbers are accepted",
			flags: []string{"Score=-1.5"},
			want:  map[string][]any{testScoreDefID: {"-1.5"}},
		},
		{
			name:  "date value forwarded",
			flags: []string{"Ship Date=2026-08-28"},
			want:  map[string][]any{testShipDateDefID: {"2026-08-28"}},
		},
		{
			// A url is stored trimmed, so the filter trims to match it.
			name:  "url value trimmed to the stored spelling",
			flags: []string{"Spec= https://example.com/spec "},
			want:  map[string][]any{testSpecDefID: {"https://example.com/spec"}},
		},
		{
			// The store cap is 2000 runes, not bytes.
			name:  "text value at the store length cap is sent",
			flags: []string{"Notes=" + strings.Repeat("é", 2000)},
			want:  map[string][]any{testNotesDefID: {strings.Repeat("é", 2000)}},
		},
		{
			name:  "equality value keeps comparison characters",
			flags: []string{"Notes=a<b"},
			want:  map[string][]any{testNotesDefID: {"a<b"}},
		},
		{
			// "=>" is not a spelling, so the value is the literal ">foo".
			name:  "equality value starting with > stays literal",
			flags: []string{"Notes=>foo"},
			want:  map[string][]any{testNotesDefID: {">foo"}},
		},
		{
			name:  "interior > in a name still resolves under =",
			flags: []string{"Risk>Reward=high"},
			want:  map[string][]any{testRiskDefID: {"high"}},
		},
		{
			name:  "number greater than",
			flags: []string{"Score>3"},
			want:  map[string][]any{testScoreDefID: {opMember("gt", "3")}},
		},
		{
			name:  "number at least",
			flags: []string{"Score>=3"},
			want:  map[string][]any{testScoreDefID: {opMember("gte", "3")}},
		},
		{
			name:  "number less than",
			flags: []string{"Score<3"},
			want:  map[string][]any{testScoreDefID: {opMember("lt", "3")}},
		},
		{
			name:  "number at most",
			flags: []string{"Score<=3"},
			want:  map[string][]any{testScoreDefID: {opMember("lte", "3")}},
		},
		{
			name:  "negative number after a comparison",
			flags: []string{"Score>=-1.5"},
			want:  map[string][]any{testScoreDefID: {opMember("gte", "-1.5")}},
		},
		{
			name:  "whitespace around a comparison is cosmetic",
			flags: []string{" Score >= 3 "},
			want:  map[string][]any{testScoreDefID: {opMember("gte", "3")}},
		},
		{
			name:  "date after",
			flags: []string{"Ship Date>2026-01-01"},
			want:  map[string][]any{testShipDateDefID: {opMember("after", "2026-01-01")}},
		},
		{
			name:  "date before",
			flags: []string{"Ship Date<2026-01-01"},
			want:  map[string][]any{testShipDateDefID: {opMember("before", "2026-01-01")}},
		},
		{
			name:  "text contains",
			flags: []string{"Notes~=review"},
			want:  map[string][]any{testNotesDefID: {opMember("contains", "review")}},
		},
		{
			name:  "url contains needs no scheme",
			flags: []string{"Spec~=example.com"},
			want:  map[string][]any{testSpecDefID: {opMember("contains", "example.com")}},
		},
		{
			// The needle is matched as typed, so spaces and = are part of it.
			name:  "contains needle is sent as typed",
			flags: []string{"Notes~= a=b "},
			want:  map[string][]any{testNotesDefID: {opMember("contains", " a=b ")}},
		},
		{
			// LIKE escaping is the server's job; escaping here too would
			// search for a literal backslash.
			name:  "contains needle is not LIKE-escaped",
			flags: []string{"Notes~=50%_off"},
			want:  map[string][]any{testNotesDefID: {opMember("contains", "50%_off")}},
		},
		{
			name:  "contains needle at the server cap is sent",
			flags: []string{"Notes~=" + strings.Repeat("é", 2000)},
			want:  map[string][]any{testNotesDefID: {opMember("contains", strings.Repeat("é", 2000))}},
		},
		{
			// The unset sentinel only means unset under =.
			name:  "contains treats __none__ as a needle",
			flags: []string{"Notes~=__none__"},
			want:  map[string][]any{testNotesDefID: {opMember("contains", "__none__")}},
		},
		{
			name:  "comparison with the property addressed by UUID",
			flags: []string{testScoreDefID + ">=3"},
			want:  map[string][]any{testScoreDefID: {opMember("gte", "3")}},
		},
		{
			// OR within a definition applies to comparisons too, so this
			// is "below 1 or above 10", not a range.
			name:  "repeated comparisons OR into one definition",
			flags: []string{"Score<1", "Score>10"},
			want:  map[string][]any{testScoreDefID: {opMember("lt", "1"), opMember("gt", "10")}},
		},
		{
			name:  "equality and comparison on one property are both sent",
			flags: []string{"Score=3", "Score>3"},
			want:  map[string][]any{testScoreDefID: {"3", opMember("gt", "3")}},
		},
		{
			name:  "duplicate comparison members collapse",
			flags: []string{"Score>=3", "Score >= 3"},
			want:  map[string][]any{testScoreDefID: {opMember("gte", "3")}},
		},
		{
			// A trailing > is a comparison, never part of a name.
			name:  "Score>=9 compares Score rather than naming Score>",
			flags: []string{"Score>=9"},
			want:  map[string][]any{testScoreDefID: {opMember("gte", "9")}},
		},
		{
			name:  "a property named Score> is reachable by UUID",
			flags: []string{testScoreGtDefID + "=9"},
			want:  map[string][]any{testScoreGtDefID: {"9"}},
		},
		{
			name:  "a property named Score> takes comparisons by UUID",
			flags: []string{testScoreGtDefID + ">=9"},
			want:  map[string][]any{testScoreGtDefID: {opMember("gte", "9")}},
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

// TestSplitPropertyFilter pins the grammar: the name ends at the first "=",
// minus a trailing comparison character; without "=", at the first < or >.
func TestSplitPropertyFilter(t *testing.T) {
	cases := []struct {
		pair, name, op, value string
		ok                    bool
	}{
		{"Impact=High", "Impact", "=", "High", true},
		{"Score>=3", "Score", ">=", "3", true},
		{"Score<=3", "Score", "<=", "3", true},
		{"Score!=3", "Score", "!=", "3", true},
		{"Notes~=a=b", "Notes", "~=", "a=b", true},
		{"Score>3", "Score", ">", "3", true},
		{"Ship Date<2026-01-01", "Ship Date", "<", "2026-01-01", true},
		{" Score >= 3 ", "Score", ">=", " 3 ", true},
		{"Score > 3", "Score", ">", " 3", true},
		{"Risk>Reward=high", "Risk>Reward", "=", "high", true},
		{"Risk>Reward>3", "Risk", ">", "Reward>3", true},
		{"Notes=a<b", "Notes", "=", "a<b", true},
		{"Notes==x", "Notes", "=", "=x", true},
		{"Score=>3", "Score", "=", ">3", true},
		{">=3", "", ">=", "3", true},
		{"Score", "", "", "", false},
		{"Score!3", "", "", "", false},
		{"Notes~foo", "", "", "", false},
	}
	for _, tc := range cases {
		name, op, value, ok := splitPropertyFilter(tc.pair)
		if name != tc.name || op != tc.op || value != tc.value || ok != tc.ok {
			t.Errorf("splitPropertyFilter(%q) = (%q, %q, %q, %v), want (%q, %q, %q, %v)",
				tc.pair, name, op, value, ok, tc.name, tc.op, tc.value, tc.ok)
		}
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
		{"comparison on a select names the type", []string{"Impact>=Medium"}, "select properties only support ="},
		{"comparison on a checkbox", []string{"Blocked>true"}, "checkbox properties only support ="},
		{"comparison on an actor", []string{"Reviewer>x"}, "actor properties only support ="},
		{"comparison on text lists contains", []string{"Notes>3"}, "text properties support ~="},
		{"contains on a number lists the comparisons", []string{"Score~=3"}, "number properties support >, >=, <, <="},
		{"contains on a date lists the comparisons", []string{"Ship Date~=2026"}, "date properties support >, <"},
		{"inclusive date lower bound names the strict form", []string{"Ship Date>=2026-01-01"}, `use "Ship Date>2025-12-31"`},
		{"inclusive date upper bound names the strict form", []string{"Ship Date<=2026-01-31"}, `use "Ship Date<2026-02-01"`},
		{"inclusive date bound still validates the date", []string{"Ship Date>=tomorrow"}, "YYYY-MM-DD"},
		{"inclusive date bound keeps a UUID reference", []string{testShipDateDefID + ">=2026-01-01"}, `use "` + testShipDateDefID + `>2025-12-31"`},
		{"not-equal on a number", []string{"Score!=3"}, "no not-equal filter"},
		{"not-equal on a select", []string{"Impact!=High"}, "no not-equal filter"},
		{"comparison on an unknown type", []string{"Sentiment>1"}, "does not know how to filter"},
		{"contains on an unknown type", []string{"Sentiment~=happy"}, "does not know how to filter"},
		{"non-numeric after a comparison", []string{"Score>high"}, "not a valid number"},
		{"NaN after a comparison", []string{"Score>=NaN"}, "not a finite number"},
		{"bad date after a comparison", []string{"Ship Date<28/01/2026"}, "YYYY-MM-DD"},
		{"arrow typo falls through to equality", []string{"Score=>3"}, "not a valid number"},
		{"empty value after a comparison", []string{"Score>="}, "value after >= cannot be empty"},
		{"empty needle", []string{"Notes~="}, "value after ~= cannot be empty"},
		{"whitespace-only needle", []string{"Notes~=   "}, "cannot be empty"},
		{"needle over the server cap", []string{"Notes~=" + strings.Repeat("a", 2001)}, "2000 characters or fewer"},
		{"url needle over the url store cap in bytes", []string{"Spec~=" + strings.Repeat("é", 1025)}, "2048 characters or fewer"},
		{"archived property with a comparison", []string{"Old Impact>=Legacy"}, "archived"},
		{"empty name before a comparison", []string{">=3"}, "Name=Value"},
		{"empty name before contains", []string{"~=x"}, "Name=Value"},
		{"bare ! is not a comparison", []string{"Score!3"}, "Name=Value"},
		{"interior > in a name needs a UUID for a comparison", []string{"Risk>Reward>3"}, "not found"},
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
