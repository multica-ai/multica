// CEREBRO-PATCH(runtime-approval-issue): JEH-1152 — auto-create / reuse an
// "Approve runtime" issue when a non-admin's agent-create gets gated on the
// group runtime allowlist. Net-new fork file in the upstream-zone path. Lives
// next to group_permissions_cerebro.go so the gate helper can call into it
// without crossing package boundaries.
package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// runtimeApprovalIssueOriginType is stamped into the issue's origin_type so
// the dedupe lookup (GetOpenRuntimeApprovalIssue) can find an existing open
// approval request for the same runtime in O(1). Keep this string stable —
// changing it would orphan every existing in-flight approval issue.
const runtimeApprovalIssueOriginType = "runtime_approval"

// runtimeAccessDeniedMessage is the human-readable copy the UI shows when a
// member's agent-create gets gated on the runtime allowlist. JEH-1152 — the
// admin reading the auto-created approval issue sees the same framing.
const runtimeAccessDeniedMessage = "This runtime must be approved by your group's admin before you can create agents with it."

// runtimeAccessDeniedCode is the machine-readable error code on the 403
// response body. The frontend keys off this to render the rich toast (issue
// link + new copy) instead of the generic ApiError fallback.
const runtimeAccessDeniedCode = "runtime_not_approved_by_group"

// approvalIssueRef is the shape surfaced on the 403 response body so the
// frontend can render a link to the approval issue. Mirrors the minimal slice
// of IssueResponse the dialog needs — id for navigation, identifier for the
// chip text, title for the tooltip / aria-label.
type approvalIssueRef struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

// runtimeAccessDeniedBody is the structured 403 response. `Error` keeps the
// existing string field so older clients (parsing `body.error`) still get a
// readable message; `Code` + `ApprovalIssue` are additive for clients that
// know to look for them.
type runtimeAccessDeniedBody struct {
	Error         string            `json:"error"`
	Code          string            `json:"code"`
	ApprovalIssue *approvalIssueRef `json:"approval_issue,omitempty"`
}

// writeRuntimeAccessDenied writes the 403 response with the structured body.
// approval may be nil — when issue creation failed (DB hiccup) the user still
// sees the new error copy, just without the link.
func writeRuntimeAccessDenied(w http.ResponseWriter, approval *approvalIssueRef) {
	writeJSON(w, http.StatusForbidden, runtimeAccessDeniedBody{
		Error:         runtimeAccessDeniedMessage,
		Code:          runtimeAccessDeniedCode,
		ApprovalIssue: approval,
	})
}

// ensureRuntimeApprovalIssue finds an existing open approval issue for the
// (workspace, runtime) pair, or creates a new one. requesterID is the user
// who attempted the agent-create; agentName is the proposed agent name (may
// be empty when the request didn't supply one yet — the description omits
// the agent line in that case).
//
// Returns (ref, nil) on success. Returns (nil, err) on DB failure — callers
// should NOT block the 403 response on this; the gate still denies the
// create, the user just sees the message without the link.
func (h *Handler) ensureRuntimeApprovalIssue(
	ctx context.Context,
	workspaceID pgtype.UUID,
	runtimeID pgtype.UUID,
	requesterID pgtype.UUID,
	agentName string,
) (*approvalIssueRef, error) {
	if !workspaceID.Valid || !runtimeID.Valid {
		return nil, errors.New("invalid workspace or runtime id")
	}

	// Dedupe: prefer the existing open issue so admins aren't drowned in
	// duplicates when many members request the same runtime.
	existing, err := h.Queries.GetOpenRuntimeApprovalIssue(ctx, db.GetOpenRuntimeApprovalIssueParams{
		WorkspaceID: workspaceID,
		OriginID:    runtimeID,
	})
	if err == nil {
		return h.refForIssue(ctx, existing), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("lookup approval issue: %w", err)
	}

	runtime, err := h.Queries.GetAgentRuntime(ctx, runtimeID)
	if err != nil {
		// Caller (the gate) already validated workspace + UUID format; a missing
		// runtime here means something raced (admin deleted it mid-request) —
		// skip the approval-issue side-effect rather than creating an
		// "Approve runtime: <unknown>" issue.
		return nil, fmt.Errorf("load runtime: %w", err)
	}

	admins, err := h.Queries.ListWorkspaceAdminUserIDs(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list admins: %w", err)
	}

	requester, _ := h.Queries.GetUser(ctx, requesterID) // best-effort; name used in description only

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	issueNumber, err := qtx.IncrementIssueCounter(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("increment issue counter: %w", err)
	}

	title := fmt.Sprintf("Approve runtime: %s", runtime.Name)
	description := buildApprovalIssueDescription(runtime, requester, agentName)

	// Assignee = first admin (deterministic by created_at via the underlying
	// member query). Falls back to unassigned when the workspace somehow has
	// no admins — still creates the issue so it shows up in the workspace
	// inbox; admins surface anyway via the subscriber list below.
	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if len(admins) > 0 {
		assigneeType = pgtype.Text{String: "member", Valid: true}
		assigneeID = admins[0]
	}

	issue, err := qtx.CreateIssueWithOrigin(ctx, db.CreateIssueWithOriginParams{
		WorkspaceID:   workspaceID,
		Title:         title,
		Description:   pgtype.Text{String: description, Valid: true},
		Status:        "todo",
		Priority:      "medium",
		AssigneeType:  assigneeType,
		AssigneeID:    assigneeID,
		CreatorType:   "member",
		CreatorID:     requesterID,
		ParentIssueID: pgtype.UUID{},
		Position:      0,
		DueDate:       pgtype.Timestamptz{},
		Number:        issueNumber,
		ProjectID:     pgtype.UUID{},
		OriginType:    pgtype.Text{String: runtimeApprovalIssueOriginType, Valid: true},
		OriginID:      runtimeID,
	})
	if err != nil {
		return nil, fmt.Errorf("create approval issue: %w", err)
	}

	// Subscribe admins + the requester. Admins so they all see updates; the
	// requester so the approval issue appears in their inbox once the admin
	// resolves it. ON CONFLICT DO NOTHING in AddIssueSubscriber dedupes safely.
	subscribers := make([]pgtype.UUID, 0, len(admins)+1)
	subscribers = append(subscribers, admins...)
	if requesterID.Valid {
		subscribers = append(subscribers, requesterID)
	}
	for _, uid := range subscribers {
		if !uid.Valid {
			continue
		}
		if err := qtx.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID:  issue.ID,
			UserType: "member",
			UserID:   uid,
			Reason:   "manual",
		}); err != nil {
			return nil, fmt.Errorf("subscribe admin: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit approval issue: %w", err)
	}

	prefix := h.getIssuePrefix(ctx, workspaceID)
	identifier := prefix + "-" + strconv.Itoa(int(issue.Number))

	// Broadcast issue:created so open clients see the new issue without a
	// manual refresh. Reusing publishToAudience keeps the event shape
	// identical to the regular CreateIssue handler — same payload key,
	// same audience semantics (nil = whole workspace, since approval issues
	// are workspace-visible).
	resp := issueToResponse(issue, prefix)
	h.publishToAudience(
		protocol.EventIssueCreated,
		uuidToString(workspaceID),
		"member",
		uuidToString(requesterID),
		map[string]any{"issue": resp},
		h.audienceForIssue(ctx, issue),
	)

	slog.Info("runtime approval issue created",
		"workspace_id", uuidToString(workspaceID),
		"runtime_id", uuidToString(runtimeID),
		"issue_id", uuidToString(issue.ID),
		"requester_id", uuidToString(requesterID),
	)

	return &approvalIssueRef{
		ID:         uuidToString(issue.ID),
		Identifier: identifier,
		Title:      issue.Title,
	}, nil
}

// refForIssue builds the response payload for an already-loaded issue. Used
// by the dedupe path where we re-link to an existing open approval issue.
func (h *Handler) refForIssue(ctx context.Context, issue db.Issue) *approvalIssueRef {
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	return &approvalIssueRef{
		ID:         uuidToString(issue.ID),
		Identifier: prefix + "-" + strconv.Itoa(int(issue.Number)),
		Title:      issue.Title,
	}
}

// buildApprovalIssueDescription composes the issue body. Kept in its own
// function so the formatting is unit-testable without an HTTP round-trip.
func buildApprovalIssueDescription(runtime db.AgentRuntime, requester db.User, agentName string) string {
	requesterLine := "unknown user"
	if requester.ID.Valid {
		switch {
		case requester.Name != "":
			requesterLine = requester.Name
		case requester.Email != "":
			requesterLine = requester.Email
		}
	}

	out := "## Runtime approval requested\n\n"
	out += fmt.Sprintf("**%s** tried to create an agent on a runtime that is not yet approved for any of their groups.\n\n", requesterLine)
	out += "### Runtime\n\n"
	out += fmt.Sprintf("- **Name:** %s\n", runtime.Name)
	out += fmt.Sprintf("- **ID:** `%s`\n", uuidToString(runtime.ID))
	if runtime.Provider != "" {
		out += fmt.Sprintf("- **Provider:** %s\n", runtime.Provider)
	}
	if runtime.RuntimeMode != "" {
		out += fmt.Sprintf("- **Mode:** %s\n", runtime.RuntimeMode)
	}
	if agentName != "" {
		out += "\n### Agent the user tried to create\n\n"
		out += fmt.Sprintf("- **Name:** %s\n", agentName)
	}
	out += "\n### Next steps\n\n"
	out += "Add the runtime to at least one group's runtime allowlist (Settings → Groups → Runtimes), or reject this request by setting the issue to *cancelled*.\n"
	return out
}

// runtimeAccessGateDeny is the wrapper the cerebro runtime-access gate calls
// once it has decided to deny. It best-effort-creates the approval issue,
// then writes the structured 403. Pulled out of cerebroRequireRuntimeAccess
// so the gate's happy-path stays a clean one-liner and the test suite can
// drive this directly without spinning up an HTTP server.
//
// agentName may be empty (gate callers that don't have a request body, e.g.
// future use from update-agent). When empty, the description omits the
// agent line — the runtime is still the primary subject of the approval.
func (h *Handler) runtimeAccessGateDeny(
	ctx context.Context,
	w http.ResponseWriter,
	workspaceID string,
	runtimeID pgtype.UUID,
	requesterUserID string,
	agentName string,
) {
	var requesterUUID pgtype.UUID
	if requesterUserID != "" {
		if u, err := util.ParseUUID(requesterUserID); err == nil {
			requesterUUID = u
		}
	}
	wsUUID, _ := util.ParseUUID(workspaceID)

	var approval *approvalIssueRef
	if wsUUID.Valid && runtimeID.Valid {
		ref, err := h.ensureRuntimeApprovalIssue(ctx, wsUUID, runtimeID, requesterUUID, agentName)
		if err != nil {
			// Log and continue — the 403 must still go out. Surfacing the
			// approval link is a nice-to-have, not a contract.
			slog.Warn("runtime approval issue side-effect failed",
				"error", err,
				"workspace_id", workspaceID,
				"runtime_id", uuidToString(runtimeID),
			)
		}
		approval = ref
	}
	writeRuntimeAccessDenied(w, approval)
}

