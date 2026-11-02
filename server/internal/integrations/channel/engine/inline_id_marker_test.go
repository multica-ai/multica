package engine

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func testRefID(t *testing.T) pgtype.UUID {
	t.Helper()
	id, err := uuid.Parse("019fe1d3-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// A marker that REFERS to an attachment is rewritten to name it, not to embed
// it. An adapter asks for this when the thing the marker stands for belongs to
// somebody else's message — a quoted picture — and embedding it would state
// this sender attached it.
func TestAnIDOnlyMarkerNamesTheAttachmentInstead(t *testing.T) {
	t.Parallel()
	id := testRefID(t)
	ref := channel.MediaRef{
		Type:              channel.MsgTypeImage,
		InlinePlaceholder: "[Image: unavailable]",
		InlineIDOnly:      true,
	}
	got := inlineMediaMarkdown(ref, id)
	want := "[Image: " + uuid.UUID(id.Bytes).String() + "]"
	if got != want {
		t.Fatalf("inlineMediaMarkdown = %q, want %q", got, want)
	}
	if strings.Contains(got, "/download") || strings.HasPrefix(got, "!") {
		t.Errorf("the marker was replaced by an embed (%q) — that says this sender attached a "+
			"picture from somebody else's message", got)
	}
}

// The ordinary case is untouched: an attachment the sender made is still
// embedded, which is what every existing adapter relies on.
func TestAnOrdinaryMarkerIsStillEmbedded(t *testing.T) {
	t.Parallel()
	id := testRefID(t)
	ref := channel.MediaRef{Type: channel.MsgTypeImage, InlinePlaceholder: "[Image]"}
	got := inlineMediaMarkdown(ref, id)
	if !strings.Contains(got, "/download") {
		t.Fatalf("inlineMediaMarkdown = %q, want the attachment embedded", got)
	}
}

// The label ahead of the colon is whatever the adapter called the thing, and
// only the state word after it is replaced. The two spellings are derived from
// each other rather than written twice, so a new label needs no change here.
func TestTheLabelSurvivesTheRewrite(t *testing.T) {
	t.Parallel()
	id := testRefID(t)
	for marker, want := range map[string]string{
		"[Image: unavailable]": "[Image: ",
		"[File: unavailable]":  "[File: ",
		"[Video: unavailable]": "[Video: ",
		"[Image]":              "[Image: ",
		"[]":                   "[",
	} {
		got := inlineAttachmentIDMarker(marker, id)
		if !strings.HasPrefix(got, want) {
			t.Errorf("inlineAttachmentIDMarker(%q) = %q, want it to start %q", marker, got, want)
		}
		if !strings.Contains(got, uuid.UUID(id.Bytes).String()) {
			t.Errorf("inlineAttachmentIDMarker(%q) = %q, want it to carry the id", marker, got)
		}
	}
}
