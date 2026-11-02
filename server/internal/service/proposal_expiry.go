package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

const (
	// proposalExpiryBatchSize bounds the rows read per query, so a backlog
	// drains in steps rather than in one large transaction.
	proposalExpiryBatchSize = 100
	// proposalExpiryMaxBatches bounds one run. A backlog larger than this
	// finishes on the next daily run.
	proposalExpiryMaxBatches = 50
)

// ProposalExpireResult is one proposal the expiry run expired.
type ProposalExpireResult struct {
	Proposal        db.Proposal
	Issue           db.Issue
	PrevIssueStatus string
	// Comment is the system note left on the proposal issue.
	Comment db.Comment
}

// ExpireDue expires the pending proposals whose decision window has passed.
//
// Each proposal is expired in its own transaction, which locks the row and
// repeats the expiry predicate, so a run that repeats, or races with a
// decision, expires a proposal at most once and wakes its leader at most once.
// A proposal that fails is logged and left for the next run; the others go on.
// Returns what was expired and the joined errors.
func (s *ProposalService) ExpireDue(ctx context.Context) ([]ProposalExpireResult, error) {
	var out []ProposalExpireResult
	var errs []error
	for batch := 0; batch < proposalExpiryMaxBatches; batch++ {
		due, err := s.Issues.Queries.ListDueProposals(ctx, proposalExpiryBatchSize)
		if err != nil {
			return out, errors.Join(append(errs, fmt.Errorf("list due proposals: %w", err))...)
		}
		expiredInBatch := 0
		for _, d := range due {
			res, expired, err := s.expireOne(ctx, d.WorkspaceID, d.ID)
			if err != nil {
				slog.Warn("proposal expiry failed; it stays pending for the next run",
					"proposal_id", util.UUIDToString(d.ID), "error", err)
				errs = append(errs, err)
				continue
			}
			if expired {
				out = append(out, res)
				expiredInBatch++
			}
		}
		// A short batch is the last one. A batch where nothing expired and
		// something failed would fail the same way again, so stop there too.
		if len(due) < proposalExpiryBatchSize || expiredInBatch == 0 {
			break
		}
	}
	return out, errors.Join(errs...)
}

// expireOne expires one proposal if it is still due. It reports false, and
// changes nothing, when the proposal was decided, snoozed or resubmitted since
// the list was read.
func (s *ProposalService) expireOne(ctx context.Context, workspaceID, proposalID pgtype.UUID) (ProposalExpireResult, bool, error) {
	tx, err := s.Issues.TxStarter.Begin(ctx)
	if err != nil {
		return ProposalExpireResult{}, false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Issues.Queries.WithTx(tx)

	if _, err := qtx.LockProposalInWorkspace(ctx, db.LockProposalInWorkspaceParams{ID: proposalID, WorkspaceID: workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProposalExpireResult{}, false, nil
		}
		return ProposalExpireResult{}, false, fmt.Errorf("lock proposal: %w", err)
	}
	expired, err := qtx.ExpireProposal(ctx, db.ExpireProposalParams{ID: proposalID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProposalExpireResult{}, false, nil
		}
		return ProposalExpireResult{}, false, fmt.Errorf("expire proposal: %w", err)
	}
	issue, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: expired.IssueID, WorkspaceID: workspaceID})
	if err != nil {
		return ProposalExpireResult{}, false, fmt.Errorf("load proposal issue: %w", err)
	}
	updated, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: issue.ID, WorkspaceID: workspaceID, Status: "cancelled",
	})
	if err != nil {
		return ProposalExpireResult{}, false, fmt.Errorf("cancel proposal issue: %w", err)
	}
	comment, err := qtx.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: workspaceID,
		// Written as the system, like the runtime notice: the zero UUID is
		// the author id, and clients branch on author_type.
		AuthorType: "system",
		AuthorID:   pgtype.UUID{Valid: true},
		Content:    proposalExpiredNote(),
		Type:       "system",
	})
	if err != nil {
		return ProposalExpireResult{}, false, fmt.Errorf("post expiry note: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalExpireResult{}, false, fmt.Errorf("commit: %w", err)
	}

	res := ProposalExpireResult{
		Proposal: expired, Issue: updated, PrevIssueStatus: issue.Status, Comment: comment.Comment(),
	}
	s.wakeLeaderAfterExpiry(ctx, res)
	return res, true, nil
}

// wakeLeaderAfterExpiry queues one run for the proposing squad's leader, on the
// expiry note, so the leader can decide whether to file it again. It runs after
// the commit, and a failure is logged: the proposal is expired either way.
func (s *ProposalService) wakeLeaderAfterExpiry(ctx context.Context, res ProposalExpireResult) {
	if s.Issues.TaskService == nil {
		return
	}
	squad, err := liveSquad(ctx, s.Issues.Queries, res.Proposal.WorkspaceID, res.Proposal.SquadID)
	if err != nil {
		slog.Info("proposal expired; squad is gone, leader not woken",
			"proposal_id", util.UUIDToString(res.Proposal.ID), "error", err)
		return
	}
	if _, err := s.Issues.TaskService.EnqueueTaskForSquadLeader(ctx, res.Issue, squad.LeaderID, squad.ID, res.Comment.ID, OriginDerived); err != nil {
		slog.Warn("proposal expired; could not wake its leader",
			"proposal_id", util.UUIDToString(res.Proposal.ID), "error", err)
	}
}

// proposalExpiredNote is the system note on an expired proposal. It is the
// text the leader reads when it is woken.
func proposalExpiredNote() string {
	return fmt.Sprintf(
		"This proposal expired: the boss made no decision within %d days, so it no longer counts "+
			"against the squad's limits. If it still matters, file it again with `multica proposal create`, "+
			"and cite this one.",
		int(ProposalExpiryAfter.Hours()/24))
}
