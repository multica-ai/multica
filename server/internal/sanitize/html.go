package sanitize

import (
	"github.com/microcosm-cc/bluemonday"
)

// policy is a shared bluemonday policy that allows safe Markdown HTML while
// stripping dangerous elements (script, iframe, object, embed, style, on*).
var policy *bluemonday.Policy

func init() {
	policy = bluemonday.UGCPolicy()
	// Remove elements that should never appear in user-generated Markdown.
	policy.AllowElements("div", "span")
	policy.AllowAttrs("data-type", "data-href", "data-filename").OnElements("div")
	policy.AllowAttrs("class").Globally()
}

// HTML sanitizes user-provided HTML/Markdown content, stripping dangerous
// tags (script, iframe, object, embed, etc.) and event-handler attributes.
func HTML(input string) string {
	return policy.Sanitize(input)
}
