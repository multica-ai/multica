package dingtalk

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func officeFile(t *testing.T, part string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range []string{"[Content_Types].xml", part} {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte("<?xml version=\"1.0\"?><document/>")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFileCallbackFailuresAreExplicit(t *testing.T) {
	for _, tt := range []struct {
		name, reason string
		data         []byte
	}{
		{"oversized", "10 MB limit", bytes.Repeat([]byte("x"), maxInboundFileBytes+1)},
		{"html pretending to be PDF", "unsupported file type", []byte("<!DOCTYPE html><html>login page</html>")},
		{"binary", "unsupported file type", []byte{0, 1, 2, 3}},
		{"empty", "empty file", nil},
		{"bad ZIP", "invalid ZIP", []byte("PK\x03\x04not a zip")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := newMediaTestEnv(t, map[string][]byte{"private-code": tt.data})
			cb := textCallback(convTypeP2P, false)
			cb.Msgtype = "file"
			cb.Content = json.RawMessage(`{"fileName":"report.pdf","downloadCode":"private-code"}`)
			msg, _ := inboundFromCallback(cb, "app-key")
			inst, messageID, _ := mediaFixture()
			got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
			if len(got.MediaRefs) != 0 || len(env.store.uploads) != 0 || !strings.Contains(got.Text, tt.reason) {
				t.Fatalf("rejected file = %+v", got)
			}
			select {
			case notice := <-env.notices:
				if !strings.Contains(notice, tt.reason) || strings.Contains(notice, "private-code") {
					t.Fatalf("notice = %s", notice)
				}
			default:
				t.Fatal("no user-facing failure notice")
			}
		})
	}
}

func TestFileCallbackMissingReference(t *testing.T) {
	for _, payload := range []string{"", "null", "{}", `{"fileName":"report.pdf"}`, `{"downloadCode":" "}`, `{"downloadCode":1}`} {
		cb := textCallback(convTypeP2P, false)
		cb.Msgtype, cb.Content = "file", json.RawMessage(payload)
		msg, ok := inboundFromCallback(cb, "app-key")
		raw, err := decodeDingTalkRaw(msg)
		if !ok || err != nil || len(raw.Media) != 0 || !strings.Contains(msg.Text, "File unavailable") {
			t.Fatalf("payload %q: %+v, %v", payload, msg, err)
		}
	}
}

func TestFileReplyKeepsSeparateImageAndFilePositions(t *testing.T) {
	env := newMediaTestEnv(t, map[string][]byte{"file": []byte("%PDF-1.7\n"), "image": pngBytes})
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "file"
	cb.Content = json.RawMessage(`{"fileName":"report.pdf","downloadCode":"file"}`)
	cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: "richText", SenderNick: "Alice", Content: botCallbackRepliedContent{
		RichText: richTextItems{{Text: "Literal [File]"}, {Type: "picture", DownloadCode: "image"}},
	}}
	msg, _ := inboundFromCallback(cb, "app-key")
	inst, messageID, _ := mediaFixture()
	got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
	if len(got.MediaRefs) != 2 {
		t.Fatalf("refs = %+v", got.MediaRefs)
	}
	if ref := got.MediaRefs[0]; ref.Type != channel.MsgTypeImage || ref.InlinePlaceholder != "[Image]" || ref.InlineIndex != 0 {
		t.Fatalf("quoted image = %+v", ref)
	}
	if ref := got.MediaRefs[1]; ref.Type != channel.MsgTypeFile || ref.InlinePlaceholder != "[File]" || ref.InlineIndex != 1 {
		t.Fatalf("current file = %+v", ref)
	}
}

func TestCleanDingTalkFilename(t *testing.T) {
	for input, want := range map[string]string{
		"../唯一编号.docx": "唯一编号.docx", `C:\temp\budget.xlsx`: "budget.xlsx", "a\r\n\x00\u202eb.pdf": "ab.pdf", "..": "", "/": "", "": "",
	} {
		if got := cleanDingTalkFilename(input); got != want {
			t.Errorf("%q: got %q, want %q", input, got, want)
		}
	}
}

func TestFileCallbackResolvesOriginalDocument(t *testing.T) {
	for _, tt := range []struct {
		name, mime string
		data       []byte
	}{
		{"唯一编号.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", officeFile(t, "word/document.xml")},
		{"budget.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", officeFile(t, "xl/workbook.xml")},
		{"report.pdf", "application/pdf", []byte("%PDF-1.7\nexample\n%%EOF")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := newMediaTestEnv(t, map[string][]byte{"file-code": tt.data})
			cb := textCallback(convTypeP2P, false)
			cb.Msgtype = "file"
			cb.Content, _ = json.Marshal(map[string]string{"fileName": "../" + tt.name, "downloadCode": "file-code"})
			msg, ok := inboundFromCallback(cb, "app-key")
			if !ok || !env.resolver.HasMedia(msg) {
				t.Fatal("file callback has no ingestable media")
			}
			inst, messageID, _ := mediaFixture()
			got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
			if len(got.MediaRefs) != 1 {
				t.Fatalf("refs = %+v, body = %q", got.MediaRefs, got.Text)
			}
			ref := got.MediaRefs[0]
			if ref.Type != channel.MsgTypeFile || ref.Filename != tt.name || ref.MimeType != tt.mime || ref.SizeBytes != int64(len(tt.data)) {
				t.Fatalf("attachment metadata = %+v", ref)
			}
			if sha256.Sum256(env.store.uploads[ref.StorageKey]) != sha256.Sum256(tt.data) {
				t.Fatal("stored checksum differs")
			}
			if len(env.ledger.records) != 1 {
				t.Fatal("missing attachment intent")
			}
		})
	}
}
