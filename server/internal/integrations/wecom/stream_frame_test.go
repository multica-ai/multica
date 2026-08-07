package wecom

// stream_frame_test.go — the two rules of the closing frame that are not
// visible in the wire format, and that a spinning bubble is the price of
// getting wrong.

import (
	"strings"
	"testing"
)

// A closing frame carrying nothing the client will render is DISCARDED by
// WeCom. The bubble it was meant to seal keeps spinning, with no edit and no
// unsend to rescue it — so the body builder refuses to produce one and every
// caller inherits the check.
func TestClosingFrameWithNothingVisibleIsRefused(t *testing.T) {
	t.Parallel()
	for _, content := range []string{"", "   ", "\n\t ", "\r\n", " \t"} {
		if _, err := respondStreamBody("S1", content, true); err == nil {
			t.Errorf("closing frame with content %q was accepted; WeCom discards it and the bubble spins forever", content)
		}
	}
}

// The same emptiness is fine on a frame that is not closing anything: the
// opening frame's whole job is to paint a bubble with no words in it.
func TestNonClosingFrameMayCarryNothingVisible(t *testing.T) {
	t.Parallel()
	if _, err := respondStreamBody("S1", streamThinkingPlaceholder, false); err != nil {
		t.Fatalf("the opening frame was refused: %v", err)
	}
}

func TestClosingFrameWithVisibleContentIsAccepted(t *testing.T) {
	t.Parallel()
	body, err := respondStreamBody("S1", "答案", true)
	if err != nil {
		t.Fatalf("respondStreamBody: %v", err)
	}
	stream, _ := body["stream"].(map[string]any)
	if stream["finish"] != true {
		t.Errorf("finish = %v, want true", stream["finish"])
	}
	if stream["id"] != "S1" {
		t.Errorf("stream id = %v, want S1", stream["id"])
	}
}

// defuseThinkTags used to walk the answer while reading strings.ToLower of it
// at the same offsets. Case folding is not length-preserving — U+212A KELVIN
// SIGN is three bytes and folds to a one-byte "k" — so the folded copy can be
// SHORTER than the original and an offset taken from the original can land
// past its end. The string being scanned is the agent's own answer, so any
// text a user can talk the agent into echoing took the backend down with it.
func TestDefuseThinkTagsSurvivesRunesThatShrinkWhenFolded(t *testing.T) {
	t.Parallel()
	const kelvin = "K" // three bytes; folds to a one-byte "k"
	for _, in := range []string{
		kelvin + kelvin + "<x",
		strings.Repeat(kelvin, 8) + "<",
		strings.Repeat(kelvin, 8) + "</think>",
		"Temperature: 300" + kelvin + " vs 301" + kelvin + ". Now <think>tail</think>",
	} {
		// A panic here fails the test by crashing it, which is the point.
		_ = defuseThinkTags(in)
	}
}

// The quieter half of the same bug: reading a shrunken copy at the original's
// offsets also makes the scanner MISS tags it should have defused, so a real
// <think> in the answer survives into the frame and folds half the reply away.
func TestDefuseThinkTagsStillDefusesAfterAShrinkingRune(t *testing.T) {
	t.Parallel()
	const kelvin = "K"
	out := defuseThinkTags(kelvin + kelvin + "<think>hi</think>")
	if strings.Contains(out, "<think>") {
		t.Errorf("a <think> tag preceded by a shrinking rune survived defusing: %q", out)
	}
	if !strings.Contains(out, "hi") {
		t.Errorf("defusing dropped the answer's own text: %q", out)
	}
}

func TestDefuseThinkTagsNeutralisesBothEnds(t *testing.T) {
	t.Parallel()
	out := defuseThinkTags("prefix <think>a</think> suffix")
	if strings.Contains(out, "<think>") || strings.Contains(out, "</think>") {
		t.Errorf("tags survived: %q", out)
	}
	if !strings.Contains(out, "prefix ") || !strings.Contains(out, " suffix") {
		t.Errorf("surrounding text was altered: %q", out)
	}
}

// Angle brackets that are not the tag are the common case — comparisons,
// generics, HTML samples — and must come through as written. (The match is a
// prefix, so a hypothetical <thinking> would also be defused; that is a
// deliberate false positive, not something this asserts against.)
func TestDefuseThinkTagsLeavesOtherAngleBracketsAlone(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"a < b && c > d", "Vec<String>", "<div>x</div>", "if x<y then"} {
		if out := defuseThinkTags(in); out != in {
			t.Errorf("defuseThinkTags(%q) = %q, want it unchanged", in, out)
		}
	}
}

// A body past the protocol's cap is refused whole rather than clipped by the
// server, so the clip has to happen here — and on a character boundary, or the
// frame carries invalid utf8.
func TestTruncateStreamContentCutsOnARuneBoundary(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("中", streamContentLimit) // three bytes each
	out := truncateStreamContent(long)
	if len(out) > streamContentLimit {
		t.Fatalf("truncated to %d bytes, over the %d cap", len(out), streamContentLimit)
	}
	if !strings.HasSuffix(out, "…") {
		t.Error("a clipped answer does not say it was clipped")
	}
	for i, r := range out {
		if r == '�' {
			t.Fatalf("truncation cut a rune in half at byte %d", i)
		}
	}
}
