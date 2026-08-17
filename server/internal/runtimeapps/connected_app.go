package runtimeapps

import (
	"encoding/json"
	"strings"
)

// ConnectedApp is non-secret task-scoped metadata that tells the daemon which
// external app capabilities were actually mounted for a run.
type ConnectedApp struct {
	Provider    string `json:"provider"`
	ServerName  string `json:"server_name"`
	ToolkitSlug string `json:"toolkit_slug"`
	ToolkitName string `json:"toolkit_name,omitempty"`
}

// MCPOverlayResult carries both the secret-bearing MCP config overlay and the
// non-secret app list used to brief the agent.
type MCPOverlayResult struct {
	MCPOverlay    json.RawMessage
	ConnectedApps []ConnectedApp
}

// DisplayNameForToolkitSlug returns a compact human-readable label without
// making a catalog call in the enqueue path. Brand casing here is intentionally
// best-effort; the slug remains the functional identifier in every brief.
func DisplayNameForToolkitSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	switch slug {
	case "github":
		return "GitHub"
	case "gmail":
		return "Gmail"
	case "linkedin":
		return "LinkedIn"
	}
	words := strings.FieldsFunc(slug, func(r rune) bool {
		return r == '_' || r == '-'
	})
	if len(words) == 0 {
		return slug
	}
	for i, word := range words {
		words[i] = titleASCII(word)
	}
	return strings.Join(words, " ")
}

func titleASCII(s string) string {
	if s == "" {
		return ""
	}
	b := []byte(strings.ToLower(s))
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}
