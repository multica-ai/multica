package runtime

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// buildTestPptx assembles a minimal .pptx (OOXML zip) whose slides carry the
// given DrawingML <a:t> text runs, in the slideN order provided.
func buildTestPptx(t *testing.T, slides map[string][]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, runs := range slides {
		w, err := zw.Create("ppt/slides/" + name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		var sb strings.Builder
		sb.WriteString(`<?xml version="1.0"?><p:sld xmlns:a="x" xmlns:p="y"><p:cSld><p:spTree>`)
		for _, r := range runs {
			sb.WriteString("<a:p><a:r><a:t>" + r + "</a:t></a:r></a:p>")
		}
		sb.WriteString(`</p:spTree></p:cSld></p:sld>`)
		if _, err := w.Write([]byte(sb.String())); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestExtractPptxText_OrdersSlidesNumerically(t *testing.T) {
	// slide10 must come after slide2, not before (lexical order would break this).
	body := buildTestPptx(t, map[string][]string{
		"slide2.xml":  {"Anden", "ELEFANTPARASOL"},
		"slide10.xml": {"Tiende"},
		"slide1.xml":  {"Foerste"},
	})
	got, err := extractPptxText(body)
	if err != nil {
		t.Fatalf("extractPptxText: %v", err)
	}
	if !strings.Contains(got, "ELEFANTPARASOL") {
		t.Errorf("missing slide content: %q", got)
	}
	i1 := strings.Index(got, "Foerste")
	i2 := strings.Index(got, "Anden")
	i10 := strings.Index(got, "Tiende")
	if !(i1 < i2 && i2 < i10) {
		t.Errorf("slides out of order: foerste@%d anden@%d tiende@%d\n%s", i1, i2, i10, got)
	}
}

func TestExtractPptxText_EmptyDeck(t *testing.T) {
	body := buildTestPptx(t, map[string][]string{"slide1.xml": {"   "}})
	if _, err := extractPptxText(body); err == nil {
		t.Fatal("expected error for deck with no extractable text")
	}
}

func TestIsPptxAttachment(t *testing.T) {
	if !isPptxAttachment("application/vnd.openxmlformats-officedocument.presentationml.presentation", "x") {
		t.Error("pptx mime not recognized")
	}
	if !isPptxAttachment("application/octet-stream", "deck.PPTX") {
		t.Error("pptx by extension not recognized (case-insensitive)")
	}
	if isPptxAttachment("text/plain", "notes.txt") {
		t.Error("txt wrongly treated as pptx")
	}
}

func TestNormalizedMediaType_Pptx(t *testing.T) {
	got := normalizedAttachmentMediaType("application/octet-stream", "deck.pptx")
	want := "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	if got != want {
		t.Errorf("normalizedAttachmentMediaType(.pptx) = %q, want %q", got, want)
	}
}
