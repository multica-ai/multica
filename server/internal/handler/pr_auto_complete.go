package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// PR merge automation (MUL-7429, MUL-7726).
//
// The whole rule, as users see it:
//
//   - A PR whose title or branch name carries an issue identifier is linked to
//     that issue, and so is one whose title or body puts it after a closing
//     keyword ("Closes MUL-1"). A member can also link or remove a PR by hand.
//     The keyword only links; it has no say in what the merge does.
//   - The workspace picks what a merge does: move the issue to one status (Done
//     by default, or any started or done status, custom ones included) or leave
//     it alone. When every PR linked to an issue is merged, the issue moves to
//     that status, unless someone turned the automation off for that one issue.
//
// The decision is evaluated only when a PR event touches the issue: a linked PR
// merges, a PR is linked, or a link is removed. Changing a setting, reopening an
// issue, or accepting it from Triage is not a PR event, so it never moves an
// issue by itself. That is what keeps a reopened issue open until new work
// lands, without any hidden per-issue switch.

// Decision states, shared with the issue page (see prAutoCompleteResponse).
// The names predate the configurable target and stay for installed clients.
const (
	prAutoCompleteNone              = "none"               // no linked PR
	prAutoCompleteWorkspaceDisabled = "workspace_disabled" // workspace leaves status alone
	prAutoCompleteIssueDisabled     = "issue_disabled"     // turned off for this issue
	prAutoCompleteTerminal          = "terminal"           // already done / cancelled
	prAutoCompleteTriage            = "triage"             // not accepted yet
	prAutoCompleteAtTarget          = "at_target"          // already in the target status
	prAutoCompleteWaiting           = "waiting"            // some PRs still open / draft
	prAutoCompleteNotMerged         = "not_merged"         // some PRs closed without merging
	prAutoCompleteAllMerged         = "all_merged"         // every linked PR merged
)

// Workspace settings keys. pr_auto_complete_enabled is the switch desktop
// clients before MUL-7726 still show and write; prMergeStatusFromSettings
// reads it when pr_merge_status is absent, and reconcilePRMergeSettings keeps
// the two in step on every write.
const (
	prMergeStatusKey        = "pr_merge_status"
	prAutoCompleteLegacyKey = "pr_auto_complete_enabled"
	// prMergeStatusNone is the value that leaves an issue's status alone.
	prMergeStatusNone = "none"
)

type prAutoCompleteDecision struct {
	State string
	// Target is the status a merge moves the issue to, or "" when the
	// workspace leaves status alone (or its choice no longer resolves).
	Target string
	// PRs the state is about: still open for waiting, closed-unmerged for
	// not_merged, every linked PR for all_merged.
	PRs []db.ListIssueLinkedPullRequestStatesRow
	// IssueDisabled is reported independently of State so the issue menu can
	// show the right toggle label even when another state wins.
	IssueDisabled bool
}

// prMergeStatusSetting reads the workspace's choice: "none", or the key of the
// status a merge moves an issue to. An unreadable settings blob means none:
// the setting authorizes a status write, so it must not default to the
// permissive side.
func prMergeStatusSetting(ws db.Workspace) string {
	if len(ws.Settings) == 0 {
		return issuestatus.Done
	}
	var s map[string]any
	if err := json.Unmarshal(ws.Settings, &s); err != nil {
		return prMergeStatusNone
	}
	return prMergeStatusFromSettings(s)
}

// prMergeStatusFromSettings is prMergeStatusSetting on decoded settings.
// Without pr_merge_status it follows the retired switch (a pod or client from
// before MUL-7726 may have written only that): off means none, else Done.
func prMergeStatusFromSettings(s map[string]any) string {
	raw, ok := s[prMergeStatusKey]
	if !ok || raw == nil {
		if prAutoCompleteLegacyOn(s) {
			return issuestatus.Done
		}
		return prMergeStatusNone
	}
	value, ok := raw.(string)
	if !ok {
		return prMergeStatusNone
	}
	if key := strings.ToLower(strings.TrimSpace(value)); key != "" {
		return key
	}
	return prMergeStatusNone
}

// prAutoCompleteLegacyOn reads the retired switch the way releases before
// MUL-7726 did: absent means on, anything but a boolean means off.
func prAutoCompleteLegacyOn(s map[string]any) bool {
	raw, ok := s[prAutoCompleteLegacyKey]
	if !ok || raw == nil {
		return true
	}
	on, ok := raw.(bool)
	return ok && on
}

// reconcilePRMergeSettings adjusts a settings write so desktop clients from
// before MUL-7726 keep working. Those clients show and flip only the retired
// switch, and send back the rest of the settings they hold. A flip of that
// switch becomes a choice here (off → none, on → Done). Every write then
// mirrors the switch from the effective choice, so an old client shows it
// correctly. stored may be nil when the current settings could not be read.
func reconcilePRMergeSettings(stored, incoming map[string]any) {
	if _, sent := incoming[prAutoCompleteLegacyKey]; sent && prAutoCompleteLegacyOn(incoming) != prAutoCompleteLegacyOn(stored) {
		if prAutoCompleteLegacyOn(incoming) {
			incoming[prMergeStatusKey] = issuestatus.Done
		} else {
			incoming[prMergeStatusKey] = prMergeStatusNone
		}
	}
	if prMergeStatusFromSettings(incoming) == prMergeStatusNone {
		incoming[prAutoCompleteLegacyKey] = false
	} else {
		delete(incoming, prAutoCompleteLegacyKey)
	}
}

// resolvePRMergeTarget returns the status a merge moves issues to in this
// workspace, or "" to leave status alone. The chosen key must still name a
// live started or done status other than Blocked; anything else (an archived
// custom status, a failed catalog read) fails closed to no write. It shares
// the delivery's resolver, so a built-in target costs no catalog read.
func (h *Handler) resolvePRMergeTarget(ctx context.Context, ws db.Workspace, resolver *issuestatus.Resolver) string {
	key := prMergeStatusSetting(ws)
	if key == prMergeStatusNone || key == issuestatus.Blocked {
		return ""
	}
	switch resolver.WritableCategory(ctx, h.issueStatusCatalog(), key) {
	case issuestatus.CategoryStarted, issuestatus.CategoryDone:
		return key
	}
	return ""
}

// decidePRAutoComplete computes the decision for one issue. resolver may be
// shared across one delivery so a webhook touching many issues reads the
// workspace status catalog at most once; nil builds a fresh one.
func (h *Handler) decidePRAutoComplete(ctx context.Context, ws db.Workspace, issue db.Issue, resolver *issuestatus.Resolver) (prAutoCompleteDecision, error) {
	prs, err := h.Queries.ListIssueLinkedPullRequestStates(ctx, issue.ID)
	if err != nil {
		return prAutoCompleteDecision{}, err
	}
	disabled, err := h.Queries.GetIssuePRAutoCompleteDisabled(ctx, issue.ID)
	if err != nil {
		return prAutoCompleteDecision{}, err
	}
	if resolver == nil {
		resolver = issuestatus.NewResolver(issue.WorkspaceID)
	}
	d := prAutoCompleteDecision{IssueDisabled: disabled, Target: h.resolvePRMergeTarget(ctx, ws, resolver)}
	if len(prs) == 0 {
		d.State = prAutoCompleteNone
		return d, nil
	}
	// An unresolvable custom key is returned unchanged and treated as not
	// terminal, the same direction the previous merge gate took.
	effective := resolver.Effective(ctx, h.issueStatusCatalog(), issue.Status)
	// The guards that make a merge change nothing come first, so the issue page
	// only speaks when a merge would move the issue.
	switch {
	case effective == issuestatus.Done || effective == issuestatus.Cancelled:
		d.State = prAutoCompleteTerminal
		return d, nil
	case issue.TriageState.Valid:
		d.State = prAutoCompleteTriage
		return d, nil
	case d.Target == "":
		d.State = prAutoCompleteWorkspaceDisabled
		return d, nil
	case disabled:
		d.State = prAutoCompleteIssueDisabled
		return d, nil
	case issue.Status == d.Target:
		d.State = prAutoCompleteAtTarget
		return d, nil
	}
	var open, closed []db.ListIssueLinkedPullRequestStatesRow
	for _, pr := range prs {
		switch pr.State {
		case "open", "draft":
			open = append(open, pr)
		case "closed":
			closed = append(closed, pr)
		}
	}
	switch {
	case len(open) > 0:
		d.State, d.PRs = prAutoCompleteWaiting, open
	case len(closed) > 0:
		d.State, d.PRs = prAutoCompleteNotMerged, closed
	default:
		d.State, d.PRs = prAutoCompleteAllMerged, prs
	}
	return d, nil
}

// maybeAutoCompleteIssue runs the decision for one issue after a PR event and
// moves it to the workspace's target status when every linked PR is merged.
// Safe to call for any issue: every guard lives in decidePRAutoComplete, and
// the status write is conditional on the status the decision saw, so
// concurrent merges move the issue once. It starts no agent run: a merge is not
// a person handing the issue to anyone.
func (h *Handler) maybeAutoCompleteIssue(ctx context.Context, workspaceID, issueID pgtype.UUID, resolver *issuestatus.Resolver) {
	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: workspaceID})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("pr auto-complete: load issue failed", "err", err, "issue_id", uuidToString(issueID))
		}
		return
	}
	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		slog.Warn("pr auto-complete: load workspace failed", "err", err, "workspace_id", uuidToString(workspaceID))
		return
	}
	d, err := h.decidePRAutoComplete(ctx, ws, issue, resolver)
	if err != nil {
		slog.Warn("pr auto-complete: decision failed", "err", err, "issue_id", uuidToString(issueID))
		return
	}
	if d.State != prAutoCompleteAllMerged {
		return
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		slog.Warn("pr auto-complete: begin failed", "err", err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	updated, err := qtx.MoveIssueFromPullRequests(ctx, db.MoveIssueFromPullRequestsParams{
		ID:             issue.ID,
		WorkspaceID:    issue.WorkspaceID,
		TargetStatus:   d.Target,
		ExpectedStatus: issue.Status,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Someone changed the status since the decision; their write wins.
		return
	}
	var cancelledWakeups []db.AgentTaskQueue
	if err == nil {
		// A no-op unless the target closes the issue.
		cancelledWakeups, err = service.StopClosedIssueWakeups(ctx, qtx, updated)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		slog.Warn("pr auto-complete: status write failed", "err", err, "issue_id", uuidToString(issueID))
		return
	}
	h.broadcastCancelledWakeups(ctx, updated.WorkspaceID, cancelledWakeups)
	// A merged PR is the most common way a sub-issue reaches done; the parent
	// hears about it on the same path as a manual status change.
	h.notifyParentOfChildDone(ctx, issue, updated)

	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	resp := issueToResponse(updated, prefix)
	h.fillStatusCategory(ctx, updated.WorkspaceID, &resp)
	h.publish(protocol.EventIssueUpdated, uuidToString(workspaceID), "system", "", map[string]any{
		"issue":          resp,
		"status_changed": true,
		"prev_status":    issue.Status,
		"creator_type":   issue.CreatorType,
		"creator_id":     uuidToString(issue.CreatorID),
		"source":         "pr_automation",
		"pull_requests":  prNumberList(d.PRs),
		// Moving out of cancelled clears a duplicate mark (MUL-7349); carry
		// both ends so the activity log and clients see the mark go.
		"duplicate_of_issue_id":      liveDuplicateMark(updated.Status, updated.DuplicateOfIssueID),
		"prev_duplicate_of_issue_id": liveDuplicateMark(issue.Status, issue.DuplicateOfIssueID),
	})
}

// prNumberList renders "#12, #15" for the activity entry.
func prNumberList(prs []db.ListIssueLinkedPullRequestStatesRow) string {
	parts := make([]string, 0, len(prs))
	for _, pr := range prs {
		parts = append(parts, fmt.Sprintf("#%d", pr.PrNumber))
	}
	return strings.Join(parts, ", ")
}

// ── Issue page read model ───────────────────────────────────────────────────

type prAutoCompleteResponse struct {
	State string `json:"state"`
	// PR ids the state refers to (see prAutoCompleteDecision.PRs).
	PullRequestIDs []string `json:"pull_request_ids"`
	IssueDisabled  bool     `json:"issue_disabled"`
	// WorkspaceEnabled is true when a merge moves issues at all.
	WorkspaceEnabled bool `json:"workspace_enabled"`
	// TargetStatus is the status key a merge moves the issue to, or "none".
	TargetStatus string `json:"target_status"`
}

func prAutoCompleteToResponse(d prAutoCompleteDecision) prAutoCompleteResponse {
	ids := make([]string, 0, len(d.PRs))
	for _, pr := range d.PRs {
		ids = append(ids, uuidToString(pr.ID))
	}
	target := d.Target
	if target == "" {
		target = prMergeStatusNone
	}
	return prAutoCompleteResponse{
		State:            d.State,
		PullRequestIDs:   ids,
		IssueDisabled:    d.IssueDisabled,
		WorkspaceEnabled: d.Target != "",
		TargetStatus:     target,
	}
}

// prLinkSource explains how a PR came to be linked: "manual", or the text the
// webhook matched ("title" / "branch"). "auto" covers the rest: a closing
// keyword in the body (which is not stored) or a link whose text changed since.
func prLinkSource(linkedByType, identifier, title, branch string) string {
	if linkedByType == "member" {
		return "manual"
	}
	if containsIdentifier(title, identifier) {
		return "title"
	}
	if containsIdentifier(branch, identifier) {
		return "branch"
	}
	return "auto"
}

func containsIdentifier(text, identifier string) bool {
	for _, id := range extractIdentifiers(text) {
		if strings.EqualFold(id, identifier) {
			return true
		}
	}
	return false
}

// ── Manual link / unlink ────────────────────────────────────────────────────

type LinkIssuePullRequestRequest struct {
	// Either a pasted PR URL or the id of an already-listed PR (undo of a
	// removal).
	URL           string `json:"url"`
	PullRequestID string `json:"pull_request_id"`
}

// prURLPattern keeps scheme://host/owner/repo/(pull|pulls|-/merge_requests)/N
// and drops anything after it (/files, ?query, #fragment).
var prURLPattern = regexp.MustCompile(`^(https?://[^/?#]+/.+?/(?:pull|pulls|merge_requests)/\d+)(?:[/?#].*)?$`)

func normalizePullRequestURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false
	}
	m := prURLPattern.FindStringSubmatch(u.Scheme + "://" + u.Host + u.EscapedPath())
	if m == nil {
		return "", false
	}
	return strings.ToLower(strings.TrimRight(m[1], "/")), true
}

// linkedPR is a PR resolved from either provider table.
type linkedPR struct {
	ID       pgtype.UUID
	Provider string // "github" or the VCS provider name
	Number   int32
	github   bool
}

func (h *Handler) findPullRequestForLink(ctx context.Context, workspaceID pgtype.UUID, req LinkIssuePullRequestRequest) (linkedPR, bool, error) {
	if req.PullRequestID != "" {
		id, err := util.ParseUUID(req.PullRequestID)
		if err != nil {
			return linkedPR{}, false, nil
		}
		return h.findPullRequestByID(ctx, workspaceID, id)
	}
	normalized, ok := normalizePullRequestURL(req.URL)
	if !ok {
		return linkedPR{}, false, nil
	}
	if pr, err := h.Queries.FindGitHubPullRequestByURL(ctx, db.FindGitHubPullRequestByURLParams{WorkspaceID: workspaceID, HtmlUrl: normalized}); err == nil {
		return linkedPR{ID: pr.ID, Provider: "github", Number: pr.PrNumber, github: true}, true, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return linkedPR{}, false, err
	}
	if pr, err := h.Queries.FindVCSPullRequestByURL(ctx, db.FindVCSPullRequestByURLParams{WorkspaceID: workspaceID, HtmlUrl: normalized}); err == nil {
		return linkedPR{ID: pr.ID, Provider: pr.Provider, Number: pr.PrNumber}, true, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return linkedPR{}, false, err
	}
	return linkedPR{}, false, nil
}

func (h *Handler) findPullRequestByID(ctx context.Context, workspaceID, id pgtype.UUID) (linkedPR, bool, error) {
	if pr, err := h.Queries.GetGitHubPullRequestInWorkspace(ctx, db.GetGitHubPullRequestInWorkspaceParams{ID: id, WorkspaceID: workspaceID}); err == nil {
		return linkedPR{ID: pr.ID, Provider: "github", Number: pr.PrNumber, github: true}, true, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return linkedPR{}, false, err
	}
	if pr, err := h.Queries.GetVCSPullRequestInWorkspace(ctx, db.GetVCSPullRequestInWorkspaceParams{ID: id, WorkspaceID: workspaceID}); err == nil {
		return linkedPR{ID: pr.ID, Provider: pr.Provider, Number: pr.PrNumber}, true, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return linkedPR{}, false, err
	}
	return linkedPR{}, false, nil
}

// LinkIssuePullRequest (POST /api/issues/{id}/pull-requests) links a PR the
// workspace already mirrors. Linking is a PR event for the issue, so it can
// complete the issue when every linked PR is merged and one of them closes it.
// A manual link carries no close intent of its own; the PR text decides that.
func (h *Handler) LinkIssuePullRequest(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req LinkIssuePullRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.URL) == "" && req.PullRequestID == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	pr, found, err := h.findPullRequestForLink(r.Context(), issue.WorkspaceID, req)
	if err != nil {
		slog.Warn("link pull request: lookup failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to link pull request")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "pull request not found; it appears here once its repository is connected and the PR has been synced")
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)
	_, actorID := h.resolveActor(r, userID, workspaceID)
	linkedBy := pgtype.UUID{}
	if parsed, err := util.ParseUUID(actorID); err == nil {
		linkedBy = parsed
	}
	var rows int64
	if pr.github {
		rows, err = h.Queries.LinkIssueToPullRequestManually(r.Context(), db.LinkIssueToPullRequestManuallyParams{
			IssueID: issue.ID, PullRequestID: pr.ID, LinkedByID: linkedBy,
		})
	} else {
		rows, err = h.Queries.LinkIssueToVCSPullRequestManually(r.Context(), db.LinkIssueToVCSPullRequestManuallyParams{
			IssueID: issue.ID, PullRequestID: pr.ID, LinkedByID: linkedBy,
		})
	}
	if err == nil {
		err = h.Queries.DeletePullRequestExclusion(r.Context(), db.DeletePullRequestExclusionParams{IssueID: issue.ID, PullRequestID: pr.ID})
	}
	if err != nil {
		slog.Warn("link pull request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to link pull request")
		return
	}
	if rows > 0 {
		h.maybeAutoCompleteIssue(r.Context(), issue.WorkspaceID, issue.ID, nil)
	}
	h.publishIssuePullRequestsChanged(workspaceID, issue.ID)
	h.ListPullRequestsForIssue(w, r)
}

// UnlinkIssuePullRequest (DELETE /api/issues/{id}/pull-requests/{prId})
// removes a link and remembers the choice, so the next webhook for that PR does
// not link it again from its title or branch.
func (h *Handler) UnlinkIssuePullRequest(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	prID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "prId"), "pull request id")
	if !ok {
		return
	}
	pr, found, err := h.findPullRequestByID(r.Context(), issue.WorkspaceID, prID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink pull request")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	excludedBy := pgtype.UUID{}
	if parsed, err := util.ParseUUID(actorID); err == nil {
		excludedBy = parsed
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink pull request")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	var rows int64
	if pr.github {
		rows, err = qtx.UnlinkIssueFromPullRequest(r.Context(), db.UnlinkIssueFromPullRequestParams{IssueID: issue.ID, PullRequestID: pr.ID})
	} else {
		rows, err = qtx.UnlinkIssueFromVCSPullRequest(r.Context(), db.UnlinkIssueFromVCSPullRequestParams{IssueID: issue.ID, PullRequestID: pr.ID})
	}
	if err == nil {
		err = qtx.ExcludePullRequestFromIssue(r.Context(), db.ExcludePullRequestFromIssueParams{
			IssueID:        issue.ID,
			PullRequestID:  pr.ID,
			WorkspaceID:    issue.WorkspaceID,
			ExcludedByType: strToText(actorType),
			ExcludedByID:   excludedBy,
		})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		slog.Warn("unlink pull request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to unlink pull request")
		return
	}
	if rows > 0 {
		// Removing the last unmerged PR can leave only merged ones behind.
		h.maybeAutoCompleteIssue(r.Context(), issue.WorkspaceID, issue.ID, nil)
	}
	h.publishIssuePullRequestsChanged(workspaceID, issue.ID)
	h.ListPullRequestsForIssue(w, r)
}

type SetIssuePRAutoCompleteRequest struct {
	Disabled *bool `json:"disabled"`
}

// SetIssuePRAutoComplete (PUT /api/issues/{id}/pr-auto-complete) turns PR
// auto-complete off (or back on) for one issue — for work that needs review or
// a release after the merge. Turning it back on does not complete the issue by
// itself; the next PR event does.
func (h *Handler) SetIssuePRAutoComplete(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req SetIssuePRAutoCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Disabled == nil {
		writeError(w, http.StatusBadRequest, "disabled is required")
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	actorUUID := pgtype.UUID{}
	if parsed, err := util.ParseUUID(actorID); err == nil {
		actorUUID = parsed
	}
	before, err := h.Queries.GetIssuePRAutoCompleteDisabled(r.Context(), issue.ID)
	if err == nil {
		err = h.Queries.SetIssuePRAutoCompleteDisabled(r.Context(), db.SetIssuePRAutoCompleteDisabledParams{
			IssueID:              issue.ID,
			WorkspaceID:          issue.WorkspaceID,
			AutoCompleteDisabled: *req.Disabled,
			UpdatedByType:        strToText(actorType),
			UpdatedByID:          actorUUID,
		})
	}
	if err != nil {
		slog.Warn("set issue pr auto-complete failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to update pull request automation")
		return
	}
	if before != *req.Disabled {
		h.recordPRAutoCompleteActivity(r.Context(), issue, actorType, actorID, *req.Disabled)
	}
	h.publishIssuePullRequestsChanged(workspaceID, issue.ID)
	h.ListPullRequestsForIssue(w, r)
}

func (h *Handler) recordPRAutoCompleteActivity(ctx context.Context, issue db.Issue, actorType, actorID string, disabled bool) {
	details, _ := json.Marshal(map[string]bool{"disabled": disabled})
	activity, err := h.Queries.CreateActivity(ctx, db.CreateActivityParams{
		ID:          dbid.NewV7(),
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		ActorType:   strToText(actorType),
		ActorID:     optionalActorUUID(actorID),
		Action:      "pr_auto_complete_changed",
		Details:     details,
	})
	if err != nil {
		slog.Warn("record pr auto-complete activity failed", "err", err)
		return
	}
	h.publish(protocol.EventActivityCreated, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{
		"issue_id": uuidToString(issue.ID),
		"entry": map[string]any{
			"type":       "activity",
			"id":         uuidToString(activity.ID),
			"actor_type": actorType,
			"actor_id":   actorID,
			"action":     activity.Action,
			"details":    json.RawMessage(details),
			"created_at": timestampToString(activity.CreatedAt),
		},
	})
}

func optionalActorUUID(id string) pgtype.UUID {
	parsed, err := util.ParseUUID(id)
	if err != nil {
		return pgtype.UUID{}
	}
	return parsed
}

// publishIssuePullRequestsChanged makes open issue pages refetch their PR list.
func (h *Handler) publishIssuePullRequestsChanged(workspaceID string, issueID pgtype.UUID) {
	h.publish(protocol.EventPullRequestUpdated, workspaceID, "system", "", map[string]any{
		"issue_id":         uuidToString(issueID),
		"linked_issue_ids": []string{uuidToString(issueID)},
	})
}
