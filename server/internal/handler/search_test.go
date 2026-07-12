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
	// args[0]=exact, args[1]=%phrase%, args[2]=phrase%, args[3]=workspace_id placeholder; term args start at args[4].
	if args[4] != "%foo%" {
		t.Errorf("expected first term arg as contains pattern, got %q", args[4])
	}
	if args[5] != "%bar%" {
		t.Errorf("expected second term arg as contains pattern, got %q", args[5])
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

// --- Project search tests ---

func TestBuildProjectSearchQuery_SingleTerm(t *testing.T) {
	query, args := buildProjectSearchQuery("Hello", []string{"Hello"}, false)

	if args[0] != "hello" {
		t.Errorf("expected phrase arg to be lowercased, got %q", args[0])
	}

	if strings.Contains(query, "ILIKE") {
		t.Error("query should not contain ILIKE")
	}
	if !strings.Contains(query, "LOWER(p.title) LIKE") {
		t.Error("query should contain LOWER(p.title) LIKE")
	}
	if !strings.Contains(query, "LOWER(COALESCE(p.description, '')) LIKE") {
		t.Error("query should contain LOWER(COALESCE(p.description, '')) LIKE")
	}

	// Should exclude completed/cancelled by default.
	if !strings.Contains(query, "NOT IN ('completed', 'cancelled')") {
		t.Error("query should exclude completed/cancelled when includeClosed=false")
	}
}

func TestBuildProjectSearchQuery_MultiTerm(t *testing.T) {
	query, args := buildProjectSearchQuery("Foo Bar", []string{"Foo", "Bar"}, false)

	if args[0] != "foo bar" {
		t.Errorf("expected phrase arg lowercased, got %q", args[0])
	}
	if args[2] != "foo" {
		t.Errorf("expected first term arg lowercased, got %q", args[2])
	}
	if args[3] != "bar" {
		t.Errorf("expected second term arg lowercased, got %q", args[3])
	}

	if !strings.Contains(query, " AND ") {
		t.Error("multi-word query should contain AND conditions for per-term matching")
	}
}

func TestBuildProjectSearchQuery_IncludeClosed(t *testing.T) {
	query, _ := buildProjectSearchQuery("test", []string{"test"}, true)

	if strings.Contains(query, "NOT IN ('completed', 'cancelled')") {
		t.Error("query should not exclude completed/cancelled when includeClosed=true")
	}
}

// --- extractSnippet regression tests ---

func TestExtractSnippet_PhraseMatch(t *testing.T) {
	content := "The quick brown fox jumps over the lazy dog near the river bank"
	snippet := extractSnippet(content, "brown fox")
	if !strings.Contains(snippet, "brown fox") {
		t.Errorf("snippet should contain the phrase 'brown fox', got %q", snippet)
	}
}

func TestExtractSnippet_MultiWordNonContiguous(t *testing.T) {
	// "deploy" and "kubernetes" both appear but not as a contiguous phrase.
	content := "We need to deploy the new service. The kubernetes cluster is ready for production workloads."
	snippet := extractSnippet(content, "deploy kubernetes")
	// Should NOT fall back to first 120 chars blindly — should center on earliest term.
	if !strings.Contains(strings.ToLower(snippet), "deploy") && !strings.Contains(strings.ToLower(snippet), "kubernetes") {
		t.Errorf("snippet should contain at least one search term, got %q", snippet)
	}
	// Specifically, "deploy" appears first so snippet should be centered around it.
	if !strings.Contains(strings.ToLower(snippet), "deploy") {
		t.Errorf("snippet should center on earliest term 'deploy', got %q", snippet)
	}
}

func TestExtractSnippet_FallbackWhenNoMatch(t *testing.T) {
	content := strings.Repeat("a", 200)
	snippet := extractSnippet(content, "zzz")
	if len([]rune(snippet)) > 124 { // 120 + "..."
		t.Errorf("snippet should be truncated to ~120 runes when no match, got len=%d", len([]rune(snippet)))
	}
}

func TestExtractSnippet_ShortContent(t *testing.T) {
	content := "short text"
	snippet := extractSnippet(content, "missing")
	if snippet != content {
		t.Errorf("short content with no match should return as-is, got %q", snippet)
	}
}

func TestExtractSnippet_CaseInsensitive(t *testing.T) {
	content := "Error in HTML rendering pipeline"
	snippet := extractSnippet(content, "html")
	if !strings.Contains(snippet, "HTML") {
		t.Errorf("snippet should find case-insensitive match, got %q", snippet)
	}
}

func TestExtractSnippet_CJKContent(t *testing.T) {
	content := "这是一段很长的中文内容，包含了搜索关键词测试用例，用来验证多字节字符不会被截断的情况"
	snippet := extractSnippet(content, "搜索关键词")
	if !strings.Contains(snippet, "搜索关键词") {
		t.Errorf("snippet should contain CJK phrase, got %q", snippet)
	}
}

// --- Ranking regression tests ---

func TestBuildSearchQuery_CommentRankTiers(t *testing.T) {
	query, _ := buildSearchQuery("test phrase", []string{"test", "phrase"}, 0, false, false)

	// Comment phrase match should be tier 7
	if !strings.Contains(query, "THEN 7") {
		t.Error("query should contain tier 7 for comment phrase match")
	}
	// Comment all-term match should be tier 8
	if !strings.Contains(query, "THEN 8") {
		t.Error("query should contain tier 8 for comment all-term match")
	}
	// Fallback should be 9, not 7
	if !strings.Contains(query, "ELSE 9") {
		t.Error("query fallback should be ELSE 9")
	}
}

func TestBuildSearchQuery_DescriptionRankTiers(t *testing.T) {
	query, _ := buildSearchQuery("foo bar", []string{"foo", "bar"}, 0, false, false)

	// Description phrase match should be tier 5
	if !strings.Contains(query, "THEN 5") {
		t.Error("query should contain tier 5 for description phrase match")
	}
	// Description all-term match should be tier 6
	if !strings.Contains(query, "THEN 6") {
		t.Error("query should contain tier 6 for description all-term match")
	}
}

func TestBuildSearchQuery_SingleTermNoAllTermTiers(t *testing.T) {
	query, _ := buildSearchQuery("html", []string{"html"}, 0, false, false)

	// Extract the rank CASE expression (ends with "ELSE 9 END") to avoid
	// false matches against statusRank which also contains THEN 4/6.
	rankEnd := strings.Index(query, "ELSE 9 END")
	if rankEnd == -1 {
		t.Fatal("query should contain rank expression with ELSE 9 END")
	}
	rankExpr := query[:rankEnd]

	// Single-term queries should NOT have tier 4 (title all-terms), 6 (desc all-terms), or 8 (comment all-terms)
	if strings.Contains(rankExpr, "THEN 4") {
		t.Error("single-term query should not have tier 4 (title all-terms)")
	}
	if strings.Contains(rankExpr, "THEN 6") {
		t.Error("single-term query should not have tier 6 (description all-terms)")
	}
	if strings.Contains(rankExpr, "THEN 8") {
		t.Error("single-term query should not have tier 8 (comment all-terms)")
	}
}

// TestBuildSearchQuery_CommentSubqueryWorkspaceScope regressions the
// MUL-4059 fix: every EXISTS / correlated subquery over `comment` MUST
// filter by c.workspace_id = $wsParam. Without this, Postgres rewrites
// the correlated subquery into a hashed subplan that materializes every
// comment in the entire table matching the LIKE — on prd this was
// 536k rows / 32.3 s for '%search%'. With the filter the hashed set
// collapses to this workspace's comments and the plan uses the
// idx_comment_workspace supporting btree.
//
// $4 is buildSearchQuery's canonical workspace_id placeholder (the
// caller writes wsUUID into args[3] before executing).
func TestBuildSearchQuery_CommentSubqueryWorkspaceScope(t *testing.T) {
	singleQuery, _ := buildSearchQuery("html", []string{"html"}, 0, false, false)

	// Every occurrence of `FROM comment c` must be followed by the
	// c.workspace_id = $4 constraint. Counting is safer than a single
	// substring check because the WHERE, rank CASE, matched_comment_content
	// subqueries all touch `comment` and must each carry the filter.
	fromCount := strings.Count(singleQuery, "FROM comment c")
	scopedCount := strings.Count(singleQuery, "c.workspace_id = $4")
	if fromCount == 0 {
		t.Fatalf("single-term query has no comment subquery — did buildSearchQuery drop it?")
	}
	if scopedCount < fromCount {
		t.Errorf("single-term query has %d comment subqueries but only %d workspace_id filters — %d unscoped subquery(ies) will trigger the MUL-4059 global-hash plan",
			fromCount, scopedCount, fromCount-scopedCount)
	}

	// Multi-term uses one extra comment subquery in the WHERE and one in
	// the rank CASE for the all-terms match — same invariant applies.
	multiQuery, _ := buildSearchQuery("foo bar", []string{"foo", "bar"}, 0, false, false)
	fromCountMulti := strings.Count(multiQuery, "FROM comment c")
	scopedCountMulti := strings.Count(multiQuery, "c.workspace_id = $4")
	if scopedCountMulti < fromCountMulti {
		t.Errorf("multi-term query has %d comment subqueries but only %d workspace_id filters",
			fromCountMulti, scopedCountMulti)
	}
}
