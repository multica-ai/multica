package statusnotify

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/issuestatus"
)

// Categories a workspace is most likely to want. These alias the canonical
// keys in internal/issuestatus rather than redeclaring the literals: a second
// copy of a durable status key is a fork waiting to happen.
const (
	CategoryBlocked  = issuestatus.Blocked
	CategoryInReview = issuestatus.InReview
)

// maxTitleRunes bounds the title carried into a notification. A title is
// user-supplied and unbounded; a platform that rejects an oversized message
// would fail the whole delivery, so it is clipped here rather than discovered
// at the wire.
const maxTitleRunes = 80

// TextRenderer renders a status change as plain text.
//
// Plain text rather than a platform card: OutboundMessage carries only Text
// today, so a card renderer would have nowhere to put its payload. The
// Capability argument is still honoured — a platform declaring CapRichCard can
// be given a richer body once OutboundMessage grows a card field, and the
// signature does not have to change for that.
type TextRenderer struct {
	// Labels maps a status category to its display name. A category with no
	// entry falls back to the raw key, which is readable enough and avoids
	// dropping a notification over a missing translation.
	Labels map[string]string
}

// Render implements Renderer.
func (r TextRenderer) Render(change Change, issueURL string, _ channel.Capability) (channel.OutboundMessage, error) {
	if change.Identifier == "" && change.Title == "" {
		return channel.OutboundMessage{}, fmt.Errorf("statusnotify: change carries neither identifier nor title")
	}

	var b strings.Builder
	b.WriteString("[")
	b.WriteString(change.Identifier)
	b.WriteString("] ")
	b.WriteString(clip(change.Title, maxTitleRunes))
	b.WriteString(" — ")
	b.WriteString(r.label(change.Category))

	if change.PrevStatus != "" {
		fmt.Fprintf(&b, "\n%s → %s", change.PrevStatus, change.Status)
	}
	if issueURL != "" {
		b.WriteString("\n")
		b.WriteString(issueURL)
	}
	return channel.OutboundMessage{Text: b.String()}, nil
}

func (r TextRenderer) label(category string) string {
	if name, ok := r.Labels[category]; ok && name != "" {
		return name
	}
	return category
}

// clip truncates on rune boundaries. Byte truncation would split a multi-byte
// character and emit a replacement glyph, which is exactly the kind of defect
// that only shows up in non-ASCII deployments.
func clip(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}
