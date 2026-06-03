package handler

// CEREBRO-PATCH(issue-context-bundle): the "bundled_read" cost saving
// (FIR-2384 step 2). Serves a single combined "issue context" read — issue +
// recent thread + workspace members + issue labels — so an agent can gather
// everything it needs to start work in ONE platform call instead of the
// separate `multica issue get` + `comment list` (+ `workspace members` +
// `issue label list`) round-trips it makes today. Exposed to agents as
// `multica issue context <id>`.
// CEREBRO-PATCH(cost-savings-context-tokens): FIR-2572 — bundled payload → context_tokens rows.
//
// Measurement: when the caller is a daemon task (X-Task-ID present, validated
// by the AllowTaskScopeForIssue middleware) and the workspace has bundled_read
// turned "on", one measurement row is written per run — keyed to the task, so
// the dashboard counts REAL bundled usage, not an assumed save. "shadow" is
// measured at task-claim time instead (see daemon_cost_bundled_cerebro.go),
// where the would-save is recorded without changing behaviour. "off" records
// nothing. The command itself always works regardless of mode — the mode only
// governs whether the saving is credited.

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// issueContextRecentThreads bounds how many of the most recently active threads
// the bundle inlines. Mirrors the `--recent 20` default the comment-triggered
// start prompt steers agents toward, so the bundle carries the same amount of
// thread context the agent would otherwise have fetched itself.
const issueContextRecentThreads = 20

// IssueContextResponse is the combined payload returned by
// GET /api/issues/{id}/context. Each section reuses the exact shape of its
// dedicated endpoint (`GetIssue`, `ListComments`, `ListMembersWithUser`,
// `ListLabelsForIssue`) so the bundle is a drop-in for the four calls it
// replaces and agents do not have to learn a new schema.
type IssueContextResponse struct {
	Issue    IssueResponse            `json:"issue"`
	Comments []CommentResponse        `json:"comments"`
	Members  []MemberWithUserResponse `json:"members"`
	Labels   []LabelResponse          `json:"labels"`
}

// GetIssueContext serves the bundled issue context in one call. Authorization,
// workspace scoping, and 404 semantics are delegated to loadIssueForUser — the
// same gate GetIssue / ListComments use — so a task token can only read its own
// bound issue (enforced again by AllowTaskScopeForIssue on the route).
func (h *Handler) GetIssueContext(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	resp := IssueContextResponse{
		Issue:    h.buildIssueContextIssue(r, issue),
		Comments: h.buildIssueContextComments(r, issue),
		Members:  h.buildIssueContextMembers(r, issue.WorkspaceID),
		Labels:   h.buildIssueContextLabels(r, issue),
	}

	// Persona redaction parity with GetIssue (JEH-1173): mask the issue body for
	// callers without the pii grant. On a hard failure it writes the response
	// itself, so bail without double-writing.
	if !h.maskIssueForCaller(w, r, issue, &resp.Issue) {
		return
	}

	h.recordBundledReadUsage(r, issue, bundledPayloadChars(resp))
	writeJSON(w, http.StatusOK, resp)
}

// buildIssueContextIssue assembles the issue section to match GetIssue's body
// (core fields + labels + dependencies + reactions + attachments). It is a
// deliberate, documented duplication of GetIssue's assembly: keeping it in this
// cerebro file avoids modifying the upstream GetIssue handler. If GetIssue's
// shape changes, this mirror must be updated alongside it.
func (h *Handler) buildIssueContextIssue(r *http.Request, issue db.Issue) IssueResponse {
	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)

	detailLabels := h.labelsByIssue(r.Context(), issue.WorkspaceID, []pgtype.UUID{issue.ID})[uuidToString(issue.ID)]
	if detailLabels == nil {
		detailLabels = []LabelResponse{}
	}
	resp.Labels = &detailLabels

	deps := h.loadDependencies(r, issue, prefix)
	resp.Blocks = &deps.Blocks
	resp.BlockedBy = &deps.BlockedBy

	if reactions, err := h.Queries.ListIssueReactions(r.Context(), issue.ID); err == nil && len(reactions) > 0 {
		resp.Reactions = make([]IssueReactionResponse, len(reactions))
		for i, rx := range reactions {
			resp.Reactions[i] = issueReactionToResponse(rx)
		}
	}

	if attachments, err := h.Queries.ListAttachmentsByIssue(r.Context(), db.ListAttachmentsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	}); err == nil && len(attachments) > 0 {
		resp.Attachments = make([]AttachmentResponse, len(attachments))
		for i, a := range attachments {
			resp.Attachments[i] = h.attachmentToResponse(a)
		}
		resp.Attachments = h.maskAttachmentsForCaller(r, uuidToString(issue.WorkspaceID), attachments, resp.Attachments)
	}

	return resp
}

// buildIssueContextComments returns the most recently active threads on the
// issue, reusing the same fetch + serialization ListComments uses for its
// `--recent` path. Best-effort: a fetch error yields an empty slice rather than
// failing the whole bundle — the agent can still fall back to `comment list`.
func (h *Handler) buildIssueContextComments(r *http.Request, issue db.Issue) []CommentResponse {
	result, err := h.fetchCommentsForList(r.Context(), fetchCommentsArgs{
		Issue:   issue,
		RecentN: issueContextRecentThreads,
	})
	if err != nil {
		slog.Warn("issue context: list comments failed", "issue_id", uuidToString(issue.ID), "error", err)
		return []CommentResponse{}
	}

	commentIDs := make([]pgtype.UUID, len(result.Comments))
	for i, c := range result.Comments {
		commentIDs[i] = c.ID
	}
	grouped := h.groupReactions(r, commentIDs)
	groupedAtt := h.groupAttachments(r, commentIDs)

	out := make([]CommentResponse, len(result.Comments))
	for i, c := range result.Comments {
		cid := uuidToString(c.ID)
		out[i] = commentToResponse(c, grouped[cid], groupedAtt[cid])
	}
	return out
}

// buildIssueContextMembers returns the workspace members (for @mention lookup),
// reusing ListMembersWithUser's serialization + persona masking. Best-effort.
func (h *Handler) buildIssueContextMembers(r *http.Request, workspaceID pgtype.UUID) []MemberWithUserResponse {
	members, err := h.Queries.ListMembersWithUser(r.Context(), workspaceID)
	if err != nil {
		slog.Warn("issue context: list members failed", "workspace_id", uuidToString(workspaceID), "error", err)
		return []MemberWithUserResponse{}
	}
	out := make([]MemberWithUserResponse, len(members))
	for i, m := range members {
		out[i] = MemberWithUserResponse{
			ID:                       uuidToString(m.ID),
			WorkspaceID:              uuidToString(m.WorkspaceID),
			UserID:                   uuidToString(m.UserID),
			Role:                     m.Role,
			CreatedAt:                timestampToString(m.CreatedAt),
			Name:                     m.UserName,
			Email:                    m.UserEmail,
			AvatarURL:                textToPtr(m.UserAvatarUrl),
			BudgetEnforcementEnabled: m.BudgetEnforcementEnabled,
		}
	}
	return h.maskMembersListForCaller(r, uuidToString(workspaceID), out)
}

// buildIssueContextLabels returns the labels attached to the issue, reusing
// ListLabelsForIssue's query + serialization. Best-effort.
func (h *Handler) buildIssueContextLabels(r *http.Request, issue db.Issue) []LabelResponse {
	labels, err := h.Queries.ListLabelsByIssue(r.Context(), db.ListLabelsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Warn("issue context: list labels failed", "issue_id", uuidToString(issue.ID), "error", err)
		return []LabelResponse{}
	}
	return labelsToResponse(labels)
}

// recordBundledReadUsage credits the bundled_read saving for THIS run when the
// caller is a daemon task and the workspace has the saving "on". The save is
// real: the agent fetched issue + comments + members + labels in this single
// call instead of the bundledReadBaseline separate calls. Idempotent per run
// (UNIQUE task_id + saving_key), so multiple `issue context` calls in one run
// still count as one bundled run. shadow/off do not record here — shadow's
// would-save is written at claim time; off is untouched. Best-effort: a
// measurement failure never affects the served response.
// bundledPayloadChars approximates the bundled response size for token scoring.
func bundledPayloadChars(resp IssueContextResponse) int64 {
	b, err := json.Marshal(resp)
	if err != nil {
		return 0
	}
	return int64(len(b))
}

func (h *Handler) recordBundledReadUsage(r *http.Request, issue db.Issue, payloadChars int64) {
	if h.CerebroQueries == nil || !issue.WorkspaceID.Valid {
		return
	}
	ts := middleware.TaskScopeFromContext(r.Context())
	if ts.TaskID == "" {
		return // not a task-scoped call (e.g. a human running the command) — nothing to attribute
	}
	taskUUID, err := util.ParseUUID(ts.TaskID)
	if err != nil {
		return
	}
	bundledMode := h.bundledReadMode(r.Context(), issue.WorkspaceID)
	snapshotMode := h.snapshotSavingMode(r.Context(), issue.WorkspaceID)
	if !shouldCreditBundledReadUsage(bundledMode, snapshotMode) {
		return
	}
	if err := h.CerebroQueries.RecordCerebroCostOptimizationMeasurement(r.Context(), bundledReadMeasurementParams(issue.WorkspaceID, taskUUID, costSavingModeOn, payloadChars)); err != nil {
		slog.Warn("bundled-read cost-saving: record usage failed", "task_id", ts.TaskID, "error", err)
	}
}
