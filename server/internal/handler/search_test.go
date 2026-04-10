package handler

import (
	"strings"
	"testing"
)

func TestBuildSearchQuery_SingleTerm(t *testing.T) {
	query, args := buildSearchQuery("Hello", []string{"Hello"}, 0, false, false)

	// Pattern should be lowercased in Go.
	if args[0] != "hello" {
		t.Errorf("expected phrase arg to be lowercased, got %q", args[0])
	}

	// Must use LOWER(column) LIKE, not ILIKE.
	if strings.Contains(query, "ILIKE") {
		t.Error("query should not contain ILIKE")
	}
	if !strings.Contains(query, "LOWER(i.title) LIKE") {
		t.Error("query should contain LOWER(i.title) LIKE")
	}
	if !strings.Contains(query, "LOWER(COALESCE(i.description, '')) LIKE") {
		t.Error("query should contain LOWER(COALESCE(i.description, '')) LIKE")
	}
	if !strings.Contains(query, "LOWER(c.content) LIKE") {
		t.Error("query should contain LOWER(c.content) LIKE")
	}

	// Exact title rank should not double-LOWER the pattern.
	if strings.Contains(query, "LOWER(i.title) = LOWER(") {
		t.Error("exact title rank should not wrap pattern in LOWER (already lowercased in Go)")
	}
	if !strings.Contains(query, "LOWER(i.title) = $1") {
		t.Error("exact title rank should compare LOWER(i.title) = $1 directly")
	}

	// Should exclude closed issues by default.
	if !strings.Contains(query, "NOT IN ('done', 'cancelled')") {
		t.Error("query should exclude done/cancelled when includeClosed=false")
	}
}

func TestBuildSearchQuery_MultiTerm(t *testing.T) {
	query, args := buildSearchQuery("Foo Bar", []string{"Foo", "Bar"}, 0, false, false)

	// Both phrase and terms should be lowercased.
	if args[0] != "foo bar" {
		t.Errorf("expected phrase arg lowercased, got %q", args[0])
	}
	// args[1] is workspace_id placeholder; term args start at args[2].
	if args[2] != "foo" {
		t.Errorf("expected first term arg lowercased, got %q", args[2])
	}
	if args[3] != "bar" {
		t.Errorf("expected second term arg lowercased, got %q", args[3])
	}

	// Multi-word query should have AND conditions.
	if !strings.Contains(query, " AND ") {
		t.Error("multi-word query should contain AND conditions for per-term matching")
	}
}

func TestBuildSearchQuery_WithNumber(t *testing.T) {
	query, args := buildSearchQuery("MUL-42", []string{"MUL-42"}, 42, true, false)

	_ = args
	// Number match should be in WHERE.
	if !strings.Contains(query, "i.number = ") {
		t.Error("query should contain number match in WHERE clause")
	}
	// Tier 0 rank for identifier match.
	if !strings.Contains(query, "THEN 0") {
		t.Error("query should contain tier 0 rank for identifier match")
	}
}

func TestBuildSearchQuery_IncludeClosed(t *testing.T) {
	query, _ := buildSearchQuery("test", []string{"test"}, 0, false, true)

	if strings.Contains(query, "NOT IN ('done', 'cancelled')") {
		t.Error("query should not exclude done/cancelled when includeClosed=true")
	}
}

func TestBuildSearchQuery_SpecialChars(t *testing.T) {
	query, args := buildSearchQuery("100%", []string{"100%"}, 0, false, false)

	_ = query
	// % should be escaped in the phrase arg.
	if escaped, ok := args[0].(string); !ok || !strings.Contains(escaped, `\%`) {
		t.Errorf("expected %% to be escaped in phrase arg, got %q", args[0])
	}
}
