package dingtalk

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"

	"github.com/multica-ai/multica/server/internal/util"
)

// sealedInputQuote keeps the owned input as the source of attribution, including
// quoted history. Materialized channel images use authenticated internal URLs
// that cannot render in DingTalk; display their original placeholder instead.
// Only the exact image form emitted by channel ingestion is replaced. Parsing
// preserves literal Markdown in code, escaped images and ordinary links.
func sealedInputQuote(body string) string {
	if !strings.Contains(body, "/api/attachments/") {
		return body
	}
	source := []byte(body)
	doc := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(source))
	var quote strings.Builder
	position := 0
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		img, ok := node.(*ast.Image)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		destination := string(img.Destination)
		if !strings.HasPrefix(destination, "/api/attachments/") {
			return ast.WalkContinue, nil
		}
		if _, ok := util.AttachmentIDFromDownloadURL(destination); !ok {
			return ast.WalkContinue, nil
		}
		literal := "![](" + destination + ")"
		start := img.Pos()
		if !strings.HasPrefix(body[start:], literal) {
			return ast.WalkContinue, nil
		}
		quote.WriteString(body[position:start])
		quote.WriteString(dingtalkImagePlaceholder)
		position = start + len(literal)
		return ast.WalkSkipChildren, nil
	})
	quote.WriteString(body[position:])
	return quote.String()
}
