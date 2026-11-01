package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issueposition"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// The boss's attention is the scarce resource, so the server enforces these
// limits instead of trusting prompts. They are constants until real use shows
// they need to be workspace settings.
const (
	// ProposalMaxPendingPerSquad caps proposals one squad may have waiting.
	ProposalMaxPendingPerSquad = 3
	// ProposalMaxCreatedPerSquadPerWeek caps proposals one squad may file over
	// the rolling seven days, whatever their state.
	ProposalMaxCreatedPerSquadPerWeek = 3
	// ProposalMaxPendingPerWorkspace bounds the whole queue, which per-squad
	// limits alone would let grow with the number of squads.
	ProposalMaxPendingPerWorkspace = 10
	// ProposalMaxUrgentPerSquadPerWeek caps proposals one squad may mark urgent
	// over the rolling seven days.
	ProposalMaxUrgentPerSquadPerWeek = 1
)

// Proposal states.
const (
	ProposalStatePending  = "pending"
	ProposalStateApproved = "approved"
	ProposalStateRejected = "rejected"
)

// Proposal decisions, as accepted by the decision endpoint.
const (
	ProposalDecisionApprove = "approve"
	ProposalDecisionReject  = "reject"
)

// ProposalOriginType is the issue origin_type of the issue that backs a
// proposal; the issue's origin_id is the proposal id.
const ProposalOriginType = "proposal"

// ProposalLimitError reports that an attention limit stopped a new proposal.
// Message is written for the filing agent: it names the limit and what to do.
type ProposalLimitError struct{ Message string }

func (e *ProposalLimitError) Error() string { return e.Message }

var (
	// ErrProposalSquadNotFound: the proposing squad is not a live squad of the
	// workspace.
	ErrProposalSquadNotFound = errors.New("squad not found in this workspace")
	// ErrProposalHandoffSquadNotFound: the squad that should start the work is
	// not a live squad of the workspace.
	ErrProposalHandoffSquadNotFound = errors.New("handoff squad not found in this workspace")
	// ErrProposalNoBoss: an urgent proposal needs a workspace owner to ask.
	ErrProposalNoBoss = errors.New("workspace has no owner to decide an urgent proposal")
	// ErrProposalNotFound: no such proposal in the workspace.
	ErrProposalNotFound = errors.New("proposal not found")
	// ErrProposalAlreadyDecided: the proposal left the pending state.
	ErrProposalAlreadyDecided = errors.New("proposal is already decided")
)

// ProposalContent is the proposal body the squad writes. The service renders
// it into the fixed-shape markdown that becomes the backing issue's
// description, so a proposal cannot skip a section.
type ProposalContent struct {
	Title          string
	Summary        string
	Evidence       []string
	Recommendation string
	Options        []string
	CostRisk       string
	Ask            string
}

// RenderDescription returns the fixed-shape markdown: Summary, Evidence,
// Recommendation, Options considered, Cost and risk, Ask.
func (c ProposalContent) RenderDescription(urgentReason string) string {
	var b strings.Builder
	if urgentReason != "" {
		fmt.Fprintf(&b, "> **Urgent:** %s\n\n", urgentReason)
	}
	b.WriteString("## Summary\n\n" + c.Summary + "\n\n")
	b.WriteString("## Evidence\n\n")
	for _, e := range c.Evidence {
		b.WriteString("- " + e + "\n")
	}
	b.WriteString("\n## Recommendation\n\n" + c.Recommendation + "\n\n")
	b.WriteString("## Options considered\n\n")
	for _, o := range c.Options {
		b.WriteString("- " + o + "\n")
	}
	b.WriteString("\n## Cost and risk\n\n" + c.CostRisk + "\n\n")
	b.WriteString("## Ask\n\n" + c.Ask + "\n")
	return b.String()
}

// ProposalService creates and decides proposals.
type ProposalService struct {
	Issues *IssueService
}

func NewProposalService(issues *IssueService) *ProposalService {
	return &ProposalService{Issues: issues}
}

// ProposalCreateParams are the already-authorized inputs to Create. The caller
// has checked that the actor may file for SquadID.
type ProposalCreateParams struct {
	WorkspaceID    pgtype.UUID
	SquadID        pgtype.UUID
	HandoffSquadID pgtype.UUID
	Content        ProposalContent
	Urgent         bool
	UrgentReason   string
	CreatorType    string // "agent" or "member"
	CreatorID      pgtype.UUID
}

// ProposalCreateResult carries the new rows. Events are the caller's to
// publish: the issue:created payload is a handler-layer type.
type ProposalCreateResult struct {
	Proposal db.Proposal
	Issue    db.Issue
}

// Create writes the proposal and its backing issue in one transaction.
//
// The issue is unassigned and starts in_review, so it raises no inbox item and
// no run: it waits for the next brief. An urgent proposal is assigned to the
// boss (the oldest workspace owner), which raises the ordinary issue_assigned
// inbox item at once.
//
// The issue row is inserted here, as the autopilot does, rather than through
// IssueService.Create: that method owns its own transaction, and the proposal
// and its issue must commit together.
func (s *ProposalService) Create(ctx context.Context, p ProposalCreateParams) (ProposalCreateResult, error) {
	issueCountPolicy := ResolveIssueCountPolicy(ctx, s.Issues.Entitlements, p.WorkspaceID)

	tx, err := s.Issues.TxStarter.Begin(ctx)
	if err != nil {
		return ProposalCreateResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Issues.Queries.WithTx(tx)

	// One create at a time per workspace, so the limits counted below cannot
	// be passed by concurrent requests.
	if err := qtx.LockProposalWorkspace(ctx, p.WorkspaceID); err != nil {
		return ProposalCreateResult{}, fmt.Errorf("lock proposal workspace: %w", err)
	}

	if _, err := liveSquad(ctx, qtx, p.WorkspaceID, p.SquadID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProposalCreateResult{}, ErrProposalSquadNotFound
		}
		return ProposalCreateResult{}, err
	}
	if p.HandoffSquadID.Valid {
		if _, err := liveSquad(ctx, qtx, p.WorkspaceID, p.HandoffSquadID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ProposalCreateResult{}, ErrProposalHandoffSquadNotFound
			}
			return ProposalCreateResult{}, err
		}
	}

	if err := s.checkLimits(ctx, qtx, p); err != nil {
		return ProposalCreateResult{}, err
	}

	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if p.Urgent {
		boss, err := qtx.GetOldestWorkspaceOwnerMember(ctx, p.WorkspaceID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ProposalCreateResult{}, ErrProposalNoBoss
			}
			return ProposalCreateResult{}, fmt.Errorf("load boss: %w", err)
		}
		assigneeType = pgtype.Text{String: "member", Valid: true}
		assigneeID = boss.UserID
	}

	issueNumber, err := AllocateIssueNumber(ctx, qtx, p.WorkspaceID, issueCountPolicy)
	if err != nil {
		return ProposalCreateResult{}, err
	}
	const status = "in_review"
	position, err := issueposition.NextTopPosition(ctx, tx, p.WorkspaceID, status)
	if err != nil {
		return ProposalCreateResult{}, fmt.Errorf("next top position: %w", err)
	}

	proposalID := dbid.NewV7()
	issue, err := qtx.CreateIssueWithOrigin(ctx, db.CreateIssueWithOriginParams{
		ID:           dbid.NewV7(),
		WorkspaceID:  p.WorkspaceID,
		Title:        p.Content.Title,
		Description:  pgtype.Text{String: p.Content.RenderDescription(p.UrgentReason), Valid: true},
		Status:       status,
		Priority:     "none",
		AssigneeType: assigneeType,
		AssigneeID:   assigneeID,
		CreatorType:  p.CreatorType,
		CreatorID:    p.CreatorID,
		Position:     position,
		Number:       issueNumber,
		OriginType:   pgtype.Text{String: ProposalOriginType, Valid: true},
		OriginID:     proposalID,
	})
	if err != nil {
		return ProposalCreateResult{}, fmt.Errorf("create proposal issue: %w", err)
	}

	proposal, err := qtx.CreateProposal(ctx, db.CreateProposalParams{
		ID:             proposalID,
		WorkspaceID:    p.WorkspaceID,
		SquadID:        p.SquadID,
		IssueID:        issue.ID,
		HandoffSquadID: p.HandoffSquadID,
		Urgent:         p.Urgent,
		UrgentReason:   p.UrgentReason,
		CreatedByType:  p.CreatorType,
		CreatedByID:    p.CreatorID,
	})
	if err != nil {
		return ProposalCreateResult{}, fmt.Errorf("create proposal: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ProposalCreateResult{}, fmt.Errorf("commit: %w", err)
	}
	return ProposalCreateResult{Proposal: proposal, Issue: issue}, nil
}

func liveSquad(ctx context.Context, q *db.Queries, workspaceID, squadID pgtype.UUID) (db.Squad, error) {
	squad, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: squadID, WorkspaceID: workspaceID})
	if err != nil {
		return db.Squad{}, err
	}
	if squad.ArchivedAt.Valid {
		return db.Squad{}, pgx.ErrNoRows
	}
	return squad, nil
}

// checkLimits enforces the attention limits. It runs under the workspace
// proposal lock. Each message tells the filing agent what to do instead.
func (s *ProposalService) checkLimits(ctx context.Context, q *db.Queries, p ProposalCreateParams) error {
	pendingInSquad, err := q.CountPendingProposalsBySquad(ctx, db.CountPendingProposalsBySquadParams{
		WorkspaceID: p.WorkspaceID, SquadID: p.SquadID,
	})
	if err != nil {
		return fmt.Errorf("count pending proposals by squad: %w", err)
	}
	if pendingInSquad >= ProposalMaxPendingPerSquad {
		return &ProposalLimitError{Message: fmt.Sprintf(
			"this squad already has %d proposals waiting for the boss's decision (the limit); "+
				"wait until some are decided before filing another. If nothing new clears the bar, "+
				"record that with `multica squad activity ... --reason`",
			pendingInSquad)}
	}

	createdThisWeek, err := q.CountRecentProposalsBySquad(ctx, db.CountRecentProposalsBySquadParams{
		WorkspaceID: p.WorkspaceID, SquadID: p.SquadID,
	})
	if err != nil {
		return fmt.Errorf("count recent proposals by squad: %w", err)
	}
	if createdThisWeek >= ProposalMaxCreatedPerSquadPerWeek {
		return &ProposalLimitError{Message: fmt.Sprintf(
			"this squad has already filed %d proposals in the last 7 days (the limit); "+
				"hold this one for next week, or fold it into a pending proposal. "+
				"Record the decision to wait with `multica squad activity ... --reason`",
			createdThisWeek)}
	}

	pendingInWorkspace, err := q.CountPendingProposalsByWorkspace(ctx, p.WorkspaceID)
	if err != nil {
		return fmt.Errorf("count pending proposals by workspace: %w", err)
	}
	if pendingInWorkspace >= ProposalMaxPendingPerWorkspace {
		return &ProposalLimitError{Message: fmt.Sprintf(
			"the boss already has %d proposals waiting across the workspace (the limit); "+
				"decide the pending ones first, then file this again. "+
				"Record the decision to wait with `multica squad activity ... --reason`",
			pendingInWorkspace)}
	}

	if p.Urgent {
		urgentThisWeek, err := q.CountRecentUrgentProposalsBySquad(ctx, db.CountRecentUrgentProposalsBySquadParams{
			WorkspaceID: p.WorkspaceID, SquadID: p.SquadID,
		})
		if err != nil {
			return fmt.Errorf("count recent urgent proposals by squad: %w", err)
		}
		if urgentThisWeek >= ProposalMaxUrgentPerSquadPerWeek {
			return &ProposalLimitError{Message: fmt.Sprintf(
				"this squad has already filed %d urgent proposal in the last 7 days (the limit); "+
					"file this one without --urgent and it will reach the boss in the next brief",
				urgentThisWeek)}
		}
	}
	return nil
}

// ProposalDecideParams are the already-authorized inputs to Decide. The caller
// has checked that the decider is an owner or admin member.
type ProposalDecideParams struct {
	WorkspaceID pgtype.UUID
	ProposalID  pgtype.UUID
	DeciderID   pgtype.UUID // the deciding member's user id
	Decision    string      // ProposalDecisionApprove or ProposalDecisionReject
	Note        string
	// Kickoff is passed through to IssueService.Create for the handoff
	// kickoff issue, so the handler can supply its issue:created payload.
	Kickoff IssueCreateOpts
}

// ProposalDecideResult carries the changed rows so the caller can publish
// events after commit.
type ProposalDecideResult struct {
	Proposal db.Proposal
	// Issue is the backing issue after its status change; PrevIssueStatus is
	// its status before.
	Issue           db.Issue
	PrevIssueStatus string
	// Comment is the rejection note, posted in the decision transaction. Nil
	// on approval.
	Comment *db.Comment
	// Project and Kickoff are set when an approval hands off to a squad.
	Project *db.Project
	Kickoff *db.Issue
}

// Decide settles a pending proposal.
//
// Approve moves the backing issue to done. With a handoff squad it also
// creates a project named after the proposal and a kickoff issue in it,
// assigned to that squad and linking back to the proposal issue.
//
// Reject moves the backing issue to cancelled and posts the note as a comment
// that mentions the proposing squad's leader, so the ordinary mention path
// wakes the leader. The note is how the boss asks for changes.
//
// The decision, the status change, the project and the rejection comment
// commit together. The kickoff issue goes through IssueService.Create, which
// owns its transaction (and enqueues the handoff leader), so it follows the
// commit; if it fails the decision is reverted and the error returned, which
// leaves the proposal pending for a retry.
func (s *ProposalService) Decide(ctx context.Context, p ProposalDecideParams) (ProposalDecideResult, error) {
	if p.Decision != ProposalDecisionApprove && p.Decision != ProposalDecisionReject {
		return ProposalDecideResult{}, fmt.Errorf("unsupported decision %q", p.Decision)
	}
	tx, err := s.Issues.TxStarter.Begin(ctx)
	if err != nil {
		return ProposalDecideResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Issues.Queries.WithTx(tx)

	current, err := qtx.LockProposalInWorkspace(ctx, db.LockProposalInWorkspaceParams{ID: p.ProposalID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProposalDecideResult{}, ErrProposalNotFound
		}
		return ProposalDecideResult{}, fmt.Errorf("lock proposal: %w", err)
	}
	if current.State != ProposalStatePending {
		return ProposalDecideResult{}, ErrProposalAlreadyDecided
	}
	issue, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: current.IssueID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		return ProposalDecideResult{}, fmt.Errorf("load proposal issue: %w", err)
	}

	result := ProposalDecideResult{PrevIssueStatus: issue.Status}
	state, issueStatus := ProposalStateApproved, "done"
	if p.Decision == ProposalDecisionReject {
		state, issueStatus = ProposalStateRejected, "cancelled"
	}

	if p.Decision == ProposalDecisionApprove && current.HandoffSquadID.Valid {
		if _, err := liveSquad(ctx, qtx, p.WorkspaceID, current.HandoffSquadID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ProposalDecideResult{}, ErrProposalHandoffSquadNotFound
			}
			return ProposalDecideResult{}, err
		}
		project, err := qtx.CreateProject(ctx, db.CreateProjectParams{
			WorkspaceID: p.WorkspaceID,
			Title:       issue.Title,
			Status:      "planned",
			Priority:    "none",
		})
		if err != nil {
			return ProposalDecideResult{}, fmt.Errorf("create handoff project: %w", err)
		}
		result.Project = &project
	}

	decided, err := qtx.DecideProposal(ctx, db.DecideProposalParams{
		ID: p.ProposalID, WorkspaceID: p.WorkspaceID,
		State: state, DecidedBy: p.DeciderID, DecisionNote: p.Note,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProposalDecideResult{}, ErrProposalAlreadyDecided
		}
		return ProposalDecideResult{}, fmt.Errorf("decide proposal: %w", err)
	}
	result.Proposal = decided

	updated, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: issue.ID, WorkspaceID: p.WorkspaceID, Status: issueStatus,
	})
	if err != nil {
		return ProposalDecideResult{}, fmt.Errorf("update proposal issue status: %w", err)
	}
	result.Issue = updated

	if p.Decision == ProposalDecisionReject {
		comment, err := s.createRejectionComment(ctx, qtx, current, issue, p)
		if err != nil {
			return ProposalDecideResult{}, err
		}
		result.Comment = &comment
	}

	if err := tx.Commit(ctx); err != nil {
		return ProposalDecideResult{}, fmt.Errorf("commit: %w", err)
	}

	if result.Project != nil {
		kickoff, err := s.createKickoff(ctx, p, current, result.Issue, *result.Project)
		if err != nil {
			if revertErr := s.revertDecision(ctx, current, issue, *result.Project); revertErr != nil {
				slog.Error("revert proposal decision after kickoff failure failed",
					"proposal_id", util.UUIDToString(current.ID), "error", revertErr)
			}
			return ProposalDecideResult{}, err
		}
		result.Kickoff = &kickoff
	}
	return result, nil
}

// createRejectionComment posts the boss's note on the proposal issue,
// mentioning the proposing squad's leader with the exact mention markdown the
// comment trigger path parses.
func (s *ProposalService) createRejectionComment(ctx context.Context, q *db.Queries, proposal db.Proposal, issue db.Issue, p ProposalDecideParams) (db.Comment, error) {
	note := strings.TrimSpace(p.Note)
	if note == "" {
		note = "The boss rejected this proposal without a note."
	}
	content := note
	if squad, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: proposal.SquadID, WorkspaceID: p.WorkspaceID}); err == nil {
		if leader, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: squad.LeaderID, WorkspaceID: p.WorkspaceID}); err == nil {
			content = fmt.Sprintf("[@%s](mention://agent/%s) %s", leader.Name, util.UUIDToString(leader.ID), note)
		}
	}
	row, err := q.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: p.WorkspaceID,
		AuthorType:  "member",
		AuthorID:    p.DeciderID,
		Content:     content,
		Type:        "comment",
	})
	if err != nil {
		return db.Comment{}, fmt.Errorf("post rejection comment: %w", err)
	}
	return row.Comment(), nil
}

// createKickoff creates the issue that starts the handed-off work, through the
// same IssueService.Create path the autopilot's issues use for their listeners:
// assigning it to a squad enqueues that squad's leader.
func (s *ProposalService) createKickoff(ctx context.Context, p ProposalDecideParams, proposal db.Proposal, proposalIssue db.Issue, project db.Project) (db.Issue, error) {
	var desc strings.Builder
	fmt.Fprintf(&desc, "The boss approved the proposal [%s](mention://issue/%s) and asked this squad to start the work.\n",
		proposalIssue.Title, util.UUIDToString(proposalIssue.ID))
	if note := strings.TrimSpace(p.Note); note != "" {
		desc.WriteString("\n## Note from the boss\n\n" + note + "\n")
	}
	res, err := s.Issues.Create(ctx, IssueCreateParams{
		WorkspaceID:    p.WorkspaceID,
		Title:          proposalIssue.Title,
		Description:    pgtype.Text{String: desc.String(), Valid: true},
		Status:         "todo",
		Priority:       "none",
		AssigneeType:   pgtype.Text{String: "squad", Valid: true},
		AssigneeID:     proposal.HandoffSquadID,
		CreatorType:    "member",
		CreatorID:      p.DeciderID,
		ProjectID:      project.ID,
		AllowDuplicate: true,
	}, p.Kickoff)
	if err != nil {
		return db.Issue{}, fmt.Errorf("create handoff kickoff issue: %w", err)
	}
	return res.Issue, nil
}

// revertDecision undoes an approval whose kickoff issue could not be created:
// the proposal goes back to pending, its issue to its previous status, and the
// handoff project is removed.
func (s *ProposalService) revertDecision(ctx context.Context, proposal db.Proposal, issue db.Issue, project db.Project) error {
	// The request context may be the reason for the failure; revert regardless.
	ctx = context.WithoutCancel(ctx)
	tx, err := s.Issues.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.Issues.Queries.WithTx(tx)
	if _, err := qtx.RevertProposalDecision(ctx, db.RevertProposalDecisionParams{ID: proposal.ID, WorkspaceID: proposal.WorkspaceID}); err != nil {
		return err
	}
	if _, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: issue.ID, WorkspaceID: issue.WorkspaceID, Status: issue.Status,
	}); err != nil {
		return err
	}
	if err := qtx.DeleteProject(ctx, db.DeleteProjectParams{ID: project.ID, WorkspaceID: project.WorkspaceID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
