package daemon

import (
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/util"
)

// toolOutputPreviewBudget keeps the existing byte budget for tool_result
// previews. It does not limit the full output consumed by the agent.
const toolOutputPreviewBudget = 8192

// toolOutputPreview returns the longest complete-rune prefix within the budget,
// and whether anything was dropped to fit it. Normalize malformed UTF-8 and
// NULs with the existing persistence sanitizer first, so normalization cannot
// expand a byte-bounded preview past the budget.
//
// truncated describes the SOURCE record only: it says the remainder was never
// uploaded and cannot be recovered by scrolling or expanding. Whatever a client
// does to fit the preview on screen is a separate, reversible concern.
func toolOutputPreview(raw string) (preview string, truncated bool) {
	output := util.SanitizeTextForPostgres(raw)
	if len(output) <= toolOutputPreviewBudget {
		return output, false
	}

	end := toolOutputPreviewBudget
	// output is valid UTF-8, so at most three continuation bytes are skipped.
	for !utf8.RuneStart(output[end]) {
		end--
	}
	return output[:end], true
}
