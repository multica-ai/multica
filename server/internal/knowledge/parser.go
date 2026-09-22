package knowledge

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const (
	maxKnowledgeSourceBytes       = 100 << 20
	maxKnowledgeArchiveEntries    = 20_000
	maxKnowledgeArchiveBytes      = 1 << 30
	maxKnowledgeArchiveMemberSize = 128 << 20
	maxKnowledgePDFPages          = 1_000
)

var pdfPagePattern = regexp.MustCompile(`/Type\s*/Page\b`)

// ParseDocument is the dependency-free baseline parser used by the worker.
// Deployments may put a Docling service behind ParserURL; the worker uses that
// service first and this parser is the deterministic fallback for the common
// text, office, table, and HTML formats. It never executes macros, formulas,
// scripts, or embedded HTML.
func ParseDocument(data []byte, filename, contentType string) (ParsedDocument, error) {
	if len(data) == 0 {
		return ParsedDocument{}, fmt.Errorf("empty source")
	}
	if len(data) > maxKnowledgeSourceBytes {
		return ParsedDocument{}, fmt.Errorf("source exceeds the 100 MiB limit")
	}
	name := filepath.Base(filename)
	ext := strings.ToLower(filepath.Ext(name))
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = parsed
	}
	if err := validateSourceEnvelope(data, ext, contentType); err != nil {
		return ParsedDocument{}, err
	}
	if (ext == ".md" || ext == ".markdown" || ext == ".mdown" || ext == ".txt" || ext == ".html" || ext == ".htm" || ext == ".csv" ||
		strings.HasPrefix(contentType, "text/")) && !utf8.Valid(data) {
		return ParsedDocument{}, fmt.Errorf("source encoding could not be determined; please re-upload as UTF-8")
	}
	var parsed ParsedDocument
	var err error
	switch {
	case ext == ".md", ext == ".markdown", ext == ".mdown":
		parsed = parseMarkdown(data, name)
	case ext == ".txt" || strings.HasPrefix(contentType, "text/plain"):
		parsed = parsePlainText(data, name)
	case ext == ".html" || ext == ".htm" || contentType == "text/html":
		parsed, err = parseHTML(data, name)
	case ext == ".csv" || strings.HasPrefix(contentType, "text/csv"):
		parsed, err = parseCSV(data, name)
	case ext == ".docx" || contentType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		parsed, err = parseDOCX(data, name)
	case ext == ".xlsx" || contentType == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		parsed, err = parseXLSX(data, name)
	case ext == ".pdf" || contentType == "application/pdf":
		parsed, err = parsePDF(data, name)
	default:
		return ParsedDocument{}, ErrUnsupportedFormat
	}
	if err != nil {
		return ParsedDocument{}, err
	}
	if parsed.SchemaVersion == "" {
		parsed.SchemaVersion = ParserSchemaVersion
	}
	if parsed.ParserVersion == "" {
		parsed.ParserVersion = ParserVersion
	}
	if parsed.Title == "" {
		parsed.Title = strings.TrimSuffix(name, filepath.Ext(name))
	}
	if parsed.Warnings == nil {
		parsed.Warnings = []string{}
	}
	if parsed.Stats == nil {
		parsed.Stats = map[string]any{}
	}
	parsed.Stats["blocks"] = len(parsed.Blocks)
	sanitized, err := sanitizeParsedDocument(parsed)
	if err != nil {
		return ParsedDocument{}, err
	}
	encoded, err := json.Marshal(sanitized)
	if err != nil {
		return ParsedDocument{}, fmt.Errorf("encode parsed source: %w", err)
	}
	if len(encoded) > maxKnowledgeArchiveMemberSize {
		return ParsedDocument{}, fmt.Errorf("parsed source exceeds the 128 MiB normalized output limit")
	}
	return sanitized, nil
}

func formatFromExtension(ext string) string {
	switch ext {
	case ".md", ".markdown", ".mdown":
		return "markdown"
	case ".txt":
		return "text"
	case ".html", ".htm":
		return "html"
	case ".csv":
		return "csv"
	case ".docx":
		return "docx"
	case ".xlsx":
		return "xlsx"
	case ".pdf":
		return "pdf"
	default:
		return ""
	}
}

func formatFromMIME(contentType string) string {
	switch {
	case contentType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "docx"
	case contentType == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "xlsx"
	case contentType == "application/pdf":
		return "pdf"
	case contentType == "text/html":
		return "html"
	case contentType == "text/csv":
		return "csv"
	case contentType == "text/plain" || strings.HasPrefix(contentType, "text/"):
		return "text"
	default:
		return ""
	}
}

func validateSourceEnvelope(data []byte, ext, contentType string) error {
	extFormat := formatFromExtension(ext)
	mimeFormat := formatFromMIME(contentType)
	if extFormat != "" && mimeFormat != "" && extFormat != mimeFormat {
		// text/plain is a valid transport MIME for Markdown uploads because
		// browsers and object stores often do not preserve text subtypes.
		if !(extFormat == "markdown" && mimeFormat == "text") {
			return fmt.Errorf("filename extension and content type identify different formats")
		}
	}
	format := extFormat
	if format == "" {
		format = mimeFormat
	}
	switch format {
	case "pdf":
		if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("%PDF-")) {
			return fmt.Errorf("invalid pdf header")
		}
	case "docx", "xlsx":
		if !bytes.HasPrefix(data, []byte("PK")) {
			return fmt.Errorf("invalid office archive header")
		}
	}
	return nil
}

var allowedLocatorKeys = map[string]struct{}{
	"kind": {}, "block_id": {}, "page": {}, "line": {}, "line_start": {}, "line_end": {},
	"paragraph": {}, "row": {}, "item": {}, "text_index": {}, "sheet": {}, "cell_range": {},
	"tag": {}, "merged_ranges": {}, "bbox": {},
	"source_block_ids": {},
}

var allowedLocatorKinds = map[string]struct{}{
	"pdf": {}, "document": {}, "text": {}, "web": {}, "table": {},
}

// sanitizeParsedDocument is the boundary between a parser implementation and
// public citation data. A parser may add internal fields in its own response,
// but only the stable locator vocabulary is persisted and returned by the API.
func sanitizeParsedDocument(parsed ParsedDocument) (ParsedDocument, error) {
	if parsed.SchemaVersion != ParserSchemaVersion {
		return ParsedDocument{}, fmt.Errorf("unsupported parser schema %q", parsed.SchemaVersion)
	}
	if strings.TrimSpace(parsed.ParserVersion) == "" {
		return ParsedDocument{}, fmt.Errorf("parser version is required")
	}
	for index := range parsed.Blocks {
		item := &parsed.Blocks[index]
		if strings.TrimSpace(item.BlockID) == "" || strings.TrimSpace(item.Text) == "" {
			return ParsedDocument{}, fmt.Errorf("parser block %d is missing an id or text", index)
		}
		if len(item.Locator) == 0 {
			return ParsedDocument{}, fmt.Errorf("parser block %d is missing a locator", index)
		}
		kind, ok := item.Locator["kind"].(string)
		if !ok {
			return ParsedDocument{}, fmt.Errorf("parser block %d has an invalid locator kind", index)
		}
		if _, ok := allowedLocatorKinds[kind]; !ok {
			return ParsedDocument{}, fmt.Errorf("parser block %d has unsupported locator kind %q", index, kind)
		}
		clean := make(map[string]any, len(item.Locator))
		for key, value := range item.Locator {
			if _, ok := allowedLocatorKeys[key]; ok {
				clean[key] = value
			}
		}
		item.Locator = clean
	}
	if parsed.Stats == nil {
		parsed.Stats = map[string]any{}
	}
	parsed.Stats["blocks"] = len(parsed.Blocks)
	return parsed, nil
}

func baseParsed(title string, blocks []DocumentBlock) ParsedDocument {
	return ParsedDocument{
		SchemaVersion: ParserSchemaVersion,
		ParserVersion: ParserVersion,
		Title:         title,
		Blocks:        blocks,
		Warnings:      []string{},
		Stats:         map[string]any{},
	}
}

func parsePlainText(data []byte, filename string) ParsedDocument {
	text := string(data)
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	blocks := make([]DocumentBlock, 0)
	for lineNo, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		blocks = append(blocks, DocumentBlock{
			BlockID: fmt.Sprintf("line-%d", lineNo+1), Kind: "paragraph", Text: line,
			Locator: map[string]any{"kind": "text", "line": lineNo + 1},
		})
	}
	return baseParsed(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)), blocks)
}

func parseMarkdown(data []byte, filename string) ParsedDocument {
	text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	var headingPath []string
	blocks := make([]DocumentBlock, 0)
	var paragraph []string
	startLine := 1
	flush := func(lineNo int) {
		if len(paragraph) == 0 {
			return
		}
		body := strings.TrimSpace(strings.Join(paragraph, "\n"))
		if body != "" {
			blocks = append(blocks, DocumentBlock{
				BlockID: fmt.Sprintf("line-%d-%d", startLine, lineNo), Kind: "paragraph", Text: body,
				HeadingPath: append([]string(nil), headingPath...),
				Locator:     map[string]any{"kind": "text", "line_start": startLine, "line_end": lineNo},
			})
		}
		paragraph = nil
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			flush(lineNo - 1)
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			if level > 0 && level < len(trimmed) && trimmed[level] == ' ' {
				title := strings.TrimSpace(trimmed[level:])
				if len(headingPath) >= level {
					headingPath = headingPath[:level-1]
				}
				headingPath = append(headingPath, title)
				blocks = append(blocks, DocumentBlock{
					BlockID: fmt.Sprintf("heading-%d", lineNo), Kind: "heading", Text: title,
					HeadingPath: append([]string(nil), headingPath...),
					Locator:     map[string]any{"kind": "text", "line": lineNo},
				})
				continue
			}
		}
		if trimmed == "" {
			flush(lineNo - 1)
			continue
		}
		if len(paragraph) == 0 {
			startLine = lineNo
		}
		paragraph = append(paragraph, line)
	}
	flush(len(lines))
	return baseParsed(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)), blocks)
}

func parseHTML(data []byte, filename string) (ParsedDocument, error) {
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return ParsedDocument{}, fmt.Errorf("parse html: %w", err)
	}
	var title string
	var blocks []DocumentBlock
	var headingPath []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "script" || node.Type == html.ElementNode && node.Data == "style" {
			return
		}
		if node.Type == html.ElementNode && node.Data == "title" {
			title = strings.TrimSpace(nodeText(node))
		}
		if node.Type == html.ElementNode && (node.Data == "h1" || node.Data == "h2" || node.Data == "h3" || node.Data == "h4") {
			value := strings.TrimSpace(nodeText(node))
			if value != "" {
				level, _ := strconv.Atoi(node.Data[1:])
				if len(headingPath) >= level {
					headingPath = headingPath[:level-1]
				}
				headingPath = append(headingPath, value)
				blocks = append(blocks, DocumentBlock{BlockID: fmt.Sprintf("heading-%d", len(blocks)+1), Kind: "heading", Text: value, HeadingPath: append([]string(nil), headingPath...), Locator: map[string]any{"kind": "web", "tag": node.Data}})
			}
		} else if node.Type == html.ElementNode && (node.Data == "p" || node.Data == "li" || node.Data == "blockquote" || node.Data == "pre" || node.Data == "td" || node.Data == "th") {
			value := strings.TrimSpace(nodeText(node))
			if value != "" {
				kind := "paragraph"
				if node.Data == "li" || node.Data == "td" || node.Data == "th" {
					kind = "table_cell"
				}
				blocks = append(blocks, DocumentBlock{BlockID: fmt.Sprintf("block-%d", len(blocks)+1), Kind: kind, Text: value, HeadingPath: append([]string(nil), headingPath...), Locator: map[string]any{"kind": "web", "tag": node.Data}})
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return baseParsed(titleOrFilename(title, filename), blocks), nil
}

func nodeText(node *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(b.String()), " ")
}

func parseCSV(data []byte, filename string) (ParsedDocument, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = false
	blocks := make([]DocumentBlock, 0)
	row := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ParsedDocument{}, fmt.Errorf("parse csv: %w", err)
		}
		row++
		parts := make([]string, len(record))
		for i, value := range record {
			parts[i] = fmt.Sprintf("%s: %s", columnName(i+1), strings.TrimSpace(value))
		}
		text := strings.Join(parts, " | ")
		if strings.TrimSpace(text) == "" {
			continue
		}
		blocks = append(blocks, DocumentBlock{
			BlockID: fmt.Sprintf("row-%d", row), Kind: "table_row", Text: text,
			Locator: map[string]any{"kind": "table", "sheet": "CSV", "row": row, "cell_range": fmt.Sprintf("A%d:%s%d", row, columnName(len(record)), row)},
		})
	}
	return baseParsed(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)), blocks), nil
}

func columnName(n int) string {
	if n <= 0 {
		return "A"
	}
	var out []byte
	for n > 0 {
		n--
		out = append([]byte{byte('A' + n%26)}, out...)
		n /= 26
	}
	return string(out)
}

type docxParagraph struct {
	XMLName xml.Name
	Text    string `xml:"t,innerxml"`
}

func parseDOCX(data []byte, filename string) (ParsedDocument, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ParsedDocument{}, fmt.Errorf("parse docx archive: %w", err)
	}
	if err := validateKnowledgeArchive(archive); err != nil {
		return ParsedDocument{}, err
	}
	var document []byte
	for _, file := range archive.File {
		if file.Name == "word/document.xml" {
			r, openErr := file.Open()
			if openErr != nil {
				return ParsedDocument{}, openErr
			}
			document, err = io.ReadAll(io.LimitReader(r, maxKnowledgeArchiveMemberSize+1))
			_ = r.Close()
			if err != nil {
				return ParsedDocument{}, err
			}
			if len(document) > maxKnowledgeArchiveMemberSize {
				return ParsedDocument{}, fmt.Errorf("zip member %q exceeds the 128 MiB limit", file.Name)
			}
		}
		if strings.Contains(strings.ToLower(file.Name), "vbaproject") {
			return ParsedDocument{}, fmt.Errorf("macro-enabled document is not supported")
		}
	}
	if len(document) == 0 {
		return ParsedDocument{}, ErrUnsupportedFormat
	}
	decoder := xml.NewDecoder(bytes.NewReader(document))
	var blocks []DocumentBlock
	var paragraph []string
	var paraNo int
	inText := false
	flush := func() {
		text := strings.TrimSpace(strings.Join(paragraph, ""))
		if text != "" {
			paraNo++
			blocks = append(blocks, DocumentBlock{BlockID: fmt.Sprintf("paragraph-%d", paraNo), Kind: "paragraph", Text: text, Locator: map[string]any{"kind": "document", "paragraph": paraNo}})
		}
		paragraph = nil
	}
	for {
		token, tokenErr := decoder.Token()
		if tokenErr == io.EOF {
			flush()
			break
		}
		if tokenErr != nil {
			return ParsedDocument{}, fmt.Errorf("parse docx xml: %w", tokenErr)
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "t" {
				inText = true
			}
		case xml.CharData:
			if inText {
				paragraph = append(paragraph, string(value))
			}
		case xml.EndElement:
			if value.Name.Local == "t" {
				inText = false
			}
			if value.Name.Local == "p" {
				flush()
			}
		}
	}
	return baseParsed(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)), blocks), nil
}

type xlsxCell struct {
	Ref     string
	Type    string
	Value   string
	Formula string
}

func parseXLSX(data []byte, filename string) (ParsedDocument, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ParsedDocument{}, fmt.Errorf("parse xlsx archive: %w", err)
	}
	if err := validateKnowledgeArchive(archive); err != nil {
		return ParsedDocument{}, err
	}
	for _, file := range archive.File {
		if strings.Contains(strings.ToLower(file.Name), "vbaproject") || strings.Contains(strings.ToLower(file.Name), "externalLinks") {
			return ParsedDocument{}, fmt.Errorf("macro or external-link workbook is not supported")
		}
	}
	shared, _ := readZipFile(archive, "xl/sharedStrings.xml")
	sharedValues := parseSharedStrings(shared)
	var sheetNames []string
	for _, file := range archive.File {
		if strings.HasPrefix(file.Name, "xl/worksheets/sheet") && strings.HasSuffix(file.Name, ".xml") {
			sheetNames = append(sheetNames, file.Name)
		}
	}
	sort.Strings(sheetNames)
	blocks := make([]DocumentBlock, 0)
	for sheetIndex, name := range sheetNames {
		data, readErr := readZipFile(archive, name)
		if readErr != nil {
			return ParsedDocument{}, readErr
		}
		cells, readErr := parseSheetCells(data)
		if readErr != nil {
			return ParsedDocument{}, readErr
		}
		rows := map[int][]xlsxCell{}
		for _, cell := range cells {
			row := rowFromCellRef(cell.Ref)
			if cell.Type == "s" {
				if index, parseErr := strconv.Atoi(cell.Value); parseErr == nil && index >= 0 && index < len(sharedValues) {
					cell.Value = sharedValues[index]
				}
			}
			rows[row] = append(rows[row], cell)
		}
		rowNumbers := make([]int, 0, len(rows))
		for row := range rows {
			rowNumbers = append(rowNumbers, row)
		}
		sort.Ints(rowNumbers)
		for _, row := range rowNumbers {
			rowCells := rows[row]
			sort.Slice(rowCells, func(i, j int) bool { return rowCells[i].Ref < rowCells[j].Ref })
			parts := make([]string, len(rowCells))
			minRef, maxRef := "", ""
			for i, cell := range rowCells {
				value := strings.TrimSpace(cell.Value)
				if cell.Formula != "" {
					if value == "" {
						value = fmt.Sprintf("[公式:%s] 公式未计算", cell.Formula)
					} else {
						value = fmt.Sprintf("[公式:%s] = %s", cell.Formula, value)
					}
				}
				parts[i] = fmt.Sprintf("%s: %s", cell.Ref, value)
				if minRef == "" || cell.Ref < minRef {
					minRef = cell.Ref
				}
				if cell.Ref > maxRef {
					maxRef = cell.Ref
				}
			}
			blocks = append(blocks, DocumentBlock{BlockID: fmt.Sprintf("sheet-%d-row-%d", sheetIndex+1, row), Kind: "table_row", Text: strings.Join(parts, " | "), Locator: map[string]any{"kind": "table", "sheet": fmt.Sprintf("Sheet%d", sheetIndex+1), "row": row, "cell_range": minRef + ":" + maxRef}})
		}
	}
	return baseParsed(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)), blocks), nil
}

func readZipFile(archive *zip.Reader, name string) ([]byte, error) {
	for _, file := range archive.File {
		if file.Name == name {
			if file.UncompressedSize64 > maxKnowledgeArchiveMemberSize {
				return nil, fmt.Errorf("zip member %q exceeds the 128 MiB limit", name)
			}
			r, err := file.Open()
			if err != nil {
				return nil, err
			}
			data, readErr := io.ReadAll(io.LimitReader(r, maxKnowledgeArchiveMemberSize+1))
			_ = r.Close()
			if readErr == nil && len(data) > maxKnowledgeArchiveMemberSize {
				return nil, fmt.Errorf("zip member %q exceeds the 128 MiB limit", name)
			}
			return data, readErr
		}
	}
	return nil, fmt.Errorf("zip member %q not found", name)
}

func validateKnowledgeArchive(archive *zip.Reader) error {
	if len(archive.File) > maxKnowledgeArchiveEntries {
		return fmt.Errorf("zip archive contains more than %d entries", maxKnowledgeArchiveEntries)
	}
	var total uint64
	for _, file := range archive.File {
		name := strings.ReplaceAll(file.Name, "\\", "/")
		clean := path.Clean(name)
		if name == "" || strings.ContainsRune(name, '\x00') || path.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("zip archive contains an unsafe member path")
		}
		if file.UncompressedSize64 > maxKnowledgeArchiveMemberSize {
			return fmt.Errorf("zip member %q exceeds the 128 MiB limit", file.Name)
		}
		if total > maxKnowledgeArchiveBytes-file.UncompressedSize64 {
			return fmt.Errorf("zip archive exceeds the 1 GiB expanded-size limit")
		}
		total += file.UncompressedSize64
	}
	return nil
}

func parseSharedStrings(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var values []string
	var current strings.Builder
	inText := false
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "t" {
				inText = true
			}
		case xml.CharData:
			if inText {
				current.Write(value)
			}
		case xml.EndElement:
			if value.Name.Local == "t" {
				inText = false
			}
			if value.Name.Local == "si" {
				values = append(values, current.String())
				current.Reset()
			}
		}
	}
	return values
}

func parseSheetCells(data []byte) ([]xlsxCell, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var cells []xlsxCell
	var cell *xlsxCell
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return cells, nil
		}
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "c" {
				cell = &xlsxCell{}
				for _, attr := range value.Attr {
					switch attr.Name.Local {
					case "r":
						cell.Ref = attr.Value
					case "t":
						cell.Type = attr.Value
					}
				}
			} else if value.Name.Local == "f" && cell != nil {
				var formula string
				if err := decoder.DecodeElement(&formula, &value); err != nil {
					return nil, err
				}
				cell.Formula = formula
			} else if value.Name.Local == "v" && cell != nil {
				var valueText string
				if err := decoder.DecodeElement(&valueText, &value); err != nil {
					return nil, err
				}
				cell.Value = valueText
			}
		case xml.EndElement:
			if value.Name.Local == "c" && cell != nil {
				cells = append(cells, *cell)
				cell = nil
			}
		}
	}
}

var cellRowPattern = regexp.MustCompile(`^[A-Z]+([0-9]+)$`)

func rowFromCellRef(ref string) int {
	match := cellRowPattern.FindStringSubmatch(ref)
	if len(match) != 2 {
		return 0
	}
	row, _ := strconv.Atoi(match[1])
	return row
}

func parsePDF(data []byte, filename string) (ParsedDocument, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("%PDF-")) {
		return ParsedDocument{}, fmt.Errorf("invalid pdf header")
	}
	pageCount := len(pdfPagePattern.FindAll(data, -1))
	if pageCount > maxKnowledgePDFPages {
		return ParsedDocument{}, fmt.Errorf("pdf exceeds the %d page limit", maxKnowledgePDFPages)
	}
	// This is intentionally a text-only extraction path. It handles literal
	// strings emitted by text PDFs and clearly leaves scanned PDFs with no
	// blocks rather than pretending OCR happened.
	var textParts []string
	for i := 0; i < len(data); i++ {
		if data[i] != '(' {
			continue
		}
		var b strings.Builder
		depth := 1
		for i = i + 1; i < len(data) && depth > 0; i++ {
			if data[i] == '\\' && i+1 < len(data) {
				i++
				switch data[i] {
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(data[i])
				}
				continue
			}
			switch data[i] {
			case '(':
				depth++
				b.WriteByte('(')
			case ')':
				depth--
				if depth > 0 {
					b.WriteByte(')')
				}
			default:
				b.WriteByte(data[i])
			}
		}
		if value := strings.TrimSpace(strings.ToValidUTF8(b.String(), "")); value != "" && utf8.ValidString(value) {
			textParts = append(textParts, value)
		}
	}
	text := strings.Join(textParts, " ")
	if text == "" {
		return ParsedDocument{SchemaVersion: ParserSchemaVersion, ParserVersion: ParserVersion, Title: strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)), Blocks: []DocumentBlock{}, Warnings: []string{"no text blocks found; scanned or unsupported PDF needs OCR"}, Stats: map[string]any{"pages": pageCount}}, nil
	}
	if pageCount == 0 {
		pageCount = 1
	}
	parsed := baseParsed(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)), []DocumentBlock{{BlockID: "pdf-text-1", Kind: "paragraph", Text: text, Locator: map[string]any{"kind": "pdf", "page": 1}}})
	parsed.Stats["pages"] = pageCount
	return parsed, nil
}

func titleOrFilename(title, filename string) string {
	if strings.TrimSpace(title) != "" {
		return strings.TrimSpace(title)
	}
	if value, err := url.PathUnescape(filename); err == nil {
		filename = value
	}
	return strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
}
