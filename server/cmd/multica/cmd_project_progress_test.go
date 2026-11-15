package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

const sampleProjectProgress = `{
	"progress": {"unit": "issues", "scope": 11, "completed": 5, "started": 3, "unstarted": 3, "cancelled": 1, "percent": 45},
	"by_assignee": [
		{"assignee_type": "agent", "assignee_id": "xyz", "scope": 6, "completed": 3, "started": 2},
		{"assignee_type": null, "assignee_id": null, "scope": 2, "completed": 0, "started": 0}
	],
	"history": [
		{"date": "2026-10-12", "scope": 6, "started": 0, "completed": 0},
		{"date": "2026-11-09", "scope": 11, "started": 3, "completed": 5}
	],
	"forecast": {"completion_date": "2026-12-13", "weekly_velocity": 1.25}
}`

func decodeSampleProgress(t *testing.T) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal([]byte(sampleProjectProgress), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestFormatProjectProgress(t *testing.T) {
	resp := decodeSampleProgress(t)
	if got := formatProjectProgress(resp["progress"]); got != "5/11 (45%)" {
		t.Fatalf("formatProjectProgress = %q", got)
	}
	if got := formatProjectProgress(nil); got != "" {
		t.Fatalf("formatProjectProgress(nil) = %q, want empty for servers without progress", got)
	}
}

func TestProjectProgressTables(t *testing.T) {
	resp := decodeSampleProgress(t)

	_, summary := projectProgressSummaryTable(resp)
	want := [][]string{{"5/11 (45%)", "11", "5", "3", "3", "1", "2026-12-13 (1.25/week)"}}
	if !reflect.DeepEqual(summary, want) {
		t.Errorf("summary = %v, want %v", summary, want)
	}

	_, history := projectProgressHistoryTable(resp)
	wantHistory := [][]string{{"2026-10-12", "6", "0", "0"}, {"2026-11-09", "11", "3", "5"}}
	if !reflect.DeepEqual(history, wantHistory) {
		t.Errorf("history = %v, want %v", history, wantHistory)
	}

	actors := actorDisplayLookup{state: &actorDisplayLookupState{
		agents:        map[string]string{"xyz": "CodeBot"},
		membersLoaded: true,
		agentsLoaded:  true,
		squadsLoaded:  true,
	}}
	_, byAssignee := projectProgressAssigneeTable(resp, actors)
	wantAssignee := [][]string{{"agent:CodeBot", "3/6 (50%)", "2"}, {"unassigned", "0/2 (0%)", "0"}}
	if !reflect.DeepEqual(byAssignee, wantAssignee) {
		t.Errorf("by assignee = %v, want %v", byAssignee, wantAssignee)
	}
}

func TestProjectOverviewTable(t *testing.T) {
	overview := map[string]any{
		"project": map[string]any{
			"title": "Mobile 2.0", "status": "in_progress", "due_date": "2026-12-18",
			"description": "Rebuild the mobile app.", "lead_type": "agent", "lead_id": "xyz",
		},
		"progress":  decodeSampleProgress(t),
		"resources": []any{map[string]any{"id": "r1"}},
	}
	actors := actorDisplayLookup{state: &actorDisplayLookupState{
		agents:        map[string]string{"xyz": "CodeBot"},
		membersLoaded: true,
		agentsLoaded:  true,
		squadsLoaded:  true,
	}}
	_, rows := projectOverviewTable(overview, actors)
	got := map[string]string{}
	for _, row := range rows {
		got[row[0]] = row[1]
	}
	want := map[string]string{
		"TITLE": "Mobile 2.0", "LEAD": "agent:CodeBot", "DUE": "2026-12-18",
		"PROGRESS": "5/11 (45%)", "FORECAST": "2026-12-13 (1.25/week)", "RESOURCES": "1",
		"DESCRIPTION": "Rebuild the mobile app.",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}
