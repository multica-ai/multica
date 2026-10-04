package wecom

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// A quoted picture used to reach the agent as four characters. The common
// shape of this in a room is somebody quoting a screenshot of an error and
// typing "这个怎么处理" underneath — the agent got the words and a label where
// the error was, and answered the label.
//
// The callback carries the quoted attachment the same way it carries one the
// sender just made: a pre-signed url and its aeskey.
func TestAQuotedPictureIsFetched(t *testing.T) {
	t.Parallel()
	var q quotedMessage
	q.MsgType = "image"
	q.Image = mediaBody{URL: "https://example.invalid/quoted.png", AESKey: "K1"}
	mc := aibotMsgCallback{MsgType: "text", Quote: q}
	mc.Text.Content = "这个怎么处理"

	got := mc.attachments()
	if len(got) != 1 {
		t.Fatalf("attachments = %d, want 1 — the quoted picture was not fetched, so the agent "+
			"answers a label instead of the screenshot", len(got))
	}
	if got[0].URL != "https://example.invalid/quoted.png" || got[0].AESKey != "K1" {
		t.Errorf("attachment = %+v, want the quote's own url and key", got[0])
	}
	if got[0].InlinePlaceholder != "[Image: unavailable]" || got[0].InlineIndex != 0 {
		t.Errorf("marker = %q#%d, want \"[Image: unavailable]\"#0 — without it nothing joins the "+
			"marker in the body to the entry in the attachment list",
			got[0].InlinePlaceholder, got[0].InlineIndex)
	}
}

// The quote block renders above the sender's own words, so its attachments
// come first. The engine binds a marker by its occurrence number in the body;
// the two lists disagreeing puts the wrong id on the wrong picture.
func TestTheQuotedAttachmentComesBeforeTheSendersOwn(t *testing.T) {
	t.Parallel()
	var q quotedMessage
	q.MsgType = "image"
	q.Image = mediaBody{URL: "https://example.invalid/quoted.png", AESKey: "Q"}
	mc := aibotMsgCallback{MsgType: "image", Quote: q}
	mc.Image = mediaBody{URL: "https://example.invalid/own.png", AESKey: "O"}

	got := mc.attachments()
	if len(got) != 2 {
		t.Fatalf("attachments = %d, want 2 (the quoted one and the sender's own)", len(got))
	}
	if got[0].AESKey != "Q" || got[1].AESKey != "O" {
		t.Fatalf("order = %q,%q, want the quoted one first — it renders first",
			got[0].AESKey, got[1].AESKey)
	}
	if got[1].InlinePlaceholder != "" {
		t.Errorf("the sender's own attachment was stamped %q; its marker is already unambiguous "+
			"and rewriting it would change what every stored wecom body says", got[1].InlinePlaceholder)
	}
}

// Counted per marker, not over the whole list: "[Image: unavailable]" and
// "[File: unavailable]" are different strings, so a quote carrying one of each
// gives each its own first occurrence.
func TestTwoKindsInOneQuoteEachCountFromZero(t *testing.T) {
	t.Parallel()
	var q quotedMessage
	q.MsgType = "mixed"
	q.Mixed.MsgItem = []mixedItem{
		{MsgType: "image", Image: mediaBody{URL: "https://example.invalid/a.png", AESKey: "A"}},
		{MsgType: "file", File: mediaBody{URL: "https://example.invalid/b.pdf", AESKey: "B"}},
		{MsgType: "image", Image: mediaBody{URL: "https://example.invalid/c.png", AESKey: "C"}},
	}

	got := quotedMessage(q).media()
	if len(got) != 3 {
		t.Fatalf("media = %d, want 3", len(got))
	}
	want := []struct {
		marker string
		index  int
	}{{"[Image: unavailable]", 0}, {"[File: unavailable]", 0}, {"[Image: unavailable]", 1}}
	for i, w := range want {
		if got[i].InlinePlaceholder != w.marker || got[i].InlineIndex != w.index {
			t.Errorf("media[%d] = %q#%d, want %q#%d — counting the markers together sends the "+
				"binder looking for an occurrence that is not there",
				i, got[i].InlinePlaceholder, got[i].InlineIndex, w.marker, w.index)
		}
	}
}

// The marker REFERS to the attachment rather than carrying it: the picture
// belongs to a message somebody else sent, so replacing the marker with an
// inline image would state this sender attached it again.
func TestAQuotedAttachmentAsksToBeNamedRatherThanEmbedded(t *testing.T) {
	t.Parallel()
	quoted := InboundMedia{Kind: channel.MsgTypeImage, InlinePlaceholder: "[Image: unavailable]", InlineIndex: 2}
	ref := quoted.inline(channel.MediaRef{Type: channel.MsgTypeImage})
	if !ref.InlineIDOnly {
		t.Error("InlineIDOnly is false — the quote's marker would be replaced by an inline image, " +
			"stating this sender attached somebody else's picture")
	}
	if ref.InlinePlaceholder != "[Image: unavailable]" || ref.InlineIndex != 2 {
		t.Errorf("ref = %q#%d, want the marker carried across", ref.InlinePlaceholder, ref.InlineIndex)
	}

	own := InboundMedia{Kind: channel.MsgTypeImage}
	if got := own.inline(channel.MediaRef{Type: channel.MsgTypeImage}); got.InlineIDOnly || got.InlinePlaceholder != "" {
		t.Errorf("the sender's own ref was changed to %+v; it must stay on the path it was on", got)
	}
}

// The two spellings are derived from one another rather than written twice, so
// they cannot drift: the label ahead of the colon is whatever this adapter
// called the thing, and the engine only replaces the state word after it.
func TestTheQuotedMarkerIsTheOrdinaryOneWithRoomInIt(t *testing.T) {
	t.Parallel()
	for _, kind := range []channel.MsgType{channel.MsgTypeImage, channel.MsgTypeVideo, channel.MsgTypeFile} {
		bare := mediaPlaceholder(kind)
		named := quotedMediaPlaceholder(kind)
		want := bare[:len(bare)-1] + ": " + mediaUnavailable + "]"
		if named != want {
			t.Errorf("quotedMediaPlaceholder(%v) = %q, want %q", kind, named, want)
		}
	}
}
