package dictation

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
)

// parseFields decodes a buildMultipart body back into its non-file form fields.
func parseFields(t *testing.T, body []byte, contentType string) map[string]string {
	t.Helper()
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("parse content type: %v", err)
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	fields := map[string]string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("next part: %v", err)
		}
		if part.FileName() != "" {
			continue
		}
		data, _ := io.ReadAll(part)
		fields[part.FormName()] = string(data)
	}
	return fields
}

func TestBuildMultipartIncludesOptionalFields(t *testing.T) {
	body, contentType, err := buildMultipart(strings.NewReader("clip"), "x.webm", "da", "Helsebixen, made4men", "true")
	if err != nil {
		t.Fatalf("buildMultipart: %v", err)
	}
	fields := parseFields(t, body, contentType)
	if fields["language"] != "da" {
		t.Errorf("language = %q, want da", fields["language"])
	}
	if fields["glossary"] != "Helsebixen, made4men" {
		t.Errorf("glossary = %q", fields["glossary"])
	}
	if fields["cleanup"] != "true" {
		t.Errorf("cleanup = %q, want true", fields["cleanup"])
	}
}

func TestBuildMultipartOmitsEmptyFields(t *testing.T) {
	body, contentType, err := buildMultipart(strings.NewReader("clip"), "x.webm", "", "", "")
	if err != nil {
		t.Fatalf("buildMultipart: %v", err)
	}
	fields := parseFields(t, body, contentType)
	for _, k := range []string{"language", "glossary", "cleanup"} {
		if _, ok := fields[k]; ok {
			t.Errorf("field %q should be omitted when empty", k)
		}
	}
}
