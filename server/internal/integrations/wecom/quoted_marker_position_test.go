package wecom

// quoted_marker_position_test.go — a quoted attachment is bound to the marker
// the agent reads, so the number that says WHICH occurrence of the marker it is
// has to be taken from the text the agent reads.
//
// The rule, in one line: every attachment handed over for a quote has its
// marker in the final body, and its InlineIndex is the position of that marker
// there — not the attachment's place in the quote's list of attachments.
//
// Two ways to break it were found by review of the first version, and they are
// the same break: that version numbered a quoted attachment by counting the
// attachments before it and rendered the text separately.
//
//   - A quoted sentence that spells the marker out ("The UI displays [Image:
//     unavailable]") is an occurrence of it to the engine's search, which looks
//     for the Nth occurrence in the stored body. The picture was numbered 0, the
//     binder rewrote the sentence, and the real marker stayed "unavailable".
//   - The 500-rune cut removed a marker that the attachment list still counted,
//     so the picture was fetched and the quote never named it.
//
// Everything here goes through the real callback decoder and
// channelMessageFromCallback, then asks the one question that matters of the
// body that comes out: does the Nth occurrence of this attachment's marker sit
// on the line that stands for it. The same two scenarios run end to end through
// the real binder in quoted_media_bind_db_test.go.

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

type wire = map[string]any

func qWords(s string) wire { return wire{"msgtype": "text", "text": wire{"content": s}} }
func qImage(url, key string) wire {
	return wire{"msgtype": "image", "image": wire{"url": url, "aeskey": key}}
}
func qFile(url, key string) wire {
	return wire{"msgtype": "file", "file": wire{"url": url, "aeskey": key}}
}

// replyTo decodes a callback that quotes a mixed message made of quoted, through
// the real JSON decoder, and builds the message the Router would be handed.
// own is the sender's own 图文混排; an empty own sends the plain words
// "这个怎么处理".
func replyTo(t *testing.T, quoted []wire, own []wire) channel.InboundMessage {
	t.Helper()
	return replyToAs(t, "wb-1", "T-alex", "MSGID-QPOS", quoted, own)
}

// replyToAs is replyTo for a bot and sender that exist in a database.
func replyToAs(t *testing.T, botID, senderID, msgID string, quoted []wire, own []wire) channel.InboundMessage {
	t.Helper()
	body := wire{
		"msgid":    msgID,
		"aibotid":  botID,
		"chattype": "single",
		"chatid":   senderID,
		"from":     wire{"userid": senderID},
		"quote":    wire{"msgtype": "mixed", "mixed": wire{"msg_item": quoted}},
	}
	if len(own) == 0 {
		body["msgtype"] = "text"
		body["text"] = wire{"content": "这个怎么处理"}
	} else {
		body["msgtype"] = "mixed"
		body["mixed"] = wire{"msg_item": own}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	var mc aibotMsgCallback
	if err := json.Unmarshal(raw, &mc); err != nil {
		t.Fatalf("decode callback: %v", err)
	}
	text, ok := mc.ownText()
	if !ok {
		t.Fatal("callback is not routable; the fixture is wrong")
	}
	return channelMessageFromCallback(botID, "", mc, text, "req-qpos")
}

// nthOccurrence is how the engine finds a marker: the Nth non-overlapping
// occurrence of the marker string anywhere in the stored body (engine
// nthSubstringIndex). Written out here so the test does not borrow the code
// whose input it is checking.
func nthOccurrence(body, marker string, n int) int {
	offset := 0
	for i := 0; ; i++ {
		found := strings.Index(body[offset:], marker)
		if found < 0 {
			return -1
		}
		found += offset
		if i == n {
			return found
		}
		offset = found + len(marker)
	}
}

// quotedAttachments is what the quote contributed to msg.Media: the entries
// stamped with a marker. The sender's own attachments carry none.
func quotedAttachments(msg channel.InboundMessage) []InboundMedia {
	var out []InboundMedia
	for _, m := range wecomMedia(msg) {
		if m.InlinePlaceholder != "" {
			out = append(out, m)
		}
	}
	return out
}

func wecomMedia(msg channel.InboundMessage) []InboundMedia {
	var raw struct {
		Media []InboundMedia `json:"media"`
	}
	// The wecom envelope rides in Raw; see channelMessageFromCallback.
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		return nil
	}
	return raw.Media
}

// realMarkerOffsets lists, in order, where a line that IS an attachment's
// marker sits in body. A real marker is a line of its own inside the quote
// block; a sentence that merely contains the marker text is not one.
func realMarkerOffsets(body string) []struct {
	offset int
	marker string
} {
	var out []struct {
		offset int
		marker string
	}
	pos := 0
	for _, line := range strings.SplitAfter(body, "\n") {
		content := strings.TrimRight(line, "\n")
		prefix := ""
		switch {
		case strings.HasPrefix(content, "> "+quotePrefix+" "):
			prefix = "> " + quotePrefix + " "
		case strings.HasPrefix(content, "> "):
			prefix = "> "
		}
		if prefix != "" {
			rest := strings.TrimSuffix(strings.TrimPrefix(content, prefix), "…")
			for _, kind := range []channel.MsgType{channel.MsgTypeImage, channel.MsgTypeFile, channel.MsgTypeVideo} {
				if rest == quotedMediaPlaceholder(kind) {
					out = append(out, struct {
						offset int
						marker string
					}{pos + len(prefix), rest})
				}
			}
		}
		pos += len(line)
	}
	return out
}

// assertEachMarkerLandsOnItsOwnLine is the invariant: for every quoted
// attachment handed over, the Nth occurrence of its marker in the body — the
// place the binder will rewrite — is the marker line that stands for it, and
// there is exactly one such line per attachment.
func assertEachMarkerLandsOnItsOwnLine(t *testing.T, msg channel.InboundMessage, wantAttachments int) {
	t.Helper()
	media := quotedAttachments(msg)
	if len(media) != wantAttachments {
		t.Fatalf("quoted attachments = %d, want %d", len(media), wantAttachments)
	}
	lines := realMarkerOffsets(msg.Text)
	if len(lines) != len(media) {
		t.Fatalf("marker lines in the body = %d, quoted attachments handed over = %d — an attachment "+
			"was fetched whose marker the agent cannot see, or a marker has nothing behind it\nbody: %q",
			len(lines), len(media), msg.Text)
	}
	for i, m := range media {
		if lines[i].marker != m.InlinePlaceholder {
			t.Errorf("attachment %d stands for %q but line %d of markers is %q", i, m.InlinePlaceholder, i, lines[i].marker)
			continue
		}
		got := nthOccurrence(msg.Text, m.InlinePlaceholder, m.InlineIndex)
		if got != lines[i].offset {
			t.Errorf("attachment %d is marker %q occurrence #%d, which sits at byte %d; its own marker line is at byte %d — "+
				"the binder would rewrite the wrong text and leave this picture unnamed\nbody: %q",
				i, m.InlinePlaceholder, m.InlineIndex, got, lines[i].offset, msg.Text)
		}
	}
}

const (
	imgMarker  = "[Image: unavailable]"
	fileMarker = "[File: unavailable]"
	urlA       = "https://example.invalid/a"
	urlB       = "https://example.invalid/b"
	urlC       = "https://example.invalid/c"
)

// Review finding 1, as described: the words quoted ahead of the picture spell
// the marker out.
func TestAQuotedSentenceThatSpellsTheMarkerDoesNotStealTheIndex(t *testing.T) {
	t.Parallel()
	msg := replyTo(t, []wire{
		qWords("The UI displays " + imgMarker),
		qImage(urlA, "A"),
	}, nil)

	media := quotedAttachments(msg)
	if len(media) != 1 {
		t.Fatalf("quoted attachments = %d, want 1", len(media))
	}
	if media[0].InlineIndex != 1 {
		t.Errorf("InlineIndex = %d, want 1 — the sentence is occurrence 0 and the picture's own marker is 1; "+
			"numbering it 0 makes the binder rewrite the sentence", media[0].InlineIndex)
	}
	assertEachMarkerLandsOnItsOwnLine(t, msg, 1)
}

// The index is a position in the rendered text, so it has to hold for every
// arrangement of words and markers around it.
func TestQuotedMarkerIndexFollowsTheRenderedText(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("字", 600)
	cases := []struct {
		name    string
		quoted  []wire
		own     []wire
		indices []int // InlineIndex of each quoted attachment, in order
	}{
		{
			name:    "a picture alone",
			quoted:  []wire{qImage(urlA, "A")},
			indices: []int{0},
		},
		{
			name:    "words that do not mention the marker",
			quoted:  []wire{qWords("看这个"), qImage(urlA, "A")},
			indices: []int{0},
		},
		{
			name:    "the literal is spelled twice before the picture",
			quoted:  []wire{qWords(imgMarker + " and again " + imgMarker), qImage(urlA, "A")},
			indices: []int{2},
		},
		{
			name:    "the literal comes after the real marker",
			quoted:  []wire{qImage(urlA, "A"), qWords("shown as " + imgMarker)},
			indices: []int{0},
		},
		{
			name: "literals between and around two pictures",
			quoted: []wire{
				qWords("see " + imgMarker), qImage(urlA, "A"), qWords("see " + imgMarker), qImage(urlB, "B"), qWords("see " + imgMarker),
			},
			indices: []int{1, 3},
		},
		{
			name:    "a literal of one kind moves only that kind's index",
			quoted:  []wire{qWords("see " + fileMarker), qImage(urlA, "A"), qFile(urlB, "B")},
			indices: []int{0, 1},
		},
		{
			name:    "the sender's own words spell the marker",
			quoted:  []wire{qImage(urlA, "A")},
			own:     []wire{qWords("it says " + imgMarker), qImage(urlC, "C")},
			indices: []int{0},
		},
		{
			// 490 + the first 10 runes of the literal survive the cut; ten runes
			// of a marker are not an occurrence of it.
			name:    "the cut splits a literal in half",
			quoted:  []wire{qWords(strings.Repeat("字", 490) + imgMarker), qImage(urlA, "A")},
			indices: []int{0},
		},
		{
			name:    "a literal past the cut is gone from the body and is not counted",
			quoted:  []wire{qWords(long + imgMarker), qImage(urlA, "A")},
			indices: []int{0},
		},
		{
			name:    "a literal inside the kept part is counted",
			quoted:  []wire{qWords(imgMarker + strings.Repeat("字", 600)), qImage(urlA, "A")},
			indices: []int{1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := replyTo(t, tc.quoted, tc.own)
			media := quotedAttachments(msg)
			if len(media) != len(tc.indices) {
				t.Fatalf("quoted attachments = %d, want %d", len(media), len(tc.indices))
			}
			for i, want := range tc.indices {
				if media[i].InlineIndex != want {
					t.Errorf("attachment %d: InlineIndex = %d, want %d\nbody: %q", i, media[i].InlineIndex, want, msg.Text)
				}
			}
			assertEachMarkerLandsOnItsOwnLine(t, msg, len(tc.indices))
		})
	}
}

// Review finding 2, as described: 501 characters, then the picture, and a new
// picture sent with the reply.
func TestACutQuoteStillNamesItsPicture(t *testing.T) {
	t.Parallel()
	msg := replyTo(t,
		[]wire{qWords(strings.Repeat("字", 501)), qImage(urlA, "Q")},
		[]wire{qImage(urlC, "O"), qWords("还有这个")},
	)

	if !strings.Contains(msg.Text, "> "+imgMarker) {
		t.Fatalf("the quoted picture's marker is not in the body:\n%q\n— it was fetched and the agent has "+
			"nothing that says which attachment it is, next to the one just sent", msg.Text)
	}
	if !strings.Contains(msg.Text, strings.Repeat("字", 500)+"…") {
		t.Errorf("the words were not cut at %d runes and marked", maxQuotedRunes)
	}
	if strings.Contains(msg.Text, strings.Repeat("字", 501)) {
		t.Errorf("the words were not cut")
	}
	assertEachMarkerLandsOnItsOwnLine(t, msg, 1)

	all := wecomMedia(msg)
	if len(all) != 2 || all[0].AESKey != "Q" || all[1].AESKey != "O" {
		t.Fatalf("media = %+v, want the quoted picture then the sender's own", all)
	}
	if all[1].InlinePlaceholder != "" {
		t.Errorf("the sender's own picture was stamped %q", all[1].InlinePlaceholder)
	}
}

// Where the cut falls relative to the markers.
func TestTheCutNeverRemovesOrSplitsAMarker(t *testing.T) {
	t.Parallel()
	words := func(n int) wire { return qWords(strings.Repeat("字", n)) }
	cases := []struct {
		name     string
		quoted   []wire
		wantCut  bool // is the block marked with an ellipsis
		wantKept int  // quoted attachments that must survive
	}{
		{"exactly the budget, then a picture", []wire{words(500), qImage(urlA, "A")}, false, 1},
		{"one under the budget, then a picture", []wire{words(499), qImage(urlA, "A")}, false, 1},
		{"one over the budget, then a picture", []wire{words(501), qImage(urlA, "A")}, true, 1},
		{"the budget is spent, more words, then a picture", []wire{words(500), qWords("更多"), qImage(urlA, "A")}, true, 1},
		{"a picture first, then a long document", []wire{qImage(urlA, "A"), words(900)}, true, 1},
		{"the marker starts inside the budget and ends past it", []wire{words(485), qImage(urlA, "A"), words(50)}, true, 1},
		{
			"pictures between long passages",
			[]wire{words(300), qImage(urlA, "A"), words(300), qImage(urlB, "B"), words(300), qImage(urlC, "C")},
			true, 3,
		},
		{"a picture and a file", []wire{words(700), qImage(urlA, "A"), qFile(urlB, "B")}, true, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := replyTo(t, tc.quoted, nil)
			assertEachMarkerLandsOnItsOwnLine(t, msg, tc.wantKept)

			block := strings.SplitN(msg.Text, "\n\n", 2)[0]
			if got := strings.Contains(block, "…"); got != tc.wantCut {
				t.Errorf("block marked as cut = %v, want %v\n%q", got, tc.wantCut, block)
			}
			// The size bound the code comment promises: the budget, plus the
			// markers (each on a line of its own), plus the ellipsis, plus the
			// block's own prefix. The prefix is "> [Quote] " on the first line
			// and "> " on each later one.
			markers := tc.wantKept
			limit := maxQuotedRunes + markers*(utf8.RuneCountInString(imgMarker)+1) + 1
			limit += utf8.RuneCountInString("> "+quotePrefix+" ") + 2*markers
			if got := utf8.RuneCountInString(block); got > limit {
				t.Errorf("block = %d runes, want at most %d (budget %d + %d markers)", got, limit, maxQuotedRunes, markers)
			}
		})
	}
}

// A quote with no attachment is cut exactly as before: the budget, the
// ellipsis, nothing else. Pinned beside the new cases because the cut was
// rewritten around the markers.
func TestACutQuoteWithoutPicturesIsCutAsItAlwaysWas(t *testing.T) {
	t.Parallel()
	msg := replyTo(t, []wire{qWords(strings.Repeat("字", 501))}, nil)
	want := "> [Quote] " + strings.Repeat("字", 500) + "…\n\n这个怎么处理"
	if msg.Text != want {
		t.Errorf("body = %q, want %q", msg.Text, want)
	}

	// Two runs, the second crossing the budget: the cut lands inside it.
	msg = replyTo(t, []wire{qWords(strings.Repeat("甲", 300)), qWords(strings.Repeat("乙", 300))}, nil)
	want = "> [Quote] " + strings.Repeat("甲", 300) + "\n> " + strings.Repeat("乙", 199) + "…\n\n这个怎么处理"
	if msg.Text != want {
		t.Errorf("body = %q, want %q", msg.Text, want)
	}
}
