package note

// FIR-1621 — pure unit tests for the send bundle + mention extraction. No DB.

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func comment(body string) cerebrodb.CerebroNoteComment {
	return cerebrodb.CerebroNoteComment{Body: body}
}

func anchoredComment(quote, body string) cerebrodb.CerebroNoteComment {
	return cerebrodb.CerebroNoteComment{
		Body:        body,
		AnchorQuote: pgtype.Text{String: quote, Valid: true},
	}
}

func TestBuildBundle(t *testing.T) {
	got := buildBundle("My note", "", []cerebrodb.CerebroNoteComment{
		comment("first point"),
		comment("  "), // blank — skipped
		comment("second point"),
	})
	if !strings.Contains(got, "My note") {
		t.Errorf("bundle missing note title: %q", got)
	}
	if !strings.Contains(got, "2 comments") {
		t.Errorf("bundle missing count: %q", got)
	}
	if !strings.Contains(got, "- first point") || !strings.Contains(got, "- second point") {
		t.Errorf("bundle missing comment bodies: %q", got)
	}
	if strings.Contains(got, "-  \n") {
		t.Errorf("blank comment should be skipped: %q", got)
	}

	single := buildBundle("X", "", []cerebrodb.CerebroNoteComment{comment("only")})
	if !strings.Contains(single, "1 comment)") {
		t.Errorf("singular count wrong: %q", single)
	}
}

// FIR-1873 — when a note has a link, the bundle deep-links the note name back to
// the source note instead of rendering a bare quoted title.
func TestBuildBundleLinksNote(t *testing.T) {
	got := buildBundle("My note", "/acme/notes?note=abc", []cerebrodb.CerebroNoteComment{comment("hi")})
	if !strings.Contains(got, "From note [My note](/acme/notes?note=abc)") {
		t.Errorf("bundle should link the note name: %q", got)
	}
}

// FIR-1873 — the dispatched comment used to read "From note “”" when the note
// had no explicit title. noteDisplayTitle falls back to the first body line.
func TestNoteDisplayTitle(t *testing.T) {
	cases := []struct {
		title, body, want string
	}{
		{"My note", "ignored body", "My note"},
		{"  ", "# First heading\nrest", "First heading"},
		{"", "> quoted first line\nmore", "quoted first line"},
		{"", "", "Untitled note"},
		{"", "   \n  \n", "Untitled note"},
	}
	for _, c := range cases {
		if got := noteDisplayTitle(c.title, c.body); got != c.want {
			t.Errorf("noteDisplayTitle(%q, %q) = %q, want %q", c.title, c.body, got, c.want)
		}
	}
}

// A comment anchored to a text selection must carry its quoted span into the
// bundle, so the agent sees which part of the note/document the remark is about.
func TestBuildBundleIncludesAnchorQuote(t *testing.T) {
	got := buildBundle("Doc", "", []cerebrodb.CerebroNoteComment{
		anchoredComment("the\n  third  paragraph", "please rewrite this"),
		comment("a general note"),
	})
	if !strings.Contains(got, "On “the third paragraph”") {
		t.Errorf("bundle missing/poorly-formatted anchor quote: %q", got)
	}
	if !strings.Contains(got, "please rewrite this") {
		t.Errorf("bundle missing anchored comment body: %q", got)
	}
	// An un-anchored comment stays a plain bullet (no empty "On “”").
	if strings.Contains(got, "On “”") {
		t.Errorf("un-anchored comment should not render an empty quote: %q", got)
	}
}

func TestHasAgentMention(t *testing.T) {
	a := "00000000-0000-0000-0000-0000000000aa"
	cases := []struct {
		body string
		want bool
	}{
		{"hi [@A](mention://agent/" + a + ") please", true},
		{"plain discussion, no tag", false},
		{"a member [@M](mention://member/" + a + ") is not an agent", false},
		{"", false},
	}
	for _, c := range cases {
		if got := hasAgentMention(c.body); got != c.want {
			t.Errorf("hasAgentMention(%q) = %v, want %v", c.body, got, c.want)
		}
	}
}

func TestDistinctAgentMentions(t *testing.T) {
	a := "00000000-0000-0000-0000-0000000000aa"
	b := "00000000-0000-0000-0000-0000000000bb"
	got := distinctAgentMentions([]cerebrodb.CerebroNoteComment{
		comment("hi [@A](mention://agent/" + a + ") please"),
		comment("also [@B](mention://agent/" + b + ") and [@A](mention://agent/" + a + ") again"),
		comment("a member [@M](mention://member/" + a + ") is not an agent"),
	})
	if len(got) != 2 {
		t.Fatalf("got %d agent mentions, want 2 (deduped): %v", len(got), got)
	}
	if got[0] != a || got[1] != b {
		t.Fatalf("order/ids wrong: %v", got)
	}
}
