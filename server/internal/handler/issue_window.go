package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
)

const (
	issueWindowErrorCode = "issue_outside_creation_window"
	// Defensive ceiling for a malformed Cloud payload. Failing open is safer
	// than accepting an effectively unbounded recursive/list query.
	maxIssueWindowLimit = 1_000_000
)

// issueWindowPolicy is the validated, request-local interpretation of the
// Cloud gate. Invalid decisions fail open and do not touch the issue table.
type issueWindowPolicy struct {
	action         entitlement.Action
	limit          int64
	policyRevision int64
}

type IssueWindowUsageResponse struct {
	Action         string  `json:"action"`
	Used           *int64  `json:"used"`
	Limit          *int64  `json:"limit"`
	HasMore        *bool   `json:"has_more"`
	PolicyRevision *int64  `json:"policy_revision"`
	CalculatedAt   *string `json:"calculated_at"`
}

func (h *Handler) issueWindowPolicy(ctx context.Context, workspaceID pgtype.UUID) (issueWindowPolicy, bool) {
	if h.Entitlements == nil || !workspaceID.Valid {
		return issueWindowPolicy{}, false
	}
	decision := h.Entitlements.Gate(ctx, uuid.UUID(workspaceID.Bytes), entitlement.GateIssueWindow)
	gate := decision.Gate
	if gate.Action == entitlement.ActionOff {
		return issueWindowPolicy{}, false
	}
	if (gate.Action != entitlement.ActionObserve && gate.Action != entitlement.ActionEnforce) ||
		gate.Limit == nil || *gate.Limit <= 0 || *gate.Limit > maxIssueWindowLimit {
		// Cloud is the source of truth for the limit. A malformed decision is
		// fail-open, matching the entitlement client's unavailable behavior.
		return issueWindowPolicy{}, false
	}
	return issueWindowPolicy{
		action:         gate.Action,
		limit:          int64(*gate.Limit),
		policyRevision: decision.PolicyRevision,
	}, true
}

// issueWindowVisibleSetSQL returns the tenant-scoped query for the newest N
// issues by immutable workspace issue number plus every ancestor needed to
// render those issues. Ancestors supplement the base N and never expose their
// other children.
func issueWindowVisibleSetSQL(workspaceRef, limitRef string) string {
	return fmt.Sprintf(`WITH RECURSIVE issue_window_base AS MATERIALIZED (
		SELECT id, parent_issue_id
		FROM issue
		WHERE workspace_id = %s
		ORDER BY number DESC
		LIMIT %s
	), issue_window_visible(id, parent_issue_id) AS (
		SELECT id, parent_issue_id FROM issue_window_base
		UNION
		SELECT parent.id, parent.parent_issue_id
		FROM issue parent
		JOIN issue_window_visible child ON child.parent_issue_id = parent.id
		WHERE parent.workspace_id = %s
	)
	SELECT id FROM issue_window_visible`, workspaceRef, limitRef, workspaceRef)
}

func issueWindowIDPredicate(issueIDExpr, workspaceRef, limitRef string) string {
	return fmt.Sprintf("%s IN (\n\t%s\n)", issueIDExpr, issueWindowVisibleSetSQL(workspaceRef, limitRef))
}

func issueWindowPredicate(issueAlias, workspaceRef, limitRef string) string {
	return issueWindowIDPredicate(issueAlias+".id", workspaceRef, limitRef)
}

// appendIssueWindow applies enforcement to a dynamic issue query. Observe and
// off deliberately leave the legacy SQL byte-for-byte unchanged.
func appendIssueWindow(where []string, addArg func(any) string, policy issueWindowPolicy, workspaceRef, issueAlias string) []string {
	if policy.action != entitlement.ActionEnforce {
		return where
	}
	return append(where, issueWindowPredicate(issueAlias, workspaceRef, addArg(policy.limit)))
}

// issueIDsWithinWindow checks a response batch against one stable window
// snapshot. It is used both for direct enforcement and observe-mode telemetry.
func (h *Handler) issueIDsWithinWindow(ctx context.Context, workspaceID pgtype.UUID, policy issueWindowPolicy, issueIDs []pgtype.UUID) (bool, error) {
	if len(issueIDs) == 0 {
		return true, nil
	}
	visible, err := h.visibleIssueIDSet(ctx, workspaceID, policy, issueIDs)
	requested := make(map[pgtype.UUID]struct{}, len(issueIDs))
	for _, id := range issueIDs {
		requested[id] = struct{}{}
	}
	return len(visible) == len(requested), err
}

func (h *Handler) visibleIssueIDSet(ctx context.Context, workspaceID pgtype.UUID, policy issueWindowPolicy, issueIDs []pgtype.UUID) (map[pgtype.UUID]struct{}, error) {
	visible := make(map[pgtype.UUID]struct{}, len(issueIDs))
	if len(issueIDs) == 0 {
		return visible, nil
	}
	query := fmt.Sprintf(`SELECT requested.id
	FROM unnest($3::uuid[]) requested(id)
	WHERE %s`, issueWindowPredicate("requested", "$1", "$2"))
	rows, err := h.DB.Query(ctx, query, workspaceID, policy.limit, issueIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		visible[id] = struct{}{}
	}
	return visible, rows.Err()
}

// issueWindowVisibleIDsUpTo returns the complete visible set only when it fits
// within maxIDs. The extra row detects ancestor expansion without loading an
// unbounded UUID slice into the application.
func (h *Handler) issueWindowVisibleIDsUpTo(ctx context.Context, workspaceID pgtype.UUID, policy issueWindowPolicy, maxIDs int64) ([]pgtype.UUID, bool, error) {
	query := fmt.Sprintf("SELECT id FROM (\n%s\n) bounded_issue_window LIMIT $3", issueWindowVisibleSetSQL("$1", "$2"))
	rows, err := h.DB.Query(ctx, query, workspaceID, policy.limit, maxIDs+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	ids := make([]pgtype.UUID, 0, maxIDs)
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, false, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if int64(len(ids)) > maxIDs {
		return nil, false, nil
	}
	return ids, true, nil
}

func (h *Handler) recordIssueWindow(action entitlement.Action, surface, result string) {
	if h.Metrics != nil {
		h.Metrics.RecordIssueWindowDecision(string(action), surface, result)
	}
}

// observeIssueWindow records whether this response would have changed under
// enforcement, without changing the response itself. Database failures remain
// fail-open and are represented as a bounded metric outcome.
func (h *Handler) observeIssueWindow(ctx context.Context, workspaceID pgtype.UUID, policy issueWindowPolicy, issueIDs []pgtype.UUID, surface string) {
	if policy.action != entitlement.ActionObserve || len(issueIDs) == 0 {
		return
	}
	allVisible, err := h.issueIDsWithinWindow(ctx, workspaceID, policy, issueIDs)
	if err != nil {
		slog.Warn("observe issue creation window failed", "error", err, "surface", surface)
		h.recordIssueWindow(policy.action, surface, "error")
		return
	}
	if allVisible {
		h.recordIssueWindow(policy.action, surface, "allowed")
		return
	}
	h.recordIssueWindow(policy.action, surface, "would_block")
}

// authorizeIssueWindow runs only after the issue was loaded inside the caller's
// workspace. This preserves cross-workspace 404s while returning a product-
// actionable response for a same-workspace issue outside an enforced window.
// Once Cloud has supplied a valid enforce policy, a database error is an
// authorization uncertainty and therefore fails closed. That is deliberately
// different from an unavailable or malformed Cloud policy, which fails open
// before any window query runs.
func (h *Handler) authorizeIssueWindow(w http.ResponseWriter, r *http.Request, issueID, workspaceID pgtype.UUID, surface string) bool {
	policy, enabled := h.issueWindowPolicy(r.Context(), workspaceID)
	if !enabled {
		return true
	}
	allVisible, err := h.issueIDsWithinWindow(r.Context(), workspaceID, policy, []pgtype.UUID{issueID})
	if err != nil {
		if policy.action == entitlement.ActionObserve {
			slog.Warn("observe issue creation window failed", "error", err, "surface", surface)
			h.recordIssueWindow(policy.action, surface, "error")
			return true
		}
		h.recordIssueWindow(policy.action, surface, "error")
		writeError(w, http.StatusInternalServerError, "failed to check issue access")
		return false
	}
	if allVisible {
		h.recordIssueWindow(policy.action, surface, "allowed")
		return true
	}
	if policy.action == entitlement.ActionObserve {
		h.recordIssueWindow(policy.action, surface, "would_block")
		return true
	}
	h.recordIssueWindow(policy.action, surface, "blocked")
	writeJSON(w, http.StatusPaymentRequired, map[string]any{
		"error":           issueWindowErrorCode,
		"message":         "This issue is outside the workspace's recently created issue window.",
		"limit":           policy.limit,
		"policy_revision": policy.policyRevision,
	})
	return false
}

// GetIssueWindowUsage returns a bounded used/limit snapshot for Billing. The
// query reads at most limit+1 index entries, so a large workspace never pays
// for a full COUNT(*). Off and malformed policies perform no issue-table read.
func (h *Handler) GetIssueWindowUsage(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	policy, enabled := h.issueWindowPolicy(r.Context(), workspaceID)
	if !enabled {
		writeJSON(w, http.StatusOK, IssueWindowUsageResponse{Action: string(entitlement.ActionOff)})
		return
	}
	var sampled int64
	err := h.DB.QueryRow(r.Context(), `SELECT COUNT(*)::bigint
		FROM (
			SELECT 1
			FROM issue
			WHERE workspace_id = $1
			ORDER BY number DESC
			LIMIT $2
		) recent`, workspaceID, policy.limit+1).Scan(&sampled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue window usage")
		return
	}
	used := sampled
	hasMore := sampled > policy.limit
	if hasMore {
		used = policy.limit
	}
	calculatedAt := time.Now().UTC().Format(time.RFC3339)
	writeJSON(w, http.StatusOK, IssueWindowUsageResponse{
		Action:         string(policy.action),
		Used:           &used,
		Limit:          &policy.limit,
		HasMore:        &hasMore,
		PolicyRevision: &policy.policyRevision,
		CalculatedAt:   &calculatedAt,
	})
}
