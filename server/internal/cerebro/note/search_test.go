package note

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func testUUID() pgtype.UUID {
	var u pgtype.UUID
	_ = u.Scan("00000000-0000-0000-0000-000000000001")
	return u
}

func TestBuildNoteSearchSQL_TextOnly(t *testing.T) {
	sql, args := buildNoteSearchSQL(noteSearchInput{
		WorkspaceID: testUUID(),
		UserID:      testUUID(),
		Text:        "deploy",
		Limit:       20,
		Offset:      0,
	})

	// ws, user, ts, trgm, raw, starts, limit, offset = 8 args (no kind).
	if len(args) != 8 {
		t.Fatalf("expected 8 args, got %d", len(args))
	}

	mustContain := []string{
		"FROM artifact a",
		"LEFT JOIN cerebro_note n ON n.artifact_id = a.id",
		"cerebro_artifact_folder_visible(a.folder_id,",
		"cerebro_note_share",
		"n.visibility = 'workspace'",
		"websearch_to_tsquery('simple',",
		"cerebro_note_comment c",
		"COUNT(*) OVER() AS total_count",
		"AS match_source",
		"AS matched_comment_body",
		"ORDER BY",
		"LIMIT $7 OFFSET $8",
	}
	for _, frag := range mustContain {
		if !strings.Contains(sql, frag) {
			t.Errorf("SQL missing fragment %q\n---\n%s", frag, sql)
		}
	}
	if strings.Contains(sql, "a.kind =") {
		t.Errorf("kind filter should be absent when Kind is empty")
	}
}

func TestBuildNoteSearchSQL_KindFilter(t *testing.T) {
	sql, args := buildNoteSearchSQL(noteSearchInput{
		WorkspaceID: testUUID(),
		UserID:      testUUID(),
		Text:        "plan",
		Kind:        "report",
		Limit:       10,
		Offset:      5,
	})
	// kind adds one arg between the where clause and limit/offset: 9 total.
	if len(args) != 9 {
		t.Fatalf("expected 9 args with kind filter, got %d", len(args))
	}
	if !strings.Contains(sql, "a.kind = $7") {
		t.Errorf("expected kind predicate bound to $7, got:\n%s", sql)
	}
	if args[6] != "report" {
		t.Errorf("expected $7 arg = report, got %v", args[6])
	}
	// limit/offset shift to $8/$9 once the kind param consumed $7.
	if !strings.Contains(sql, "LIMIT $8 OFFSET $9") {
		t.Errorf("expected LIMIT $8 OFFSET $9, got:\n%s", sql)
	}
}

func TestNoteSnippet(t *testing.T) {
	// Term found → snippet centred on it.
	got := noteSnippet("The quarterly deploy plan covers staging and prod rollout.", "deploy")
	if !strings.Contains(strings.ToLower(got), "deploy") {
		t.Errorf("snippet should contain the matched term: %q", got)
	}

	// Term absent → head of the text, no panic.
	got = noteSnippet("a body with no match here at all", "missing")
	if got == "" {
		t.Errorf("snippet of non-empty text should not be empty")
	}

	// Empty body → empty snippet.
	if noteSnippet("", "x") != "" {
		t.Errorf("empty body should yield empty snippet")
	}

	// Danish multibyte content must not panic and must stay valid UTF-8.
	got = noteSnippet(strings.Repeat("æøå ", 200)+"deploy hændelse", "deploy")
	if !strings.Contains(got, "deploy") {
		t.Errorf("danish snippet should contain the matched term: %q", got)
	}
	if !utf8Valid(got) {
		t.Errorf("snippet is not valid UTF-8: %q", got)
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
