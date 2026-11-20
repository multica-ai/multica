package daemon

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// contextUsageReporter decides which of a backend's context observations reach
// the transcript.
//
// A backend reports occupancy once per model response, which on a long run is
// hundreds of rows that mostly repeat the previous figure — the window grows
// by a few hundred tokens per turn while a reader only ever acts on the
// change. Lifecycle events (a compaction) are never throttled: they are the
// events, not samples of a value.
//
// Not safe for concurrent use; the transcript drain loop owns one per
// execution and is its only caller.
type contextUsageReporter struct {
	reportedUsed int64
	reported     bool
}

// contextUsageReportStepFraction is the share of the window occupancy must
// move by before the change is worth a row: 2% of a 200K window is ~4K tokens,
// about one substantial file read.
const contextUsageReportStepFraction = 50

// contextUsageReportStepTokens is the step used when no window is known. The
// row then carries an absolute figure only, so the threshold has to be one in
// absolute terms too.
const contextUsageReportStepTokens int64 = 4_000

func (r *contextUsageReporter) shouldReport(info agent.ContextInfo) bool {
	if info.Phase != agent.ContextPhaseUsage {
		return true
	}
	if info.UsedTokens <= 0 {
		return false
	}
	if !r.reported {
		r.reported = true
		r.reportedUsed = info.UsedTokens
		return true
	}
	step := contextUsageReportStepTokens
	if info.LimitTokens > 0 {
		step = info.LimitTokens / contextUsageReportStepFraction
	}
	delta := info.UsedTokens - r.reportedUsed
	if delta < 0 {
		delta = -delta
	}
	if delta < step {
		return false
	}
	r.reportedUsed = info.UsedTokens
	return true
}

// contextEventInput is the structured payload clients render from. Zero and
// empty fields are omitted so a reader can tell "not reported" from a real
// zero — an unknown window must not arrive as `limit_tokens: 0`.
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
	if info.Phase != agent.ContextPhaseUsage {
		input["phase"] = info.Phase
	}
	if info.PreTokens > 0 {
		input["pre_tokens"] = info.PreTokens
	}
	if info.Trigger != "" {
		input["trigger"] = info.Trigger
	}
	return input
}

// formatContextEvent renders an observation as one line of English prose.
// Clients localize from the structured payload; this is the fallback reading
// and what a copied transcript shows.
func formatContextEvent(info agent.ContextInfo) string {
	if info.Phase == agent.ContextPhaseCompacted {
		parts := []string{"Context compacted"}
		if info.PreTokens > 0 {
			parts = append(parts, fmt.Sprintf("%s before", formatTokenCount(info.PreTokens)))
		}
		if info.Trigger != "" {
			parts = append(parts, info.Trigger)
		}
		if len(parts) == 1 {
			return parts[0]
		}
		return parts[0] + " (" + strings.Join(parts[1:], ", ") + ")"
	}
	if info.LimitTokens > 0 {
		return fmt.Sprintf("Context %s/%s (%d%%)",
			formatTokenCount(info.UsedTokens),
			formatTokenCount(info.LimitTokens),
			contextUsagePercent(info.UsedTokens, info.LimitTokens),
		)
	}
	return fmt.Sprintf("Context %s", formatTokenCount(info.UsedTokens))
}

// contextUsagePercent rounds toward the nearest whole percent, and never
// rounds a non-empty window down to 0% or a non-full one up to 100%: the two
// extremes are read as states ("fresh", "out of room"), not as figures.
func contextUsagePercent(used, limit int64) int64 {
	if limit <= 0 || used <= 0 {
		return 0
	}
	pct := (used*100 + limit/2) / limit
	if pct <= 0 {
		return 1
	}
	if pct >= 100 && used < limit {
		return 99
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// formatTokenCount abbreviates to one decimal place from 1K up, which is where
// every figure in a context window lands. Whole thousands drop the decimal so
// a declared window reads as "200K", not "200.0K".
func formatTokenCount(tokens int64) string {
	switch {
	case tokens >= 1_000_000:
		return trimTokenDecimal(float64(tokens)/1_000_000) + "M"
	case tokens >= 1_000:
		return trimTokenDecimal(float64(tokens)/1_000) + "K"
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

func trimTokenDecimal(value float64) string {
	s := fmt.Sprintf("%.1f", value)
	return strings.TrimSuffix(s, ".0")
}
