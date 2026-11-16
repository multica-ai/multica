package channelmedia

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const markerPrefix = "<!-- multica:channel-media:"

var markerPattern = regexp.MustCompile(`<!-- multica:channel-media:([0-9a-fA-F-]{36}) -->`)

// DownloadPath is the durable, authorization-aware attachment URL persisted in
// issue and chat Markdown.
func DownloadPath(id string) string {
	return "/api/attachments/" + id + "/download"
}

// Marker records that a Markdown attachment was materialized asynchronously by
// channel ingestion. Markdown renderers ignore the comment, while issue updates
// use it to distinguish a late channel-media write from an intentional edit.
func Marker(id string) string {
	return markerPrefix + id + " -->"
}

// Block renders one channel attachment plus its durable provenance marker.
func Block(id, filename string, image bool) string {
	downloadPath := DownloadPath(id)
	var markdown string
	if image {
		markdown = "![](" + downloadPath + ")"
	} else {
		label := strings.NewReplacer(
			"\\", "\\\\",
			"[", "\\[",
			"]", "\\]",
			"\r", " ",
			"\n", " ",
		).Replace(filename)
		if label == "" {
			label = "attachment"
		}
		markdown = "[" + label + "](" + downloadPath + ")"
	}
	return markdown + "\n\n" + Marker(id)
}

// MarkedIDs returns valid channel-media attachment ids in document order,
// de-duplicated. Invalid marker-shaped comments are ignored.
func MarkedIDs(markdown string) []string {
	matches := markerPattern.FindAllStringSubmatch(markdown, -1)
	ids := make([]string, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, match := range matches {
		parsed, err := uuid.Parse(match[1])
		if err != nil {
			continue
		}
		id := parsed.String()
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// HasMarker reports whether markdown already carries the provenance marker for
// id. It deliberately does not treat a bare attachment URL as provenance.
func HasMarker(markdown, id string) bool {
	return strings.Contains(markdown, Marker(id))
}

// Append adds a Markdown block using the same spacing contract as issue media
// materialization.
func Append(markdown, block string) string {
	if markdown == "" {
		return block
	}
	return markdown + "\n\n" + block
}
