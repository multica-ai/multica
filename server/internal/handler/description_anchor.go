package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"
)

// DescriptionAnchor positions count UTF-16 code units in the shared client's
// rendered-text index (block boundaries/newlines included, controls excluded).
// Snapshots are untrusted plain text, not HTML or an instruction to an agent.
// The owning issue is checked against the comment's already authorized issue.
type DescriptionAnchor struct {
	Version    int    `json:"version"`
	ID         string `json:"id"`
	IssueID    string `json:"issue_id"`
	Quote      string `json:"quote"`
	Start      int    `json:"start"`
	Prefix     string `json:"prefix"`
	Suffix     string `json:"suffix"`
	SourceText string `json:"source_text"`
}

func validateDescriptionAnchors(anchors []DescriptionAnchor, issueID, commentType string) ([]byte, error) {
	if len(anchors) == 0 {
		return []byte("[]"), nil
	}
	if commentType != "comment" || len(anchors) > 20 {
		return nil, fmt.Errorf("description anchors require a plain comment and at most 20 selections")
	}
	seen := map[string]bool{}
	for _, a := range anchors {
		quote := utf16.Encode([]rune(a.Quote))
		text := utf16.Encode([]rune(a.SourceText))
		if a.Version != 1 || a.IssueID != issueID || a.ID == "" || len(a.ID) > 128 || seen[a.ID] || a.Start < 0 || len(quote) > 4000 || strings.TrimSpace(a.Quote) == "" || (a.SourceText != "" && a.Start > len(text)-len(quote)) || strings.ContainsRune(a.SourceText, 0) || strings.ContainsRune(a.Quote, 0) || len(utf16.Encode([]rune(a.Prefix))) > 32 || len(utf16.Encode([]rune(a.Suffix))) > 32 {
			return nil, fmt.Errorf("invalid description anchor")
		}
		seen[a.ID] = true
		// Older saved drafts did not capture the full rendered source. Keep
		// their quote/context/hint as explicit outdated metadata; only a
		// positively identified live draft is hydrated with a new snapshot.
		if a.SourceText == "" {
			continue
		}

		// Compare encoded units directly so even a supplementary Unicode character
		// before a selection has precisely the browser's offset semantics.
		equal := func(x, y []uint16) bool {
			if len(x) != len(y) {
				return false
			}
			for i := range x {
				if x[i] != y[i] {
					return false
				}
			}
			return true
		}
		if !equal(text[a.Start:a.Start+len(quote)], quote) {
			return nil, fmt.Errorf("description anchor quote does not match its snapshot")
		}
		pre := utf16.Encode([]rune(a.Prefix))
		suf := utf16.Encode([]rune(a.Suffix))
		end := a.Start + len(quote)
		if a.Start < len(pre) || end+len(suf) > len(text) || !equal(text[a.Start-len(pre):a.Start], pre) || !equal(text[end:end+len(suf)], suf) {
			return nil, fmt.Errorf("description anchor context does not match its snapshot")
		}
		seen[a.ID] = true
	}
	data, err := json.Marshal(anchors)
	if err != nil {
		return nil, err
	}
	if len(data) > 1024*1024 {
		return nil, fmt.Errorf("description anchor snapshots exceed 1 MiB")
	}
	return data, nil
}

func descriptionAnchorsFromJSON(data []byte, deleted bool) []DescriptionAnchor {
	if deleted {
		return nil
	}
	var anchors []DescriptionAnchor
	if json.Unmarshal(data, &anchors) != nil {
		return nil
	}
	return anchors
}
