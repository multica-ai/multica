package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

// A joined display string cannot be split back into option names once an
// option name contains a comma, so multi-value rows also carry the names as
// an array that stays index-parallel with the stored ids.
func TestBuildIssuePropertyRowsMultiValueDisplays(t *testing.T) {
	platforms := propertyDTO{ID: testPlatformsDefID, Name: "Platforms", Type: "multi_select"}
	platforms.Config.Options = []propertyOptionDTO{
		{ID: testPlatformsIOSID, Name: "iOS"},
		{ID: "ffffffff-3333-4333-8333-333333333333", Name: "Android, tablets"},
	}
	impact := propertyDTO{ID: testImpactDefID, Name: "Impact", Type: "select"}
	impact.Config.Options = []propertyOptionDTO{{ID: testImpactHighID, Name: "High"}}
	owners := propertyDTO{ID: testReviewerDefID, Name: "Owners", Type: "multi_actor"}
	catalog := []propertyDTO{impact, platforms, owners}

	bag := map[string]any{
		testImpactDefID:    testImpactHighID,
		testPlatformsDefID: []any{testPlatformsIOSID, "ffffffff-3333-4333-8333-333333333333", float64(7)},
		testReviewerDefID:  []any{"member:abababab-2222-4222-8222-222222222222", "member:abababab-3333-4333-8333-333333333333"},
	}
	actorNames := map[string]string{"member:abababab-2222-4222-8222-222222222222": "Ada"}

	rows := buildIssuePropertyRows(catalog, bag, actorNames)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3: %+v", len(rows), rows)
	}

	if rows[0].DisplayValues != nil {
		t.Errorf("select row carries display_values %v; only multi-value types should", rows[0].DisplayValues)
	}
	if rows[0].Display != "High" {
		t.Errorf("select display = %q, want High", rows[0].Display)
	}

	// The stray number keeps its slot so display_values lines up with value.
	wantPlatforms := []string{"iOS", "Android, tablets", "7"}
	if !reflect.DeepEqual(rows[1].DisplayValues, wantPlatforms) {
		t.Errorf("multi_select display_values = %v, want %v", rows[1].DisplayValues, wantPlatforms)
	}
	if rows[1].Display != "iOS, Android, tablets, 7" {
		t.Errorf("multi_select display = %q", rows[1].Display)
	}

	// An unknown member keeps its raw reference rather than an empty name.
	wantOwners := []string{"Ada", "member:abababab-3333-4333-8333-333333333333"}
	if !reflect.DeepEqual(rows[2].DisplayValues, wantOwners) {
		t.Errorf("multi_actor display_values = %v, want %v", rows[2].DisplayValues, wantOwners)
	}
}

func TestFormatIssuePropertyValueMultiValueNotArray(t *testing.T) {
	platforms := propertyDTO{ID: testPlatformsDefID, Name: "Platforms", Type: "multi_select"}
	// A value that is not an array falls through to the raw rendering and
	// yields no display_values, the same as before the array existed.
	if got := formatIssuePropertyValue(platforms, "not-a-list", nil); got != "not-a-list" {
		t.Errorf("display = %q, want the raw value", got)
	}
	if got := issuePropertyDisplayValues(platforms, "not-a-list", nil); got != nil {
		t.Errorf("display_values = %v, want nil", got)
	}
}

// An actor --value becomes an id through the member list, and the rows
// printed afterwards turn that id back into a name; one command, one request.
func TestRunIssuePropertySetSharesMembersRequest(t *testing.T) {
	srv := newResolveTestServer(t, testIssue("issue-1", "MUL-1", nil))
	cmd := &cobra.Command{Use: "set"}
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("value", "", "")
	cmd.Flags().String("output", "json", "")
	_ = cmd.Flags().Set("name", "Reviewer")
	_ = cmd.Flags().Set("value", "Ada")
	out, err := captureStdout(t, func() error { return runIssuePropertySet(cmd, []string{"MUL-1"}) })
	if err != nil {
		t.Fatalf("runIssuePropertySet: %v", err)
	}
	if srv.membersCalls != 1 {
		t.Errorf("members calls = %d, want one shared by the value and the printed rows", srv.membersCalls)
	}
	var rows []issuePropertyValueRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}
	want := []issuePropertyValueRow{{PropertyID: testReviewerDefID, Name: "Reviewer", Type: "actor", Value: "member:" + testMemberAdaID, Display: "Ada"}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %#v, want %#v", rows, want)
	}
}
