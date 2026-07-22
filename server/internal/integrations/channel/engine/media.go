package engine

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// This file is the inbound-media seam. A channel whose platform delivers
// media as opaque download handles (DingTalk downloadCode) registers a
// MediaIngester on its ResolverSet; the Router calls it AFTER dedup,
// identity, membership and session resolution so unauthorized or replayed
// messages never trigger a download. Channels without inbound media leave
// the seam nil and the pipeline is untouched.

// StagedMedia is one fetched-and-persisted media object, staged in object
// storage by the MediaIngester. The attachment row is NOT created here —
// that happens inside the binder's append transaction so the row and the
// chat_message commit atomically; the pre-minted ID doubles as the
// attachment id and the storage key stem.
type StagedMedia struct {
	ID          pgtype.UUID // pre-minted UUIDv7: attachment id == storage key stem
	StorageKey  string      // workspaces/<wsid>/<uuid><ext>
	URL         string      // persistable URL returned by storage upload
	Filename    string      // server-generated image-<n>.<ext> (platforms may send no name)
	ContentType string      // sniffed and allowlisted by the ingester
	SizeBytes   int64
}

// IngestParams carries the inputs for MediaIngester.Ingest.
type IngestParams struct {
	// Installation carries the adapter's platform row, which holds the
	// credentials the download API needs.
	Installation ResolvedInstallation
	// WorkspaceID scopes the storage key prefix.
	WorkspaceID pgtype.UUID
	// Media are the pending references from the inbound message, in
	// Segment.MediaIdx order.
	Media []channel.PendingMedia
}

// MediaIngester fetches every pending media item and persists the bytes to
// object storage. All-or-nothing: on any failure it cleans up its own
// already-uploaded objects (best-effort) and returns the error. Discard
// removes staged objects when a later pipeline step fails after a
// successful Ingest.
type MediaIngester interface {
	Ingest(ctx context.Context, p IngestParams) ([]StagedMedia, error)
	Discard(ctx context.Context, staged []StagedMedia)
}

// ComposeBody flattens Segments into the stored message body, replacing
// each media segment with a markdown image reference "![<filename>](<url>)"
// pointing at the staged object's own storage URL (never the provider's
// expiring download link). The UI surfaces issue/chat attachments only
// through markdown references in the content — the same contract the web
// upload flow follows — so a plain marker would leave the image invisible.
// No Segments → Text unchanged (plain-text fast path).
func ComposeBody(msg channel.InboundMessage, staged []StagedMedia) string {
	if len(msg.Segments) == 0 {
		return msg.Text
	}
	var b strings.Builder
	lastWasMarker := false
	for _, seg := range msg.Segments {
		if seg.Text != "" {
			// Separate a marker from following text with exactly one space,
			// without doubling whitespace the user already typed.
			if lastWasMarker && !startsWithSpace(seg.Text) {
				b.WriteByte(' ')
			}
			b.WriteString(seg.Text)
			lastWasMarker = false
			continue
		}
		if b.Len() > 0 && !endsWithSpace(b.String()) {
			b.WriteByte(' ')
		}
		if seg.MediaIdx >= 0 && seg.MediaIdx < len(staged) {
			sm := staged[seg.MediaIdx]
			b.WriteString("![" + sm.Filename + "](" + sm.URL + ")")
		} else {
			b.WriteString("[image]")
		}
		lastWasMarker = true
	}
	return strings.TrimSpace(b.String())
}

func endsWithSpace(s string) bool {
	return s != "" && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t')
}

func startsWithSpace(s string) bool {
	return s != "" && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t')
}
