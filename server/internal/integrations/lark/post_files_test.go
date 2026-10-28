package lark

import (
	"context"
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

const postDOCX = `{"title":"Review","content":[[{"tag":"text","text":"Please read"}]],"files":[{"file_key":"file_docx","file_name":"demo.docx","is_folder":false}]}`

func TestPostFilesCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, raw, text string
		resources       int
	}{
		{"top-level DOCX", postDOCX, "Review\nPlease read\n[File: demo.docx]", 1},
		{"files only", `{"files":[{"file_key":"f"}]}`, "[File]", 1},
		{"folders and missing keys", `{"files":[{"file_key":"folder","is_folder":true},{"file_name":"missing.docx"}]}`, "", 0},
		{"repeated files", `{"files":[{"file_key":"f","name":"demo.docx"},{"file_key":"f","name":"demo.docx"}]}`, "[File: demo.docx]", 1},
		{"span and file", `{"content":[[{"tag":"file","file_key":"f","file_name":"demo.docx"}]],"files":[{"file_key":"f","file_name":"demo.docx"}]}`, "[File: demo.docx]", 1},
		{"file metadata fills span", `{"content":[[{"tag":"file","file_key":"f"}]],"files":[{"file_key":"f","file_name":"demo.docx"}]}`, "[File: demo.docx]", 1},
		{"video span and file", `{"content":[[{"tag":"media","file_key":"f"}]],"files":[{"file_key":"f"}]}`, "[Video]", 1},
		{"legacy wins", `{"content":[[{"tag":"text","text":"legacy"}]],"content_v2":[[{"tag":"text","text":"new"}]]}`, "legacy", 0},
		{"v2 array", `{"content":[],"content_v2":[[{"tag":"text","text":"new"},{"tag":"file","file_key":"f"}]]}`, "new [File]", 1},
		{"null legacy fallback", `{"content":null,"content_v2":[[{"tag":"text","text":"new"}]],"files":[{"file_key":"f"}]}`, "new\n[File]", 1},
		{"image and file namespaces", `{"content":[[{"tag":"img","image_key":"same"}]],"files":[{"file_key":"same"}]}`, "[Image]\n[File]", 2},
		{"v2 encoded", `{"content_v2":"[[{\"tag\":\"text\",\"text\":\"new\"},{\"tag\":\"img\",\"image_key\":\"i\"}]]"}`, "new [Image]", 1},
		{"invalid legacy fallback", `{"content":{},"content_v2":[[{"tag":"text","text":"new"}]]}`, "new", 0},
		{"invalid v2 preserves files", `{"content_v2":"invalid","files":[{"file_key":"f"}]}`, "[File]", 1},
		{"malformed JSON", `{`, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := flattenPostContent(tc.raw); got != tc.text {
				t.Fatalf("text = %q, want %q", got, tc.text)
			}
			lm := InboundMessage{MessageID: "om_post", MessageType: "post", Content: tc.raw}
			if got := mediaResourcesFromMessage(lm); len(got) != tc.resources {
				t.Fatalf("resources = %+v, want %d", got, tc.resources)
			}
			resolver := NewFeishuMediaResolver(nil, nil, nil, nil, nil)
			if got := resolver.HasMedia(channelMessageFromLark(lm)); got != (tc.resources > 0) {
				t.Fatalf("HasMedia = %v", got)
			}
		})
	}
}

func TestPostDOCXResolution(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "download failure"
		}
		t.Run(name, func(t *testing.T) {
			sender := &fakeSender{downloaded: DownloadedResource{Data: []byte("synthetic document"), ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", SizeBytes: 18}}
			if fail {
				sender.downloadErr = errors.New("unavailable")
			}
			storage := &fakeMediaStorage{}
			resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, &fakeMediaLedger{}, newDiscardLogger())
			lm := InboundMessage{MessageID: "om_docx", MessageType: "post", Content: postDOCX, Body: flattenPostContent(postDOCX)}
			got := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
				uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), channelMessageFromLark(lm))
			if len(sender.downloadCalls) != 1 {
				t.Fatalf("downloads = %d", len(sender.downloadCalls))
			}
			call := sender.downloadCalls[0]
			if call.MessageID != "om_docx" || call.FileKey != "file_docx" || call.Type != "file" {
				t.Fatalf("download params = %+v", call)
			}
			if got.Text != "Review\nPlease read\n[File: demo.docx]" {
				t.Fatalf("text = %q", got.Text)
			}
			if fail {
				if len(got.MediaRefs) != 0 || len(storage.uploads) != 0 {
					t.Fatalf("failed download created attachments: %+v", got)
				}
				return
			}
			if len(got.MediaRefs) != 1 || len(storage.uploads) != 1 {
				t.Fatalf("refs = %+v, uploads = %d", got.MediaRefs, len(storage.uploads))
			}
			if string(storage.uploads[0].data) != "synthetic document" {
				t.Fatalf("uploaded bytes = %q", storage.uploads[0].data)
			}
			ref := got.MediaRefs[0]
			if ref.Filename != "demo.docx" || ref.Type != channel.MsgTypeFile || ref.MimeType != sender.downloaded.ContentType {
				t.Fatalf("ref = %+v", ref)
			}
		})
	}
}
