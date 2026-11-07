package channel

import (
	"bufio"
	"bytes"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	htmlrenderer "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var replyIssueIdentifier = regexp.MustCompile(`[A-Z0-9]{1,10}-[0-9]+`)
var replyIssueProtectedURL = regexp.MustCompile(
	`(?i)(?:[a-z][a-z0-9+.-]*://|www\.|mailto:|[/\\])[^\s\p{Z}\x{0085}\x0b<>]+` +
		"|[a-z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-z0-9.-]+")

type referenceEdit struct {
	start, end      int
	kind, reference string
	prefix, suffix  string
}

// LinkReferences rewrites outbound Multica references using a caller-owned
// resolver. Code, existing URLs and unresolved references retain their source.
// Broadcast mentions have no detail route and become inert display text.
func LinkReferences(content string, resolve func(kind, reference string) string) string {
	if resolve == nil || (!strings.Contains(content, "[") && !replyIssueIdentifier.MatchString(content)) {
		return content
	}
	var out strings.Builder
	last := 0
	for _, edit := range referenceEdits(content) {
		replacement := edit.prefix
		if edit.kind != "all" || edit.reference != "all" {
			link := resolve(edit.kind, edit.reference)
			if link == "" {
				continue
			}
			replacement = edit.prefix + link + edit.suffix
		}
		out.WriteString(content[last:edit.start])
		out.WriteString(replacement)
		last = edit.end
	}
	if last == 0 {
		return content
	}
	out.WriteString(content[last:])
	return out.String()
}

func referenceEdits(content string) []referenceEdit {
	inlines := parser.DefaultInlineParsers()
	for i := range inlines {
		inlines[i].Value = referenceSourceParser{inlines[i].Value.(parser.InlineParser)}
	}
	p := parser.NewParser(parser.WithBlockParsers(parser.DefaultBlockParsers()...), parser.WithInlineParsers(inlines...), parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...))
	source := []byte(content)
	md := goldmark.New(goldmark.WithParser(p), goldmark.WithExtensions(extension.Table))
	doc := md.Parser().Parse(text.NewReader(source))
	urls := referenceURLRanges(source, doc)
	var edits []referenceEdit
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.Link:
			if target, ok := strings.CutPrefix(string(util.URLEscape(n.Destination, true)), "mention://"); ok {
				kind, reference, valid := strings.Cut(target, "/")
				if !valid || kind == "" || reference == "" {
					return ast.WalkSkipChildren, nil
				}
				edit := referenceEdit{kind: kind, reference: reference}
				labelEnd, _ := n.AttributeString("reference_label_end")
				end, _ := n.AttributeString("source_end")
				if labelEnd, ok := labelEnd.(int); ok {
					if kind == "all" && reference == "all" {
						if end, ok := end.(int); ok && n.Pos() >= 0 && n.Pos() < labelEnd {
							edit.start, edit.end, edit.prefix = n.Pos(), end, string(source[n.Pos()+1:labelEnd])
							edits = append(edits, edit)
						}
						return ast.WalkSkipChildren, nil
					}
					if end, ok := end.(int); ok && n.Reference != nil {
						edit.start, edit.end, edit.prefix, edit.suffix = labelEnd+1, end, "(", inlineReferenceTitle(n.Title)+")"
						edits = append(edits, edit)
					} else {
						start, _ := n.AttributeString("reference_destination_start")
						stop, _ := n.AttributeString("reference_destination_end")
						if start, ok := start.(int); ok {
							if stop, ok := stop.(int); ok && start >= 0 && start < stop && stop <= len(source) {
								edit.start, edit.end = start, stop
								edits = append(edits, edit)
							}
						}
					}
				}
			}
			return ast.WalkSkipChildren, nil
		case *ast.CodeSpan, *ast.CodeBlock, *ast.FencedCodeBlock, *ast.Image, *ast.AutoLink, *ast.RawHTML, *ast.HTMLBlock:
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			for _, match := range replyIssueIdentifier.FindAllIndex(source[n.Segment.Start:n.Segment.Stop], -1) {
				start, end := n.Segment.Start+match[0], n.Segment.Start+match[1]
				urlIndex := sort.Search(len(urls), func(i int) bool { return urls[i].Stop > start })
				if urlIndex < len(urls) && urls[urlIndex].Start < end {
					continue
				}
				if bareIssueInProse(source, n, start, end) {
					reference := string(source[start:end])
					edits = append(edits, referenceEdit{start: start, end: end, kind: "issue", reference: reference, prefix: "[" + reference + "](", suffix: ")"})
				}
			}
		}
		return ast.WalkContinue, nil
	})
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	return edits
}

// Match raw URLs across text/emphasis nodes, but never through a complete
// Markdown construct. A URL in a link destination must not protect later prose.
func referenceURLRanges(source []byte, doc ast.Node) []text.Segment {
	var markup []text.Segment
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node.(type) {
		case *extast.TableCell:
			// A raw URL may span emphasis nodes, but cannot cross into the
			// next GFM cell. Escaped pipes inside a cell remain URL content.
			for i := 0; i < node.Lines().Len(); i++ {
				line := node.Lines().At(i)
				markup = append(markup, text.NewSegment(line.Start, line.Start), text.NewSegment(line.Stop, line.Stop))
			}
		case *ast.Link, *ast.Image, *ast.CodeSpan, *ast.AutoLink, *ast.RawHTML:
			start, _ := node.AttributeString("source_start")
			end, _ := node.AttributeString("source_end")
			if start, ok := start.(int); ok {
				if end, ok := end.(int); ok && start >= 0 && start < end && end <= len(source) {
					markup = append(markup, text.NewSegment(start, end))
				}
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	sort.Slice(markup, func(i, j int) bool { return markup[i].Start < markup[j].Start })
	var urls []text.Segment
	add := func(start, end int) {
		for _, match := range replyIssueProtectedURL.FindAllIndex(source[start:end], -1) {
			urls = append(urls, text.NewSegment(start+match[0], start+match[1]))
		}
	}
	last := 0
	for _, span := range markup {
		if span.Start > last {
			add(last, span.Start)
		}
		if span.Stop > last {
			last = span.Stop
		}
	}
	add(last, len(source))
	return urls
}

// Capture source boundaries through Goldmark so nested and reference labels
// retain their original Markdown rather than being reconstructed.
type referenceSourceParser struct{ parser.InlineParser }

func (p referenceSourceParser) Parse(parent ast.Node, reader text.Reader, pc parser.Context) ast.Node {
	beforeLine, before := reader.Position()
	n := p.InlineParser.Parse(parent, reader, pc)
	switch n.(type) {
	case *ast.Link, *ast.Image, *ast.CodeSpan, *ast.AutoLink, *ast.RawHTML:
		endLine, end := reader.Position()
		start := before.Start
		switch n.(type) {
		case *ast.Link:
			n.SetAttributeString("reference_label_end", before.Start)
			start = n.Pos()
			link := n.(*ast.Link)
			if link.Reference == nil {
				// Reuse the block reader's container-aware whitespace handling;
				// raw source scanning would stop at a continuation line's '>'.
				reader.SetPosition(beforeLine, before)
				reader.Advance(1) // closing label bracket
				if reader.Peek() == '(' {
					reader.Advance(1)
					reader.SkipSpaces()
					if reader.Peek() == '<' {
						reader.Advance(1)
					}
					_, destination := reader.Position()
					stop := destination.Start + len(link.Destination)
					if destination.Start >= 0 && stop <= end.Start && bytes.HasPrefix(reader.Source()[destination.Start:], link.Destination) {
						n.SetAttributeString("reference_destination_start", destination.Start)
						n.SetAttributeString("reference_destination_end", stop)
					}
				}
				reader.SetPosition(endLine, end)
			}
		case *ast.Image:
			start = n.Pos()
		}
		n.SetAttributeString("source_start", start)
		n.SetAttributeString("source_end", end.Start)
	}
	return n
}

func (p referenceSourceParser) CloseBlock(parent ast.Node, reader text.Reader, pc parser.Context) {
	if closer, ok := p.InlineParser.(parser.CloseBlocker); ok {
		closer.CloseBlock(parent, reader, pc)
	}
}

func inlineReferenceTitle(title []byte) string {
	if title == nil {
		return ""
	}
	// Use Goldmark's own title decoding, then keep the expanded title on one
	// source line so reference links in quotes/lists retain their containers.
	var escaped bytes.Buffer
	w := bufio.NewWriter(&escaped)
	htmlrenderer.DefaultWriter.Write(w, title)
	_ = w.Flush()
	value := strings.NewReplacer("\\", "&#92;", "\r", "&#13;", "\n", "&#10;").Replace(escaped.String())
	return ` "` + value + `"`
}

func bareIssueInProse(source []byte, node *ast.Text, start, end int) bool {
	isToken := func(c byte) bool {
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_-./\\@", rune(c))
	}
	// Only parsed emphasis delimiters are exempt from the underscore token
	// guard. Underscores inside words and URL ranges remain protected.
	_, emphasis := node.Parent().(*ast.Emphasis)
	leadingEmphasis := emphasis && start == node.Segment.Start && node.PreviousSibling() == nil && start > 0 && source[start-1] == '_'
	trailingEmphasis := emphasis && end == node.Segment.Stop && node.NextSibling() == nil && end < len(source) && source[end] == '_'
	if start > 0 && isToken(source[start-1]) && !leadingEmphasis || end < len(source) && isToken(source[end]) && source[end] != '.' && !trailingEmphasis {
		return false
	}
	if end+1 < len(source) && source[end] == '.' && isToken(source[end+1]) {
		return false
	}
	if start >= 2 && source[start-1] == '[' && source[start-2] == '\\' {
		return false
	}
	return true
}
