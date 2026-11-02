package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func TestContextMediaCollectQuotedAndForwardedImages(t *testing.T) {
	for _, kind := range []string{"quote", "forward", "quoted-forward"} {
		t.Run(kind, func(t *testing.T) {
			fake := newEnricherFake()
			image := LarkMessage{MessageID: "om_picture", MessageType: "post", SenderID: "ou_other", SenderType: "user", CreateTime: "1000",
				Content: `{"content":[[{"tag":"text","text":"before"},{"tag":"img","image_key":"img_context"},{"tag":"text","text":"after"},{"tag":"img","image_key":"img_context"}]]}`}
			in := InboundMessage{MessageID: "om_trigger", MessageType: "text", Body: "inspect", CommandBody: "inspect", ChatID: "oc_group", ChatType: ChatTypeGroup, AddressedToBot: true, CreateTime: "2000"}
			cfg := InboundEnricherConfig{}
			owner := image.MessageID
			switch kind {
			case "quote":
				in.ParentID = image.MessageID
				fake.byID[in.ParentID] = []LarkMessage{image}
			case "forward", "quoted-forward":
				owner = "om_forward"
				fake.byID[owner] = []LarkMessage{{MessageID: owner, MessageType: "merge_forward"}, image}
				if kind == "forward" {
					in.MessageID = owner
					in.MessageType = "merge_forward"
				} else {
					in.ParentID = owner
				}
			}
			out := enrich(t, fake, in, cfg)
			if len(out.ContextMedia) != 1 || out.ContextMedia[0].Occurrences != 2 || out.ContextMedia[0].MessageID != owner {
				t.Fatalf("context media = %+v, want one repeated image owned by %s", out.ContextMedia, owner)
			}
			marker := out.ContextMedia[0].Placeholder
			if !strings.Contains(out.Body, "before "+marker+" after "+marker) || out.CommandBody != "inspect" {
				t.Fatalf("image positions or command changed: %+v", out)
			}
			api := &fakeSender{downloaded: DownloadedResource{Data: []byte("image bytes"), ContentType: "image/png", Filename: "context.png"}}
			storage := &fakeMediaStorage{}
			resolver := NewFeishuMediaResolver(api, fakeCreds{secret: "plain"}, storage, &fakeMediaLedger{}, newDiscardLogger())
			msg := channelMessageFromLark(out)
			if !resolver.HasMedia(msg) {
				t.Fatal("context-only image did not suspend dispatch for ingestion")
			}
			resolved := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
				uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), msg)
			if len(api.downloadCalls) != 1 || api.downloadCalls[0].MessageID != owner || api.downloadCalls[0].FileKey != "img_context" ||
				len(storage.uploads) != 1 || len(resolved.MediaRefs) != 1 {
				t.Fatalf("context image not downloaded exactly once: calls=%+v refs=%+v", api.downloadCalls, resolved.MediaRefs)
			}
			ref := resolved.MediaRefs[0]
			if ref.InlinePlaceholder != marker || ref.InlineCount != 2 || !ref.InlineIDOnly {
				t.Fatalf("missing source markers: %+v", ref)
			}
		})
	}
}

func TestContextMediaSkipsTruncatedForwardChildren(t *testing.T) {
	fake := newEnricherFake()
	image := func(id, at string) LarkMessage {
		return LarkMessage{MessageID: id, MessageType: "image", Content: `{"image_key":"img_` + id + `"}`, CreateTime: at}
	}
	fake.byID["om_forward"] = []LarkMessage{{MessageID: "om_forward", MessageType: "merge_forward"}, image("child", "100"), image("truncated", "200")}
	in := InboundMessage{MessageID: "trigger", MessageType: "text", Body: "inspect", ParentID: "om_forward"}
	out := enrich(t, fake, in, InboundEnricherConfig{MaxForwardChildren: 1})
	if len(out.ContextMedia) != 1 || out.ContextMedia[0].MessageID != "om_forward" || out.ContextMedia[0].ResourceKey != "img_child" {
		t.Fatalf("unexpected images downloaded beyond selected context: %+v", out.ContextMedia)
	}
	if !strings.Contains(out.Body, "1 more truncated") {
		t.Fatalf("forward cap changed: %s", out.Body)
	}
}

func TestContextMediaFailedQuoteFetchDoesNotCollectResources(t *testing.T) {
	fake := newEnricherFake()
	fake.errByID["parent"] = errors.New("denied")
	out := enrich(t, fake, InboundMessage{MessageType: "text", Body: "inspect", ParentID: "parent"}, InboundEnricherConfig{})
	if len(out.ContextMedia) != 0 || !strings.Contains(out.Body, "unable to fetch") {
		t.Fatalf("failed fetch: %+v", out)
	}
}

func TestContextMediaOneFailedDownloadKeepsOtherImageAndSourcePositions(t *testing.T) {
	fake := newEnricherFake()
	fake.byID["parent"] = []LarkMessage{{MessageID: "parent", MessageType: "post", Content: `{"content":[[{"tag":"img","image_key":"bad"},{"tag":"text","text":"between"},{"tag":"img","image_key":"good"}]]}`}}
	out := enrich(t, fake, InboundMessage{MessageID: "trigger", MessageType: "text", Body: "inspect", ParentID: "parent"}, InboundEnricherConfig{})
	api := &fakeSender{downloadErrByKey: map[string]error{"bad": errors.New("denied")}, downloaded: DownloadedResource{Data: []byte("good image"), ContentType: "image/png"}}
	resolver := NewFeishuMediaResolver(api, fakeCreds{secret: "plain"}, &fakeMediaStorage{}, &fakeMediaLedger{}, newDiscardLogger())
	got := resolver.ResolveMedia(t.Context(), testMediaInstallation(t), engine.ResolvedIdentity{}, uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), channelMessageFromLark(out))
	if len(got.MediaRefs) != 1 || got.MediaRefs[0].InlinePlaceholder != out.ContextMedia[1].Placeholder || got.Text != out.Body || !strings.Contains(got.Text, "unavailable") {
		t.Fatalf("failed image shifted the successful picture: %+v", got)
	}
}

func TestContextMediaCollectFilesAudioAndVideo(t *testing.T) {
	for _, source := range []string{"quote", "forward", "quoted-forward"} {
		for _, messageType := range []string{"file", "audio", "media", "video"} {
			t.Run(source+"/"+messageType, func(t *testing.T) {
				fake := newEnricherFake()
				content := `{"file_key":"file_context","file_name":"source.bin","mime_type":"application/octet-stream","size_bytes":7}`
				parent := LarkMessage{MessageID: "om_resource", MessageType: messageType, Content: content, CreateTime: "1000"}
				in := InboundMessage{MessageID: "trigger", MessageType: "text", Body: "inspect", CommandBody: "inspect", ChatID: "group", ChatType: ChatTypeGroup, AddressedToBot: true, CreateTime: "2000"}
				cfg := InboundEnricherConfig{}
				owner := parent.MessageID
				switch source {
				case "quote":
					in.ParentID = owner
					fake.byID[owner] = []LarkMessage{parent}
				default:
					owner = "outer_forward"
					fake.byID[owner] = []LarkMessage{{MessageID: owner, MessageType: "merge_forward"}, parent}
					if source == "forward" {
						in.MessageID, in.MessageType = owner, "merge_forward"
					} else {
						in.ParentID = owner
					}
				}
				out := enrich(t, fake, in, cfg)
				if len(out.ContextMedia) != 1 {
					t.Fatalf("selected context media = %+v", out.ContextMedia)
				}
				item := out.ContextMedia[0]
				kind := channel.MsgTypeFile
				if messageType == "audio" {
					kind = channel.MsgTypeAudio
				} else if messageType != "file" {
					kind = channel.MsgTypeVideo
				}
				if item.MessageID != owner || item.Type != kind || item.FetchType != "file" || item.Filename != "source.bin" || item.MimeType != "application/octet-stream" || out.CommandBody != "inspect" {
					t.Fatalf("media metadata or command lost: %+v", out)
				}
				if !strings.Contains(out.Body, item.Placeholder+" (source.bin)") {
					t.Fatalf("source position/name lost: %s", out.Body)
				}
				data := []byte{0, 1, 2, 3, 4, 5, 255}
				api := &fakeSender{downloaded: DownloadedResource{Data: data}}
				storage := &fakeMediaStorage{}
				ledger := &fakeMediaLedger{}
				got := resolveContextMedia(t, out, api, storage, ledger)
				if len(api.downloadCalls) != 1 || api.downloadCalls[0] != (DownloadResourceParams{MessageID: owner, FileKey: "file_context", Type: "file"}) || len(storage.uploads) != 1 || len(ledger.records) != 1 || len(got.MediaRefs) != 1 {
					t.Fatalf("resource not ingested once: calls=%+v uploads=%+v refs=%+v", api.downloadCalls, storage.uploads, got.MediaRefs)
				}
				ref := got.MediaRefs[0]
				wantMIME := "application/octet-stream"
				if messageType == "audio" {
					wantMIME = "audio/opus" // Existing Feishu audio fallback for a generic response.
				}
				if !bytes.Equal(storage.uploads[0].data, data) || ref.Type != kind || ref.Filename != "source.bin" || ref.MimeType != wantMIME || ref.SizeBytes != 7 || ref.InlinePlaceholder != item.Placeholder || !ref.InlineIDOnly || ref.InlineCount != 1 {
					t.Fatalf("attachment bytes, metadata or source marker changed: %+v", ref)
				}
			})
		}
	}
}

func resolveContextMedia(t *testing.T, out InboundMessage, api *fakeSender, storage *fakeMediaStorage, ledger *fakeMediaLedger) channel.InboundMessage {
	t.Helper()
	resolver := NewFeishuMediaResolver(api, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
	msg := channelMessageFromLark(out)
	if !resolver.HasMedia(msg) {
		t.Fatal("context-only media did not suspend dispatch")
	}
	return resolver.ResolveMedia(t.Context(), testMediaInstallation(t), engine.ResolvedIdentity{}, uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), msg)
}

func TestContextMediaMixedPostDeduplicatesRepeatedSpans(t *testing.T) {
	for _, paragraphs := range []string{"content", "content_v2"} {
		t.Run(paragraphs, func(t *testing.T) {
			fake := newEnricherFake()
			parent := LarkMessage{MessageID: "parent", MessageType: "post", CreateTime: "1000", Content: `{"` + paragraphs + `":[[{"tag":"text","text":"before"},{"tag":"file","file_key":"document","file_name":"notes.txt","mime_type":"text/plain"},{"tag":"text","text":"between"},{"tag":"media","file_key":"video","file_name":"clip.mp4"},{"tag":"img","image_key":"image"},{"tag":"file","file_key":"document"},{"tag":"file","file_key":"folder","is_folder":true}]],"files":[{"file_key":"document","file_name":"notes.txt"},{"file_key":"extra","file_name":"extra.pdf"},{"file_key":"ignored_folder","is_folder":true}]}`}
			fake.byChat["group"] = []LarkMessage{parent}
			fake.byID["parent"] = []LarkMessage{parent}
			out := enrich(t, fake, InboundMessage{MessageID: "trigger", MessageType: "text", Body: "inspect", ParentID: "parent", ChatID: "group", ChatType: ChatTypeGroup, AddressedToBot: true, CreateTime: "2000"}, InboundEnricherConfig{RecentContextSize: 5})
			if len(out.ContextMedia) != 4 {
				t.Fatalf("mixed post resources: %+v", out.ContextMedia)
			}
			if out.ContextMedia[0].Occurrences != 2 || out.ContextMedia[1].Occurrences != 1 || out.ContextMedia[2].Occurrences != 1 || out.ContextMedia[3].Occurrences != 1 {
				t.Fatalf("repeated source occurrences lost: %+v", out.ContextMedia)
			}
			if strings.Count(out.Body, "before "+out.ContextMedia[0].Placeholder+" (notes.txt) between "+out.ContextMedia[1].Placeholder+" (clip.mp4) "+out.ContextMedia[2].Placeholder) != 1 || strings.Contains(out.Body, "ignored_folder") {
				t.Fatalf("mixed post positions lost: %s", out.Body)
			}
			api := &fakeSender{downloadedByKey: map[string]DownloadedResource{
				"document": {Data: []byte("实际文本内容\n"), ContentType: "text/plain"},
				"video":    {Data: []byte{0, 1, 255}, ContentType: "video/mp4"},
				"image":    {Data: []byte{1, 2}, ContentType: "image/png"},
				"extra":    {Data: []byte("%PDF-opaque"), ContentType: "application/pdf"},
			}}
			storage := &fakeMediaStorage{}
			got := resolveContextMedia(t, out, api, storage, &fakeMediaLedger{})
			if len(got.MediaRefs) != 4 || len(api.downloadCalls) != 4 || len(storage.uploads) != 4 {
				t.Fatalf("repeated resources downloaded more than once: %+v", api.downloadCalls)
			}
			for i, call := range api.downloadCalls {
				if call.MessageID != "parent" || !bytes.Equal(storage.uploads[i].data, api.downloadedByKey[call.FileKey].Data) || got.MediaRefs[i].InlineCount != out.ContextMedia[i].Occurrences {
					t.Fatalf("resource bytes, ownership or source count changed: %+v", call)
				}
			}
		})
	}
}

func TestContextMediaForwardPartialDownloadFailure(t *testing.T) {
	fake := newEnricherFake()
	makeFile := func(id, kind, at string) LarkMessage {
		return LarkMessage{MessageID: id, MessageType: kind, CreateTime: at, Content: `{"file_key":"` + id + `","file_name":"` + id + `.bin"}`}
	}
	fake.byID["forward"] = []LarkMessage{{MessageID: "forward", MessageType: "merge_forward"}, makeFile("bad", "file", "50"), makeFile("included", "video", "100"), makeFile("truncated", "file", "200")}
	out := enrich(t, fake, InboundMessage{MessageID: "trigger", MessageType: "text", Body: "inspect", ParentID: "forward"}, InboundEnricherConfig{MaxForwardChildren: 2})
	if len(out.ContextMedia) != 2 || out.ContextMedia[0].ResourceKey != "bad" || out.ContextMedia[1].ResourceKey != "included" || out.ContextMedia[1].MessageID != "forward" {
		t.Fatalf("media collected outside existing selection: %+v", out.ContextMedia)
	}
	api := &fakeSender{downloadErrByKey: map[string]error{"bad": errors.New("denied")}, downloaded: DownloadedResource{Data: []byte{0, 255}}}
	storage := &fakeMediaStorage{}
	got := resolveContextMedia(t, out, api, storage, &fakeMediaLedger{})
	if len(api.downloadCalls) != 2 || len(got.MediaRefs) != 1 || got.MediaRefs[0].InlinePlaceholder != out.ContextMedia[1].Placeholder || got.Text != out.Body || !strings.Contains(got.Text, out.ContextMedia[0].Placeholder+" (bad.bin)") {
		t.Fatalf("partial failure shifted successful media: %+v", got)
	}
}

func TestContextMediaResourceOwnership(t *testing.T) {
	fake := newEnricherFake()
	file := func(id string) LarkMessage {
		return LarkMessage{MessageID: id, MessageType: "file", CreateTime: "1000", Content: `{"file_key":"same_key"}`}
	}
	fake.byID["first"] = []LarkMessage{file("first")}
	out := enrich(t, fake, InboundMessage{MessageID: "trigger", ParentID: "first", MessageType: "file", Content: `{"file_key":"same_key"}`, Body: "inspect", ChatID: "group", ChatType: ChatTypeGroup, AddressedToBot: true, CreateTime: "2000"}, InboundEnricherConfig{RecentContextSize: 5})
	if len(out.ContextMedia) != 1 {
		t.Fatalf("same key from different owners collapsed: %+v", out.ContextMedia)
	}
	storage := &fakeMediaStorage{}
	got := resolveContextMedia(t, out, &fakeSender{downloaded: DownloadedResource{Data: []byte("x")}}, storage, &fakeMediaLedger{})
	if len(got.MediaRefs) != 2 || storage.uploads[0].key == storage.uploads[1].key {
		t.Fatal("different message owners shared an object")
	}
}

func TestContextMediaMissingKeysDoNotQueueDownloads(t *testing.T) {
	for _, kind := range []string{"image", "file", "audio", "media", "video", "post"} {
		t.Run(kind, func(t *testing.T) {
			fake := newEnricherFake()
			fake.byID["parent"] = []LarkMessage{{MessageID: "parent", MessageType: kind, Content: `{"content":[[{"tag":"img"},{"tag":"file"}]]}`}}
			out := enrich(t, fake, InboundMessage{MessageID: "trigger", MessageType: "text", ParentID: "parent", Body: "inspect"}, InboundEnricherConfig{})
			resolver := NewFeishuMediaResolver(nil, nil, nil, nil, newDiscardLogger())
			if len(out.ContextMedia) != 0 || resolver.HasMedia(channelMessageFromLark(out)) || !strings.Contains(out.Body, "unavailable]") {
				t.Fatalf("missing resource key was collected or hidden: %+v", out)
			}
		})
	}
}

func TestContextMediaPassesAttributionBeforeSenderLabelledIssueCommand(t *testing.T) {
	fake := newEnricherFake()
	fake.userNames = map[string]string{"ou_sender": "Reporter", "ou_source": "Alice"}
	fake.byID["parent"] = []LarkMessage{{MessageID: "parent", MessageType: "file", SenderID: "ou_source", SenderType: "user", Content: `{"file_key":"original-file","file_name":"source.txt"}`}}
	command := "/issue Investigate\nDetails"
	out := enrich(t, fake, InboundMessage{MessageID: "trigger", MessageType: "text", Body: command, CommandBody: command, SenderOpenID: "ou_sender", ParentID: "parent", ChatID: "group", ChatType: ChatTypeGroup, AddressedToBot: true}, InboundEnricherConfig{})
	if !strings.Contains(out.Body, "[Reporter]: /issue Investigate") || strings.Contains(out.MediaSourceContext, "/issue Investigate") || !strings.Contains(out.MediaSourceContext, `sender="Alice"`) {
		t.Fatalf("source context mixed with the current command: %+v", out)
	}
	api := &fakeSender{downloaded: DownloadedResource{Data: []byte("source text"), ContentType: "text/plain"}}
	resolved := resolveContextMedia(t, out, api, &fakeMediaStorage{}, &fakeMediaLedger{})
	if len(resolved.MediaRefs) != 1 || resolved.MediaRefs[0].SourceContext != out.MediaSourceContext {
		t.Fatalf("attribution lost across Raw or media resolution: %+v", resolved.MediaRefs)
	}
}

func TestContextMediaOriginalTextCollision(t *testing.T) {
	for _, kind := range []string{"image", "file", "audio", "video"} {
		for _, source := range []string{"forward", "recent-and-quote"} {
			t.Run(kind+"/"+source, func(t *testing.T) {
				label := map[string]string{"image": "Image", "file": "File", "audio": "Audio", "video": "Video"}[kind]
				literal := fmt.Sprintf("Literal [%s 1: unavailable] and [%s 2: unavailable]", label, label)
				fake := newEnricherFake()
				text := textMsg("literal", "alice", literal, "1000")
				media := LarkMessage{MessageID: "picture", SenderID: "bob", SenderType: "user", MessageType: kind,
					Content: `{"image_key":"image","file_key":"file","file_name":"source.bin"}`, CreateTime: "1500"}
				in := InboundMessage{MessageID: "trigger", MessageType: "text", Body: "inspect " + literal, CommandBody: "inspect " + literal,
					ChatID: "group", ChatType: ChatTypeGroup, AddressedToBot: true, CreateTime: "2000"}
				cfg := InboundEnricherConfig{RecentContextSize: 5}
				if source == "forward" {
					cfg.RecentContextSize = 0
					in.MessageID, in.MessageType = "forward", "merge_forward"
					fake.byID["forward"] = []LarkMessage{{MessageID: "forward", MessageType: "merge_forward"}, text, media}
				} else {
					fake.byChat[in.ChatID] = []LarkMessage{text, media}
					in.ParentID = media.MessageID
					fake.byID[in.ParentID] = []LarkMessage{media}
				}
				out := enrich(t, fake, in, cfg)
				if len(out.ContextMedia) != 1 {
					t.Fatalf("selected media changed: %+v", out.ContextMedia)
				}
				item := out.ContextMedia[0]
				wantCount := 1
				if strings.Contains(literal, item.Placeholder) || strings.Count(out.Body, item.Placeholder) != wantCount || item.Occurrences != wantCount {
					t.Fatalf("literal text acquired a media marker: marker=%q occurrences=%d body=%s", item.Placeholder, item.Occurrences, out.Body)
				}
				if !strings.Contains(out.Body, "[User 1]: "+literal) || out.CommandBody != in.CommandBody {
					t.Fatalf("original sender/text or command changed: %s", out.Body)
				}
				wantFetches := 1
				if source == "recent-and-quote" {
					wantFetches++
				}
				if len(fake.calls)+len(fake.listCalls) != wantFetches {
					t.Fatalf("collision fetched extra context: get=%v list=%v", fake.calls, fake.listCalls)
				}
			})
		}
	}
}

func TestContextMediaRichTextCollision(t *testing.T) {
	fake := newEnricherFake()
	literal := "[Image 1: unavailable]"
	content, err := json.Marshal(map[string]any{"title": "literal " + literal, "content": [][]map[string]string{{
		{"tag": "text", "text": "[Image 1:"}, {"tag": "text", "text": "unavailable]"},
		{"tag": "img", "image_key": "repeated"}, {"tag": "text", "text": literal},
		{"tag": "img", "image_key": "repeated"}, {"tag": "text", "text": literal},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	fake.userNames = map[string]string{"alice": "Alice " + literal}
	fake.byID["parent"] = []LarkMessage{{MessageID: "parent", MessageType: "post", Content: string(content), SenderID: "alice", SenderType: "user"}}
	out := enrich(t, fake, InboundMessage{MessageID: "trigger", MessageType: "text", Body: "/issue Investigate", CommandBody: "/issue Investigate",
		ParentID: "parent", ChatID: "group", ChatType: ChatTypeGroup, AddressedToBot: true}, InboundEnricherConfig{})
	if len(out.ContextMedia) != 1 {
		t.Fatalf("repeated rich-text image lost: %+v", out.ContextMedia)
	}
	marker := out.ContextMedia[0].Placeholder
	if marker == literal || strings.Count(out.Body, marker) != 2 || strings.Count(out.MediaSourceContext, marker) != 2 ||
		!strings.Contains(out.Body, literal+" "+marker+" "+literal+" "+marker+" "+literal) {
		t.Fatalf("rich-text source positions collided: %s", out.Body)
	}
	if !strings.Contains(out.MediaSourceContext, `sender="Alice `+literal+`"`) || len(fake.calls) != 1 || len(fake.userCalls) != 1 {
		t.Fatalf("source attribution or fetch scope changed: %+v", out)
	}
}

func TestContextMediaMetadataCollision(t *testing.T) {
	for _, source := range []string{"sender", "filename"} {
		t.Run(source, func(t *testing.T) {
			fake := newEnricherFake()
			kind, name, filename := "image", "Alice [Image 1: unavailable]", "source.bin"
			body := "Inspect"
			if source == "filename" {
				kind, name, filename = "file", "Alice", "notes [File 1: unavailable].txt"
				body = "Inspect [File 2: unavailable]"
			}
			content, err := json.Marshal(map[string]string{"image_key": "image", "file_key": "file", "file_name": filename})
			if err != nil {
				t.Fatal(err)
			}
			fake.userNames = map[string]string{"alice": name}
			fake.byID["parent"] = []LarkMessage{{MessageID: "parent", MessageType: kind, SenderID: "alice", SenderType: "user", Content: string(content)}}
			out := enrich(t, fake, InboundMessage{MessageID: "trigger", Body: body, CommandBody: body, ParentID: "parent",
				ChatID: "group", ChatType: ChatTypeGroup}, InboundEnricherConfig{})
			if len(out.ContextMedia) != 1 {
				t.Fatalf("media lost: %+v", out)
			}
			marker := out.ContextMedia[0].Placeholder
			if strings.Contains(name+filename+body, marker) || strings.Count(out.Body, marker) != 1 || !strings.Contains(out.Body, name) {
				t.Fatalf("metadata acquired an attachment marker: %s", out.Body)
			}
			if source == "filename" && !strings.Contains(out.Body, filename) {
				t.Fatalf("filename changed: %s", out.Body)
			}
		})
	}
}
