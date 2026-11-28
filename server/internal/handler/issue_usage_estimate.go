package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
)

// issueUsageEstimate is the estimated cost for GET /api/issues/{id}/usage, so
// the CLI shows the same figure as the issue page. Priced per run and model
// from the backend rate table (metrics.EstimateUsageCost).
func (h *Handler) issueUsageEstimate(ctx context.Context, issueID pgtype.UUID) (obsmetrics.UsageEstimate, error) {
	rows, err := h.Queries.ListIssueTaskUsage(ctx, issueID)
	if err != nil {
		return obsmetrics.UsageEstimate{}, err
	}
	usage := make([]obsmetrics.UsageRow, 0, len(rows))
	for _, r := range rows {
		row := obsmetrics.UsageRow{
			Provider:         r.Provider,
			Model:            r.Model,
			InputTokens:      r.InputTokens,
			OutputTokens:     r.OutputTokens,
			CacheReadTokens:  r.CacheReadTokens,
			CacheWriteTokens: r.CacheWriteTokens,
		}
		if r.CostUsdTicks.Valid {
			ticks := r.CostUsdTicks.Int64
			row.CostUSDTicks = &ticks
		}
		usage = append(usage, row)
	}
	return obsmetrics.EstimateUsageCost(usage), nil
}
