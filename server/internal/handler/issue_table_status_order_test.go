package handler

import (
	"reflect"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueTableStatusOrder(t *testing.T) {
	entries := []db.IssueStatus{
		{Key: "qa", Category: "started", Position: -1},
		{Key: "in_progress", Category: "started", Position: 20, IsSystem: true},
		{Key: "blocked", Category: "started", Position: 10, IsSystem: true},
		{Key: "in_review", Category: "started", Position: 0, IsSystem: true},
		{Key: "shipped", Category: "done", Position: -100},
	}
	want := []string{"backlog", "todo", "qa", "in_review", "blocked", "in_progress", "shipped", "done", "cancelled"}
	got := issueTableStatusOrder(entries)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if entries[0].Key != "qa" {
		t.Fatal("ordering mutated the catalog")
	}
	var argument any
	expression := (resolvedIssueTableGroup{kind: "status", statusOrder: got}).orderExpression(func(value any) string { argument = value; return "$2" })
	if expression != "COALESCE(array_position($2::text[], group_value), 100000)" || !reflect.DeepEqual(argument, want) {
		t.Fatalf("unparameterized or incorrect order: %s %v", expression, argument)
	}
}

func TestIssueTableStatusSeedOrder(t *testing.T) {
	want := []string{"backlog", "todo", "in_progress", "in_review", "blocked", "done", "cancelled"}
	if got := issueTableStatusOrder(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("unseeded order = %v", got)
	}
}
