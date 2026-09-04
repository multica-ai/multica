package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func newIssueTimelineTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "timeline"}
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Bool("activity-only", false, "")
	cmd.Flags().StringSlice("action", nil, "")
	cmd.Flags().String("since", "", "")
	cmd.Flags().Int("tail", 0, "")
	cmd.Flags().Bool("full-id", false, "")
	return cmd
}

// timelineFixture mirrors the #7040 scenario: an agent comment that says the PR
// is still waiting for review, followed by the status transitions that made it
// stale.
func timelineFixture() []map[string]any {
	return []map[string]any{
		{
			"type": "activity", "id": "a1", "actor_type": "member", "actor_id": "m1",
			"created_at": "2026-08-18T10:00:00Z", "action": "created",
			"details": map[string]any{},
		},
		{
			"type": "comment", "id": "c1", "actor_type": "agent", "actor_id": "g1",
			"created_at": "2026-08-18T11:00:00Z",
			"content":    "PR open, waiting for human review/merge.",
		},
		{
			"type": "activity", "id": "a2", "actor_type": "member", "actor_id": "m1",
			"created_at": "2026-08-19T09:00:00Z", "action": "status_changed",
			"details": map[string]any{"from": "in_progress", "to": "in_review"},
		},
		{
			"type": "activity", "id": "a3", "actor_type": "system", "actor_id": "",
			"created_at": "2026-08-20T08:00:00Z", "action": "status_changed",
			"details": map[string]any{"from": "in_review", "to": "done"},
		},
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

func entryIDs(entries []map[string]any) []string {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, strVal(e, "id"))
	}
	return ids
}

func TestTimelineFilterApply(t *testing.T) {
	tests := []struct {
		name   string
		filter timelineFilter
		want   []string
	}{
		{
			name:   "no filter keeps everything",
			filter: timelineFilter{},
			want:   []string{"a1", "c1", "a2", "a3"},
		},
		{
			name:   "activity-only drops comments",
			filter: timelineFilter{activityOnly: true},
			want:   []string{"a1", "a2", "a3"},
		},
		{
			name:   "action filter narrows to matching activities",
			filter: timelineFilter{activityOnly: true, actions: map[string]bool{"status_changed": true}},
			want:   []string{"a2", "a3"},
		},
		{
			name:   "since keeps only strictly later entries",
			filter: timelineFilter{since: mustTime(t, "2026-08-18T11:00:00Z")},
			want:   []string{"a2", "a3"},
		},
		{
			name:   "tail keeps the most recent entries",
			filter: timelineFilter{tail: 2},
			want:   []string{"a2", "a3"},
		},
		{
			name:   "tail larger than the result is a no-op",
			filter: timelineFilter{tail: 99},
			want:   []string{"a1", "c1", "a2", "a3"},
		},
		{
			name:   "tail applies after the other filters",
			filter: timelineFilter{activityOnly: true, tail: 1},
			want:   []string{"a3"},
		},
		{
			name:   "unknown action matches nothing",
			filter: timelineFilter{activityOnly: true, actions: map[string]bool{"nope": true}},
			want:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := entryIDs(tt.filter.apply(timelineFixture()))
			if len(got) != len(tt.want) {
				t.Fatalf("ids = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ids = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestTimelineFilterApplyDropsUnparsableTimestampsWhenSinceSet(t *testing.T) {
	entries := []map[string]any{
		{"type": "activity", "id": "bad", "created_at": "not-a-timestamp"},
		{"type": "activity", "id": "good", "created_at": "2026-08-20T08:00:00Z"},
	}
	f := timelineFilter{since: mustTime(t, "2026-08-01T00:00:00Z")}
	if got := entryIDs(f.apply(entries)); len(got) != 1 || got[0] != "good" {
		t.Fatalf("ids = %v, want [good]", got)
	}
}

func TestTimelineFilterFromFlagsActionImpliesActivityOnly(t *testing.T) {
	cmd := newIssueTimelineTestCmd()
	_ = cmd.Flags().Set("action", "status_changed")

	f, err := timelineFilterFromFlags(cmd)
	if err != nil {
		t.Fatalf("timelineFilterFromFlags: %v", err)
	}
	if !f.activityOnly {
		t.Fatal("activityOnly = false, want true when --action is set")
	}
	if !f.actions["status_changed"] {
		t.Fatalf("actions = %v, want status_changed", f.actions)
	}
}

func TestTimelineFilterFromFlagsRejectsBadInput(t *testing.T) {
	t.Run("since", func(t *testing.T) {
		cmd := newIssueTimelineTestCmd()
		_ = cmd.Flags().Set("since", "yesterday")
		if _, err := timelineFilterFromFlags(cmd); err == nil {
			t.Fatal("expected an error for a non-RFC3339 --since")
		}
	})
	t.Run("tail", func(t *testing.T) {
		cmd := newIssueTimelineTestCmd()
		_ = cmd.Flags().Set("tail", "-1")
		if _, err := timelineFilterFromFlags(cmd); err == nil {
			t.Fatal("expected an error for a negative --tail")
		}
	})
}

func TestTimelineDetail(t *testing.T) {
	var actors actorDisplayLookup // zero value: no workspace lookup, id fallback

	tests := []struct {
		name  string
		entry map[string]any
		want  string
	}{
		{
			name:  "status transition",
			entry: map[string]any{"type": "activity", "details": map[string]any{"from": "in_review", "to": "done"}},
			want:  "in_review → done",
		},
		{
			name:  "assignee set from nobody",
			entry: map[string]any{"type": "activity", "details": map[string]any{"to_type": "agent", "to_id": "abcdefgh1234"}},
			want:  "(none) → agent:abcdefgh",
		},
		{
			name:  "unassigned",
			entry: map[string]any{"type": "activity", "details": map[string]any{"from_type": "member", "from_id": "abcdefgh1234"}},
			want:  "member:abcdefgh → (none)",
		},
		{
			name:  "task_completed carries no from/to and falls back",
			entry: map[string]any{"type": "activity", "details": map[string]any{"task_id": "t1"}},
			want:  "task_id=t1",
		},
		{
			name:  "empty details render as blank",
			entry: map[string]any{"type": "activity", "details": map[string]any{}},
			want:  "",
		},
		{
			name:  "unknown detail shape falls back to sorted key=value",
			entry: map[string]any{"type": "activity", "details": map[string]any{"outcome": "no_action", "task_id": "t1"}},
			want:  "outcome=no_action task_id=t1",
		},
		{
			name:  "comment body is flattened to one line",
			entry: map[string]any{"type": "comment", "content": "PR open,\n\nwaiting for review."},
			want:  "PR open, waiting for review.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := timelineDetail(tt.entry, actors, false); got != tt.want {
				t.Fatalf("timelineDetail = %q, want %q", got, tt.want)
			}
		})
	}
}

// A GitHub PR merge flipping the issue to done publishes with actor type
// "system" and no actor id (server/internal/handler/github.go). That is the
// transition this command exists to explain, so the actor must not render
// blank.
func TestTimelineActorSystemWithoutIDRendersType(t *testing.T) {
	var actors actorDisplayLookup

	if got := timelineActor("system", "", "", actors, false); got != "system" {
		t.Fatalf("system actor = %q, want %q", got, "system")
	}
	if got := timelineActor("", "", "", actors, false); got != "" {
		t.Fatalf("empty actor = %q, want empty", got)
	}
	if got := timelineActor("member", "abcdefgh1234", "", actors, false); got != "member:abcdefgh" {
		t.Fatalf("member actor = %q, want member:abcdefgh", got)
	}
	if got := timelineActor("member", "abcdefgh1234", "", actors, true); got != "member:abcdefgh1234" {
		t.Fatalf("member actor with --full-id = %q, want the full id", got)
	}
	if got := timelineActor("member", "abcdefgh1234", "Former Member", actors, false); got != "member:Former Member" {
		t.Fatalf("hydrated former member = %q, want member:Former Member", got)
	}
}

func TestWarnTimelineTruncated(t *testing.T) {
	t.Run("silent when the header is absent", func(t *testing.T) {
		var buf strings.Builder
		warnTimelineTruncated(&buf, "")
		if buf.String() != "" {
			t.Fatalf("warning = %q, want none", buf.String())
		}
	})

	t.Run("names the truncated kinds and voids duration claims", func(t *testing.T) {
		var buf strings.Builder
		warnTimelineTruncated(&buf, "activity,comment")
		got := buf.String()
		for _, want := range []string{"truncated", "activity,comment", "Durations"} {
			if !strings.Contains(got, want) {
				t.Fatalf("warning %q missing %q", got, want)
			}
		}
	})
}

// The truncation signal is the difference between "this is the whole history"
// and "the transition you need may have fallen off the back", so it must
// survive the round trip — and land on stderr, where it cannot corrupt a piped
// --output json read.
func TestRunIssueTimelineReportsTruncationOnStderr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/issues/MUL-6253":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "issue-uuid", "identifier": "MUL-6253"})
		case "/api/issues/issue-uuid/timeline":
			w.Header().Set("X-Timeline-Truncated", "activity")
			_ = json.NewEncoder(w).Encode(timelineFixture())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueTimelineTestCmd()
	_ = cmd.Flags().Set("output", "json")

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	err := runIssueTimeline(cmd, []string{"MUL-6253"})
	_ = outW.Close()
	_ = errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	stdout, _ := io.ReadAll(outR)
	stderr, _ := io.ReadAll(errR)
	if err != nil {
		t.Fatalf("runIssueTimeline: %v", err)
	}

	if !strings.Contains(string(stderr), "truncated") || !strings.Contains(string(stderr), "activity") {
		t.Fatalf("stderr = %q, want a truncation warning naming the kind", string(stderr))
	}
	// stdout must stay valid JSON: the warning belongs on stderr only.
	var entries []map[string]any
	if err := json.Unmarshal(stdout, &entries); err != nil {
		t.Fatalf("stdout is not clean JSON: %v\n%s", err, string(stdout))
	}
	if len(entries) != len(timelineFixture()) {
		t.Fatalf("entries = %d, want %d", len(entries), len(timelineFixture()))
	}
}

func TestRunIssueTimelineSilentWhenNotTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/issues/MUL-6253":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "issue-uuid", "identifier": "MUL-6253"})
		case "/api/issues/issue-uuid/timeline":
			_ = json.NewEncoder(w).Encode(timelineFixture())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueTimelineTestCmd()
	_ = cmd.Flags().Set("output", "json")

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	err := runIssueTimeline(cmd, []string{"MUL-6253"})
	_ = outW.Close()
	_ = errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	_, _ = io.ReadAll(outR)
	stderr, _ := io.ReadAll(errR)
	if err != nil {
		t.Fatalf("runIssueTimeline: %v", err)
	}
	if len(stderr) != 0 {
		t.Fatalf("stderr = %q, want nothing when the response is complete", string(stderr))
	}
}

func TestClipTimelineText(t *testing.T) {
	if got := clipTimelineText("短", 10); got != "短" {
		t.Fatalf("clip short = %q", got)
	}
	// Clipping counts runes, not bytes, so multi-byte text is not cut mid-character.
	if got := clipTimelineText("一二三四五六七八九十", 5); got != "一二..." {
		t.Fatalf("clip multibyte = %q, want 一二...", got)
	}
}

// The endpoint only returns the flat oldest-first array when NO pagination
// parameter is present; sending limit= flips it to a wrapped object and the
// decode into []map[string]any would silently yield nothing.
func TestRunIssueTimelineRequestsFlatShapeAndFilters(t *testing.T) {
	var gotPaths []string
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch r.URL.Path {
		case "/api/issues/MUL-6253":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-uuid",
				"identifier": "MUL-6253",
				"title":      "timeline CLI",
			})
		case "/api/issues/issue-uuid/timeline":
			gotRawQuery = r.URL.RawQuery
			_ = json.NewEncoder(w).Encode(timelineFixture())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueTimelineTestCmd()
	_ = cmd.Flags().Set("output", "json")
	_ = cmd.Flags().Set("action", "status_changed")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runIssueTimeline(cmd, []string{"MUL-6253"})
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("runIssueTimeline: %v", err)
	}

	if gotRawQuery != "" {
		t.Fatalf("timeline query = %q, want empty (any pagination param changes the response shape)", gotRawQuery)
	}
	want := []string{"/api/issues/MUL-6253", "/api/issues/issue-uuid/timeline"}
	if len(gotPaths) != len(want) || gotPaths[0] != want[0] || gotPaths[1] != want[1] {
		t.Fatalf("paths = %v, want %v", gotPaths, want)
	}

	var entries []map[string]any
	if err := json.Unmarshal(out, &entries); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, string(out))
	}
	if got := entryIDs(entries); len(got) != 2 || got[0] != "a2" || got[1] != "a3" {
		t.Fatalf("ids = %v, want [a2 a3]", got)
	}
}

func TestRunIssueTimelineEmptyResultPrintsEmptyJSONArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/issues/MUL-6253":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "issue-uuid", "identifier": "MUL-6253"})
		case "/api/issues/issue-uuid/timeline":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueTimelineTestCmd()
	_ = cmd.Flags().Set("output", "json")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runIssueTimeline(cmd, []string{"MUL-6253"})
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("runIssueTimeline: %v", err)
	}

	var entries []map[string]any
	if err := json.Unmarshal(out, &entries); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, string(out))
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %v, want empty", entries)
	}
}
