package lark

import "testing"

// TestPrependMentionWireShapes pins the two spellings apart. They are easy
// to swap by accident and the failure is silent-ish: the wrong one renders
// as literal `<at ...>` markup in the user's transcript instead of a
// mention, so the member still gets no notification — the bug #8234
// reported, with extra noise on top.
func TestPrependMentionWireShapes(t *testing.T) {
	t.Parallel()
	if got, want := prependTextMention("ou_x", "hi"), `<at user_id="ou_x"></at> hi`; got != want {
		t.Errorf("text mention = %q, want %q", got, want)
	}
	if got, want := prependMarkdownMention("ou_x", "hi"), "<at id=ou_x></at> hi"; got != want {
		t.Errorf("markdown mention = %q, want %q", got, want)
	}
}

// TestPrependMentionDegradesWithoutIdentity is the "never guess" contract:
// with no usable open_id the body goes out verbatim. A reply that lost its
// mention is a small regression; a reply that mentions the wrong colleague
// is the failure the issue explicitly asked us to avoid, and a malformed id
// would corrupt the card JSON envelope it is interpolated into.
func TestPrependMentionDegradesWithoutIdentity(t *testing.T) {
	t.Parallel()
	// Anything that could break out of the attribute or the card JSON is
	// treated as "no identity" rather than escaped — real Feishu ids are
	// "ou_" + hex, so a value like this is a bug upstream, not a name.
	for _, openID := range []string{
		"",
		`ou_x" onclick="`,
		"ou_x<br>",
		"ou_x ou_y",
		"ou_x\nou_y",
		`ou_x\`,
	} {
		if got := prependTextMention(openID, "hi"); got != "hi" {
			t.Errorf("prependTextMention(%q) = %q, want the body unchanged", openID, got)
		}
		if got := prependMarkdownMention(openID, "hi"); got != "hi" {
			t.Errorf("prependMarkdownMention(%q) = %q, want the body unchanged", openID, got)
		}
	}
}

// TestPrependMentionKeepsBodyIntact guards the seam: the agent's answer is
// forwarded byte-for-byte after the separator, including leading markdown
// that the caller already used to pick the wire shape.
func TestPrependMentionKeepsBodyIntact(t *testing.T) {
	t.Parallel()
	body := "# heading\n- bullet\n\n```go\nfmt.Println()\n```"
	got := prependMarkdownMention("ou_x", body)
	if want := "<at id=ou_x></at> " + body; got != want {
		t.Errorf("markdown mention = %q, want %q", got, want)
	}
}
