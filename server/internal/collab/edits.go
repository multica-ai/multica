package collab

import "strings"

// TextEdit replaces OldText with NewText. OldText must occur exactly once, or
// at least once with ReplaceAll.
type TextEdit struct {
	OldText    string `json:"old_text"`
	NewText    string `json:"new_text"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

// EditFailure says which edit could not be applied and why: "not_found",
// "ambiguous" (with Matches) or "empty".
type EditFailure struct {
	Reason  string `json:"reason"`
	Index   int    `json:"index"`
	Matches int    `json:"matches,omitempty"`
}

// ApplyTextEdits applies edits in order, each against the text the previous
// ones left. It mirrors apps/collab/src/edits.ts, which applies the same
// edits to the live document; this copy serves docs nobody has open when the
// collaboration service is not configured.
func ApplyTextEdits(text string, edits []TextEdit) (string, *EditFailure) {
	for i, edit := range edits {
		if edit.OldText == "" {
			return "", &EditFailure{Reason: "empty", Index: i}
		}
		matches := strings.Count(text, edit.OldText)
		if matches == 0 {
			return "", &EditFailure{Reason: "not_found", Index: i}
		}
		if matches > 1 && !edit.ReplaceAll {
			return "", &EditFailure{Reason: "ambiguous", Index: i, Matches: matches}
		}
		if edit.ReplaceAll {
			text = strings.ReplaceAll(text, edit.OldText, edit.NewText)
		} else {
			text = strings.Replace(text, edit.OldText, edit.NewText, 1)
		}
	}
	return text, nil
}
