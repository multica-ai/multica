package daemon

import (
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/util"
)

// toolOutputPreviewBudget keeps the existing byte budget for tool_result
// previews. It does not limit the full output consumed by the agent.
const toolOutputPreviewBudget = 8192

// toolOutputPreview returns the longest complete-rune prefix within the budget.
// Normalize malformed UTF-8 and NULs with the existing persistence sanitizer
// first, so normalization cannot expand a byte-bounded preview past the budget.
func toolOutputPreview(raw string) string {
	output := util.SanitizeTextForPostgres(raw)
	if len(output) <= toolOutputPreviewBudget {
		return output
	}

	end := toolOutputPreviewBudget
	// output is valid UTF-8, so at most three continuation bytes are skipped.
	for !utf8.RuneStart(output[end]) {
		end--
	}
	return output[:end]
}
