package search

import (
	"fmt"
	"regexp"
	"sort"
	"testing"
)

// assertContiguousPlaceholders fails if the SQL references a $N set that is
// not exactly $1..$len(args). A gap (e.g. $1 then $6) is the FIR-2595
// filter-only bug: Postgres rejects "bind supplies N, statement requires M".
func assertContiguousPlaceholders(t *testing.T, q Query) {
	t.Helper()
	re := regexp.MustCompile(`\$(\d+)`)
	seen := map[int]bool{}
	for _, m := range re.FindAllStringSubmatch(q.SQL, -1) {
		var n int
		fmt.Sscanf(m[1], "%d", &n)
		seen[n] = true
	}
	got := []int{}
	for n := range seen {
		got = append(got, n)
	}
	sort.Ints(got)
	if len(got) != len(q.Args) {
		t.Fatalf("placeholder/arg mismatch: %d distinct placeholders %v but %d args", len(got), got, len(q.Args))
	}
	for i, n := range got {
		if n != i+1 {
			t.Fatalf("non-contiguous placeholders %v (gap at $%d) — Postgres will reject the bind", got, i+1)
		}
	}
}

func TestBuild_FilterOnly_ContiguousPlaceholders(t *testing.T) {
	cases := []struct {
		name string
		in   QueryInput
	}{
		{"status", QueryInput{WorkspaceID: "WS", StatusValues: []string{"todo"}, Limit: 20}},
		{"assignee_unassigned", QueryInput{WorkspaceID: "WS", AssigneeUnassigned: true, Limit: 20}},
		{"from_user", QueryInput{WorkspaceID: "WS", FromUserIDs: []any{"U1"}, Limit: 20}},
		{"has_attachment", QueryInput{WorkspaceID: "WS", HasAttachment: true, Limit: 20}},
		{"status_plus_from", QueryInput{WorkspaceID: "WS", StatusValues: []string{"todo"}, FromUserIDs: []any{"U1"}, Limit: 20}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := Build(c.in)
			if q.HasText {
				t.Fatalf("expected HasText=false for filter-only %s", c.name)
			}
			assertContiguousPlaceholders(t, q)
		})
	}
}

func TestBuild_WithText_StillContiguous(t *testing.T) {
	q := Build(QueryInput{WorkspaceID: "WS", Text: "deploy", StatusValues: []string{"todo"}, Limit: 20})
	if !q.HasText {
		t.Fatal("expected HasText=true")
	}
	assertContiguousPlaceholders(t, q)
}
