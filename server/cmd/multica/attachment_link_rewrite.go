package main

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"net/url"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/multica-ai/multica/server/internal/cli"
)

// An agent places a screenshot under the paragraph it illustrates by writing
// `![](./a.png)` in the body and passing `--attachment ./a.png` in the same
// call (MUL-9028). That relative path means nothing to any reader, so before
// the body is published every link/image destination naming one of this
// call's attachments is replaced with that attachment's durable URL — the
// first published revision is already the correct one.
//
// The rules mirror the local-path lint (local_path_link_lint.go):
//
//   - Only real inline Link / Image destinations are rewritten. A path quoted
//     in a code span or fenced block is not a destination and stays verbatim.
//   - Only targets that resolve to exactly one attachment are rewritten. A
//     target that names no attachment is left alone for the lint to judge.

// attachmentLinkRewrite is one destination that was swapped for an uploaded
// attachment's URL, reported on stderr so the agent can check the result.
type attachmentLinkRewrite struct {
	Target string
	URL    string
}

// attachmentLinkMatcher resolves a markdown destination to the --attachment
// it names.
type attachmentLinkMatcher struct {
	byPath map[string]int
	byName map[string][]int
}

func pathKey(s string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(s)
	}
	return s
}

func newAttachmentLinkMatcher(paths []string) *attachmentLinkMatcher {
	m := &attachmentLinkMatcher{byPath: map[string]int{}, byName: map[string][]int{}}
	for i, p := range paths {
		if strings.TrimSpace(p) == "" || isHTTPURL(p) {
			continue
		}
		keys := attachmentPathKeys(p)
		if len(keys) == 0 {
			continue
		}
		if _, dup := m.byPath[pathKey(keys[0])]; dup {
			// The same file attached twice; the first upload wins and the
			// duplicate must not make its bare name ambiguous.
			continue
		}
		for _, key := range keys {
			m.byPath[pathKey(key)] = i
		}
		base := pathKey(filepath.Base(p))
		m.byName[base] = append(m.byName[base], i)
	}
	return m
}

// attachmentPathKeys returns the absolute path of p plus, when it differs, its
// symlink-resolved form, so `/var/...` and `/private/var/...` (macOS) match.
func attachmentPathKeys(p string) []string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil
	}
	keys := []string{abs}
	if real, err := filepath.EvalSymlinks(abs); err == nil && real != abs {
		keys = append(keys, real)
	}
	return keys
}

// match returns the index of the attachment target names. A target matches
// when it resolves to the same file as an attachment path, or — for a bare
// file name such as `a.png` or `./a.png` — when exactly one attachment has
// that name.
func (m *attachmentLinkMatcher) match(target string) (int, bool) {
	raw := strings.TrimSpace(target)
	if raw == "" {
		return -1, false
	}
	candidates := []string{raw}
	if unescaped := unescapeMarkdownDestination(raw); unescaped != raw {
		candidates = append(candidates, unescaped)
	}
	for _, c := range append([]string(nil), candidates...) {
		if decoded, err := url.PathUnescape(c); err == nil && decoded != c {
			candidates = append(candidates, decoded)
		}
	}
	for _, c := range candidates {
		if i, ok := m.matchPath(c); ok {
			return i, true
		}
	}
	return -1, false
}

func unescapeMarkdownDestination(s string) string {
	var b strings.Builder
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && util.IsPunct(s[i+1]) {
			if i > start {
				b.WriteString(html.UnescapeString(s[start:i]))
			}
			b.WriteByte(s[i+1])
			i++
			start = i + 1
		}
	}
	if start == 0 {
		return html.UnescapeString(s)
	}
	if start < len(s) {
		b.WriteString(html.UnescapeString(s[start:]))
	}
	return b.String()
}

func (m *attachmentLinkMatcher) matchPath(target string) (int, bool) {
	// A Windows drive path parses as a URL with scheme "c", so test for an
	// absolute path before sniffing schemes — same order as the lint.
	if !filepath.IsAbs(target) {
		parsed, err := url.Parse(target)
		if err == nil && strings.EqualFold(parsed.Scheme, "file") {
			target = parsed.Path
			if len(target) > 2 && target[0] == '/' && target[2] == ':' {
				target = target[1:] // file:///C:/x → C:/x
			}
		} else if err == nil && parsed.Scheme != "" {
			return -1, false
		}
	}
	local := filepath.FromSlash(target)
	for _, key := range attachmentPathKeys(local) {
		if i, ok := m.byPath[pathKey(key)]; ok {
			return i, true
		}
	}
	cleaned := filepath.Clean(local)
	if !filepath.IsAbs(cleaned) && !strings.ContainsRune(cleaned, filepath.Separator) {
		if idx := m.byName[pathKey(cleaned)]; len(idx) == 1 {
			return idx[0], true
		}
	}
	return -1, false
}

type attachmentLinkParser struct {
	parser.InlineParser
	destSpans map[ast.Node]text.Segment
}

func (p *attachmentLinkParser) Parse(parent ast.Node, reader text.Reader, pc parser.Context) ast.Node {
	line, _ := reader.PeekLine()
	if len(line) == 0 || line[0] != ']' {
		return p.InlineParser.Parse(parent, reader, pc)
	}
	openLine, openSeg := reader.Position()
	n := p.InlineParser.Parse(parent, reader, pc)
	var dest []byte
	var ref *ast.ReferenceLink
	switch node := n.(type) {
	case *ast.Link:
		dest, ref = node.Destination, node.Reference
	case *ast.Image:
		dest, ref = node.Destination, node.Reference
	default:
		return n
	}
	if ref != nil || len(dest) == 0 {
		return n
	}
	endLine, endSeg := reader.Position()
	reader.SetPosition(openLine, openSeg)
	reader.Advance(1)
	if reader.Peek() == '(' {
		reader.Advance(1)
		reader.SkipSpaces()
		if reader.Peek() == '<' {
			reader.Advance(1)
		}
		_, destSeg := reader.Position()
		p.destSpans[n] = text.NewSegment(destSeg.Start, destSeg.Start+len(dest))
	}
	reader.SetPosition(endLine, endSeg)
	return n
}

func (p *attachmentLinkParser) CloseBlock(parent ast.Node, reader text.Reader, pc parser.Context) {
	p.InlineParser.(parser.CloseBlocker).CloseBlock(parent, reader, pc)
}

// rewriteAttachmentLinks replaces every inline link/image destination in body
// that names one of paths with the URL at the same index in urls. Bodies with
// nothing to rewrite are returned unchanged byte for byte.
func rewriteAttachmentLinks(body string, paths, urls []string) (string, []attachmentLinkRewrite) {
	if body == "" || len(paths) == 0 {
		return body, nil
	}
	matcher := newAttachmentLinkMatcher(paths)
	if len(matcher.byPath) == 0 {
		return body, nil
	}

	source := []byte(body)
	links := &attachmentLinkParser{
		InlineParser: parser.NewLinkParser(),
		destSpans:    map[ast.Node]text.Segment{},
	}
	inlineParsers := parser.DefaultInlineParsers()
	for i := range inlineParsers {
		if inlineParsers[i].Value == links.InlineParser {
			inlineParsers[i].Value = links
		}
	}
	markdown := parser.NewParser(
		parser.WithBlockParsers(parser.DefaultBlockParsers()...),
		parser.WithInlineParsers(inlineParsers...),
		parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
	)
	doc := markdown.Parse(text.NewReader(source))

	type edit struct {
		start, stop int
		url         string
	}
	var edits []edit
	var rewrites []attachmentLinkRewrite
	reported := map[string]bool{}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var dest []byte
		switch node := n.(type) {
		case *ast.Link:
			dest = node.Destination
		case *ast.Image:
			dest = node.Destination
		default:
			return ast.WalkContinue, nil
		}
		span, ok := links.destSpans[n]
		if !ok || span.Start < 0 || span.Stop > len(source) || !bytes.Equal(source[span.Start:span.Stop], dest) {
			return ast.WalkContinue, nil
		}
		target := string(dest)
		idx, ok := matcher.match(target)
		if !ok || idx >= len(urls) || urls[idx] == "" {
			return ast.WalkContinue, nil
		}
		edits = append(edits, edit{start: span.Start, stop: span.Stop, url: urls[idx]})
		if !reported[target] {
			reported[target] = true
			rewrites = append(rewrites, attachmentLinkRewrite{Target: target, URL: urls[idx]})
		}
		return ast.WalkContinue, nil
	})
	if len(edits) == 0 {
		return body, nil
	}

	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var b strings.Builder
	last := 0
	for _, e := range edits {
		b.Write(source[last:e.start])
		b.WriteString(e.url)
		last = e.stop
	}
	b.Write(source[last:])
	return b.String(), rewrites
}

// previewAttachmentLinks rewrites body against placeholder URLs, before
// anything is uploaded, so the local-path lint judges the body that will
// actually be published: a link to a file attached in the same call is not a
// dead local link, and rejecting it after the upload would orphan the file.
func previewAttachmentLinks(body string, paths []string) string {
	placeholders := make([]string, len(paths))
	for i := range paths {
		placeholders[i] = fmt.Sprintf("https://attachment.invalid/%d", i)
	}
	rewritten, _ := rewriteAttachmentLinks(body, paths, placeholders)
	return rewritten
}

// applyAttachmentLinks rewrites body against the uploaded attachments (aligned
// with pending) and reports each rewritten target on w.
func applyAttachmentLinks(w io.Writer, body, field string, pending []pendingAttachment, uploaded []cli.AttachmentResponse) string {
	paths := make([]string, len(pending))
	urls := make([]string, len(pending))
	for i, att := range pending {
		paths[i] = att.path
		if i < len(uploaded) {
			urls[i] = attachmentBodyURL(uploaded[i])
		}
	}
	rewritten, rewrites := rewriteAttachmentLinks(body, paths, urls)
	for _, r := range rewrites {
		fmt.Fprintf(w, "Linked %s in the %s to the uploaded attachment %s\n", r.Target, field, r.URL)
	}
	return rewritten
}

// attachmentBodyURL is the URL a body embeds for an uploaded attachment: the
// server's durable markdown_url, or the per-attachment download path an older
// server would have built it from.
func attachmentBodyURL(att cli.AttachmentResponse) string {
	if att.MarkdownURL != "" {
		return att.MarkdownURL
	}
	if att.ID != "" {
		return "/api/attachments/" + att.ID + "/download"
	}
	return ""
}
