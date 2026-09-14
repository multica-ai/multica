package dingtalk

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// Quotes need no escaping in Markdown text. Keeping them literal also preserves
// URL boundaries for GFM autolinking inside an escaped HTML source fragment.
var quotedHTMLEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// Selected text is provider source, including literal HTML and legal ||. Do not
// infer an opaque envelope or decode it from punctuation. Missing/unsupported
// snapshots are handled by renderDingTalkQuotedMessage instead.
//
// Escape raw HTML in the canonical Markdown shared by the agent and Web, where
// HTML rendering/sanitization would otherwise hide source such as <tag>. Parse
// source spans so code, Markdown links, autolinks and existing entities survive.
// This applies only to selected context, not the current input or Bot answers.
func escapeDingTalkQuotedHTML(body string) string {
	parser := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()
	for {
		source := []byte(body)
		doc := parser.Parse(text.NewReader(source))
		var escaped strings.Builder
		position := 0
		escape := func(segment text.Segment) {
			escaped.Write(source[position:segment.Start])
			escaped.WriteString(quotedHTMLEscaper.Replace(string(source[segment.Start:segment.Stop])))
			position = segment.Stop
		}
		_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			switch node := node.(type) {
			case *ast.RawHTML:
				for i := 0; i < node.Segments.Len(); i++ {
					escape(node.Segments.At(i))
				}
			case *ast.HTMLBlock:
				for i := 0; i < node.Lines().Len(); i++ {
					escape(node.Lines().At(i))
				}
				if node.HasClosure() {
					escape(node.ClosureLine)
				}
			}
			return ast.WalkContinue, nil
		})
		if position == 0 {
			return body
		}
		escaped.Write(source[position:])
		body = escaped.String()
		// Escaping an HTML block changes Markdown block boundaries and can
		// expose another raw tag in a lazy continuation. Normalize the result
		// until it contains no raw HTML nodes. Every pass removes at least one
		// literal '<' and introduces none, so progress is bounded by the source.
	}
}
