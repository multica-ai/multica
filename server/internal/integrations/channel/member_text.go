package channel

import "strings"

// BreakMarkdownLinkAdjacency separates the standard inline Markdown "](" link
// adjacency in untrusted text. Platform-native markup needs its own guard.
// Text without "](" is returned byte-for-byte unchanged.
func BreakMarkdownLinkAdjacency(s string) string {
	return strings.ReplaceAll(s, "](", "] (")
}
