package daemon

import (
	"github.com/multica-ai/multica/server/pkg/agent"
)

// contextUsageReporter decides which of a backend's context observations reach
// the transcript.
//
// A backend reports occupancy once per model response, which on a long run is
// hundreds of rows that mostly repeat the previous figure — the window grows
// by a few hundred tokens per turn while a reader only ever acts on the
// change.
//
// Not safe for concurrent use; the transcript drain loop owns one per
// execution and is its only caller.
type contextUsageReporter struct {
	reportedUsed  int64
	reportedLimit int64
	reported      bool
}

// contextUsageReportStepFraction is the share of the window occupancy must
// move by before the change is worth a row: 2% of a 200K window is ~4K tokens,
// about one substantial file read.
const contextUsageReportStepFraction = 50

func (r *contextUsageReporter) shouldReport(info agent.ContextInfo) bool {
	// A backend only reports occupancy it can measure against a window, so
	// the step is always a share of one. An observation missing either half
	// is not reportable in the first place.
	if info.UsedTokens <= 0 || info.LimitTokens <= 0 {
		return false
	}
	if !r.reported {
		r.reported = true
		r.reportedUsed = info.UsedTokens
		r.reportedLimit = info.LimitTokens
		return true
	}
	// A corrected window is a change the reader must see even when occupancy
	// stands still: the same prompt against 200K and against 1M are different
	// statements. The backend re-states a measurement when the runtime's own
	// window answer arrives after it, and that correction has to get through
	// a throttle that only watches occupancy.
	if info.LimitTokens != r.reportedLimit {
		r.reportedUsed = info.UsedTokens
		r.reportedLimit = info.LimitTokens
		return true
	}
	step := info.LimitTokens / contextUsageReportStepFraction
	delta := info.UsedTokens - r.reportedUsed
	if delta < 0 {
		delta = -delta
	}
	if delta < step {
		return false
	}
	r.reportedUsed = info.UsedTokens
	r.reportedLimit = info.LimitTokens
	return true
}

// contextEventInput is the structured payload clients render from. Zero and
// empty fields are omitted so a reader can tell "not reported" from a real
// zero; a reported observation always carries both figures.
func contextEventInput(info agent.ContextInfo) map[string]any {
	input := map[string]any{}
	if info.UsedTokens > 0 {
		input["used_tokens"] = info.UsedTokens
	}
	if info.LimitTokens > 0 {
		input["limit_tokens"] = info.LimitTokens
	}
	if info.Model != "" {
		input["model"] = info.Model
	}
	return input
}
