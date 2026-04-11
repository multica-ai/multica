package sanitize

import (
	"regexp"

	"github.com/microcosm-cc/bluemonday"
)

// httpURL matches only http:// and https:// URLs — blocks javascript:, data:, etc.
var httpURL = regexp.MustCompile(`^https?://`)

// policy is a shared bluemonday policy that allows safe Markdown HTML while
// stripping dangerous elements (script, iframe, object, embed, style, on*).
//
// Note: bluemonday operates on raw text, so HTML inside Markdown code blocks
// (e.g. ```<script>```) will also be stripped. This is an acceptable trade-off
// for defense-in-depth — the primary sanitization happens in the frontend via
// rehype-sanitize which understands the Markdown AST.
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

// HTML sanitizes user-provided HTML/Markdown content, stripping dangerous
// tags (script, iframe, object, embed, etc.) and event-handler attributes.
func HTML(input string) string {
	return policy.Sanitize(input)
}
