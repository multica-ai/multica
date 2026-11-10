package projectaccess

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TaskQuerier is the subset of *db.Queries TaskProjects reads.
type TaskQuerier interface {
	GetIssueProjectID(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)
	GetAutopilotRun(ctx context.Context, id pgtype.UUID) (db.AutopilotRun, error)
	GetAutopilot(ctx context.Context, id pgtype.UUID) (db.Autopilot, error)
}

// quickCreateContext is the part of a quick-create task's context that names
// projects. It mirrors service.QuickCreateContext, which this package cannot
// import.
type quickCreateContext struct {
	Type          string `json:"type"`
	ProjectID     string `json:"project_id"`
	ParentIssueID string `json:"parent_issue_id"`
}

// TaskProjects returns the projects whose content a run is about: its issue's
// project; for an issue-less run, the project of the autopilot that started
// it, or the target and parent projects of a quick-create. A viewer may see a
// run, its transcript and its output only when it can see every one of them.
//
// Chat runs return none: a chat run's content belongs to the chat session's
// creator alone, and every route checks that first.
func TaskProjects(ctx context.Context, q TaskQuerier, task db.AgentTaskQueue) ([]pgtype.UUID, error) {
	if task.IssueID.Valid {
		projectID, err := q.GetIssueProjectID(ctx, task.IssueID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []pgtype.UUID{projectID}, nil
	}
	if task.ChatSessionID.Valid {
		return nil, nil
	}
	if task.AutopilotRunID.Valid {
		run, err := q.GetAutopilotRun(ctx, task.AutopilotRunID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		ap, err := q.GetAutopilot(ctx, run.AutopilotID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []pgtype.UUID{ap.ProjectID}, nil
	}
	var qc quickCreateContext
	if len(task.Context) == 0 || json.Unmarshal(task.Context, &qc) != nil || qc.Type != "quick_create" {
		return nil, nil
	}
	var projects []pgtype.UUID
	if id, err := util.ParseUUID(qc.ProjectID); err == nil {
		projects = append(projects, id)
	}
	if parent, err := util.ParseUUID(qc.ParentIssueID); err == nil {
		projectID, err := q.GetIssueProjectID(ctx, parent)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		projects = append(projects, projectID)
	}
	return projects, nil
}

// CanSeeProjects reports whether every listed project is visible.
func (h Hidden) CanSeeProjects(projectIDs []pgtype.UUID) bool {
	for _, id := range projectIDs {
		if !h.CanSeeProject(id) {
			return false
		}
	}
	return true
}
