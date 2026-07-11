package handler

import (
	"context"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestUsageExplorerRunsQueryMatchesCurrentSchemaAndAcceptsGoIntDays(t *testing.T) {
	rows, err := testPool.Query(context.Background(), usageExplorerRunsQuery, parseUUID(testWorkspaceID), 7)
	if err != nil {
		t.Fatalf("query usage explorer with Go int days: %v", err)
	}
	rows.Close()
}

func TestUsageExplorerMatchesORWithinDimensionANDAcrossDimensions(t *testing.T) {
	filter := usageExplorerFilter{Include: map[string][]string{"model": {"gpt-5", "claude"}, "status": {"completed"}}, Exclude: map[string][]string{"provider": {"legacy"}}}
	if !usageExplorerMatches(filter, map[string][]string{"model": {"gpt-5"}, "status": {"completed"}, "provider": {"openai"}}) {
		t.Fatal("expected matching row")
	}
	if usageExplorerMatches(filter, map[string][]string{"model": {"gpt-5"}, "status": {"failed"}, "provider": {"openai"}}) {
		t.Fatal("expected AND across dimensions")
	}
	if usageExplorerMatches(filter, map[string][]string{"model": {"claude"}, "status": {"completed"}, "provider": {"legacy"}}) {
		t.Fatal("expected exclusion to win")
	}
}

func TestUsageExplorerFacetValuesPreserveUnknownAndDeleted(t *testing.T) {
	got := usageExplorerFacetValues([]string{"", "Deleted", "openai", "openai"})
	want := []string{"Deleted", "Unknown", "openai"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseUsageExplorerFilterSupportsIncludeExcludeAndSkills(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/dashboard/usage/explorer?days=30&model=gpt-5&model=claude&exclude.status=failed&skill=test-driven-development", nil)
	got, err := parseUsageExplorerFilter(req)
	if err != nil {
		t.Fatalf("parse filter: %v", err)
	}
	if got.Days != 30 || len(got.Include["model"]) != 2 || got.Exclude["status"][0] != "failed" || got.Include["skill"][0] != "test-driven-development" {
		t.Fatalf("unexpected filter: %#v", got)
	}
}

func TestParseUsageExplorerFilterRejectsInvalidBounds(t *testing.T) {
	for _, query := range []string{"days=0", "days=366", "limit=0", "limit=501"} {
		req := httptest.NewRequest("GET", "/api/dashboard/usage/explorer?"+query, nil)
		if _, err := parseUsageExplorerFilter(req); err == nil {
			t.Fatalf("expected %q to fail", query)
		}
	}
}

func TestParseSkillUsageReportRequiresNameAndPositiveCount(t *testing.T) {
	for _, body := range []string{`{"skills":[]}`, `{"skills":[{"name":"","count":1}]}`, `{"skills":[{"name":"TDD","count":0}]}`} {
		req := httptest.NewRequest("POST", "/api/daemon/tasks/task/skill-usage", strings.NewReader(body))
		if _, err := parseSkillUsageReport(req); err == nil {
			t.Fatalf("expected body to fail: %s", body)
		}
	}
}
