package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// BriefInquiryDailyLimit is how many questions the Secretary may put to squad
// leaders in one workspace per UTC day (spec 2.5). It is a constant, not a
// setting: the spec adds a setting only if real use shows the number needs
// tuning.
const BriefInquiryDailyLimit = 3

// BriefInquiryQuestionMax bounds a question, in characters. The table checks the
// same number.
const BriefInquiryQuestionMax = 1000

// BriefInquiryAnswerExcerptMax bounds the leader's reply kept on an answered
// inquiry, in characters.
const BriefInquiryAnswerExcerptMax = 280

var (
	// ErrBriefInquirySquadNotFound: the asked squad is not a live squad of the
	// workspace.
	ErrBriefInquirySquadNotFound = errors.New("squad not found in this workspace")
	// ErrBriefInquiryIssueNotFound: the issue the question is asked on is not in
	// the workspace.
	ErrBriefInquiryIssueNotFound = errors.New("issue not found in this workspace")
)

// BriefInquiryLimitError reports that the daily cap refused an ask. Message is
// written for the Secretary: it names the cap and what to do instead.
type BriefInquiryLimitError struct{ Message string }

func (e *BriefInquiryLimitError) Error() string { return e.Message }

// BriefInquiryAskParams are the already-authorized inputs to AskBriefInquiry.
// The caller has checked that the actor is the workspace's Secretary.
type BriefInquiryAskParams struct {
	WorkspaceID pgtype.UUID
	// ActorID is the Secretary agent, who writes the question and is recorded as
	// the asker.
	ActorID  pgtype.UUID
	SquadID  pgtype.UUID
	IssueID  pgtype.UUID
	Question string
	// Now decides the UTC day the ask counts against.
	Now time.Time
}

// BriefInquiryAskResult carries the new rows so the caller can publish events
// and wake the leader after commit.
type BriefInquiryAskResult struct {
	Inquiry db.BriefInquiry
	Issue   db.Issue
	Comment db.Comment
	// CommentIssueRevision is the issue revision the comment insert returned.
	CommentIssueRevision int64
}

// AskBriefInquiry posts the question on the issue as a comment mentioning the
// squad's leader, and writes the inquiry row, in one transaction. The count of
// the day's asks is taken under the workspace lock, so concurrent asks cannot
// pass the cap.
//
// The comment is what wakes the leader, through the ordinary mention path, once
// the caller has committed it and run the comment triggers.
func AskBriefInquiry(ctx context.Context, txs TxStarter, q *db.Queries, p BriefInquiryAskParams) (BriefInquiryAskResult, error) {
	tx, err := txs.Begin(ctx)
	if err != nil {
		return BriefInquiryAskResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := q.WithTx(tx)

	if err := qtx.LockBriefInquiryWorkspace(ctx, p.WorkspaceID); err != nil {
		return BriefInquiryAskResult{}, fmt.Errorf("lock brief inquiries: %w", err)
	}
	squad, err := liveSquad(ctx, qtx, p.WorkspaceID, p.SquadID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BriefInquiryAskResult{}, ErrBriefInquirySquadNotFound
		}
		return BriefInquiryAskResult{}, err
	}
	issue, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: p.IssueID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BriefInquiryAskResult{}, ErrBriefInquiryIssueNotFound
		}
		return BriefInquiryAskResult{}, fmt.Errorf("load inquiry issue: %w", err)
	}
	leader, err := qtx.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: squad.LeaderID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		return BriefInquiryAskResult{}, fmt.Errorf("load squad leader: %w", err)
	}

	day := briefInquiryDay(p.Now)
	asked, err := qtx.CountBriefInquiriesOnDay(ctx, db.CountBriefInquiriesOnDayParams{
		WorkspaceID: p.WorkspaceID, AskedOn: pgtype.Date{Time: day, Valid: true},
	})
	if err != nil {
		return BriefInquiryAskResult{}, fmt.Errorf("count brief inquiries: %w", err)
	}
	if asked >= BriefInquiryDailyLimit {
		return BriefInquiryAskResult{}, &BriefInquiryLimitError{Message: fmt.Sprintf(
			"the Secretary has already asked %d questions of squad leaders today (the limit, reset at midnight UTC); "+
				"put this one in the next brief's Blockers and risks instead of asking it now",
			asked)}
	}

	content := fmt.Sprintf("[@%s](mention://agent/%s) %s", leader.Name, util.UUIDToString(leader.ID), p.Question)
	commentRow, err := qtx.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: p.WorkspaceID,
		AuthorType:  "agent",
		AuthorID:    p.ActorID,
		Content:     content,
		Type:        "comment",
	})
	if err != nil {
		return BriefInquiryAskResult{}, fmt.Errorf("post inquiry comment: %w", err)
	}
	inquiry, err := qtx.CreateBriefInquiry(ctx, db.CreateBriefInquiryParams{
		ID:             dbid.NewV7(),
		WorkspaceID:    p.WorkspaceID,
		AskedByAgentID: p.ActorID,
		SquadID:        squad.ID,
		IssueID:        issue.ID,
		CommentID:      commentRow.ID,
		Question:       p.Question,
		AskedOn:        pgtype.Date{Time: day, Valid: true},
	})
	if err != nil {
		return BriefInquiryAskResult{}, fmt.Errorf("record brief inquiry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return BriefInquiryAskResult{}, fmt.Errorf("commit: %w", err)
	}
	return BriefInquiryAskResult{
		Inquiry:              inquiry,
		Issue:                issue,
		Comment:              commentRow.Comment(),
		CommentIssueRevision: commentRow.IssueRevision,
	}, nil
}

// RecordBriefInquiryAnswer marks the open inquiries on an issue as answered when
// a member agent of the asked squad comments on that issue. Both agent comment
// paths call it after their insert commits: the CLI-facing comment handler, and
// the task service's fallback comment. A failure is the caller's to log; the
// comment itself stands.
func RecordBriefInquiryAnswer(ctx context.Context, q *db.Queries, workspaceID, issueID, agentID pgtype.UUID, content string) error {
	_, err := q.AnswerOpenBriefInquiries(ctx, db.AnswerOpenBriefInquiriesParams{
		AnswerExcerpt: pgtype.Text{String: briefInquiryExcerpt(content), Valid: true},
		WorkspaceID:   workspaceID,
		IssueID:       issueID,
		AgentID:       agentID,
	})
	return err
}

// briefInquiryExcerpt returns the first characters of a reply, trimmed.
func briefInquiryExcerpt(content string) string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) > BriefInquiryAnswerExcerptMax {
		runes = runes[:BriefInquiryAnswerExcerptMax]
	}
	return string(runes)
}

// briefInquiryDay is the UTC day an ask counts against.
func briefInquiryDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
