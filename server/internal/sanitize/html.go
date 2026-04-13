package sanitize

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// httpURL matches only http:// and https:// URLs — blocks javascript:, data:, etc.
var httpURL = regexp.MustCompile(`^https?://`)

// policy is a shared bluemonday policy that allows safe Markdown HTML while
// stripping dangerous elements (script, iframe, object, embed, style, on*).
var policy *bluemonday.Policy

func init() {
	policy = bluemonday.UGCPolicy()
	policy.AllowElements("div", "span")
	// Allow file-card data attributes, but restrict data-href to http(s) only
	// to prevent javascript: and other dangerous URL schemes.
	policy.AllowAttrs("data-type", "data-filename").OnElements("div")
	policy.AllowAttrs("data-href").Matching(httpURL).OnElements("div")
	policy.AllowAttrs("class").OnElements("code", "div", "span", "pre")
}

// fencedCodeBlock matches ``` or ~~~ fenced code blocks (with optional language tag).
var fencedCodeBlock = regexp.MustCompile("(?m)^(```|~~~)[^\n]*\n[\\s\\S]*?\n(```|~~~)[ \t]*$")

// inlineCode matches backtick-delimited inline code spans.
// Ordered longest-delimiter-first so triple backticks match before doubles/singles.
var inlineCode = regexp.MustCompile("```[^`]+```|``[^`]+``|`[^`]+`")

// HTML sanitizes user-provided HTML/Markdown content, stripping dangerous
// tags (script, iframe, object, embed, etc.) and event-handler attributes.
//
// Code blocks and inline code spans are preserved verbatim so that bluemonday
// does not HTML-escape their contents (e.g. && → &amp;&amp;).
func HTML(input string) string {
	// 1. Extract fenced code blocks, replacing with unique placeholders.
	var blocks []string
	placeholder := func(i int) string { return fmt.Sprintf("\x00CODEBLOCK_%d\x00", i) }
	result := fencedCodeBlock.ReplaceAllStringFunc(input, func(m string) string {
		idx := len(blocks)
		blocks = append(blocks, m)
		return placeholder(idx)
	})

	// 2. Extract inline code spans.
	var inlines []string
	inlinePH := func(i int) string { return fmt.Sprintf("\x00INLINE_%d\x00", i) }
	result = inlineCode.ReplaceAllStringFunc(result, func(m string) string {
		idx := len(inlines)
		inlines = append(inlines, m)
		return inlinePH(idx)
	})

	// 3. Sanitize the non-code portions.
	result = policy.Sanitize(result)

	// 4. Restore inline code spans, then fenced code blocks.
	for i, code := range inlines {
		result = strings.Replace(result, inlinePH(i), code, 1)
	}
	for i, block := range blocks {
		result = strings.Replace(result, placeholder(i), block, 1)
	}

	return result
}
