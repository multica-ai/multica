package knowledge

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestParseDocumentAcceptsMarkdownWithPlainTextMIME(t *testing.T) {
	parsed, err := ParseDocument([]byte("# Title\n\nBody\n"), "note.md", "text/plain")
	if err != nil {
		t.Fatalf("ParseDocument() error = %v", err)
	}
	if len(parsed.Blocks) != 2 || parsed.Blocks[0].Kind != "heading" {
		t.Fatalf("unexpected blocks: %+v", parsed.Blocks)
	}
}

func TestParseDocumentRejectsInvalidPDFHeader(t *testing.T) {
	if _, err := ParseDocument([]byte("not a PDF"), "note.pdf", "application/pdf"); err == nil || !strings.Contains(err.Error(), "invalid pdf header") {
		t.Fatalf("ParseDocument() error = %v, want invalid pdf header", err)
	}
}

func TestParseDocumentRejectsUnsafeArchiveMemberPath(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	file, err := archive.Create("../word/document.xml")
	if err != nil {
		t.Fatalf("create zip member: %v", err)
	}
	if _, err := file.Write([]byte("<document />")); err != nil {
		t.Fatalf("write zip member: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if _, err := ParseDocument(buffer.Bytes(), "note.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"); err == nil || !strings.Contains(err.Error(), "unsafe member path") {
		t.Fatalf("ParseDocument() error = %v, want unsafe member path", err)
	}
}

func TestParseDocumentKeepsXLSXFormulaAndCachedValue(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	file, err := archive.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatalf("create sheet: %v", err)
	}
	_, _ = file.Write([]byte(`<worksheet><sheetData><row r="1"><c r="A1"><f>SUM(B1:B2)</f><v>3</v></c></row></sheetData></worksheet>`))
	if err := archive.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	parsed, err := ParseDocument(buffer.Bytes(), "book.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	if err != nil {
		t.Fatalf("ParseDocument() error = %v", err)
	}
	if len(parsed.Blocks) != 1 || !strings.Contains(parsed.Blocks[0].Text, "[公式:SUM(B1:B2)] = 3") {
		t.Fatalf("unexpected formula block: %+v", parsed.Blocks)
	}
}

func TestParseDocumentRejectsPDFPageLimit(t *testing.T) {
	data := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("/Type /Page\n"), maxKnowledgePDFPages+1)...)
	if _, err := ParseDocument(data, "large.pdf", "application/pdf"); err == nil || !strings.Contains(err.Error(), "page limit") {
		t.Fatalf("ParseDocument() error = %v, want page limit", err)
	}
}
