package search

import (
	"strings"
	"testing"
)

func TestBuild_FreeText(t *testing.T) {
	q := Build(QueryInput{
		WorkspaceID: "ws",
		Text:        "deploy bug",
		Limit:       20,
	})
	if !q.HasText {
		t.Error("HasText should be true with free text")
	}
	if !strings.Contains(q.SQL, "websearch_to_tsquery") {
		t.Error("SQL should use websearch_to_tsquery for text search")
	}
	if !strings.Contains(q.SQL, "<% lower(i.title)") {
		t.Error("SQL should use the `<%` operator for the title trigram fallback so gin_trgm_ops index is used (FIR-2664)")
	}
	if !strings.Contains(q.SQL, "<% lower(coalesce(i.description, ''))") {
		t.Error("SQL should use the `<%` operator for the description trigram fallback (FIR-2664)")
	}
	// FIR-2664 (round 2): comment.content fuzzy was the dominant cost on
	// prod — Seq Scan on comment table because OR-with-FTS prevented
	// BitmapOr on idx_comment_content_trgm. Comment match is now strict FTS;
	// typo-tolerance lives on title + description only. Lock that in.
	if strings.Contains(q.SQL, "<% lower(c.content)") {
		t.Error("comment content trigram must NOT appear — it forces seq scan on comment (FIR-2664)")
	}
	if !strings.Contains(q.SQL, "search_tsv @@") {
		t.Error("SQL should match against the generated tsvector column")
	}
	if !strings.Contains(q.SQL, "ts_rank") {
		t.Error("SQL should rank with ts_rank")
	}
	if !strings.Contains(q.SQL, "COUNT(*) OVER()") {
		t.Error("SQL should keep total_count window for pagination")
	}
}

func TestBuild_FiltersOnly(t *testing.T) {
	q := Build(QueryInput{
		WorkspaceID:  "ws",
		Text:         "",
		StatusValues: []string{"todo"},
		Limit:        20,
	})
	if q.HasText {
		t.Error("HasText should be false when only filters supplied")
	}
	if strings.Contains(q.SQL, "websearch_to_tsquery") && strings.Contains(q.SQL, "search_tsv @@ websearch_to_tsquery") {
		t.Error("SQL should NOT include FTS text-matching predicate when text is empty")
	}
	if !strings.Contains(q.SQL, "i.status IN") {
		t.Error("SQL should constrain status when StatusValues supplied")
	}
	if !strings.Contains(q.SQL, "ORDER BY i.updated_at DESC") {
		t.Error("SQL should sort by updated_at when no text relevance signal")
	}
}

func TestBuild_StatusFilter(t *testing.T) {
	q := Build(QueryInput{
		WorkspaceID:  "ws",
		Text:         "x",
		StatusValues: []string{"todo", "in_progress"},
		Limit:        20,
	})
	if !strings.Contains(q.SQL, "i.status IN") {
		t.Error("status filter missing from SQL")
	}
	// $1=ws, $2/3/4/5=text params, $6=todo, $7=in_progress
	if q.Args[5] != "todo" || q.Args[6] != "in_progress" {
		t.Errorf("status args = %v, want [..., todo, in_progress, ...]", q.Args)
	}
}

func TestBuild_IncludeClosedDefault(t *testing.T) {
	q := Build(QueryInput{
		WorkspaceID: "ws",
		Text:        "x",
		Limit:       20,
	})
	if !strings.Contains(q.SQL, "NOT IN ('done', 'cancelled')") {
		t.Error("closed issues should be excluded when IncludeClosed=false and no explicit status filter")
	}
}

func TestBuild_IncludeClosedTrueDropsExclusion(t *testing.T) {
	q := Build(QueryInput{
		WorkspaceID:   "ws",
		Text:          "x",
		IncludeClosed: true,
		Limit:         20,
	})
	if strings.Contains(q.SQL, "NOT IN ('done', 'cancelled')") {
		t.Error("closed-exclusion should be dropped when IncludeClosed=true")
	}
}

func TestBuild_ExplicitStatusOverridesIncludeClosed(t *testing.T) {
	q := Build(QueryInput{
		WorkspaceID:  "ws",
		Text:         "x",
		StatusValues: []string{"done"},
		Limit:        20,
	})
	if strings.Contains(q.SQL, "NOT IN ('done', 'cancelled')") {
		t.Error("explicit status:done should override the default closed exclusion")
	}
}

func TestBuild_AssigneeUnassigned(t *testing.T) {
	q := Build(QueryInput{
		WorkspaceID:        "ws",
		Text:               "x",
		AssigneeUnassigned: true,
		Limit:              20,
	})
	if !strings.Contains(q.SQL, "i.assignee_id IS NULL") {
		t.Error("assignee:none should produce IS NULL predicate")
	}
}

func TestBuild_HasAttachment(t *testing.T) {
	q := Build(QueryInput{
		WorkspaceID:   "ws",
		Text:          "x",
		HasAttachment: true,
		Limit:         20,
	})
	if !strings.Contains(q.SQL, "FROM attachment") {
		t.Error("has:attachment should add EXISTS attachment predicate")
	}
}

func TestBuild_ProjectFilter(t *testing.T) {
	q := Build(QueryInput{
		WorkspaceID: "ws",
		Text:        "x",
		ProjectIDs:  []any{"p1", "p2"},
		Limit:       20,
	})
	if !strings.Contains(q.SQL, "i.project_id IN (") {
		t.Error("project filter should produce IN predicate")
	}
}

func TestBuild_FromMemberAndComment(t *testing.T) {
	q := Build(QueryInput{
		WorkspaceID: "ws",
		Text:        "x",
		FromUserIDs: []any{"u1"},
		Limit:       20,
	})
	if !strings.Contains(q.SQL, "creator_type = 'member'") {
		t.Error("from:<member> should match issues created by the member")
	}
	if !strings.Contains(q.SQL, "FROM comment c") {
		t.Error("from:<member> should also match issues with a comment by the member")
	}
	if !strings.Contains(q.SQL, "c.author_type = 'member'") {
		t.Error("comment-author predicate should specify member type")
	}
}

func TestBuild_AssigneeAgent(t *testing.T) {
	q := Build(QueryInput{
		WorkspaceID:      "ws",
		Text:             "x",
		AssigneeAgentIDs: []any{"a1"},
		Limit:            20,
	})
	if !strings.Contains(q.SQL, "i.assignee_type = 'agent'") {
		t.Error("assignee:<agent> should constrain assignee_type to agent")
	}
}

func TestBuild_NumberMatchBoost(t *testing.T) {
	q := Build(QueryInput{
		WorkspaceID: "ws",
		Text:        "MUL-42",
		QueryNumber: 42,
		HasNumber:   true,
		Limit:       20,
	})
	if !strings.Contains(q.SQL, "i.number = ") {
		t.Error("identifier match should appear in WHERE")
	}
	if !strings.Contains(q.SQL, "THEN 100.0") {
		t.Error("identifier-number match should add a strong relevance boost")
	}
}
