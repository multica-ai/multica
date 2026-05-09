// Phase 7a — Release lifecycle.
//
// A Release is a coordinated batch of PRs the team wants to ship
// together: a single channel, a single tracking issue, a single set
// of approvers, a single deploy story. Phase 7a is read + create only.
// Phase 7b/7c/7d add merge orchestration, staging promotion, and
// production cutover; the schema lands in full so this file's
// CreateRelease is sufficient for v1 and the stage transitions
// (UpdateReleaseStage / DeactivateReleasePullRequests calls below)
// are reused by 7b+.
//
// The service deliberately speaks only to sqlc + small helper
// interfaces (ChannelOps / IssueOps). Channel and issue auto-create
// happens through those interfaces because the underlying services
// live in the handler package — depending on `handler` from `service`
// would create a cycle, and the spec calls out matching the Phase 4
// PR-conversation-channel pattern. This is the same shape Phase 6.5
// uses for `PRChannelPoster` (a function-typed bridge).

package ship

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Active stage set — anything that's not done / rolled_back / cancelled.
// Centralised so handler code and service code agree on what "active"
// means; matches the SQL filter in ListActiveReleasesByWorkspace.
func IsTerminalStage(s db.ReleaseStage) bool {
	switch s {
	case db.ReleaseStageDone, db.ReleaseStageRolledBack, db.ReleaseStageCancelled:
		return true
	}
	return false
}

// IsAssemblingStage is the predicate for "the release is still being
// curated" — the only stage in which Phase 7a allows mutating the PR
// set. Phase 7b extends this to also accept `merging` for the
// removal-on-failure path.
func IsAssemblingStage(s db.ReleaseStage) bool {
	return s == db.ReleaseStageAssembling
}

// ChannelMember is the actor descriptor used when seeding members of
// the auto-created release channel. Mirrors the channel.Actor type but
// stays here so the service package doesn't import the channel
// package.
type ChannelMember struct {
	Type string // "member" | "agent"
	ID   pgtype.UUID
}

// ChannelOps abstracts the bits of the channel service the release
// flow needs. The handler package supplies an implementation that
// calls h.ChannelService.Create + AddMember.
type ChannelOps interface {
	CreateReleaseChannel(
		ctx context.Context,
		workspaceID pgtype.UUID,
		name, displayName, description string,
		creator ChannelMember,
		members []ChannelMember,
	) (db.Channel, error)
	// ArchiveReleaseChannel is called when the release transitions to
	// a terminal stage. Best-effort: the caller logs and ignores
	// errors so a flaky channel service can't block the release flow.
	ArchiveReleaseChannel(ctx context.Context, channelID pgtype.UUID) error
}

// IssueOps abstracts issue creation for the release tracking issue.
// Same rationale as ChannelOps.
type IssueOps interface {
	CreateReleaseIssue(
		ctx context.Context,
		workspaceID, projectID pgtype.UUID,
		title, description string,
		creator pgtype.UUID,
	) (db.Issue, error)
	// CloseReleaseIssue moves the tracking issue to "cancelled"
	// (release cancelled) or "done" (release shipped). Phase 7a only
	// uses the cancellation path.
	CloseReleaseIssue(ctx context.Context, issueID pgtype.UUID, status string) error
}

// CreateReleaseParams is the input to Service.CreateRelease.
type CreateReleaseParams struct {
	WorkspaceID      pgtype.UUID
	ProjectID        pgtype.UUID
	Title            string
	Description      string
	PullRequestIDs   []pgtype.UUID
	ApproverID       *pgtype.UUID // nil ↔ no approver set
	SecondApproverID *pgtype.UUID
	CreatedBy        pgtype.UUID
}

// CreateReleaseResult is what the handler echoes back to the client.
// `Warnings` is the soft-gate channel: cross-PR file overlap, missing
// approver for the computed risk, etc. The dialog surfaces them
// inline; a "would block start_merge" gate is enforced by Phase 7b's
// CanStartMerge() rather than CreateRelease itself.
type CreateReleaseResult struct {
	Release  db.ShipRelease
	PRs      []db.PullRequest
	Channel  *db.Channel
	Issue    *db.Issue
	Events   []db.ShipReleaseEvent
	Warnings []string
}

// Sentinel errors so the handler can map to clean status codes.
var (
	ErrReleaseNoPullRequests       = errors.New("release: must include at least one pull request")
	ErrReleasePullRequestNotFound  = errors.New("release: pull request not found in workspace")
	ErrReleasePullRequestIneligible = errors.New("release: pull request not eligible for release")
	ErrReleasePullRequestInActive  = errors.New("release: pull request is already in an active release")
	ErrReleasePullRequestProjectMismatch = errors.New("release: pull request belongs to a different project than the release")
	ErrReleaseNotAssembling        = errors.New("release: only releases in 'assembling' stage can be modified")
)

// CreateRelease is the entry point used by the POST /api/projects/{id}/releases
// handler. The flow is deliberately not transactional across all of
// {ship_release insert, channel create, issue create, event log}: each
// of those auxiliary writes is allowed to fail without unwinding the
// release row, because the release IS the durable thing the user
// just intended to create — losing the channel auto-create can be
// retried later from the detail page; losing the release row would
// erase that intent.
func (s *Service) CreateRelease(
	ctx context.Context,
	params CreateReleaseParams,
	channelOps ChannelOps,
	issueOps IssueOps,
) (*CreateReleaseResult, error) {
	if len(params.PullRequestIDs) == 0 {
		return nil, ErrReleaseNoPullRequests
	}
	title := strings.TrimSpace(params.Title)
	if title == "" {
		return nil, errors.New("release: title is required")
	}

	// Step 1 — pre-flight checks. Load every PR and verify it's
	// eligible (open, not draft, mergeable, CI green, approved) and
	// belongs to this project + workspace. We do this as a single
	// pass rather than streaming because the dialog's PR set is
	// small (typically <10) and the eligibility verdict is needed
	// up-front.
	prs := make([]db.PullRequest, 0, len(params.PullRequestIDs))
	warnings := []string{}
	for _, prID := range params.PullRequestIDs {
		pr, err := s.Q.GetPullRequest(ctx, prID)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrReleasePullRequestNotFound, uuidString(prID))
		}
		if uuidString(pr.WorkspaceID) != uuidString(params.WorkspaceID) {
			return nil, fmt.Errorf("%w: %s", ErrReleasePullRequestNotFound, uuidString(prID))
		}
		if uuidString(pr.ProjectID) != uuidString(params.ProjectID) {
			return nil, fmt.Errorf("%w: %s", ErrReleasePullRequestProjectMismatch, uuidString(prID))
		}
		if reason, ok := releaseEligibilityReason(pr); !ok {
			return nil, fmt.Errorf("%w: PR #%d %s", ErrReleasePullRequestIneligible, pr.PrNumber, reason)
		}
		// Cannot already be in another active release. The partial
		// unique index would catch this at INSERT time anyway, but
		// we surface a clean error here so the dialog can highlight
		// the offending PR rather than show a database-shaped
		// failure.
		if _, err := s.Q.GetActiveReleaseForPullRequest(ctx, pr.ID); err == nil {
			return nil, fmt.Errorf("%w: PR #%d", ErrReleasePullRequestInActive, pr.PrNumber)
		}
		prs = append(prs, pr)
	}

	// Step 2 — compute the release's risk level as max() over the PR
	// set. The risk classifier already populated each PR's
	// risk_level; we just take the max.
	riskLevel := highestRisk(prs)

	// Step 3 — soft warnings. File overlap (two PRs touching the
	// same path) is a soft warning because the user might know full
	// well that the overlap is intentional (e.g. a stack); we surface
	// it inline so the dialog can render a chip but don't block.
	// Phase 7a doesn't read changed-files via the GitHub API (would
	// require a token round-trip per PR); we approximate via the
	// pull_request table's labels as a placeholder. Subsequent phases
	// will plug the real changed-files signal in.
	if msgs := riskWarnings(prs, riskLevel, params.ApproverID, params.SecondApproverID); len(msgs) > 0 {
		warnings = append(warnings, msgs...)
	}

	// Step 4 — INSERT the release row. We do this BEFORE the channel
	// / issue auto-create so that a transient failure on the
	// auxiliary side leaves a usable release the user can heal from
	// the detail page (Phase 7a doesn't expose heal endpoints, but
	// the rows are FK-NULL-able so a follow-up phase can).
	createParams := db.CreateReleaseParams{
		WorkspaceID: params.WorkspaceID,
		ProjectID:   params.ProjectID,
		Title:       title,
		Description: pgtype.Text{String: params.Description, Valid: params.Description != ""},
		RiskLevel:   riskLevel,
	}
	if params.ApproverID != nil {
		createParams.ApproverID = *params.ApproverID
	}
	if params.SecondApproverID != nil {
		createParams.SecondApproverID = *params.SecondApproverID
	}
	if params.CreatedBy.Valid {
		createParams.CreatedBy = params.CreatedBy
	}
	release, err := s.Q.CreateRelease(ctx, createParams)
	if err != nil {
		return nil, fmt.Errorf("create release: %w", err)
	}

	// Step 5 — wire each PR into the join table. The unique-index
	// guard catches concurrent calls (two operators creating
	// overlapping releases at the same time), so on conflict we
	// return a meaningful error AFTER the release row landed; the
	// caller can DELETE that release. Phase 7a's flow makes this
	// race vanishingly unlikely (the dialog disables submit for
	// already-claimed PRs), so we don't add transactional rollback
	// for a pathological case.
	for i, pr := range prs {
		if _, err := s.Q.AddPullRequestToRelease(ctx, db.AddPullRequestToReleaseParams{
			ReleaseID:     release.ID,
			PullRequestID: pr.ID,
			Position:      int32(i),
		}); err != nil {
			return nil, fmt.Errorf("attach PR to release: %w", err)
		}
	}

	// Step 6 — audit log: "created" event with PR set + risk + approvers.
	createdEvent, err := s.insertReleaseEvent(ctx, release.ID, "created", params.CreatedBy, map[string]any{
		"title":              title,
		"description":        params.Description,
		"risk_level":         string(riskLevel),
		"pr_count":           len(prs),
		"approver_id":        ptrUUIDString(params.ApproverID),
		"second_approver_id": ptrUUIDString(params.SecondApproverID),
	})
	events := []db.ShipReleaseEvent{}
	if err == nil {
		events = append(events, createdEvent)
	}

	// Step 7 — auto-create channel + issue. Best effort; failures are
	// logged into Warnings rather than failing the whole call.
	var channelOut *db.Channel
	var issueOut *db.Issue
	if channelOps != nil {
		ch, w := s.autoCreateReleaseChannel(ctx, release, prs, params, channelOps)
		warnings = append(warnings, w...)
		if ch != nil {
			channelOut = ch
			// Persist the link onto the release row.
			if updated, err := s.Q.UpdateReleaseChannel(ctx, db.UpdateReleaseChannelParams{
				ID:        release.ID,
				ChannelID: ch.ID,
			}); err == nil {
				release = updated
			}
			if ev, err := s.insertReleaseEvent(ctx, release.ID, "channel_created", params.CreatedBy, map[string]any{
				"channel_id": uuidString(ch.ID),
				"name":       ch.Name,
			}); err == nil {
				events = append(events, ev)
			}
		}
	}
	if issueOps != nil {
		issue, w := s.autoCreateReleaseIssue(ctx, release, prs, params, channelOut, issueOps)
		warnings = append(warnings, w...)
		if issue != nil {
			issueOut = issue
			if updated, err := s.Q.UpdateReleaseIssue(ctx, db.UpdateReleaseIssueParams{
				ID:      release.ID,
				IssueID: issue.ID,
			}); err == nil {
				release = updated
			}
			if ev, err := s.insertReleaseEvent(ctx, release.ID, "issue_created", params.CreatedBy, map[string]any{
				"issue_id": uuidString(issue.ID),
				"title":    issue.Title,
			}); err == nil {
				events = append(events, ev)
			}
		}
	}

	return &CreateReleaseResult{
		Release:  release,
		PRs:      prs,
		Channel:  channelOut,
		Issue:    issueOut,
		Events:   events,
		Warnings: warnings,
	}, nil
}

// AddPullRequestToRelease attaches a PR to an existing release. Only
// allowed when the release is in 'assembling' stage. The PR must not
// already be in another active release.
func (s *Service) AddPullRequestToRelease(
	ctx context.Context,
	releaseID, prID, addedBy pgtype.UUID,
) (db.PullRequest, error) {
	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return db.PullRequest{}, fmt.Errorf("get release: %w", err)
	}
	if !IsAssemblingStage(release.Stage) {
		return db.PullRequest{}, ErrReleaseNotAssembling
	}
	pr, err := s.Q.GetPullRequest(ctx, prID)
	if err != nil {
		return db.PullRequest{}, ErrReleasePullRequestNotFound
	}
	if uuidString(pr.WorkspaceID) != uuidString(release.WorkspaceID) ||
		uuidString(pr.ProjectID) != uuidString(release.ProjectID) {
		return db.PullRequest{}, ErrReleasePullRequestProjectMismatch
	}
	if reason, ok := releaseEligibilityReason(pr); !ok {
		return db.PullRequest{}, fmt.Errorf("%w: %s", ErrReleasePullRequestIneligible, reason)
	}
	if existing, err := s.Q.GetActiveReleaseForPullRequest(ctx, pr.ID); err == nil &&
		uuidString(existing.ID) != uuidString(release.ID) {
		return db.PullRequest{}, ErrReleasePullRequestInActive
	}
	// Append at the end of the existing PR list.
	count, err := s.Q.CountActiveReleasePullRequests(ctx, release.ID)
	if err != nil {
		return db.PullRequest{}, fmt.Errorf("count release PRs: %w", err)
	}
	if _, err := s.Q.AddPullRequestToRelease(ctx, db.AddPullRequestToReleaseParams{
		ReleaseID:     release.ID,
		PullRequestID: pr.ID,
		Position:      count,
	}); err != nil {
		return db.PullRequest{}, fmt.Errorf("add PR to release: %w", err)
	}
	_, _ = s.insertReleaseEvent(ctx, release.ID, "pr_added", addedBy, map[string]any{
		"pull_request_id": uuidString(pr.ID),
		"pr_number":       pr.PrNumber,
	})
	// Risk level may have changed — re-aggregate.
	if err := s.refreshReleaseRiskLevel(ctx, release.ID); err != nil {
		// Soft fail — the PR is attached; risk level just stays at
		// the previous max. The detail page recomputes anyway.
		_ = err
	}
	return pr, nil
}

// RemovePullRequestFromRelease detaches a PR from an assembling release.
func (s *Service) RemovePullRequestFromRelease(
	ctx context.Context,
	releaseID, prID, removedBy pgtype.UUID,
) error {
	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return fmt.Errorf("get release: %w", err)
	}
	if !IsAssemblingStage(release.Stage) {
		return ErrReleaseNotAssembling
	}
	if err := s.Q.RemovePullRequestFromRelease(ctx, db.RemovePullRequestFromReleaseParams{
		ReleaseID:     releaseID,
		PullRequestID: prID,
	}); err != nil {
		return fmt.Errorf("remove PR from release: %w", err)
	}
	_, _ = s.insertReleaseEvent(ctx, releaseID, "pr_removed", removedBy, map[string]any{
		"pull_request_id": uuidString(prID),
	})
	if err := s.refreshReleaseRiskLevel(ctx, releaseID); err != nil {
		_ = err
	}
	return nil
}

// CancelRelease aborts a release. Phase 7a only allows cancellation
// from the assembling stage; phase 7b will extend to allow it from
// 'merging' too. Flips is_active=FALSE on join rows so PRs become
// available for the next release, archives the channel, and closes
// the issue.
func (s *Service) CancelRelease(
	ctx context.Context,
	releaseID pgtype.UUID,
	reason string,
	cancelledBy pgtype.UUID,
	channelOps ChannelOps,
	issueOps IssueOps,
) (db.ShipRelease, error) {
	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("get release: %w", err)
	}
	if !IsAssemblingStage(release.Stage) {
		return db.ShipRelease{}, ErrReleaseNotAssembling
	}

	now := s.now()
	updated, err := s.Q.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
		ID:             releaseID,
		Stage:          db.ReleaseStageCancelled,
		DoneAt:         pgtype.Timestamptz{Time: now, Valid: true},
		RollbackReason: pgtype.Text{String: reason, Valid: reason != ""},
	})
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("update release stage: %w", err)
	}

	// Free the PRs for re-use.
	if err := s.Q.DeactivateReleasePullRequests(ctx, releaseID); err != nil {
		return updated, fmt.Errorf("deactivate release PRs: %w", err)
	}

	// Best-effort channel archive + issue close. Failures are
	// recorded as audit-log warnings but don't propagate.
	if release.ChannelID.Valid && channelOps != nil {
		if err := channelOps.ArchiveReleaseChannel(ctx, release.ChannelID); err != nil {
			_, _ = s.insertReleaseEvent(ctx, releaseID, "warning", cancelledBy, map[string]any{
				"reason": "channel archive failed: " + err.Error(),
			})
		}
	}
	if release.IssueID.Valid && issueOps != nil {
		if err := issueOps.CloseReleaseIssue(ctx, release.IssueID, "cancelled"); err != nil {
			_, _ = s.insertReleaseEvent(ctx, releaseID, "warning", cancelledBy, map[string]any{
				"reason": "issue close failed: " + err.Error(),
			})
		}
	}

	_, _ = s.insertReleaseEvent(ctx, releaseID, "cancelled", cancelledBy, map[string]any{
		"reason": reason,
	})
	return updated, nil
}

// UpdateReleaseMetadata patches title / description / approver fields.
// Per CLAUDE.md "internal non-boundary code": we don't preserve a
// "transitional both-fields" semantics — caller's nil leaves the
// field untouched, sets caller's value otherwise.
func (s *Service) UpdateReleaseMetadata(
	ctx context.Context,
	releaseID pgtype.UUID,
	title, description *string,
	approverID, secondApproverID *pgtype.UUID,
	approverIDProvided, secondApproverIDProvided bool,
	editedBy pgtype.UUID,
) (db.ShipRelease, error) {
	params := db.UpdateReleaseMetadataParams{ID: releaseID}
	if title != nil {
		params.Title = pgtype.Text{String: *title, Valid: true}
	}
	if description != nil {
		params.Description = pgtype.Text{String: *description, Valid: true}
	}
	params.ApproverIDSet = approverIDProvided
	if approverIDProvided && approverID != nil {
		params.ApproverID = *approverID
	}
	params.SecondApproverIDSet = secondApproverIDProvided
	if secondApproverIDProvided && secondApproverID != nil {
		params.SecondApproverID = *secondApproverID
	}
	updated, err := s.Q.UpdateReleaseMetadata(ctx, params)
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("update release metadata: %w", err)
	}
	eventType := "metadata_updated"
	if approverIDProvided || secondApproverIDProvided {
		eventType = "approver_set"
	}
	_, _ = s.insertReleaseEvent(ctx, releaseID, eventType, editedBy, map[string]any{
		"title_set":              title != nil,
		"description_set":        description != nil,
		"approver_id_set":        approverIDProvided,
		"second_approver_id_set": secondApproverIDProvided,
	})
	return updated, nil
}

// ----- helpers --------------------------------------------------------------

// releaseEligibilityReason returns ("", true) if the PR can be added to
// a release, or (reason, false) describing why not. We err on the
// side of strict — the dialog hides ineligible PRs from the picker
// anyway, so this is the defensive layer.
func releaseEligibilityReason(pr db.PullRequest) (string, bool) {
	if pr.State != db.PullRequestStateOpen {
		return "is not open", false
	}
	if pr.IsDraft {
		return "is a draft", false
	}
	if pr.Mergeable.Valid && pr.Mergeable.String == "CONFLICTING" {
		return "has merge conflicts", false
	}
	if pr.CiStatus.Valid && pr.CiStatus.String != "" && pr.CiStatus.String != "success" {
		return "CI is " + pr.CiStatus.String, false
	}
	if pr.ReviewDecision.Valid && pr.ReviewDecision.String != "" && pr.ReviewDecision.String != "APPROVED" {
		return "review status: " + pr.ReviewDecision.String, false
	}
	return "", true
}

// highestRisk returns the max risk_level across the PR set. Risk
// levels are ordered low < medium < high < critical.
func highestRisk(prs []db.PullRequest) db.RiskLevel {
	rank := map[db.RiskLevel]int{
		db.RiskLevelLow:      0,
		db.RiskLevelMedium:   1,
		db.RiskLevelHigh:     2,
		db.RiskLevelCritical: 3,
	}
	highest := db.RiskLevelLow
	for _, pr := range prs {
		if rank[pr.RiskLevel] > rank[highest] {
			highest = pr.RiskLevel
		}
	}
	if len(prs) == 0 {
		return db.RiskLevelMedium
	}
	return highest
}

// riskWarnings surfaces the soft-gate concerns the dialog should
// render inline. The dialog still allows submission — Phase 7b's
// CanStartMerge() is the hard gate.
func riskWarnings(
	prs []db.PullRequest,
	risk db.RiskLevel,
	approverID, secondApproverID *pgtype.UUID,
) []string {
	out := []string{}
	hasApprover := approverID != nil && approverID.Valid
	hasSecond := secondApproverID != nil && secondApproverID.Valid
	if (risk == db.RiskLevelMedium || risk == db.RiskLevelHigh || risk == db.RiskLevelCritical) && !hasApprover {
		out = append(out, fmt.Sprintf("Risk level %s requires an approver", risk))
	}
	if risk == db.RiskLevelCritical && !hasSecond {
		out = append(out, "Critical risk requires a second approver")
	}
	// Cross-PR overlap on labels (Phase 7a placeholder for the
	// changed-files signal). The classifier already records
	// risk_reasons so we don't double-count those; we look for two
	// PRs that share any label.
	prsByLabel := map[string][]int32{}
	for _, pr := range prs {
		var labels []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(pr.Labels, &labels); err == nil {
			for _, l := range labels {
				prsByLabel[l.Name] = append(prsByLabel[l.Name], pr.PrNumber)
			}
		}
	}
	overlapKeys := []string{}
	for k, nums := range prsByLabel {
		if len(nums) > 1 {
			overlapKeys = append(overlapKeys, k)
		}
	}
	sort.Strings(overlapKeys) // deterministic warning order for tests
	for _, k := range overlapKeys {
		nums := prsByLabel[k]
		out = append(out, fmt.Sprintf("Label \"%s\" shared by PR #%d and PR #%d", k, nums[0], nums[1]))
	}
	return out
}

// refreshReleaseRiskLevel recomputes the release's risk_level from the
// current PR set and writes the new value to the row. Used after
// AddPullRequest / RemovePullRequest. Phase 7a doesn't have a sqlc
// query for "update risk_level only", so we go through metadata
// (which preserves untouched fields).
func (s *Service) refreshReleaseRiskLevel(ctx context.Context, releaseID pgtype.UUID) error {
	rows, err := s.Q.ListReleasePullRequests(ctx, releaseID)
	if err != nil {
		return err
	}
	prs := make([]db.PullRequest, 0, len(rows))
	for _, r := range rows {
		prs = append(prs, db.PullRequest{
			ID:        r.ID,
			RiskLevel: r.RiskLevel,
		})
	}
	risk := highestRisk(prs)
	// Direct SQL update bypassing the metadata-narg dance because
	// risk_level isn't part of the metadata API surface.
	_, err = s.Q.UpdateReleaseRiskLevel(ctx, db.UpdateReleaseRiskLevelParams{
		ID:        releaseID,
		RiskLevel: risk,
	})
	return err
}

// insertReleaseEvent is a thin wrapper around InsertReleaseEvent that
// JSON-marshals the payload and tolerates a nil actor. Returns the
// inserted row so callers can append it to the response.
func (s *Service) insertReleaseEvent(
	ctx context.Context,
	releaseID pgtype.UUID,
	eventType string,
	actor pgtype.UUID,
	payload map[string]any,
) (db.ShipReleaseEvent, error) {
	var payloadBytes []byte
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			payloadBytes = b
		}
	}
	row, err := s.Q.InsertReleaseEvent(ctx, db.InsertReleaseEventParams{
		ReleaseID:   releaseID,
		EventType:   eventType,
		ActorUserID: actor,
		Payload:     payloadBytes,
	})
	return row, err
}

// autoCreateReleaseChannel mirrors the Phase 4 PR-conversation channel
// pattern. On any failure we return a warning string so the caller
// can surface it; the release is still created.
func (s *Service) autoCreateReleaseChannel(
	ctx context.Context,
	release db.ShipRelease,
	prs []db.PullRequest,
	params CreateReleaseParams,
	ops ChannelOps,
) (*db.Channel, []string) {
	warnings := []string{}
	dateSlug := s.now().Format("2006-01-02")
	titleSlug := slugifyForChannel(params.Title, 30)
	channelName := fmt.Sprintf("release-%s-%s", dateSlug, titleSlug)
	displayName := fmt.Sprintf("Release · %s", truncateForDisplay(params.Title, 60))
	description := fmt.Sprintf("Coordination channel for release \"%s\" (%d PR%s)",
		params.Title, len(prs), plural(len(prs)))

	// Resolve workspace orchestrator for creator + member seeding.
	ws, err := s.Q.GetWorkspace(ctx, params.WorkspaceID)
	if err != nil {
		return nil, []string{"channel auto-create skipped: workspace lookup failed"}
	}
	creator := ChannelMember{Type: "member", ID: params.CreatedBy}
	if ws.OrchestratorAgentID.Valid {
		creator = ChannelMember{Type: "agent", ID: ws.OrchestratorAgentID}
	}

	members := []ChannelMember{}
	if params.CreatedBy.Valid {
		members = append(members, ChannelMember{Type: "member", ID: params.CreatedBy})
	}
	if params.ApproverID != nil && params.ApproverID.Valid {
		members = append(members, ChannelMember{Type: "member", ID: *params.ApproverID})
	}
	if ws.OrchestratorAgentID.Valid {
		members = append(members, ChannelMember{Type: "agent", ID: ws.OrchestratorAgentID})
	}
	// Each PR's author, IF they're a workspace member by github_login.
	for _, pr := range prs {
		if pr.AuthorLogin == "" {
			continue
		}
		// Best-effort: a workspace with no GitHub-login mapping just
		// skips this. Phase 7e will tighten this.
		// For now we don't have a per-workspace GH-login → user_id
		// helper, so we skip silently. The release issue + dialog
		// already lists every PR author for visibility.
		_ = pr
	}

	ch, err := ops.CreateReleaseChannel(ctx, params.WorkspaceID, channelName, displayName, description, creator, members)
	if err != nil {
		warnings = append(warnings, "channel auto-create failed: "+err.Error())
		return nil, warnings
	}
	_ = release
	return &ch, warnings
}

// autoCreateReleaseIssue auto-creates the tracking issue. The body
// is a markdown-formatted PR checklist plus risk + channel + approver
// metadata so the issue page reads as the "release ledger".
func (s *Service) autoCreateReleaseIssue(
	ctx context.Context,
	release db.ShipRelease,
	prs []db.PullRequest,
	params CreateReleaseParams,
	channel *db.Channel,
	ops IssueOps,
) (*db.Issue, []string) {
	warnings := []string{}
	body := buildReleaseIssueBody(release, prs, params, channel)
	issue, err := ops.CreateReleaseIssue(ctx, params.WorkspaceID, params.ProjectID, params.Title, body, params.CreatedBy)
	if err != nil {
		warnings = append(warnings, "issue auto-create failed: "+err.Error())
		return nil, warnings
	}
	return &issue, warnings
}

// buildReleaseIssueBody is exposed (lowercase but pkg-internal) so
// tests can lock its shape without hitting the full create path.
func buildReleaseIssueBody(
	release db.ShipRelease,
	prs []db.PullRequest,
	params CreateReleaseParams,
	channel *db.Channel,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Release: %s\n\n", params.Title)
	if params.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", params.Description)
	}
	fmt.Fprintf(&b, "PRs in this release:\n")
	for _, pr := range prs {
		fmt.Fprintf(&b, "- [ ] #%d — %s (@%s)\n", pr.PrNumber, pr.Title, pr.AuthorLogin)
	}
	fmt.Fprintf(&b, "\nRisk: %s\n", riskBadge(release.RiskLevel))
	if channel != nil {
		fmt.Fprintf(&b, "Channel: #%s\n", channel.Name)
	}
	fmt.Fprintf(&b, "\nThis issue auto-closes when the release reaches stage=done. The channel auto-archives at the same time.\n")
	return b.String()
}

func riskBadge(r db.RiskLevel) string {
	switch r {
	case db.RiskLevelLow:
		return "low"
	case db.RiskLevelMedium:
		return "medium"
	case db.RiskLevelHigh:
		return "high"
	case db.RiskLevelCritical:
		return "critical"
	}
	return string(r)
}

// slugifyForChannel produces a channel-name-safe slug from a title.
// Lowercases, replaces non-alnum with "-", collapses runs of "-",
// trims to maxLen.
func slugifyForChannel(s string, maxLen int) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var out []rune
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			out = append(out, r)
			prevDash = false
		case r == '-' || r == '_' || r == ' ':
			if !prevDash {
				out = append(out, '-')
				prevDash = true
			}
		}
		if len(out) >= maxLen {
			break
		}
	}
	result := strings.Trim(string(out), "-")
	if result == "" {
		return "release"
	}
	return result
}

func truncateForDisplay(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func ptrUUIDString(p *pgtype.UUID) string {
	if p == nil {
		return ""
	}
	return uuidString(*p)
}

// truncateForDisplay alternative time helper: zero-time defensive.
var _ = time.Time{}
