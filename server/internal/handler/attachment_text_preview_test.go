package handler

import (
	"fmt"
	"testing"
)

func TestAttachmentTextPreviewEmbeddedWhitelist(t *testing.T) {
	for _, prefix := range attachmentTextPreview.contentTypePrefixes {
		t.Run("prefix/"+prefix, func(t *testing.T) {
			if !isTextPreviewable(prefix+"plain", "payload.bin") {
				t.Fatalf("content type prefix %q was not accepted", prefix)
			}
		})
	}
	for contentType := range attachmentTextPreview.contentTypes {
		t.Run("content-type/"+contentType, func(t *testing.T) {
			if !isTextPreviewable(contentType, "payload") {
				t.Fatalf("content type %q was not accepted", contentType)
			}
		})
	}
	for extension := range attachmentTextPreview.extensions {
		t.Run("extension/"+extension, func(t *testing.T) {
			filename := "source." + extension
			if !isTextPreviewable("application/octet-stream", filename) {
				t.Fatalf("extension %q was not accepted", extension)
			}
		})
	}
	for basename := range attachmentTextPreview.basenames {
		t.Run("basename/"+basename, func(t *testing.T) {
			if !isTextPreviewable("application/octet-stream", basename) {
				t.Fatalf("basename %q was not accepted", basename)
			}
		})
	}
}

func TestAttachmentTextPreviewNormalizesInputs(t *testing.T) {
	for _, tc := range []struct {
		contentType string
		filename    string
	}{
		{"Application/JSON; Charset=UTF-8", "payload"},
		{"application/octet-stream", `nested\MAIN.GO`},
		{"application/octet-stream", "nested/Dockerfile"},
	} {
		t.Run(fmt.Sprintf("%s/%s", tc.contentType, tc.filename), func(t *testing.T) {
			if !isTextPreviewable(tc.contentType, tc.filename) {
				t.Fatalf("isTextPreviewable(%q, %q) = false, want true", tc.contentType, tc.filename)
			}
		})
	}
}

func TestParseAttachmentTextPreviewWhitelistRejectsInvalidSources(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"malformed JSON", `{`},
		{
			"missing list",
			`{"content_type_prefixes":["text/"],"content_types":["application/json"],"extensions":["txt"]}`,
		},
		{
			"duplicate entry",
			`{"content_type_prefixes":["text/"],"content_types":["application/json","application/json"],"extensions":["txt"],"basenames":["makefile"]}`,
		},
		{
			"non-normalized entry",
			`{"content_type_prefixes":["text/"],"content_types":["Application/JSON"],"extensions":["txt"],"basenames":["makefile"]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseAttachmentTextPreviewWhitelist([]byte(tc.raw)); err == nil {
				t.Fatal("parseAttachmentTextPreviewWhitelist() error = nil, want error")
			}
		})
	}
}
