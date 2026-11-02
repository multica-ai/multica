package channelmedia

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const sourceEnd = "<!-- /multica:channel-media-source -->"

var sourcePattern = regexp.MustCompile(`(?ms)^<!-- multica:channel-media-source:([0-9a-fA-F,-]+) -->\n\n(.*?)\n\n<!-- /multica:channel-media-source -->$`)

// Source retains a historical transcript as one attributed section, independently
// of reporter-owned attachment embeds. IDs are its bound attachment identities.
type Source struct {
	IDs      []string
	Content  string
	Markdown string
}

// SourceBlock adds merge provenance outside the literal source, where renderers
// can omit it without changing the quoted transcript.
func SourceBlock(ids []string, content string) string {
	return "<!-- multica:channel-media-source:" + strings.Join(ids, ",") + " -->\n\n" + content + "\n\n" + sourceEnd
}

// Sources returns complete, valid provenance sections. Comments in the quoted
// transcript cannot close a section because delimiters must start at column zero.
func Sources(markdown string) []Source {
	var sources []Source
	for _, match := range sourcePattern.FindAllStringSubmatch(markdown, -1) {
		ids := strings.Split(match[1], ",")
		valid := true
		for n, id := range ids {
			parsed, err := uuid.Parse(id)
			if err != nil {
				valid = false
				break
			}
			ids[n] = parsed.String()
		}
		if valid {
			sources = append(sources, Source{IDs: ids, Content: match[2], Markdown: match[0]})
		}
	}
	return sources
}
