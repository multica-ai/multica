package daemon

import (
	"fmt"

	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// toolOutputPreviewBudget bounds the bytes of a single tool_result stored in
// task_message.output. It is a preview budget, not a size limit on what the
// agent saw: the agent consumed the whole output, and this is what the
// Execution log keeps of it.
//
// The value matches the byte cut this replaced, so existing transcripts stay
// comparable and the change is not also a silent capacity change.
const toolOutputPreviewBudget = 8192

// toolOutputTruncatedNotice is appended, in-band, to every truncated preview.
//
// It exists for one deployment case: a new daemon reporting to a server that
// predates the structured output_truncated / output_original_bytes fields. That
// server drops the fields, and without this line its UI would show a
// naturally-ending prefix with nothing marking the loss — the exact silent
// truncation this work removes. Its bytes are reserved inside the budget rather
// than appended after it, so the persisted preview never exceeds the budget.
//
// Deliberately not written as "truncated: N bytes": the generic credential rule
// in package redact matches KEYWORD[=:]VALUE shapes, and our own metadata
// should not be shaped like something a redactor is meant to eat.
const toolOutputTruncatedNotice = "\n[… truncated, %d bytes total]"

// toolOutputImageOmitted replaces a structured image result that does not fit
// the budget. Byte-slicing base64 produces neither a viewable image nor
// readable text, so the whole block is dropped in favour of a description.
const toolOutputImageOmitted = "[… image result omitted, %d bytes total]"

// toolOutputPreview converts a raw tool_result into what gets persisted.
//
// Returns the preview, whether it was truncated, and the original size in
// bytes. The size is measured on the logical output — after any transport
// unwrapping — so the number means the same thing regardless of which provider
// produced it, and it carries no content.
//
// truncated is a definite answer in both directions. A caller that reports
// false is asserting the output is complete, which is what lets the UI
// distinguish "complete" from "we don't know" (an older daemon that sends no
// field at all).
//
// isStructuredImage marks output the adapter has already recognised as an image
// content block. Such output is kept whole or replaced whole — never cut.
func toolOutputPreview(raw string, isStructuredImage bool) (preview string, truncated bool, originalBytes int) {
	// Measure before normalising: the caller asked how big the output was, not
	// how big its repaired form is.
	originalBytes = len(raw)

	// Normalise first so every path below operates on storable text. This is the
	// one place where "short output is byte-for-byte unchanged" cannot hold:
	// input that is not valid UTF-8, or that carries a NUL, has to change to be
	// storable at all. Normalisation wins.
	//
	// Called unconditionally rather than behind a utf8.ValidString check: NUL is
	// *valid* UTF-8, so that check would wave it through, and the helper already
	// fast-paths clean strings back unchanged.
	norm := util.SanitizeTextForPostgres(raw)

	if isStructuredImage {
		// An image that fits is left exactly as it is, so the transcript keeps
		// rendering the picture it renders today. Only an oversized one is
		// replaced, and then wholly.
		if len(norm) <= toolOutputPreviewBudget {
			return norm, false, originalBytes
		}
		return fmt.Sprintf(toolOutputImageOmitted, originalBytes), true, originalBytes
	}

	// Short-output fast path. The budget check runs against the REDACTED length
	// because redaction is what finally lands in the database and it can grow
	// text — "TOKEN=x" is 7 bytes and its placeholder is 21, so an 8 KiB input
	// of short credentials expands well past the budget. Judging the raw length
	// here would store an over-budget row while reporting truncated=false.
	//
	// Ordinary output — which is almost all output — is unaffected by redaction
	// and is returned unchanged, byte for byte.
	if len(norm) <= toolOutputPreviewBudget && len(redact.Text(norm)) <= toolOutputPreviewBudget {
		return norm, false, originalBytes
	}

	notice := fmt.Sprintf(toolOutputTruncatedNotice, originalBytes)
	room := toolOutputPreviewBudget - len(notice)
	if room > len(norm) {
		// Reachable whenever the input is short but redaction expanded it past
		// the budget; without the clamp this slices beyond the string.
		room = len(norm)
	}
	if room < 0 {
		room = 0
	}

	// Cut to the budget, then to a rune boundary, so the partial-secret matchers
	// below run on valid UTF-8 rather than on a fragment ending mid-rune.
	window := util.TrimToRuneBoundary(norm[:room])

	// Drop any trailing text that could be the head of a secret whose tail lies
	// outside the window, then redact what remains. Cutting first and redacting
	// afterwards would leave a straddling credential unmatched and therefore in
	// plaintext; redacting the entire input instead would be safe but costs
	// ~0.6us/byte on output whose tail is about to be discarded anyway.
	_, out := redact.PreviewPrefix(window)

	// Redaction may have grown the text past the budget. Re-clamp, and realign:
	// this second cut is by bytes and can land inside a rune again.
	if len(out) > room {
		out = util.TrimToRuneBoundary(out[:room])
	}

	return out + notice, true, originalBytes
}
