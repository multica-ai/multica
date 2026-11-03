package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issueposition"
	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// The boss's attention is the scarce resource, so the server enforces these
// limits instead of trusting prompts. The per-squad pair are defaults: a squad's
// effective caps move with its acceptance rate and can be pinned (see
// attention.go). The workspace cap and the urgent rule never move.
const (
	// ProposalDefaultPendingPerSquad is a squad's default cap on proposals waiting.
	ProposalDefaultPendingPerSquad = 3
	// ProposalDefaultWeeklyPerSquad is a squad's default cap on proposals filed
	// over the rolling seven days, whatever their state. A revision is not a new
	// filing and does not count here.
	ProposalDefaultWeeklyPerSquad = 3
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
	// ProposalStateRevising: the boss asked for changes and the squad has not
	// resubmitted yet. The proposal is not in the boss's queue and does not
	// count towards the pending caps.
	ProposalStateRevising = "revising"
	// ProposalStateExpired: no decision within ProposalExpiryAfter. Set by the
	// daily expiry job, which records it as a non-acceptance.
	ProposalStateExpired = "expired"
)

// Proposal decisions, as accepted by the decision endpoint.
const (
	ProposalDecisionApprove = "approve"
	ProposalDecisionReject  = "reject"
	// ProposalDecisionRevise asks the squad to change the proposal. It needs a note.
	ProposalDecisionRevise = "revise"
	// ProposalDecisionSnooze keeps the proposal pending until a date.
	ProposalDecisionSnooze = "snooze"
)

const (
	// ProposalExpiryAfter is how long a pending proposal waits for a decision.
	// The window restarts when the squad resubmits, and a snooze moves it to a
	// full window after the snooze ends.
	ProposalExpiryAfter = 14 * 24 * time.Hour
	// ProposalMaxSnooze bounds how far ahead a snooze may put a proposal, so a
	// snooze cannot park one indefinitely.
	ProposalMaxSnooze = 30 * 24 * time.Hour
)

// ProposalOriginType is the issue origin_type of the issue that backs a
// proposal; the issue's origin_id is the proposal id.
const ProposalOriginType = "proposal"

// ProposalPayloadError reports that the proposal kind refused the filing's
// payload. Message names the problem for the filing agent.
type ProposalPayloadError struct{ Message string }

func (e *ProposalPayloadError) Error() string { return e.Message }

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
	// ErrProposalRevising: the boss asked for changes and the squad has not
	// resubmitted, so no decision can be made until it does.
	ErrProposalRevising = errors.New("proposal is waiting for the squad's revised version")
	// ErrProposalNotRevising: a resubmission for a proposal that did not ask
	// for changes.
	ErrProposalNotRevising = errors.New("proposal is not waiting for changes")
	// ErrProposalSquadUnavailable: the proposing squad no longer exists or is
	// archived, so a revision cannot be sent back to it.
	ErrProposalSquadUnavailable = errors.New("proposing squad no longer exists or is archived")
	// ErrProposalNoteRequired: asking for changes without saying what to change.
	ErrProposalNoteRequired = errors.New("a note is required when asking for changes")
	// ErrProposalSnoozeOutOfRange: the snooze date is not later than now, or is
	// further away than ProposalMaxSnooze.
	ErrProposalSnoozeOutOfRange = errors.New("until must be later than now and at most 30 days away")
	// ErrProposalKindNotFilable: the kind is filed by the system, not by a squad.
	ErrProposalKindNotFilable = errors.New("this kind of proposal is filed by the system, not by a squad")
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

// proposalExpiryInterval is ProposalExpiryAfter in the form the queries take.
func proposalExpiryInterval() pgtype.Interval {
	return pgtype.Interval{Microseconds: ProposalExpiryAfter.Microseconds(), Valid: true}
}

// ProposalService creates, decides and expires proposals.
type ProposalService struct {
	Issues *IssueService
	// Kinds dispatches a filing and its decision to the kind's handler. Nil
	// means the decision kind alone, which is what phase 1 and 2 callers get.
	Kinds *proposalkinds.Registry
	// Knowledge, when set, receives a record of each decision after it
	// commits. A failure to record never fails the decision.
	Knowledge DecisionRecorder
	// DecisionSearch, when set, finds decisions related to a new proposal. A
	// failure never fails the filing.
	DecisionSearch DecisionSearcher
	// AppURL is the web app origin, for the link in the decision record.
	AppURL string
}

// NewProposalService builds the proposal service. initiatives serves the kinds
// that act on initiatives; it may be nil when no such kind is filed.
func NewProposalService(issues *IssueService, initiatives proposalkinds.Initiatives) *ProposalService {
	return &ProposalService{Issues: issues, Kinds: proposalkinds.NewRegistry(initiatives)}
}

// registry returns the kind registry, building the decision-only one when the
// service was built without it.
func (s *ProposalService) registry() *proposalkinds.Registry {
	if s.Kinds == nil {
		s.Kinds = proposalkinds.NewRegistry(nil)
	}
	return s.Kinds
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
	// Kind selects the proposal kind; empty is a decision. Payload is the
	// kind's fields, checked by the kind's Validate.
	Kind    string
	Payload []byte
	// SystemFiled marks a filing the system makes (a stage gate). Only those
	// may file a kind the squads may not.
	SystemFiled bool
}

// ProposalCreateResult carries the new rows. Events are the caller's to
// publish: the issue:created payload is a handler-layer type.
type ProposalCreateResult struct {
	Proposal db.Proposal
	Issue    db.Issue
	// Related lists earlier proposals and decisions that look related. It is
	// computed after the commit and is empty when the lookup fails.
	Related []ProposalRelated
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
	kind, err := s.registry().Lookup(p.Kind)
	if err != nil {
		return ProposalCreateResult{}, err
	}
	if !kind.FiledByAgents() && !p.SystemFiled {
		return ProposalCreateResult{}, ErrProposalKindNotFilable
	}
	payload, err := kind.Validate(ctx, proposalkinds.Env{Q: s.Issues.Queries, WorkspaceID: p.WorkspaceID, SquadID: p.SquadID}, p.Payload)
	if err != nil {
		return ProposalCreateResult{}, &ProposalPayloadError{Message: err.Error()}
	}
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

	caps, err := EffectiveSquadCaps(ctx, qtx, p.WorkspaceID, p.SquadID)
	if err != nil {
		return ProposalCreateResult{}, err
	}
	if err := s.checkLimits(ctx, qtx, p, caps); err != nil {
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
		ExpiryWindow:   proposalExpiryInterval(),
		Kind:           kind.Key(),
		Payload:        payload,
	})
	if err != nil {
		return ProposalCreateResult{}, fmt.Errorf("create proposal: %w", err)
	}
	// Revision 1 is the body as filed. It is written here, not on a later read,
	// so the history never lacks the first body.
	if err := qtx.CreateProposalRevision(ctx, db.CreateProposalRevisionParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   p.WorkspaceID,
		ProposalID:    proposal.ID,
		Revision:      proposal.Revision,
		Body:          issue.Description.String,
		CreatedByType: p.CreatorType,
		CreatedByID:   p.CreatorID,
	}); err != nil {
		return ProposalCreateResult{}, fmt.Errorf("record proposal revision: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ProposalCreateResult{}, fmt.Errorf("commit: %w", err)
	}
	related := s.findPriorArt(ctx, p.WorkspaceID, proposal.ID, p.Content.Title, p.Content.Summary)
	return ProposalCreateResult{Proposal: proposal, Issue: issue, Related: related}, nil
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

// checkLimits enforces the attention limits on a new filing. It runs under the
// workspace proposal lock. Each message tells the filing agent what to do
// instead.
func (s *ProposalService) checkLimits(ctx context.Context, q *db.Queries, p ProposalCreateParams, caps SquadCaps) error {
	if err := s.checkPendingSquadCap(ctx, q, p.WorkspaceID, p.SquadID, caps.Pending, "filing another"); err != nil {
		return err
	}

	createdThisWeek, err := q.CountRecentProposalsBySquad(ctx, db.CountRecentProposalsBySquadParams{
		WorkspaceID: p.WorkspaceID, SquadID: p.SquadID,
	})
	if err != nil {
		return fmt.Errorf("count recent proposals by squad: %w", err)
	}
	if createdThisWeek >= int64(caps.Weekly) {
		return &ProposalLimitError{Message: fmt.Sprintf(
			"this squad has already filed %d proposals in the last 7 days (the limit); "+
				"hold this one for next week, or fold it into a pending proposal. "+
				"Record the decision to wait with `multica squad activity ... --reason`",
			createdThisWeek)}
	}

	if err := s.checkPendingWorkspaceCap(ctx, q, p.WorkspaceID, "file this again"); err != nil {
		return err
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

// checkPendingSquadCap refuses when the squad already has the maximum waiting
// for the boss. action completes "...before %s" in the message: the filing
// agent reads it as the thing it was about to do.
func (s *ProposalService) checkPendingSquadCap(ctx context.Context, q *db.Queries, workspaceID, squadID pgtype.UUID, limit int, action string) error {
	pendingInSquad, err := q.CountPendingProposalsBySquad(ctx, db.CountPendingProposalsBySquadParams{
		WorkspaceID: workspaceID, SquadID: squadID,
	})
	if err != nil {
		return fmt.Errorf("count pending proposals by squad: %w", err)
	}
	if pendingInSquad >= int64(limit) {
		return &ProposalLimitError{Message: fmt.Sprintf(
			"this squad already has %d proposals waiting for the boss's decision (the limit); "+
				"wait until some are decided before %s. If nothing new clears the bar, "+
				"record that with `multica squad activity ... --reason`",
			pendingInSquad, action)}
	}
	return nil
}

// checkPendingWorkspaceCap refuses when the boss's queue is full. retry
// completes "decide the pending ones first, then ...": the verb phrase the
// caller was about to carry out.
func (s *ProposalService) checkPendingWorkspaceCap(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, retry string) error {
	pendingInWorkspace, err := q.CountPendingProposalsByWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("count pending proposals by workspace: %w", err)
	}
	if pendingInWorkspace >= ProposalMaxPendingPerWorkspace {
		return &ProposalLimitError{Message: fmt.Sprintf(
			"the boss already has %d proposals waiting across the workspace (the limit); "+
				"decide the pending ones first, then %s. "+
				"Record the decision to wait with `multica squad activity ... --reason`",
			pendingInWorkspace, retry)}
	}
	return nil
}

// ProposalDecideParams are the already-authorized inputs to Decide. The caller
// has checked that the decider is an owner or admin member.
type ProposalDecideParams struct {
	WorkspaceID pgtype.UUID
	ProposalID  pgtype.UUID
	DeciderID   pgtype.UUID // the deciding member's user id
	Decision    string      // one of the ProposalDecision values
	// Note is the rejection reason, the changes asked for, or empty for approve
	// and snooze.
	Note string
	// Until is the snooze end. Only used by ProposalDecisionSnooze.
	Until time.Time
	// Outcome is the boss's choice at an operate-stage review gate: continue,
	// scale or sunset. Only a stage_gate approval takes one.
	Outcome string
	// Kickoff is passed through to IssueService.Create for the handoff
	// kickoff issue, so the handler can supply its issue:created payload.
	Kickoff IssueCreateOpts
}

// ProposalDecideResult carries the changed rows so the caller can publish
// events after commit.
type ProposalDecideResult struct {
	Proposal db.Proposal
	// Issue is the backing issue after the decision; PrevIssueStatus is its
	// status before.
	Issue           db.Issue
	PrevIssueStatus string
	// Comment is the note posted on the proposal issue in the decision
	// transaction: the rejection note or the changes asked for. Nil otherwise.
	Comment *db.Comment
	// Project and Kickoff are set when an approval hands off to a squad.
	Project *db.Project
	Kickoff *db.Issue
}

// Decide settles a proposal according to the decision.
//
// Approve moves the backing issue to done. With a handoff squad it also
// creates a project named after the proposal and a kickoff issue in it,
// assigned to that squad and linking back to the proposal issue.
//
// Reject moves the backing issue to cancelled and posts the note as a comment
// that mentions the proposing squad's leader, so the ordinary mention path
// wakes the leader.
//
// Revise moves the proposal to revising, assigns the backing issue to the
// proposing squad and posts the changes asked for, which wakes the leader the
// same way. Snooze keeps the proposal pending until a date.
//
// Every decision except snooze is refused while the proposal is revising.
func (s *ProposalService) Decide(ctx context.Context, p ProposalDecideParams) (ProposalDecideResult, error) {
	switch p.Decision {
	case ProposalDecisionApprove, ProposalDecisionReject:
		return s.settle(ctx, p)
	case ProposalDecisionRevise:
		return s.askForChanges(ctx, p)
	case ProposalDecisionSnooze:
		return s.snooze(ctx, p)
	}
	return ProposalDecideResult{}, fmt.Errorf("unsupported decision %q", p.Decision)
}

// lockOpenProposal locks the proposal for a decision and refuses it unless it
// is pending. A revising proposal has its own error so the caller can say the
// squad has to resubmit first.
func lockOpenProposal(ctx context.Context, q *db.Queries, workspaceID, proposalID pgtype.UUID) (db.Proposal, error) {
	current, err := q.LockProposalInWorkspace(ctx, db.LockProposalInWorkspaceParams{ID: proposalID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Proposal{}, ErrProposalNotFound
		}
		return db.Proposal{}, fmt.Errorf("lock proposal: %w", err)
	}
	switch current.State {
	case ProposalStatePending:
		return current, nil
	case ProposalStateRevising:
		return db.Proposal{}, ErrProposalRevising
	}
	return db.Proposal{}, ErrProposalAlreadyDecided
}

// settle approves or rejects a pending proposal.
func (s *ProposalService) settle(ctx context.Context, p ProposalDecideParams) (ProposalDecideResult, error) {
	tx, err := s.Issues.TxStarter.Begin(ctx)
	if err != nil {
		return ProposalDecideResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Issues.Queries.WithTx(tx)

	current, err := lockOpenProposal(ctx, qtx, p.WorkspaceID, p.ProposalID)
	if err != nil {
		return ProposalDecideResult{}, err
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

	kind, err := s.registry().Lookup(current.Kind)
	if err != nil {
		return ProposalDecideResult{}, err
	}
	decision := proposalkinds.Decision{
		Tx: tx, WorkspaceID: p.WorkspaceID, Proposal: current, Issue: issue,
		DeciderID: p.DeciderID, Note: p.Note, Outcome: p.Outcome,
	}
	if p.Decision == ProposalDecisionApprove {
		effect, err := kind.Apply(ctx, qtx, decision)
		if err != nil {
			return ProposalDecideResult{}, mapKindError(err)
		}
		result.Project = effect.Project
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
		note := strings.TrimSpace(p.Note)
		if note == "" {
			note = "The boss rejected this proposal without a note."
		}
		comment, err := s.postNoteToLeader(ctx, qtx, current, issue, p.WorkspaceID, p.DeciderID, note)
		if err != nil {
			return ProposalDecideResult{}, err
		}
		result.Comment = &comment
		if err := kind.Reject(ctx, qtx, decision); err != nil {
			return ProposalDecideResult{}, mapKindError(err)
		}
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
	s.recordDecision(ctx, p, result)
	return result, nil
}

// askForChanges moves a pending proposal back to the squad. The backing issue
// goes to the squad, and the note goes on it mentioning the leader, so the
// leader is woken by the ordinary mention path. The proposal stays out of the
// boss's queue until the squad resubmits.
func (s *ProposalService) askForChanges(ctx context.Context, p ProposalDecideParams) (ProposalDecideResult, error) {
	note := strings.TrimSpace(p.Note)
	if note == "" {
		return ProposalDecideResult{}, ErrProposalNoteRequired
	}
	tx, err := s.Issues.TxStarter.Begin(ctx)
	if err != nil {
		return ProposalDecideResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Issues.Queries.WithTx(tx)

	current, err := lockOpenProposal(ctx, qtx, p.WorkspaceID, p.ProposalID)
	if err != nil {
		return ProposalDecideResult{}, err
	}
	squad, err := liveSquad(ctx, qtx, p.WorkspaceID, current.SquadID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProposalDecideResult{}, ErrProposalSquadUnavailable
		}
		return ProposalDecideResult{}, err
	}
	issue, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: current.IssueID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		return ProposalDecideResult{}, fmt.Errorf("load proposal issue: %w", err)
	}

	decided, err := qtx.RequestProposalRevision(ctx, db.RequestProposalRevisionParams{
		ID: p.ProposalID, WorkspaceID: p.WorkspaceID, DecidedBy: p.DeciderID, DecisionNote: note,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProposalDecideResult{}, ErrProposalAlreadyDecided
		}
		return ProposalDecideResult{}, fmt.Errorf("request revision: %w", err)
	}

	updated, err := qtx.UpdateIssue(ctx, reassignIssueParams(issue, squad.ID))
	if err != nil {
		return ProposalDecideResult{}, fmt.Errorf("assign proposal issue to squad: %w", err)
	}

	comment, err := s.postNoteToLeader(ctx, qtx, current, issue, p.WorkspaceID, p.DeciderID, note)
	if err != nil {
		return ProposalDecideResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalDecideResult{}, fmt.Errorf("commit: %w", err)
	}
	return ProposalDecideResult{Proposal: decided, Issue: updated, PrevIssueStatus: issue.Status, Comment: &comment}, nil
}

// mapKindError turns a kind's refusal into the proposal service's error, so the
// handler answers a stale handoff squad the same way for every kind.
func mapKindError(err error) error {
	if errors.Is(err, proposalkinds.ErrSquadNotLive) {
		return ErrProposalHandoffSquadNotFound
	}
	return err
}

// reassignIssueParams changes only the assignee of an issue. Fields that the
// UpdateIssue query writes unconditionally are carried over from the issue, so
// they are not cleared.
func reassignIssueParams(issue db.Issue, squadID pgtype.UUID) db.UpdateIssueParams {
	return db.UpdateIssueParams{
		ID:                 issue.ID,
		AssigneeType:       pgtype.Text{String: "squad", Valid: true},
		AssigneeID:         squadID,
		StartDate:          issue.StartDate,
		DueDate:            issue.DueDate,
		ParentIssueID:      issue.ParentIssueID,
		ProjectID:          issue.ProjectID,
		Stage:              issue.Stage,
		DuplicateOfIssueID: issue.DuplicateOfIssueID,
	}
}

// snooze keeps a pending proposal pending until the given time. It counts
// towards the caps, and its decision window moves past the snooze.
func (s *ProposalService) snooze(ctx context.Context, p ProposalDecideParams) (ProposalDecideResult, error) {
	now := time.Now()
	if !p.Until.After(now) || p.Until.After(now.Add(ProposalMaxSnooze)) {
		return ProposalDecideResult{}, ErrProposalSnoozeOutOfRange
	}
	tx, err := s.Issues.TxStarter.Begin(ctx)
	if err != nil {
		return ProposalDecideResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Issues.Queries.WithTx(tx)

	current, err := lockOpenProposal(ctx, qtx, p.WorkspaceID, p.ProposalID)
	if err != nil {
		return ProposalDecideResult{}, err
	}
	issue, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: current.IssueID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		return ProposalDecideResult{}, fmt.Errorf("load proposal issue: %w", err)
	}
	snoozed, err := qtx.SnoozeProposal(ctx, db.SnoozeProposalParams{
		ID:           p.ProposalID,
		WorkspaceID:  p.WorkspaceID,
		SnoozedUntil: pgtype.Timestamptz{Time: p.Until, Valid: true},
		ExpiryWindow: proposalExpiryInterval(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProposalDecideResult{}, ErrProposalAlreadyDecided
		}
		return ProposalDecideResult{}, fmt.Errorf("snooze proposal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalDecideResult{}, fmt.Errorf("commit: %w", err)
	}
	return ProposalDecideResult{Proposal: snoozed, Issue: issue, PrevIssueStatus: issue.Status}, nil
}

// ProposalResubmitParams are the already-authorized inputs to Resubmit. The
// caller has checked that the actor may act for the proposal's squad.
type ProposalResubmitParams struct {
	WorkspaceID pgtype.UUID
	ProposalID  pgtype.UUID
	Content     ProposalContent
	// CreatorType and CreatorID are the actor that sent the revision; the
	// history records them with the new body.
	CreatorType string // "agent" or "member"
	CreatorID   pgtype.UUID
}

// ProposalResubmitResult carries the changed rows for the caller's events.
type ProposalResubmitResult struct {
	Proposal        db.Proposal
	Issue           db.Issue
	PrevIssueStatus string
}

// Resubmit replaces a revising proposal's body and returns it to the boss's
// queue. The revision number goes up, and the proposal's creation time stays,
// so the weekly creation cap does not count the revision. The pending caps do
// apply, because the proposal is going back into the queue. The urgency and the
// handoff squad are kept as they were filed.
func (s *ProposalService) Resubmit(ctx context.Context, p ProposalResubmitParams) (ProposalResubmitResult, error) {
	tx, err := s.Issues.TxStarter.Begin(ctx)
	if err != nil {
		return ProposalResubmitResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Issues.Queries.WithTx(tx)

	// The same workspace lock as Create, so the pending caps counted below
	// cannot be passed by a concurrent filing or resubmission.
	if err := qtx.LockProposalWorkspace(ctx, p.WorkspaceID); err != nil {
		return ProposalResubmitResult{}, fmt.Errorf("lock proposal workspace: %w", err)
	}
	current, err := qtx.LockProposalInWorkspace(ctx, db.LockProposalInWorkspaceParams{ID: p.ProposalID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProposalResubmitResult{}, ErrProposalNotFound
		}
		return ProposalResubmitResult{}, fmt.Errorf("lock proposal: %w", err)
	}
	switch current.State {
	case ProposalStateRevising:
	case ProposalStatePending:
		return ProposalResubmitResult{}, ErrProposalNotRevising
	default:
		return ProposalResubmitResult{}, ErrProposalAlreadyDecided
	}

	caps, err := EffectiveSquadCaps(ctx, qtx, p.WorkspaceID, current.SquadID)
	if err != nil {
		return ProposalResubmitResult{}, err
	}
	if err := s.checkPendingSquadCap(ctx, qtx, p.WorkspaceID, current.SquadID, caps.Pending, "sending this revision back"); err != nil {
		return ProposalResubmitResult{}, err
	}
	if err := s.checkPendingWorkspaceCap(ctx, qtx, p.WorkspaceID, "send this revision back again"); err != nil {
		return ProposalResubmitResult{}, err
	}

	issue, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: current.IssueID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		return ProposalResubmitResult{}, fmt.Errorf("load proposal issue: %w", err)
	}

	// Urgent proposals go back to the boss, as they were filed. Others are
	// unassigned until the next brief, so the assignee is cleared.
	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if current.Urgent {
		boss, err := qtx.GetOldestWorkspaceOwnerMember(ctx, p.WorkspaceID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ProposalResubmitResult{}, ErrProposalNoBoss
			}
			return ProposalResubmitResult{}, fmt.Errorf("load boss: %w", err)
		}
		assigneeType = pgtype.Text{String: "member", Valid: true}
		assigneeID = boss.UserID
	}

	updated, err := qtx.UpdateIssue(ctx, db.UpdateIssueParams{
		ID:                 issue.ID,
		Title:              pgtype.Text{String: p.Content.Title, Valid: true},
		Description:        pgtype.Text{String: p.Content.RenderDescription(current.UrgentReason), Valid: true},
		AssigneeType:       assigneeType,
		AssigneeID:         assigneeID,
		StartDate:          issue.StartDate,
		DueDate:            issue.DueDate,
		ParentIssueID:      issue.ParentIssueID,
		ProjectID:          issue.ProjectID,
		Stage:              issue.Stage,
		DuplicateOfIssueID: issue.DuplicateOfIssueID,
	})
	if err != nil {
		return ProposalResubmitResult{}, fmt.Errorf("update proposal issue: %w", err)
	}

	resubmitted, err := qtx.ResubmitProposal(ctx, db.ResubmitProposalParams{
		ID: p.ProposalID, WorkspaceID: p.WorkspaceID, ExpiryWindow: proposalExpiryInterval(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProposalResubmitResult{}, ErrProposalNotRevising
		}
		return ProposalResubmitResult{}, fmt.Errorf("resubmit proposal: %w", err)
	}
	// The new body and its history row commit together.
	if err := qtx.CreateProposalRevision(ctx, db.CreateProposalRevisionParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   p.WorkspaceID,
		ProposalID:    resubmitted.ID,
		Revision:      resubmitted.Revision,
		Body:          updated.Description.String,
		CreatedByType: p.CreatorType,
		CreatedByID:   p.CreatorID,
	}); err != nil {
		return ProposalResubmitResult{}, fmt.Errorf("record proposal revision: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalResubmitResult{}, fmt.Errorf("commit: %w", err)
	}
	return ProposalResubmitResult{Proposal: resubmitted, Issue: updated, PrevIssueStatus: issue.Status}, nil
}

// proposalKnowledgeTimeout bounds the decision record write, so a sidecar that
// hangs cannot hold the decision response.
const proposalKnowledgeTimeout = 3 * time.Second

// recordDecision writes the decision to the knowledge base. The decision has
// already committed, so any failure is logged and left to the retry the
// knowledge service keeps for its failed writes.
func (s *ProposalService) recordDecision(ctx context.Context, p ProposalDecideParams, res ProposalDecideResult) {
	if s.Knowledge == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), proposalKnowledgeTimeout)
	defer cancel()

	rec := DecisionRecord{
		ProposalID:     res.Proposal.ID,
		Title:          res.Issue.Title,
		Recommendation: markdownSection(res.Issue.Description.String, "Recommendation"),
		Decision:       res.Proposal.State,
		Note:           p.Note,
		DecidedAt:      time.Now(),
	}
	if res.Proposal.DecidedAt.Valid {
		rec.DecidedAt = res.Proposal.DecidedAt.Time
	}
	q := s.Issues.Queries
	if squad, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: res.Proposal.SquadID, WorkspaceID: p.WorkspaceID}); err == nil {
		rec.Squad = squad.Name
	}
	if ws, err := q.GetWorkspace(ctx, p.WorkspaceID); err == nil {
		rec.Link = s.AppURL + "/" + ws.Slug + "/issues/" + IssueIdentifier(ws.IssuePrefix, res.Issue.Number)
	}
	if err := s.Knowledge.WriteDecisionRecord(ctx, p.WorkspaceID, rec); err != nil {
		slog.Warn("knowledge: could not record a proposal decision; it is kept to retry",
			"proposal_id", util.UUIDToString(res.Proposal.ID), "error", err)
	}
}

// markdownSection returns the body of a "## name" section of a rendered
// proposal, up to the next "## " heading.
func markdownSection(markdown, name string) string {
	_, rest, found := strings.Cut(markdown, "\n## "+name+"\n")
	if !found {
		if after, ok := strings.CutPrefix(markdown, "## "+name+"\n"); ok {
			rest, found = after, true
		}
	}
	if !found {
		return ""
	}
	body, _, _ := strings.Cut(rest, "\n## ")
	return strings.TrimSpace(body)
}

// postNoteToLeader posts a boss's note on the proposal issue, mentioning the
// proposing squad's leader with the exact mention markdown the comment trigger
// path parses. Rejections and requests for changes both use it.
func (s *ProposalService) postNoteToLeader(ctx context.Context, q *db.Queries, proposal db.Proposal, issue db.Issue, workspaceID, deciderID pgtype.UUID, note string) (db.Comment, error) {
	content := note
	if squad, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: proposal.SquadID, WorkspaceID: workspaceID}); err == nil {
		if leader, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: squad.LeaderID, WorkspaceID: workspaceID}); err == nil {
			content = fmt.Sprintf("[@%s](mention://agent/%s) %s", leader.Name, util.UUIDToString(leader.ID), note)
		}
	}
	row, err := q.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: workspaceID,
		AuthorType:  "member",
		AuthorID:    deciderID,
		Content:     content,
		Type:        "comment",
	})
	if err != nil {
		return db.Comment{}, fmt.Errorf("post boss note: %w", err)
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
