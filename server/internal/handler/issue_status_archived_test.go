package handler

import (
	"strings"
	"testing"
)

// `archived` is a first-class status (KTD1): the handler-side validation slice
// must accept it so writes get a clean 200, while unknown values still 400.
func TestValidIssueStatuses_Archived(t *testing.T) {
	found := false
	for _, s := range validIssueStatuses {
		if s == "archived" {
			found = true
		}
	}
	if !found {
		t.Errorf("validIssueStatuses %v does not include %q", validIssueStatuses, "archived")
	}
	// A near-miss typo must not be present (the DB CHECK also rejects it).
	for _, s := range validIssueStatuses {
		if s == "archive" {
			t.Errorf("validIssueStatuses must not contain the typo %q", "archive")
		}
	}
}

// The lifecycle sort CASE expressions (search rank + List/ListGrouped status
// sort) must place archived alongside cancelled at the bottom, above the ELSE
// catch-all (KTD2 sort sites).
func TestBuildSearchQuery_SortCaseArchived(t *testing.T) {
	query, _ := buildSearchQuery("test", []string{"test"}, 0, false, true)
	if !strings.Contains(query, "WHEN 'archived' THEN 7") {
		t.Error("search status rank should map archived to the tier just above ELSE")
	}
	// cancelled must stay at its tier and the ELSE catch-all must move past archived.
	if !strings.Contains(query, "WHEN 'cancelled' THEN 6") {
		t.Error("search status rank should keep cancelled at tier 6")
	}
	if !strings.Contains(query, "ELSE 8") {
		t.Error("search status rank ELSE should be 8 after adding the archived tier")
	}
}
