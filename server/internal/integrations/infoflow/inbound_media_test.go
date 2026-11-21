package infoflow

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// A media-only message must reach the core. Before inbound media landed, the
// adapter dropped it, so a user who sent a screenshot to the bot got silence.

func TestInboundFromEvent_PrivateImageIsIngested(t *testing.T) {
	ev := eventFromPayload(t, `{
		"MsgType":"image",
		"MsgId":"img-1",
		"FromUserId":"user-1",
		"image":{"fid":"fid-abc","filename":"shot.png","mime_type":"image/png","size":2048}
	}`)
	msg, ok := inboundFromEvent("appkey-A", ev)
	if !ok {
		t.Fatal("expected a private image message to be ingested, got a drop")
	}
	if msg.Type != channel.MsgTypeImage {
		t.Errorf("Type = %q, want image", msg.Type)
	}
	if msg.Text != mediaImagePlaceholder {
		t.Errorf("Text = %q, want the placeholder %q", msg.Text, mediaImagePlaceholder)
	}
	raw, err := decodeRawEvent(msg)
	if err != nil {
		t.Fatalf("decodeRawEvent: %v", err)
	}
	if len(raw.Media) != 1 {
		t.Fatalf("Media = %+v, want one descriptor", raw.Media)
	}
	got := raw.Media[0]
	if got.Kind != channel.MsgTypeImage || got.FID != "fid-abc" || got.Filename != "shot.png" {
		t.Errorf("Media[0] = %+v", got)
	}
	if got.SizeBytes != 2048 || got.MimeType != "image/png" {
		t.Errorf("Media[0] metadata = %+v", got)
	}
}

func TestInboundFromEvent_GroupMixedMessageCarriesTextAndMedia(t *testing.T) {
	// An 图文混排: one message, several body parts. The text parts must still
	// reach the prompt and each media part must get its own descriptor.
	ev := eventFromPayload(t, `{
		"groupid": 456,
		"message": {
			"header": {"msgtype": "mixed", "messageid": "gmix-1", "fromuserid": "member-9"},
			"body": [
				{"type": "TEXT", "content": "这个报错怎么解决"},
				{"type": "IMAGE", "content": "fid-img-1", "filename": "a.png"},
				{"type": "FILE", "content": "fid-doc-1", "filename": "trace.log"}
			]
		}
	}`)
	msg, ok := inboundFromEvent("appkey-B", ev)
	if !ok {
		t.Fatal("expected a group mixed message to be ingested, got a drop")
	}
	if !strings.Contains(msg.Text, "这个报错怎么解决") {
		t.Errorf("Text lost the message body: %q", msg.Text)
	}
	if !strings.Contains(msg.Text, mediaImagePlaceholder) {
		t.Errorf("Text missing the image placeholder: %q", msg.Text)
	}
	if !strings.Contains(msg.Text, "[File: trace.log]") {
		t.Errorf("Text missing the file placeholder: %q", msg.Text)
	}
	raw, err := decodeRawEvent(msg)
	if err != nil {
		t.Fatalf("decodeRawEvent: %v", err)
	}
	if len(raw.Media) != 2 {
		t.Fatalf("Media = %+v, want two descriptors", raw.Media)
	}
	if raw.Media[0].Kind != channel.MsgTypeImage || raw.Media[0].Ref != "fid-img-1" {
		t.Errorf("Media[0] = %+v", raw.Media[0])
	}
	if raw.Media[1].Kind != channel.MsgTypeFile || raw.Media[1].Ref != "fid-doc-1" {
		t.Errorf("Media[1] = %+v", raw.Media[1])
	}
}

func TestInboundFromEvent_ObjectKeyOnlyImageIsStillIngested(t *testing.T) {
	// The outbound image API speaks object keys, and a payload may carry one
	// instead of an FID. It must not be dropped — it becomes a descriptor the
	// resolver can turn into a URL.
	ev := eventFromPayload(t, `{
		"MsgType":"image",
		"MsgId":"img-key",
		"FromUserId":"user-2",
		"image":{"object_key":"bucket/robot/inbound/9f2c.png"}
	}`)
	msg, ok := inboundFromEvent("appkey-A", ev)
	if !ok {
		t.Fatal("expected an object-key image to be ingested, got a drop")
	}
	raw, err := decodeRawEvent(msg)
	if err != nil {
		t.Fatalf("decodeRawEvent: %v", err)
	}
	if len(raw.Media) != 1 || raw.Media[0].ObjectKey == "" {
		t.Fatalf("Media = %+v, want one object-key descriptor", raw.Media)
	}
	if raw.Media[0].FID != "" {
		t.Errorf("FID = %q, want empty", raw.Media[0].FID)
	}
}

func TestInboundFromEvent_DropsMediaWithoutAReference(t *testing.T) {
	// A part that names neither an FID nor an object key is nothing the
	// resolver can fetch. It must not reach the core as a ref that can only fail.
	ev := eventFromPayload(t, `{
		"MsgType":"image",
		"MsgId":"img-empty",
		"FromUserId":"user-3",
		"image":{"width":100,"height":100}
	}`)
	if _, ok := inboundFromEvent("appkey-A", ev); ok {
		t.Fatal("expected a media message with no resolvable reference to be dropped")
	}
}

func TestInboundFromEvent_PlaceholdersNameTheFile(t *testing.T) {
	// The placeholder is what the agent reads when the bytes cannot be bound, so
	// it has to name the file: "[File]" says nothing, "[File: trace.log]"
	// usually says everything.
	cases := []struct {
		name string
		desc mediaDescriptor
		want string
	}{
		{"image", mediaDescriptor{Kind: channel.MsgTypeImage}, mediaImagePlaceholder},
		{"file named", mediaDescriptor{Kind: channel.MsgTypeFile, FID: "f", Filename: "spec.pdf"}, "[File: spec.pdf]"},
		{"file unnamed", mediaDescriptor{Kind: channel.MsgTypeFile, FID: "f"}, mediaFilePlaceholder},
		{"audio", mediaDescriptor{Kind: channel.MsgTypeAudio, FID: "f"}, "[Audio]"},
		{"video named", mediaDescriptor{Kind: channel.MsgTypeVideo, FID: "f", Filename: "demo.mp4"}, "[Video: demo.mp4]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mediaPlaceholderLine(tc.desc); got != tc.want {
				t.Errorf("mediaPlaceholderLine(%+v) = %q, want %q", tc.desc, got, tc.want)
			}
		})
	}
}

func TestMediaKindForType_IgnoresTextParts(t *testing.T) {
	for _, part := range []string{"TEXT", "MD", "RICHTEXT", "LINK"} {
		if got := mediaKindForType(part); got != channel.MsgTypeUnknown {
			t.Errorf("mediaKindForType(%q) = %q, want unknown", part, got)
		}
	}
	if got := mediaKindForType("IMAGE"); got != channel.MsgTypeImage {
		t.Errorf("mediaKindForType(IMAGE) = %q, want image", got)
	}
	if got := mediaKindForType("file"); got != channel.MsgTypeFile {
		t.Errorf("mediaKindForType(file) = %q, want file", got)
	}
}

func TestMediaDescriptorFromMap_AcceptsPlatformCasing(t *testing.T) {
	// The SDK classifies by MsgType but does not pin the body shape, and both
	// transports carry the provider's own casing.
	payload := payloadFromJSON(t, `{"FID":"f1","FileName":"b.txt","FileSize":12,"ContentType":"text/plain"}`)
	got := mediaDescriptorFromMap(payload, channel.MsgTypeFile)
	if got.FID != "f1" || got.Filename != "b.txt" || got.SizeBytes != 12 || got.MimeType != "text/plain" {
		t.Errorf("mediaDescriptorFromMap = %+v", got)
	}
	if !got.fetchable() {
		t.Error("fetchable() = false for a descriptor with an FID")
	}
}
