package main

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
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
		if _, dup := m.byPath[keys[0]]; dup {
			// The same file attached twice; the first upload wins and the
			// duplicate must not make its bare name ambiguous.
			continue
		}
		for _, key := range keys {
			m.byPath[key] = i
		}
		base := filepath.Base(p)
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
	if unescaped := string(util.UnescapePunctuations([]byte(raw))); unescaped != raw {
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
		if i, ok := m.byPath[key]; ok {
			return i, true
		}
	}
	cleaned := filepath.Clean(local)
	if !filepath.IsAbs(cleaned) && !strings.ContainsRune(cleaned, filepath.Separator) {
		if idx := m.byName[cleaned]; len(idx) == 1 {
			return idx[0], true
		}
	}
	return -1, false
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
	doc := goldmark.New().Parser().Parse(text.NewReader(source))

	var codeSpans []text.Segment
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && n.Kind() == ast.KindCodeSpan {
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					codeSpans = append(codeSpans, t.Segment)
				}
			}
		}
		return ast.WalkContinue, nil
	})

	type edit struct {
		start, stop int
		url         string
	}
	var edits []edit
	claimed := map[int]bool{}
	var rewrites []attachmentLinkRewrite
	reported := map[string]bool{}
	// Post-order: an image nested in a link label claims its own destination
	// before the enclosing link searches past it for the outer one.
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			return ast.WalkContinue, nil
		}
		var link *ast.Link
		switch node := n.(type) {
		case *ast.Link:
			link = node
		case *ast.Image:
			link = &ast.Link{}
			link.Destination, link.Reference = node.Destination, node.Reference
		default:
			return ast.WalkContinue, nil
		}
		// Reference-style links keep their destination in a definition
		// elsewhere in the body; they are left for the lint.
		if link.Reference != nil || len(link.Destination) == 0 || n.Pos() < 0 {
			return ast.WalkContinue, nil
		}
		target := string(link.Destination)
		idx, ok := matcher.match(target)
		if !ok || idx >= len(urls) || urls[idx] == "" {
			return ast.WalkContinue, nil
		}
		start := locateLinkDestination(source, n.Pos(), link.Destination, claimed, codeSpans)
		if start < 0 {
			return ast.WalkContinue, nil
		}
		claimed[start] = true
		edits = append(edits, edit{start: start, stop: start + len(link.Destination), url: urls[idx]})
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

// locateLinkDestination returns the byte offset of a link's destination,
// searching from the link's start for the first unclaimed occurrence that
// directly follows the `](` closing its label. The parser does not record the
// destination's own position, and the same text may also appear in the label.
func locateLinkDestination(source []byte, from int, dest []byte, claimed map[int]bool, codeSpans []text.Segment) int {
	for from < len(source) {
		i := bytes.Index(source[from:], dest)
		if i < 0 {
			return -1
		}
		start := from + i
		if !claimed[start] && !insideSegments(start, codeSpans) && followsLinkOpen(source, start) {
			return start
		}
		from = start + 1
	}
	return -1
}

func followsLinkOpen(source []byte, start int) bool {
	j := start - 1
	if j >= 0 && source[j] == '<' {
		j--
	}
	for j >= 0 && (source[j] == ' ' || source[j] == '\t' || source[j] == '\n' || source[j] == '\r') {
		j--
	}
	return j >= 1 && source[j] == '(' && source[j-1] == ']'
}

func insideSegments(pos int, segments []text.Segment) bool {
	for _, s := range segments {
		if pos >= s.Start && pos < s.Stop {
			return true
		}
	}
	return false
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
