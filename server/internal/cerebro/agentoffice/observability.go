package agentoffice

// Context observability (FIR-1775 Phase 4 — "overblik og målinger"). The final
// brick of Agent Office: a read-only overview that answers three questions
// across a workspace or for one agent —
//
//   1. How often does an agent's context change?  (version counts, last change)
//   2. Who approves those changes?                 (approver leaderboard)
//   3. Where does drift concentrate?               (lint findings per agent)
//
// The change-frequency and approver numbers come from the aggregate queries in
// agent_context.sql; drift is the existing Phase 3 lint (lint.go), reused so the
// two surfaces can never disagree. This file holds the response shapes and the
// pure folding helpers; the HTTP wiring is in observability_handler.go.

import (
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// ChangeRequestCounts is the per-status tally of an agent's change requests.
type ChangeRequestCounts struct {
	Pending  int64 `json:"pending"`
	Approved int64 `json:"approved"`
	Rejected int64 `json:"rejected"`
	Merged   int64 `json:"merged"`
	Total    int64 `json:"total"`
}

// ApproverStat is one reviewer's decision counts on agent-context change
// requests. Name resolves inline (query-side join) so the overview never shows a
// bare UUID.
type ApproverStat struct {
	UserID   string `json:"user_id"`
	Name     string `json:"name"`
	Approved int64  `json:"approved"`
	Merged   int64  `json:"merged"`
	Rejected int64  `json:"rejected"`
	Total    int64  `json:"total"`
}

// DriftSummary is the severity breakdown of an agent's lint findings.
type DriftSummary struct {
	Total    int `json:"total"`
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Infos    int `json:"infos"`
}

// AgentObservabilityResponse is the per-agent overview (GET
// /api/agents/{id}/context/observability).
type AgentObservabilityResponse struct {
	AgentID         string              `json:"agent_id"`
	AgentName       string              `json:"agent_name"`
	ContextVersion  string              `json:"context_version"`
	VersionCount    int64               `json:"version_count"`
	VersionsLast30d int64               `json:"versions_last_30d"`
	LastChangedAt   *string             `json:"last_changed_at"`
	ChangeRequests  ChangeRequestCounts `json:"change_requests"`
	Approvers       []ApproverStat      `json:"approvers"`
	Drift           DriftSummary        `json:"drift"`
}

// AgentObservabilityRow is one agent's line in the workspace overview.
type AgentObservabilityRow struct {
	AgentID         string              `json:"agent_id"`
	AgentName       string              `json:"agent_name"`
	ContextVersion  string              `json:"context_version"`
	VersionCount    int64               `json:"version_count"`
	VersionsLast30d int64               `json:"versions_last_30d"`
	LastChangedAt   *string             `json:"last_changed_at"`
	ChangeRequests  ChangeRequestCounts `json:"change_requests"`
	DriftFindings   int                 `json:"drift_findings"`
}

// WorkspaceObservabilityTotals is the headline strip of the workspace overview.
type WorkspaceObservabilityTotals struct {
	Agents                int   `json:"agents"`
	VersionsLast30d       int64 `json:"versions_last_30d"`
	PendingChangeRequests int64 `json:"pending_change_requests"`
	DriftFindings         int   `json:"drift_findings"`
}

// WorkspaceObservabilityResponse is the whole-workspace overview (GET
// /api/agents/context/observability): headline totals, one row per agent
// (most-churned first), and the workspace-wide approver leaderboard.
type WorkspaceObservabilityResponse struct {
	Totals    WorkspaceObservabilityTotals `json:"totals"`
	Agents    []AgentObservabilityRow      `json:"agents"`
	Approvers []ApproverStat               `json:"approvers"`
}

// foldChangeRequestCounts adds one grouped (status, count) row into a
// ChangeRequestCounts, keeping Total in sync. Unknown statuses still count
// toward Total so a future status value never silently disappears.
func foldChangeRequestCounts(counts *ChangeRequestCounts, status string, count int64) {
	switch status {
	case "pending":
		counts.Pending += count
	case "approved":
		counts.Approved += count
	case "rejected":
		counts.Rejected += count
	case "merged":
		counts.Merged += count
	}
	counts.Total += count
}

// approverAccumulator folds grouped (reviewer, status, count) rows into a
// stable-ordered ApproverStat slice. Reviewers appear in first-seen order so the
// output is deterministic for tests and the UI.
type approverAccumulator struct {
	order []string
	byID  map[string]*ApproverStat
}

func newApproverAccumulator() *approverAccumulator {
	return &approverAccumulator{byID: map[string]*ApproverStat{}}
}

// add folds one grouped row. Only reviewed statuses (approved/merged/rejected)
// carry a reviewer, so pending rows never reach here.
func (a *approverAccumulator) add(userID, name, status string, count int64) {
	if userID == "" {
		return
	}
	stat, ok := a.byID[userID]
	if !ok {
		stat = &ApproverStat{UserID: userID, Name: name}
		a.byID[userID] = stat
		a.order = append(a.order, userID)
	}
	if stat.Name == "" && name != "" {
		stat.Name = name
	}
	switch status {
	case "approved":
		stat.Approved += count
	case "merged":
		stat.Merged += count
	case "rejected":
		stat.Rejected += count
	}
	stat.Total += count
}

// result returns the accumulated approvers in first-seen order (never nil, so it
// JSON-encodes as [] not null).
func (a *approverAccumulator) result() []ApproverStat {
	out := make([]ApproverStat, 0, len(a.order))
	for _, id := range a.order {
		out = append(out, *a.byID[id])
	}
	return out
}

// summarizeDrift buckets lint findings by severity.
func summarizeDrift(findings []LintFinding) DriftSummary {
	d := DriftSummary{Total: len(findings)}
	for _, f := range findings {
		switch f.Severity {
		case "error":
			d.Errors++
		case "warning":
			d.Warnings++
		case "info":
			d.Infos++
		}
	}
	return d
}

// versionStatsToRow maps a workspace version-stats query row into the response
// row's change-frequency fields (change requests + drift are filled by the
// handler).
func versionStatsToRow(v cerebrodb.ListAgentContextVersionStatsByWorkspaceRow, lastChanged *string) AgentObservabilityRow {
	return AgentObservabilityRow{
		AgentID:         util.UUIDToString(v.AgentID),
		AgentName:       v.AgentName,
		ContextVersion:  v.ContextVersion,
		VersionCount:    v.VersionCount,
		VersionsLast30d: v.VersionsLast30d,
		LastChangedAt:   lastChanged,
	}
}
