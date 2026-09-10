package handler

import (
	"strings"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
)

type sourceContextDescriptionAttachmentReference struct {
	AttachmentID string
	Filename     string
}

type sourceContextMarkdownReference struct {
	Start    int
	End      int
	URLStart int
	URLEnd   int
	Filename string
	URL      string
}

// sourceContextDescriptionProjection separates editor-owned attachment nodes
// from the rest of the Markdown. The snapshot attachment inventory remains the
// authority for historical downloads, but it is not itself user-visible issue
// content: only references embedded in the description participate in this
// comparison.
func sourceContextDescriptionProjection(
	description *string,
	capturedAttachments, currentAttachments []service.SourceContextAttachment,
) (string, []sourceContextDescriptionAttachmentReference) {
	if description == nil || *description == "" {
		return "", []sourceContextDescriptionAttachmentReference{}
	}
	content := strings.ReplaceAll(*description, "\r\n", "\n")
	knownFilenames := sourceContextKnownAttachmentFilenames(capturedAttachments, currentAttachments)
	aliases := sourceContextAttachmentAliases(capturedAttachments, currentAttachments)
	references := make([]sourceContextDescriptionAttachmentReference, 0)
	var body strings.Builder
	last := 0
	for offset := 0; offset < len(content); {
		candidate, ok := scanSourceContextMarkdownReference(content, offset)
		if !ok {
			offset++
			continue
		}
		attachmentID := sourceContextDescriptionAttachmentID(candidate.URL, knownFilenames)
		if attachmentID == "" {
			offset = candidate.End
			continue
		}
		if canonicalID, ok := aliases[strings.ToLower(attachmentID)]; ok {
			attachmentID = canonicalID
		}
		removeStart, removeEnd := sourceContextStandaloneReferenceRange(content, candidate.Start, candidate.End)
		if removeStart < last {
			removeStart = candidate.Start
			removeEnd = candidate.End
		}
		body.WriteString(content[last:removeStart])
		last = removeEnd
		filename := unescapeSourceContextMarkdownLabel(candidate.Filename)
		if filename == "" {
			filename = knownFilenames[strings.ToLower(attachmentID)]
		}
		references = append(references, sourceContextDescriptionAttachmentReference{
			AttachmentID: attachmentID,
			Filename:     filename,
		})
		offset = candidate.End
	}
	body.WriteString(content[last:])
	return strings.TrimSpace(body.String()), references
}

func sourceContextKnownAttachmentFilenames(groups ...[]service.SourceContextAttachment) map[string]string {
	result := make(map[string]string)
	for _, items := range groups {
		for _, item := range items {
			if item.ID != "" {
				result[strings.ToLower(item.ID)] = item.Filename
			}
			if item.SourceAttachmentID != "" {
				result[strings.ToLower(item.SourceAttachmentID)] = item.Filename
			}
		}
	}
	return result
}

func sourceContextAttachmentAliases(groups ...[]service.SourceContextAttachment) map[string]string {
	result := make(map[string]string)
	for _, items := range groups {
		for _, item := range items {
			canonicalID := item.SourceAttachmentID
			if canonicalID == "" {
				canonicalID = item.ID
			}
			canonicalID = strings.ToLower(canonicalID)
			if canonicalID == "" {
				continue
			}
			result[canonicalID] = canonicalID
			if item.ID != "" {
				result[strings.ToLower(item.ID)] = canonicalID
			}
		}
	}
	return result
}

// sourceContextMarkdownContentProjection preserves the authored Markdown but
// replaces recognized attachment destinations with their original attachment
// identity. A persisted clone and the live source then compare equal without
// weakening ordinary text or external-link comparisons.
func sourceContextMarkdownContentProjection(
	content string,
	capturedAttachments, currentAttachments []service.SourceContextAttachment,
) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	knownFilenames := sourceContextKnownAttachmentFilenames(capturedAttachments, currentAttachments)
	aliases := sourceContextAttachmentAliases(capturedAttachments, currentAttachments)
	var projected strings.Builder
	last := 0
	for offset := 0; offset < len(content); {
		candidate, ok := scanSourceContextMarkdownReference(content, offset)
		if !ok {
			offset++
			continue
		}
		attachmentID := sourceContextDescriptionAttachmentID(candidate.URL, knownFilenames)
		canonicalID, recognized := aliases[strings.ToLower(attachmentID)]
		if attachmentID == "" || !recognized {
			offset = candidate.End
			continue
		}
		projected.WriteString(content[last:candidate.URLStart])
		projected.WriteString("source-context-attachment:")
		projected.WriteString(canonicalID)
		last = candidate.URLEnd
		offset = candidate.End
	}
	projected.WriteString(content[last:])
	return projected.String()
}

func sourceContextDescriptionAttachmentID(rawURL string, knownFilenames map[string]string) string {
	if id, ok := util.AttachmentIDFromDownloadURL(rawURL); ok {
		return id
	}
	stableURL := strings.ToLower(strings.SplitN(strings.SplitN(strings.TrimSpace(rawURL), "#", 2)[0], "?", 2)[0])
	for id := range knownFilenames {
		if strings.Contains(stableURL, id) {
			return id
		}
	}
	return ""
}

func scanSourceContextMarkdownReference(content string, start int) (sourceContextMarkdownReference, bool) {
	if start > 0 && content[start-1] == '\\' {
		return sourceContextMarkdownReference{}, false
	}
	prefixLength := 0
	switch {
	case strings.HasPrefix(content[start:], "!file["):
		prefixLength = len("!file[")
	case strings.HasPrefix(content[start:], "!["):
		prefixLength = len("![")
	case strings.HasPrefix(content[start:], "["):
		prefixLength = len("[")
	default:
		return sourceContextMarkdownReference{}, false
	}
	labelStart := start + prefixLength
	labelEnd := sourceContextUnescapedDelimiter(content, labelStart, ']')
	if labelEnd < 0 || labelEnd+1 >= len(content) || content[labelEnd+1] != '(' {
		return sourceContextMarkdownReference{}, false
	}
	urlStart := labelEnd + 2
	urlEnd := sourceContextUnescapedDelimiter(content, urlStart, ')')
	if urlEnd < 0 {
		return sourceContextMarkdownReference{}, false
	}
	return sourceContextMarkdownReference{
		Start: start, End: urlEnd + 1, URLStart: urlStart, URLEnd: urlEnd,
		Filename: content[labelStart:labelEnd], URL: content[urlStart:urlEnd],
	}, true
}

func sourceContextUnescapedDelimiter(content string, start int, delimiter byte) int {
	escaped := false
	for index := start; index < len(content); index++ {
		if escaped {
			escaped = false
			continue
		}
		if content[index] == '\\' {
			escaped = true
			continue
		}
		if content[index] == delimiter {
			return index
		}
	}
	return -1
}

func sourceContextStandaloneReferenceRange(content string, start, end int) (int, int) {
	lineStart := strings.LastIndex(content[:start], "\n") + 1
	lineEndRelative := strings.Index(content[end:], "\n")
	lineEnd := len(content)
	if lineEndRelative >= 0 {
		lineEnd = end + lineEndRelative + 1
	}
	lineContentEnd := lineEnd
	if lineContentEnd > 0 && content[lineContentEnd-1] == '\n' {
		lineContentEnd--
	}
	if strings.TrimSpace(content[lineStart:start]) != "" || strings.TrimSpace(content[end:lineContentEnd]) != "" {
		return start, end
	}

	// Remove one adjacent blank separator with a standalone attachment block.
	// This keeps the ordinary Markdown projection identical to what the editor
	// serializes after the block itself is deleted.
	if lineStart > 0 {
		previousLineEnd := lineStart - 1
		previousLineStart := strings.LastIndex(content[:previousLineEnd], "\n") + 1
		if strings.TrimSpace(content[previousLineStart:previousLineEnd]) == "" {
			lineStart = previousLineStart
			return lineStart, lineEnd
		}
	}
	if lineEnd < len(content) {
		nextLineEndRelative := strings.Index(content[lineEnd:], "\n")
		nextLineEnd := len(content)
		if nextLineEndRelative >= 0 {
			nextLineEnd = lineEnd + nextLineEndRelative + 1
		}
		nextContentEnd := nextLineEnd
		if nextContentEnd > lineEnd && content[nextContentEnd-1] == '\n' {
			nextContentEnd--
		}
		if strings.TrimSpace(content[lineEnd:nextContentEnd]) == "" {
			lineEnd = nextLineEnd
		}
	}
	return lineStart, lineEnd
}

func unescapeSourceContextMarkdownLabel(value string) string {
	replacer := strings.NewReplacer(`\[`, `[`, `\]`, `]`, `\(`, `(`, `\)`, `)`, `\\`, `\`)
	return replacer.Replace(value)
}

func sourceContextDescriptionAttachmentChanges(
	captured, current []sourceContextDescriptionAttachmentReference,
) []sourceContextDescriptionAttachmentChange {
	currentByID := make(map[string][]sourceContextDescriptionAttachmentReference)
	for _, reference := range current {
		currentByID[reference.AttachmentID] = append(currentByID[reference.AttachmentID], reference)
	}
	matchedCurrent := make(map[string]int)
	changes := make([]sourceContextDescriptionAttachmentChange, 0)
	for _, reference := range captured {
		matchIndex := matchedCurrent[reference.AttachmentID]
		matches := currentByID[reference.AttachmentID]
		if matchIndex >= len(matches) {
			changes = append(changes, sourceContextDescriptionAttachmentChange{
				Kind: "removed", AttachmentID: reference.AttachmentID, Filename: reference.Filename,
			})
			continue
		}
		currentReference := matches[matchIndex]
		matchedCurrent[reference.AttachmentID] = matchIndex + 1
		if reference.Filename != currentReference.Filename {
			changes = append(changes, sourceContextDescriptionAttachmentChange{
				Kind: "replaced", AttachmentID: reference.AttachmentID, Filename: currentReference.Filename,
				PreviousFilename: reference.Filename,
			})
		}
	}
	seenCurrent := make(map[string]int)
	for _, reference := range current {
		seenCurrent[reference.AttachmentID]++
		if seenCurrent[reference.AttachmentID] <= matchedCurrent[reference.AttachmentID] {
			continue
		}
		changes = append(changes, sourceContextDescriptionAttachmentChange{
			Kind: "added", AttachmentID: reference.AttachmentID, Filename: reference.Filename,
		})
	}
	return changes
}
