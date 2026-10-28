package lark

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Feishu interprets Markdown image destinations as uploaded image keys, not
// URLs. Initially keep agent images as ordinary links; bound uploaded images
// can subsequently replace them in this same card. Parsing avoids rewriting code,
// escaped examples, nested labels, or reference definitions by guesswork.
func markdownImageLinks(markdown string) string {
	source := []byte(markdown)
	doc := goldmark.DefaultParser().Parse(text.NewReader(source))
	var out strings.Builder
	last := 0
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		image, ok := node.(*ast.Image)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}
		pos := image.Pos()
		if pos >= last && pos+1 < len(source) && source[pos] == '!' && source[pos+1] == '[' {
			out.Write(source[last:pos])
			last = pos + 1
		}
		return ast.WalkContinue, nil
	})
	if last == 0 {
		return markdown
	}
	out.Write(source[last:])
	return out.String()
}
