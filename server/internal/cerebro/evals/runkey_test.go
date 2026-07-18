package evals

import "testing"

func TestFormatIssueKey(t *testing.T) {
	prefix := "MUL"
	number := int32(123)

	if got := formatIssueKey(&prefix, &number); got != "MUL-123" {
		t.Fatalf("want MUL-123, got %q", got)
	}

	// A run with no issue, or a drifted row, yields no key so the UI can fall
	// back to the raw id instead of rendering a broken "-123" / "MUL-".
	if got := formatIssueKey(nil, &number); got != "" {
		t.Fatalf("nil prefix: want empty, got %q", got)
	}
	if got := formatIssueKey(&prefix, nil); got != "" {
		t.Fatalf("nil number: want empty, got %q", got)
	}
	empty := ""
	if got := formatIssueKey(&empty, &number); got != "" {
		t.Fatalf("empty prefix: want empty, got %q", got)
	}
}
