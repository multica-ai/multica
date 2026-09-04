package handler

import (
	"strings"
	"testing"
)

func TestBuildSearchQuery_SingleTerm(t *testing.T) {
	query, args := buildSearchQuery("Hello", []string{"Hello"}, 0, false, false, []string{"done", "cancelled"})

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
	if !strings.Contains(query, "NOT (i.status = ANY(") {
		t.Error("query should exclude the expanded terminal status keys when includeClosed=false")
	}
	if strings.Contains(query, "issue_effective_status") {
		t.Error("query should not resolve status categories once per issue row")
	}
}

func TestBuildSearchQuery_OmitsUnusedExactTotal(t *testing.T) {
	query, _ := buildSearchQuery("Hello", []string{"Hello"}, 0, false, false, []string{"done", "cancelled"})

	if strings.Contains(query, "COUNT(*) OVER()") || strings.Contains(query, "total_count") {
		t.Fatalf("search query should not calculate an unused exact total:\n%s", query)
	}
}

func TestBuildProjectSearchQuery_OmitsUnusedExactTotal(t *testing.T) {
	query, _ := buildProjectSearchQuery("Hello", []string{"Hello"}, false)

	if strings.Contains(query, "COUNT(*) OVER()") || strings.Contains(query, "total_count") {
		t.Fatalf("project search query should not calculate an unused exact total:\n%s", query)
	}
}

func TestBuildSearchQuery_CustomTerminalStatuses(t *testing.T) {
	terminalStatusKeys := []string{"done", "cancelled", "verified"}
	query, args := buildSearchQuery(
		"Hello",
		[]string{"Hello"},
		0,
		false,
		false,
		terminalStatusKeys,
	)

	if !strings.Contains(query, "NOT (i.status = ANY($5::text[]))") {
		t.Fatalf("query does not filter through the expanded terminal keys:\n%s", query)
	}
	got, ok := args[4].([]string)
	if !ok || len(got) != len(terminalStatusKeys) {
		t.Fatalf("terminal status argument = %#v, want %#v", args[4], terminalStatusKeys)
	}
	for i := range terminalStatusKeys {
		if got[i] != terminalStatusKeys[i] {
			t.Fatalf("terminal status argument = %#v, want %#v", got, terminalStatusKeys)
		}
	}
}

func TestBuildSearchQuery_MultiTerm(t *testing.T) {
	query, args := buildSearchQuery("Foo Bar", []string{"Foo", "Bar"}, 0, false, false, []string{"done", "cancelled"})

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
	query, args := buildSearchQuery("MUL-42", []string{"MUL-42"}, 42, true, false, []string{"done", "cancelled"})

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
	query, _ := buildSearchQuery("test", []string{"test"}, 0, false, true, nil)

	if strings.Contains(query, "i.status = ANY(") {
		t.Error("query should not exclude done/cancelled when includeClosed=true")
	}
}

func TestBuildSearchQuery_SpecialChars(t *testing.T) {
	query, args := buildSearchQuery("100%", []string{"100%"}, 0, false, false, []string{"done", "cancelled"})

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
	query, _ := buildSearchQuery("test phrase", []string{"test", "phrase"}, 0, false, false, []string{"done", "cancelled"})

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
	query, _ := buildSearchQuery("foo bar", []string{"foo", "bar"}, 0, false, false, []string{"done", "cancelled"})

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
	query, _ := buildSearchQuery("html", []string{"html"}, 0, false, false, []string{"done", "cancelled"})

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
// MUL-4059 fix: the single comment scan and the final point hydration MUST
// both filter by c.workspace_id = $wsParam. Without this, Postgres can read
// matching comments from every workspace — on prd this was 536k rows / 32.3 s
// for '%search%'.
//
// $4 is buildSearchQuery's canonical workspace_id placeholder (the
// caller writes wsUUID into args[3] before executing).
func TestBuildSearchQuery_CommentSubqueryWorkspaceScope(t *testing.T) {
	singleQuery, _ := buildSearchQuery("html", []string{"html"}, 0, false, false, []string{"done", "cancelled"})

	// Candidate-first search aggregates comments in one workspace-first pass;
	// it must not grow a second scan for eligibility, ranking, or snippets.
	fromCount := strings.Count(singleQuery, "FROM comment c")
	scopedCount := strings.Count(singleQuery, "c.workspace_id = $4")
	if fromCount != 1 {
		t.Fatalf("single-term query has %d comment scans, want exactly one:\n%s", fromCount, singleQuery)
	}
	if scopedCount < 2 {
		t.Errorf("single-term query has %d workspace_id filters, want one on aggregation and one on hydration:\n%s", scopedCount, singleQuery)
	}
	if !strings.Contains(singleQuery, "comment_matches AS MATERIALIZED") {
		t.Errorf("comment aggregation is not materialized:\n%s", singleQuery)
	}
	if strings.Contains(singleQuery, "EXISTS (SELECT 1 FROM comment") {
		t.Errorf("candidate-first query regressed to correlated comment EXISTS checks:\n%s", singleQuery)
	}

	// Adding terms adds boolean aggregates, not extra comment relation scans.
	multiQuery, _ := buildSearchQuery("foo bar", []string{"foo", "bar"}, 0, false, false, []string{"done", "cancelled"})
	fromCountMulti := strings.Count(multiQuery, "FROM comment c")
	if fromCountMulti != 1 {
		t.Errorf("multi-term query has %d comment scans, want exactly one:\n%s", fromCountMulti, multiQuery)
	}
	if !strings.Contains(multiQuery, "BOOL_OR((LOWER(c.content) LIKE") || !strings.Contains(multiQuery, "AS comment_all_terms") {
		t.Errorf("multi-term query does not retain the same-comment all-terms flag:\n%s", multiQuery)
	}
}

func TestBuildSearchQuery_HydratesOnlyTheSelectedPage(t *testing.T) {
	query, _ := buildSearchQuery("foo bar", []string{"foo", "bar"}, 0, false, false, []string{"done", "cancelled"})

	if !strings.Contains(query, "issue_matches AS MATERIALIZED") || !strings.Contains(query, "page_candidates AS MATERIALIZED") {
		t.Fatalf("query does not materialize narrow issue flags and the selected page:\n%s", query)
	}
	limitAt := strings.Index(query, "LIMIT ")
	issueHydrationAt := strings.Index(query, "JOIN issue i ON i.id = pc.issue_id")
	commentHydrationAt := strings.Index(query, "LEFT JOIN comment c ON c.id = pc.snippet_comment_id")
	if limitAt == -1 || issueHydrationAt < limitAt || commentHydrationAt < limitAt {
		t.Fatalf("full issue/comment hydration must happen after LIMIT/OFFSET:\n%s", query)
	}
	if lastTextMatch := strings.LastIndex(query, "LOWER(c.content) LIKE"); lastTextMatch > limitAt {
		t.Errorf("comment hydration repeats a text search after LIMIT/OFFSET:\n%s", query)
	}
}

// --- MUL-5824: cancelled work must not outrank live work ---

// orderByClause returns everything after the final ORDER BY, so ranking-order
// assertions cannot be satisfied by an expression that merely appears in the
// SELECT list or the WHERE clause.
func orderByClause(t *testing.T, query string) string {
	t.Helper()
	i := strings.LastIndex(query, "ORDER BY ")
	if i == -1 {
		t.Fatalf("query has no ORDER BY clause:\n%s", query)
	}
	return query[i+len("ORDER BY "):]
}

// The cancelled demotion must sort BEFORE the relevance tiers, not after.
// As a tie-breaker it would be inert: statusRank only orders issues that
// already landed in the same tier, so an exactly-titled cancelled issue
// (tier 1) would still beat an in_progress title-contains match (tier 3).
func TestBuildSearchQuery_CancelledDemotedAheadOfRelevance(t *testing.T) {
	query := buildSearchQueryForTest(t, "login bug", []string{"login", "bug"}, 0, false, true)
	if !strings.Contains(query, "CASE WHEN im.status = 'cancelled' AND NOT (im.title_exact) THEN 1 ELSE 0 END AS cancelled_rank") {
		t.Fatalf("ranked candidates have no cancelled demotion:\n%s", query)
	}

	orderBy := orderByClause(t, query)
	if !strings.HasPrefix(orderBy, "pc.cancelled_rank, pc.relevance_rank, pc.status_rank") {
		t.Errorf("cancelled demotion does not sort before relevance and status:\n%s", orderBy)
	}

	// The demotion must not replace the existing status ordering.
	if !strings.Contains(query, "WHEN 'in_progress' THEN 0") {
		t.Errorf("statusRank was dropped from ranked candidates:\n%s", query)
	}
	if !strings.Contains(orderBy, "pc.updated_at DESC") {
		t.Errorf("recency tie-breaker was dropped from ORDER BY:\n%s", orderBy)
	}
	if !strings.Contains(orderBy, "pc.issue_id ASC") {
		t.Errorf("stable issue ID tie-breaker was dropped from ORDER BY:\n%s", orderBy)
	}
}

// Searching an exact title or an exact identifier is unambiguous targeting —
// the searcher already knows which issue they want, so demoting it would just
// hide the row they asked for.
func TestBuildSearchQuery_CancelledDirectHitExempt(t *testing.T) {
	textOnly := buildSearchQueryForTest(t, "ship it", []string{"ship", "it"}, 0, false, true)
	if !strings.Contains(textOnly, "im.status = 'cancelled' AND NOT (im.title_exact)") {
		t.Errorf("exact-title hit is not exempt from the cancelled demotion:\n%s", textOnly)
	}
	if strings.Contains(textOnly, "number_exact") {
		t.Errorf("non-numeric query should not reference i.number in the demotion:\n%s", textOnly)
	}

	withNumber := buildSearchQueryForTest(t, "MUL-42", []string{"MUL-42"}, 42, true, true)
	if !strings.Contains(withNumber, "NOT (im.title_exact OR im.number_exact)") {
		t.Errorf("identifier lookup is not exempt from the cancelled demotion, so MUL-42 sinks below every fuzzy match:\n%s", withNumber)
	}
}

// 'done' is finished work worth referencing; only 'cancelled' is thrown away.
func TestBuildSearchQuery_DoneNotDemotedAheadOfRelevance(t *testing.T) {
	query := buildSearchQueryForTest(t, "login", []string{"login"}, 0, false, true)
	if strings.Contains(query, "CASE WHEN im.status = 'done'") {
		t.Errorf("done issues were demoted ahead of relevance; only cancelled should be:\n%s", query)
	}
	if !strings.Contains(query, "WHEN 'done' THEN 5") {
		t.Errorf("done status tie-breaker was dropped:\n%s", query)
	}
}

// Project search has no statusRank at all, and the command palette renders
// projects above issues — an undemoted cancelled project can be the first row
// of the entire result list.
func TestBuildProjectSearchQuery_CancelledDemotedAheadOfRelevance(t *testing.T) {
	query, _ := buildProjectSearchQuery("platform", []string{"platform"}, true)
	orderBy := orderByClause(t, query)

	cancelledAt := strings.Index(orderBy, "p.status = 'cancelled'")
	if cancelledAt == -1 {
		t.Fatalf("project ORDER BY has no cancelled demotion:\n%s", orderBy)
	}
	relevanceEndsAt := strings.Index(orderBy, "ELSE 5 END")
	if relevanceEndsAt == -1 {
		t.Fatalf("project ORDER BY has no relevance rank CASE:\n%s", orderBy)
	}
	if cancelledAt > relevanceEndsAt {
		t.Errorf("cancelled projects sort after the relevance tiers:\n%s", orderBy)
	}
	if !strings.Contains(orderBy, "LOWER(p.title) <> $1") {
		t.Errorf("exact-title hit is not exempt from the cancelled demotion:\n%s", orderBy)
	}
	if !strings.Contains(orderBy, "p.updated_at DESC") {
		t.Errorf("recency tie-breaker was dropped from project ORDER BY:\n%s", orderBy)
	}
}

// buildSearchQuery mutates the terms slice in place (lowercasing); this wrapper
// keeps each test's literals independent.
func buildSearchQueryForTest(t *testing.T, phrase string, terms []string, num int, hasNum bool, includeClosed bool) string {
	t.Helper()
	query, _ := buildSearchQuery(phrase, append([]string(nil), terms...), num, hasNum, includeClosed, []string{"done", "cancelled"})
	return query
}
