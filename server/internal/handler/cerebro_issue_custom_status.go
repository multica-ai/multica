package handler

// CEREBRO-PATCH(cerebro-issue-custom-status-enricher): FIR-1550 v2b — joins
// the per-issue custom_status sidecar onto the upstream issue response so
// agents and the UI see label + description alongside the base status.
//
// This file is cerebro-only (prefix matches the cerebro_*.go convention used
// by other cerebro hooks in this package) and is the single seam through
// which the upstream GetIssue handler enriches its response. Keeping the
// type and lookup logic together keeps the upstream-zone footprint small —
// the upstream IssueResponse struct only carries one CEREBRO-PATCH field
// and the GetIssue handler only carries one CEREBRO-PATCH call site.

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
)

// IssueCustomStatusPayload is the per-issue sidecar joined onto an issue
// response. Empty when the issue has no model-bound custom status (the
// project either has no model assigned or the issue is in default mode).
type IssueCustomStatusPayload struct {
	StatusModelID   string `json:"status_model_id"`
	CustomStatusKey string `json:"custom_status_key"`
	BaseStatus      string `json:"base_status"`
	Label           string `json:"label,omitempty"`
	Description     string `json:"description,omitempty"`
}

// cerebroLoadIssueCustomStatus fetches the sidecar row for an issue and
// joins it with the matching statusEntry from the model. Returns nil on any
// failure (no sidecar, missing model, missing entry) — the enrichment is
// best-effort and must never break the upstream response.
func (h *Handler) cerebroLoadIssueCustomStatus(ctx context.Context, issueID pgtype.UUID) *IssueCustomStatusPayload {
	if h.CerebroQueries == nil {
		return nil
	}
	side, err := h.CerebroQueries.GetCerebroIssueStatus(ctx, issueID)
	if err != nil {
		return nil
	}
	out := &IssueCustomStatusPayload{
		StatusModelID:   uuidToString(side.StatusModelID),
		CustomStatusKey: side.CustomStatusKey,
		BaseStatus:      side.BaseStatus,
	}
	if model, err := h.CerebroQueries.GetCerebroStatusModel(ctx, side.StatusModelID); err == nil {
		var entries []cerebroStatusEntryView
		if err := json.Unmarshal(model.Statuses, &entries); err == nil {
			for _, e := range entries {
				if e.Key == side.CustomStatusKey {
					out.Label = e.Label
					out.Description = e.Description
					break
				}
			}
		}
	}
	return out
}

// minimal statusEntry shape — we only need label + description here. Kept
// duplicated from the cerebro statusmodels package so the upstream handler
// doesn't have to import that package directly (avoids tangling the
// upstream-zone import graph with cerebro internals).
type cerebroStatusEntryView struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}
