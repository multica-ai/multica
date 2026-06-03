// Package costmeasure holds shared cost-saving measurement helpers used by
// both the daemon claim handler and the gateway runtime (FIR-2786).
package costmeasure

import (
	"github.com/multica-ai/multica/server/pkg/pricing"
)

const (
	MetricTokens = "tokens"

	// ReferencePricingModel prices daemon claim-time token savings when no run
	// model exists yet.
	ReferencePricingModel = "claude-sonnet-4-6"

	approxCharsPerToken = 4

	typicalIssueGetTokens    = int64(800)
	typicalCommentListTokens = int64(2400)
	typicalMembersTokens     = int64(400)
	typicalLabelsTokens      = int64(200)
	typicalBundledContextTokens = int64(3200)
)

// SnapshotTypicalTokens is the baseline when no snapshot body is available (shadow).
func SnapshotTypicalTokens() int64 {
	return typicalIssueGetTokens + typicalCommentListTokens
}

// SnapshotTokensEstimate returns baseline/effective tokens for snapshot_prompt.
func SnapshotTokensEstimate(snapshotChars int) (baseline, effective int64) {
	typical := SnapshotTypicalTokens()
	measured := CharsToTokens(snapshotChars)
	if measured > typical {
		baseline = measured
	} else {
		baseline = typical
	}
	return baseline, 0
}

// BundledTokensEstimate returns baseline/effective for bundled_read.
func BundledTokensEstimate() (baseline, effective int64) {
	baseline = typicalIssueGetTokens + typicalCommentListTokens + typicalMembersTokens + typicalLabelsTokens
	effective = typicalBundledContextTokens
	if effective > baseline {
		effective = baseline
	}
	return baseline, effective
}

func CharsToTokens(chars int) int64 {
	if chars <= 0 {
		return 0
	}
	return int64((chars + approxCharsPerToken/2) / approxCharsPerToken)
}

// TokenSavingCentsAtReference prices saved input tokens for dashboard $ hints.
func TokenSavingCentsAtReference(savedTokens int64) int64 {
	if savedTokens <= 0 {
		return 0
	}
	if !pricing.Known(ReferencePricingModel) {
		return 0
	}
	return pricing.ComputeCents(ReferencePricingModel, pricing.Usage{InputTokens: savedTokens})
}
