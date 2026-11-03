package service

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"strings"

	pdf "github.com/ledongthuc/pdf"
)

// uploadHeadingReaders maps an upload's extension to the reader of its headings.
// A file whose extension is absent here is stored without a heading check: WeKnora
// parses it only after storage. Each reader returns the headings as Markdown lines,
// so MissingSections checks every format the same way.
var uploadHeadingReaders = map[string]func([]byte) (string, error){
	".md":       textHeadings,
	".markdown": textHeadings,
	".txt":      textHeadings,
	".docx":     docxHeadings,
	".pdf":      pdfHeadings,
}

// uploadHeadingReader returns the heading reader for a file name, if the format has one.
func uploadHeadingReader(name string) (func([]byte) (string, error), bool) {
	read, ok := uploadHeadingReaders[strings.ToLower(path.Ext(name))]
	return read, ok
}

// unreadableUpload is the refusal for a file whose headings cannot be read. It
// names the format and not the parser error, which is for the logs, not the person.
func unreadableUpload(format string) error {
	return &KnowledgeInvalidError{fmt.Sprintf("the %s file cannot be read, so its required headings cannot be checked", format)}
}

func textHeadings(body []byte) (string, error) {
	return string(body), nil
}

const wordNamespace = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

var (
	// wordHeadingName matches a built-in heading style by its name. Word stores
	// the built-in names in English whatever the interface language, so a German
	// Word file still names the style "heading 1".
	wordHeadingName = regexp.MustCompile(`(?i)^heading ([1-9])$`)
	// wordHeadingID matches the style id Word gives a heading when styles.xml
	// does not name it.
	wordHeadingID = regexp.MustCompile(`^Heading([1-9])$`)
)

// docxHeadings returns the paragraphs of a Word document that use a heading style,
// as Markdown headings. The level comes from the style, not from bold or font
// size, because a bold line is not a heading in the document's structure.
func docxHeadings(body []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", unreadableUpload("Word")
	}
	levels := map[string]int{}
	if part, err := openZipPart(zr, "word/styles.xml"); err == nil {
		levels, err = docxStyleLevels(part)
		part.Close()
		if err != nil {
			return "", unreadableUpload("Word")
		}
	}
	document, err := openZipPart(zr, "word/document.xml")
	if err != nil {
		return "", unreadableUpload("Word")
	}
	defer document.Close()
	headings, err := docxParagraphHeadings(document, levels)
	if err != nil {
		return "", unreadableUpload("Word")
	}
	return headings, nil
}

func openZipPart(zr *zip.Reader, name string) (io.ReadCloser, error) {
	for _, f := range zr.File {
		if f.Name == name {
			return f.Open()
		}
	}
	return nil, fs.ErrNotExist
}

// docxStyleLevels maps each heading style id in styles.xml to its level.
func docxStyleLevels(r io.Reader) (map[string]int, error) {
	levels := map[string]int{}
	dec := xml.NewDecoder(r)
	styleID := ""
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return levels, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Space != wordNamespace {
			continue
		}
		switch start.Name.Local {
		case "style":
			styleID = wordAttr(start.Attr, "styleId")
		case "name":
			if m := wordHeadingName.FindStringSubmatch(wordAttr(start.Attr, "val")); m != nil && styleID != "" {
				levels[styleID] = int(m[1][0] - '0')
			}
		}
	}
}

// docxParagraphHeadings reads document.xml as a stream. A paragraph inside another
// (a text box) is read on its own, so its text does not count toward the outer one.
func docxParagraphHeadings(r io.Reader, levels map[string]int) (string, error) {
	type paragraph struct {
		style string
		text  strings.Builder
	}
	dec := xml.NewDecoder(r)
	var open []*paragraph
	var headings []string
	inText := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wordNamespace {
				continue
			}
			switch t.Name.Local {
			case "p":
				open = append(open, &paragraph{})
			case "pStyle":
				if len(open) > 0 {
					open[len(open)-1].style = wordAttr(t.Attr, "val")
				}
			case "t":
				inText = true
			}
		case xml.EndElement:
			if t.Name.Space != wordNamespace {
				continue
			}
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if len(open) == 0 {
					continue
				}
				p := open[len(open)-1]
				open = open[:len(open)-1]
				level := headingLevel(p.style, levels)
				text := strings.Join(strings.Fields(p.text.String()), " ")
				if level > 0 && text != "" {
					headings = append(headings, strings.Repeat("#", level)+" "+text)
				}
			}
		case xml.CharData:
			if inText && len(open) > 0 {
				open[len(open)-1].text.Write(t)
			}
		}
	}
	return strings.Join(headings, "\n"), nil
}

// headingLevel returns the heading level of a paragraph style, or 0 when the
// style is not a heading. Markdown has six levels, so deeper ones are clamped.
func headingLevel(style string, levels map[string]int) int {
	level, ok := levels[style]
	if !ok {
		if m := wordHeadingID.FindStringSubmatch(style); m != nil {
			level = int(m[1][0] - '0')
		}
	}
	return min(level, 6)
}

func wordAttr(attrs []xml.Attr, local string) string {
	for _, a := range attrs {
		if a.Name.Local == local && a.Name.Space == wordNamespace {
			return a.Value
		}
	}
	return ""
}

// pdfHeadings returns each line of a PDF's text as a Markdown line. A PDF has no
// heading structure in its text, so a line that equals a required heading counts,
// including a body line that repeats the heading's words. WeKnora decides real
// headings after storage, from font size.
func pdfHeadings(body []byte) (headings string, err error) {
	// The library's page parser can panic on a malformed file. A panic is a
	// refusal here, not a crash of the request.
	defer func() {
		if r := recover(); r != nil {
			headings, err = "", unreadableUpload("PDF")
		}
	}()
	reader, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", unreadableUpload("PDF")
	}
	// Rows, not GetPlainText: the plain text drops the line breaks, so a heading
	// would run into the sentence before it.
	var b strings.Builder
	for page := 1; page <= reader.NumPage(); page++ {
		rows, err := reader.Page(page).GetTextByRow()
		if err != nil {
			return "", unreadableUpload("PDF")
		}
		for _, row := range rows {
			if line := pdfRowText(row.Content); line != "" {
				b.WriteString("## " + line + "\n")
			}
		}
	}
	return b.String(), nil
}

// pdfRowText joins the text runs of one row from left to right. A run starts a
// new word when it begins past the end of the run before it.
func pdfRowText(runs pdf.TextHorizontal) string {
	var b strings.Builder
	prevEnd := 0.0
	for i, run := range runs {
		if i > 0 && run.X-prevEnd > run.FontSize*0.15 {
			b.WriteByte(' ')
		}
		b.WriteString(run.S)
		prevEnd = run.X + run.W
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
