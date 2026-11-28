package issuesync

import (
	"strings"
	"testing"
	"time"
)

func at(day int) time.Time {
	return time.Date(2026, time.March, day, 12, 0, 0, 0, time.UTC)
}

func TestParseMarker(t *testing.T) {
	body := "Fix the widget\n\n<!-- gitlab: {\"project\": 42, \"iid\": 7} -->"
	m, ok := ParseMarker(body, SideGitLab)
	if !ok {
		t.Fatal("marker not found")
	}
	if m.Project != 42 || m.IID != 7 {
		t.Errorf("marker = %+v, want project 42 iid 7", m)
	}
	if _, ok := ParseMarker(body, SideMultica); ok {
		t.Error("a gitlab marker must not read as a multica marker")
	}
}

func TestParseMarkerIgnoresNonMarkers(t *testing.T) {
	cases := map[string]string{
		"no comment":            "plain description",
		"other tag":             "<!-- note: {\"project\": 1} -->",
		"no payload":            "<!-- gitlab: -->",
		"payload not an object": "<!-- gitlab: 7 -->",
		"broken json":           "<!-- gitlab: {\"iid\":} -->",
		"empty object":          "<!-- gitlab: {} -->",
		"unterminated":          "<!-- gitlab: {\"iid\": 7}",
	}
	for name, body := range cases {
		if _, ok := ParseMarker(body, SideGitLab); ok {
			t.Errorf("%s: parsed as a marker", name)
		}
	}
}

func TestParseMarkerSkipsBrokenCommentBeforeValidOne(t *testing.T) {
	body := "<!-- gitlab: not json -->\n\n<!-- gitlab: {\"iid\": 9} -->"
	m, ok := ParseMarker(body, SideGitLab)
	if !ok || m.IID != 9 {
		t.Fatalf("marker = %+v ok = %v, want iid 9", m, ok)
	}
}

func TestSetMarkerIsIdempotent(t *testing.T) {
	once := SetMarker("Fix the widget", SideGitLab, Marker{Project: 42, IID: 7})
	twice := SetMarker(once, SideGitLab, Marker{Project: 42, IID: 7})
	if once != twice {
		t.Errorf("second SetMarker changed the body:\n%q\n%q", once, twice)
	}
	if strings.Count(twice, "<!-- gitlab:") != 1 {
		t.Errorf("marker duplicated: %q", twice)
	}
}

func TestSetMarkerReplacesRatherThanAppends(t *testing.T) {
	body := SetMarker("Fix the widget", SideGitLab, Marker{Project: 42, IID: 7})
	body = SetMarker(body, SideGitLab, Marker{Project: 42, IID: 8})
	if strings.Count(body, "<!-- gitlab:") != 1 {
		t.Fatalf("marker stacked: %q", body)
	}
	m, ok := ParseMarker(body, SideGitLab)
	if !ok || m.IID != 8 {
		t.Errorf("marker = %+v ok = %v, want iid 8", m, ok)
	}
}

func TestSetMarkerKeepsTheOtherSide(t *testing.T) {
	body := SetMarker("Fix the widget", SideMultica, Marker{Identifier: "PROJECT-1", ID: "abc"})
	body = SetMarker(body, SideGitLab, Marker{Project: 42, IID: 7})
	if _, ok := ParseMarker(body, SideMultica); !ok {
		t.Error("multica marker lost when setting the gitlab one")
	}
	if _, ok := ParseMarker(body, SideGitLab); !ok {
		t.Error("gitlab marker missing")
	}
}

func TestSetMarkerOnEmptyBody(t *testing.T) {
	body := SetMarker("   \n", SideGitLab, Marker{Project: 42, IID: 7})
	if strings.HasPrefix(body, "\n") {
		t.Errorf("body = %q, want the marker alone", body)
	}
	if _, ok := ParseMarker(body, SideGitLab); !ok {
		t.Errorf("body = %q, want a marker", body)
	}
}

func TestStripMarkers(t *testing.T) {
	body := "Fix the widget\n\n<!-- gitlab: {\"project\": 42, \"iid\": 7} -->\n\n<!-- multica: {\"identifier\": \"PROJECT-1\"} -->"
	got := StripMarkers(body)
	if strings.Contains(got, "<!--") {
		t.Errorf("StripMarkers left a comment: %q", got)
	}
	if strings.TrimSpace(got) != "Fix the widget" {
		t.Errorf("StripMarkers = %q, want the description text", got)
	}
}

func TestDiffIgnoresMarkersAndLineEndings(t *testing.T) {
	a := Issue{Side: SideMultica, Title: "Fix", Body: "Fix the widget", Status: "todo"}
	b := Issue{
		Side:   SideGitLab,
		Title:  "Fix",
		Body:   "Fix the widget\r\n\r\n<!-- multica: {\"identifier\": \"PROJECT-1\"} -->\n",
		Status: "todo",
	}
	if fields := Diff(a, b); len(fields) != 0 {
		t.Errorf("Diff = %v, want no differences", fields)
	}
}

func TestDiffReportsEachField(t *testing.T) {
	base := Issue{Title: "Fix", Body: "body", Status: "todo", Priority: "high", DueDate: "2026-03-01"}
	cases := map[string]struct {
		mutate func(Issue) Issue
		want   string
	}{
		"title":       {func(i Issue) Issue { i.Title = "Other"; return i }, FieldTitle},
		"description": {func(i Issue) Issue { i.Body = "other"; return i }, FieldDescription},
		"status":      {func(i Issue) Issue { i.Status = "done"; return i }, FieldStatus},
		"priority":    {func(i Issue) Issue { i.Priority = "low"; return i }, FieldPriority},
		"due date":    {func(i Issue) Issue { i.DueDate = "2026-04-01"; return i }, FieldDueDate},
	}
	for name, tc := range cases {
		fields := Diff(base, tc.mutate(base))
		if len(fields) != 1 || fields[0] != tc.want {
			t.Errorf("%s: Diff = %v, want [%s]", name, fields, tc.want)
		}
	}
}

func TestDiffSkipsPriorityWhenOneSideHasNone(t *testing.T) {
	a := Issue{Title: "Fix", Body: "body", Status: "todo", Priority: "high"}
	b := Issue{Title: "Fix", Body: "body", Status: "todo"}
	if fields := Diff(a, b); len(fields) != 0 {
		t.Errorf("Diff = %v, want no differences: a tracker without priorities must not report one", fields)
	}
}

func TestComparedFieldsExcludesStartDate(t *testing.T) {
	for _, field := range ComparedFields() {
		if field == "start_date" {
			t.Fatal("start_date must stay out of the comparison: GitLab CE drops it, so every issue with one would be rewritten forever")
		}
	}
}

func TestDecideNoChange(t *testing.T) {
	a := Issue{Side: SideMultica, Title: "Fix", Body: "body", Status: "todo", UpdatedAt: at(1)}
	b := Issue{Side: SideGitLab, Title: "Fix", Body: "body", Status: "todo", UpdatedAt: at(5)}
	if d := Decide(a, b); d.Kind != NoChange || d.Changed() {
		t.Errorf("Decide = %+v, want NoChange", d)
	}
}

func TestDecideNewerSideWins(t *testing.T) {
	a := Issue{Side: SideMultica, Title: "Fix", Body: "body", Status: "todo", UpdatedAt: at(5)}
	b := Issue{Side: SideGitLab, Title: "Fix", Body: "body", Status: "done", UpdatedAt: at(1)}
	d := Decide(a, b)
	if d.Kind != UpdateFrom || d.Source != SideMultica {
		t.Fatalf("Decide = %+v, want UpdateFrom multica", d)
	}
	if len(d.Fields) != 1 || d.Fields[0] != FieldStatus {
		t.Errorf("Fields = %v, want [status]", d.Fields)
	}
}

func TestDecideEqualTimestampsConflict(t *testing.T) {
	a := Issue{Side: SideMultica, Title: "Fix", Body: "body", Status: "todo", UpdatedAt: at(3)}
	b := Issue{Side: SideGitLab, Title: "Fix", Body: "body", Status: "done", UpdatedAt: at(3)}
	if d := Decide(a, b); d.Kind != Conflict {
		t.Errorf("Decide = %+v, want Conflict", d)
	}
}

func TestDecideContentBeatsTimestamps(t *testing.T) {
	// The pair agrees, but the tracker was touched later. Content decides, so
	// the run writes nothing — otherwise the write would bump updated_at and
	// hand the next run to the other side, forever.
	a := Issue{Side: SideMultica, Title: "Fix", Body: "body", Status: "todo", UpdatedAt: at(1)}
	b := Issue{Side: SideGitLab, Title: "Fix", Body: "body", Status: "todo", UpdatedAt: at(9)}
	if d := Decide(a, b); d.Kind != NoChange {
		t.Errorf("Decide = %+v, want NoChange", d)
	}
}

func TestLinkByTrackerMarker(t *testing.T) {
	multica := []Issue{{Side: SideMultica, Key: "PROJECT-1", Body: "Fix the widget"}}
	tracker := []Issue{{
		Side: SideGitLab,
		Key:  "7",
		Body: SetMarker("Fix the widget", SideMultica, Marker{Identifier: "PROJECT-1", ID: "uuid-1"}),
	}}
	result := Link(multica, tracker)
	if len(result.Pairs) != 1 {
		t.Fatalf("pairs = %d, want 1", len(result.Pairs))
	}
	if result.Pairs[0].Multica.Key != "PROJECT-1" || result.Pairs[0].Tracker.Key != "7" {
		t.Errorf("pair = %+v", result.Pairs[0])
	}
	if len(result.MulticaOnly) != 0 || len(result.TrackerOnly) != 0 {
		t.Errorf("unpaired = %d/%d, want 0/0", len(result.MulticaOnly), len(result.TrackerOnly))
	}
}

func TestLinkByMulticaMarker(t *testing.T) {
	// The tracker lost its marker; the Multica side still names it.
	multica := []Issue{{
		Side: SideMultica,
		Key:  "PROJECT-1",
		Body: SetMarker("Fix the widget", SideGitLab, Marker{Project: 42, IID: 7}),
	}}
	tracker := []Issue{{Side: SideGitLab, Key: "7", Body: "Fix the widget"}}
	result := Link(multica, tracker)
	if len(result.Pairs) != 1 {
		t.Fatalf("pairs = %d, want 1", len(result.Pairs))
	}
}

func TestLinkFallsBackToMulticaID(t *testing.T) {
	// The identifier was renamed on the Multica side; the stored UUID still
	// resolves the pair.
	multica := []Issue{{
		Side: SideMultica,
		Key:  "PROJECT-2",
		Body: SetMarker("Fix the widget", SideGitLab, Marker{Project: 42, IID: 7}),
	}}
	tracker := []Issue{{
		Side: SideGitLab,
		Key:  "7",
		Body: SetMarker("Fix the widget", SideMultica, Marker{Identifier: "PROJECT-1", ID: "uuid-1"}),
	}}
	result := Link(multica, tracker)
	if len(result.Pairs) != 1 {
		t.Fatalf("pairs = %d, want 1", len(result.Pairs))
	}
}

func TestLinkReportsUnpaired(t *testing.T) {
	multica := []Issue{
		{Side: SideMultica, Key: "PROJECT-1", Body: "linked"},
		{Side: SideMultica, Key: "PROJECT-2", Body: "new on the multica side"},
	}
	tracker := []Issue{
		{Side: SideGitLab, Key: "7", Body: SetMarker("linked", SideMultica, Marker{Identifier: "PROJECT-1"})},
		{Side: SideGitLab, Key: "8", Body: "new on the tracker side"},
	}
	result := Link(multica, tracker)
	if len(result.Pairs) != 1 {
		t.Fatalf("pairs = %d, want 1", len(result.Pairs))
	}
	if len(result.MulticaOnly) != 1 || result.MulticaOnly[0].Key != "PROJECT-2" {
		t.Errorf("MulticaOnly = %+v", result.MulticaOnly)
	}
	if len(result.TrackerOnly) != 1 || result.TrackerOnly[0].Key != "8" {
		t.Errorf("TrackerOnly = %+v", result.TrackerOnly)
	}
}

func TestLinkDoesNotPairOneIssueTwice(t *testing.T) {
	multica := []Issue{{Side: SideMultica, Key: "PROJECT-1", Body: "Fix"}}
	tracker := []Issue{
		{Side: SideGitLab, Key: "7", Body: SetMarker("Fix", SideMultica, Marker{Identifier: "PROJECT-1"})},
		{Side: SideGitLab, Key: "8", Body: SetMarker("Fix again", SideMultica, Marker{Identifier: "PROJECT-1"})},
	}
	result := Link(multica, tracker)
	if len(result.Pairs) != 1 {
		t.Fatalf("pairs = %d, want 1: one Multica issue cannot pair with two tracker issues", len(result.Pairs))
	}
	if len(result.TrackerOnly) != 1 {
		t.Errorf("TrackerOnly = %d, want the second tracker issue left unpaired", len(result.TrackerOnly))
	}
}

func TestStatusLabelRoundTrip(t *testing.T) {
	for _, status := range []string{"backlog", "todo", "in_progress", "in_review", "blocked", "done", "cancelled"} {
		label, ok := StatusLabel(status)
		if !ok {
			t.Fatalf("StatusLabel(%q) not ok", status)
		}
		got, ok := StatusFromLabels([]string{"area::code", label})
		if !ok || got != status {
			t.Errorf("round trip of %q = %q ok = %v", status, got, ok)
		}
	}
}

func TestStatusLabelRejectsUnknown(t *testing.T) {
	if _, ok := StatusLabel("human_review"); ok {
		t.Error("a custom status key must not become a tracker label")
	}
}

func TestStatusFromLabelsIgnoresUnknown(t *testing.T) {
	if _, ok := StatusFromLabels([]string{"status::human-review", "area::code"}); ok {
		t.Error("an unknown status label must be ignored, not guessed at")
	}
	if _, ok := StatusFromLabels(nil); ok {
		t.Error("no labels must not resolve a status")
	}
}

func TestIsClosing(t *testing.T) {
	for _, status := range []string{"done", "cancelled"} {
		if !IsClosing(status) {
			t.Errorf("IsClosing(%q) = false, want true", status)
		}
	}
	for _, status := range []string{"backlog", "todo", "in_progress", "in_review", "blocked"} {
		if IsClosing(status) {
			t.Errorf("IsClosing(%q) = true, want false", status)
		}
	}
}
