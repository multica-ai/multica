// Package filegen turns inline content produced by a chat/gateway agent into
// real file bytes ready to be attached to a chat or issue (TECH-3416, Fase 2a).
//
// Chat-gateway agents have no filesystem, so unlike the coding-agent
// add_attachment tool (which reads a path off disk), file creation here starts
// from inline content the model emits: either a text body (markdown, html,
// csv, …) or structured rows (csv, xlsx). This package is deliberately free of
// any DB, storage, HTTP, or permission dependency — it only produces bytes +
// the right content-type. Upload, metadata, and permission-gating are wired on
// top of it by the create_file tool and the toolpolicy chain.
package filegen

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// MaxOutputBytes caps a single generated file so a runaway model cannot blow
// memory or storage. 25 MB comfortably covers documents and spreadsheets while
// staying well under the 100 MB upload limit.
const MaxOutputBytes = 25 << 20

// Result is the generated file: its raw bytes, the MIME content-type to store
// it under, and the canonical extension (without a leading dot).
type Result struct {
	Bytes       []byte
	ContentType string
	Ext         string
}

// Request describes one file to generate. Format selects the encoder; the
// other fields carry the payload. Text-based formats read Content; tabular
// formats (csv, xlsx) read Rows (first row may be a header). Both may be set
// for csv/xlsx — Rows wins when non-empty.
type Request struct {
	Format  string     // md|txt|csv|json|html|svg|xml|xlsx (case-insensitive)
	Content string     // raw body for text-based formats
	Rows    [][]string // tabular data for csv
}

// formatSpec is the per-format content-type + extension metadata.
type formatSpec struct {
	contentType string
	ext         string
	tabular     bool
}

var specs = map[string]formatSpec{
	"md":   {contentType: "text/markdown; charset=utf-8", ext: "md"},
	"txt":  {contentType: "text/plain; charset=utf-8", ext: "txt"},
	"json": {contentType: "application/json; charset=utf-8", ext: "json"},
	"html": {contentType: "text/html; charset=utf-8", ext: "html"},
	"svg":  {contentType: "image/svg+xml; charset=utf-8", ext: "svg"},
	"xml":  {contentType: "application/xml; charset=utf-8", ext: "xml"},
	"csv":  {contentType: "text/csv; charset=utf-8", ext: "csv", tabular: true},
}

// Office binary formats (xlsx, docx, pdf) are intentionally NOT here yet: they
// each need a third-party generator dependency, which touches the upstream
// server/go.mod zone and is handled as its own marked follow-up commit (Fase
// 2c). Until then, tabular data is served as csv, which every spreadsheet
// opens natively.

// SupportedFormats returns the format keys this package can generate, sorted.
// Used by the create_file tool to advertise its enum and by tests.
func SupportedFormats() []string {
	out := make([]string, 0, len(specs))
	for k := range specs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Build generates the file described by req. It validates the format and
// payload, enforces MaxOutputBytes, and returns the bytes + content-type.
func Build(req Request) (Result, error) {
	format := strings.ToLower(strings.TrimSpace(req.Format))
	spec, ok := specs[format]
	if !ok {
		return Result{}, fmt.Errorf("filegen: unsupported format %q (supported: %s)", req.Format, strings.Join(SupportedFormats(), ", "))
	}

	var (
		data []byte
		err  error
	)
	switch format {
	case "csv":
		data, err = buildCSV(req)
	case "json":
		data, err = buildJSON(req)
	default: // md, txt, html, svg, xml — store the body verbatim
		if strings.TrimSpace(req.Content) == "" {
			return Result{}, fmt.Errorf("filegen: %s requires non-empty content", format)
		}
		data = []byte(req.Content)
	}
	if err != nil {
		return Result{}, err
	}
	if len(data) == 0 {
		return Result{}, fmt.Errorf("filegen: %s produced no content", format)
	}
	if len(data) > MaxOutputBytes {
		return Result{}, fmt.Errorf("filegen: %s output %d bytes exceeds limit of %d", format, len(data), MaxOutputBytes)
	}
	return Result{Bytes: data, ContentType: spec.contentType, Ext: spec.ext}, nil
}

// buildJSON validates that Content is well-formed JSON before storing it, so a
// .json file is never silently corrupt.
func buildJSON(req Request) ([]byte, error) {
	body := strings.TrimSpace(req.Content)
	if body == "" {
		return nil, fmt.Errorf("filegen: json requires non-empty content")
	}
	if !json.Valid([]byte(body)) {
		return nil, fmt.Errorf("filegen: content is not valid JSON")
	}
	return []byte(body), nil
}

// buildCSV prefers structured Rows; if none are given it falls back to the raw
// Content (already-formatted CSV text).
func buildCSV(req Request) ([]byte, error) {
	if len(req.Rows) == 0 {
		if strings.TrimSpace(req.Content) == "" {
			return nil, fmt.Errorf("filegen: csv requires rows or content")
		}
		return []byte(req.Content), nil
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.WriteAll(req.Rows); err != nil {
		return nil, fmt.Errorf("filegen: write csv: %w", err)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("filegen: flush csv: %w", err)
	}
	return buf.Bytes(), nil
}
