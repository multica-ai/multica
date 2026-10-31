package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// ErrStageDispatchInvalid maps to 400 in the handler.
var ErrStageDispatchInvalid = errors.New("stage dispatch: invalid request")

// StageDispatchService assigns a parent issue's stages to a squad on the
// server (#7345): the unassigned sub-issues of the first open stage go to the
// squad once every earlier stage is closed, and sub-issues someone assigned
// ahead of time leave backlog. Each write goes through Start, which is the
// update path a person's edit takes, so it starts the assignee's run, appears
// on the timeline and reaches notifications and workflow handoffs the same
// way. A stage that closed with cancelled work halts dispatch: the next stage
// would build on work that was never done, so the parent's assignee is woken
// to decide instead.
type StageDispatchService struct {
	Issues *IssueService
	Flags  *featureflag.Service
	Start  StageStarter
}

// StageStarter opens one sub-issue of the open stage on the revision it was
// read at: an unassigned one goes to the dispatch squad, one in backlog moves
// to todo, and one already in todo gets its held run. It reports whether a run
// started. A sub-issue that changed
// meanwhile is left alone and reports false without an error.
type StageStarter func(ctx context.Context, issue db.Issue, cfg db.IssueStageDispatch) (started bool, err error)

// Enabled reports whether stage dispatch runs at all (stage_dispatch_v1).
func (s *StageDispatchService) Enabled(ctx context.Context) bool {
	return featureflags.StageDispatchV1Enabled(ctx, s.Flags)
}

// Set turns stage dispatch on for the parent (a squad of its workspace,
// answering to the member by) or off (an invalid squad id), and dispatches
// right away when turned on.
func (s *StageDispatchService) Set(ctx context.Context, parent db.Issue, squadID, by pgtype.UUID) (*db.IssueStageDispatch, error) {
	q := s.Issues.Queries
	if !squadID.Valid {
		return nil, q.DeleteIssueStageDispatch(ctx, db.DeleteIssueStageDispatchParams{IssueID: parent.ID, WorkspaceID: parent.WorkspaceID})
	}
	squad, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: squadID, WorkspaceID: parent.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && squad.ArchivedAt.Valid) {
		return nil, errors.Join(ErrStageDispatchInvalid, errors.New("squad not found in this workspace"))
	}
	if err != nil {
		return nil, err
	}
	row, err := q.UpsertIssueStageDispatch(ctx, db.UpsertIssueStageDispatchParams{
		IssueID: parent.ID, WorkspaceID: parent.WorkspaceID, SquadID: squadID, DispatchBy: by,
	})
	if err != nil {
		return nil, err
	}
	if _, err = s.Dispatch(ctx, parent.ID); err != nil {
		slog.Warn("stage dispatch: first pass failed; the sweep retries", "parent_id", util.UUIDToString(parent.ID), "error", err)
	}
	return &row, nil
}

// stagedChild is a sub-issue as dispatch reads it.
type stagedChild struct {
	issue     db.Issue
	closed    bool
	cancelled bool
}

// Dispatch opens the open stage of one parent and returns how many runs it
// started. In that stage an unassigned sub-issue goes to the squad, and a
// sub-issue assigned ahead of time starts now: it leaves backlog, or in todo
// gets the run held back until its stage opened. One that is under way or has
// had a run is left alone. A parent that is gone or closed drops its row.
func (s *StageDispatchService) Dispatch(ctx context.Context, parentID pgtype.UUID) (int, error) {
	if !s.Enabled(ctx) {
		return 0, nil
	}
	if s.Start == nil {
		return 0, errors.New("stage dispatch: no starter configured")
	}
	q := s.Issues.Queries
	parent, err := q.GetIssue(ctx, parentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, s.drop(ctx, parentID, pgtype.UUID{})
	}
	if err != nil {
		return 0, err
	}
	cfg, err := q.GetIssueStageDispatch(ctx, db.GetIssueStageDispatchParams{IssueID: parentID, WorkspaceID: parent.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if category, cerr := issuestatus.CategoryWithError(ctx, q, parent.WorkspaceID, parent.Status); cerr != nil {
		return 0, cerr
	} else if category == "done" || category == "closed" {
		return 0, s.drop(ctx, parentID, parent.WorkspaceID)
	}
	children, err := s.loadStagedChildren(ctx, parent)
	if err != nil {
		return 0, err
	}
	open := openStage(children)
	if open == nil {
		return 0, nil
	}
	squadUsable, err := s.squadUsable(ctx, cfg)
	if err != nil {
		return 0, err
	}
	started := 0
	for _, ch := range children {
		if !ch.issue.Stage.Valid || ch.issue.Stage.Int32 != *open || ch.closed || ch.issue.TriageState.Valid {
			continue
		}
		due, err := s.startDue(ctx, ch.issue, squadUsable)
		if err != nil {
			return started, err
		}
		if !due {
			continue
		}
		ran, err := s.Start(ctx, ch.issue, cfg)
		if err != nil {
			return started, err
		}
		if ran {
			started++
		}
	}
	return started, nil
}

// startDue reports whether a sub-issue of the open stage still has to be
// started: an unassigned one goes to the squad, and one assigned ahead of time
// leaves backlog or, sitting in todo, gets the run that was held back until
// its stage opened. Work that has had a run, or is under way, is left alone.
func (s *StageDispatchService) startDue(ctx context.Context, issue db.Issue, squadUsable bool) (bool, error) {
	q := s.Issues.Queries
	if !issue.AssigneeID.Valid {
		return squadUsable, nil
	}
	switch issuestatus.Effective(ctx, q, issue.WorkspaceID, issue.Status) {
	case "backlog":
		return true, nil
	case "todo":
		if issue.AssigneeType.String != "agent" && issue.AssigneeType.String != "squad" {
			return false, nil
		}
		ran, err := q.HasTaskForIssue(ctx, issue.ID)
		return !ran, err
	}
	return false, nil
}

// Held reports whether the issue is a sub-issue of a later stage than the
// open one of a parent the server dispatches. Assigning it or moving it to
// todo then starts no run: the stage opening does. With no open stage (the
// last one closed with cancelled work, say) dispatch starts nothing more, so
// a person's assignment runs. A stage dispatch that cannot be read holds,
// since the next pass starts the run anyway.
func (s *StageDispatchService) Held(ctx context.Context, issue db.Issue) bool {
	if !issue.ParentIssueID.Valid || !issue.Stage.Valid || !s.Enabled(ctx) {
		return false
	}
	q := s.Issues.Queries
	_, err := q.GetIssueStageDispatch(ctx, db.GetIssueStageDispatchParams{IssueID: issue.ParentIssueID, WorkspaceID: issue.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		slog.Warn("stage dispatch: reading the dispatch of a parent failed; the run waits for its stage",
			"issue_id", util.UUIDToString(issue.ID), "error", err)
		return true
	}
	children, err := s.loadStagedChildren(ctx, db.Issue{ID: issue.ParentIssueID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		slog.Warn("stage dispatch: reading the stages of a parent failed; the run waits for its stage",
			"issue_id", util.UUIDToString(issue.ID), "error", err)
		return true
	}
	open := openStage(children)
	return open != nil && issue.Stage.Int32 > *open
}

// stageHeld is Held for the issue write paths, which have no dispatch service
// of their own.
func (s *IssueService) stageHeld(ctx context.Context, issue db.Issue) bool {
	if s.TaskService == nil {
		return false
	}
	return (&StageDispatchService{Issues: s, Flags: s.TaskService.FeatureFlags}).Held(ctx, issue)
}

// squadUsable reports whether the dispatch squad can still take work: the
// squad may have been archived after dispatch was turned on.
func (s *StageDispatchService) squadUsable(ctx context.Context, cfg db.IssueStageDispatch) (bool, error) {
	squad, err := s.Issues.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: cfg.SquadID, WorkspaceID: cfg.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if squad.ArchivedAt.Valid {
		slog.Warn("stage dispatch: squad is archived; unassigned sub-issues wait",
			"parent_id", util.UUIDToString(cfg.IssueID), "squad_id", util.UUIDToString(cfg.SquadID))
		return false, nil
	}
	return true, nil
}

// drop removes the row of a parent that is closed or gone. The workspace is
// unknown for a parent that no longer exists, so its row is found by issue id.
func (s *StageDispatchService) drop(ctx context.Context, parentID, workspaceID pgtype.UUID) error {
	q := s.Issues.Queries
	if !workspaceID.Valid {
		return q.DeleteStageDispatchOfMissingIssue(ctx, parentID)
	}
	return q.DeleteIssueStageDispatch(ctx, db.DeleteIssueStageDispatchParams{IssueID: parentID, WorkspaceID: workspaceID})
}

func (s *StageDispatchService) loadStagedChildren(ctx context.Context, parent db.Issue) ([]stagedChild, error) {
	q := s.Issues.Queries
	rows, err := q.ListChildIssues(ctx, parent.ID)
	if err != nil {
		return nil, err
	}
	children := make([]stagedChild, 0, len(rows))
	for _, issue := range rows {
		if issue.WorkspaceID != parent.WorkspaceID {
			continue
		}
		category, err := issuestatus.CategoryWithError(ctx, q, issue.WorkspaceID, issue.Status)
		if err != nil {
			return nil, err
		}
		children = append(children, stagedChild{issue: issue, closed: category == "done" || category == "closed", cancelled: category == "closed"})
	}
	return children, nil
}

// openStage is the first stage with work left. Only stages that have
// sub-issues count. A stage that closed with a cancelled sub-issue halts
// dispatch: nothing past it opens until that sub-issue is reopened, deleted
// or moved out of the stage.
func openStage(children []stagedChild) *int32 {
	var stages []int32
	for _, ch := range children {
		if ch.issue.Stage.Valid && !slices.Contains(stages, ch.issue.Stage.Int32) {
			stages = append(stages, ch.issue.Stage.Int32)
		}
	}
	slices.Sort(stages)
	for _, stage := range stages {
		open, cancelled := false, false
		for _, ch := range children {
			if !ch.issue.Stage.Valid || ch.issue.Stage.Int32 != stage {
				continue
			}
			open = open || !ch.closed
			cancelled = cancelled || ch.cancelled
		}
		if open {
			return &stage
		}
		if cancelled {
			return nil
		}
	}
	return nil
}

// stageCancelled reports whether the stages the child_done reading counts as
// closed (stage:N and every one before it) hold a cancelled sub-issue. Stage
// dispatch halts there, so that wake still reaches the parent's assignee.
func stageCancelled(observed map[string]any) bool {
	closed, _ := observed["stage"].(int32)
	stages, _ := observed["stages"].([]stageCount)
	for _, sc := range stages {
		if sc.Stage <= closed && sc.Cancelled > 0 {
			return true
		}
	}
	return false
}

// sweepPageSize bounds one page of parents read by the sweep.
const sweepPageSize = 500

// Sweep dispatches every parent with stage dispatch on, page by page so no
// parent waits behind a full page of others. It catches what the pass right
// after a sub-issue change missed: a failed pass, and changes that record no
// sub-issue event.
func (s *StageDispatchService) Sweep(ctx context.Context) error {
	if !s.Enabled(ctx) {
		return nil
	}
	var errs []error
	after := pgtype.UUID{Valid: true}
	for {
		ids, err := s.Issues.Queries.ListStageDispatchParents(ctx, db.ListStageDispatchParentsParams{After: after, PageSize: sweepPageSize})
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		for _, id := range ids {
			if _, err := s.Dispatch(ctx, id); err != nil {
				errs = append(errs, err)
			}
		}
		if len(ids) < sweepPageSize {
			return errors.Join(errs...)
		}
		after = ids[len(ids)-1]
	}
}
