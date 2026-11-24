package projectaccess

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TaskQuerier is the subset of *db.Queries TaskProjects reads.
type TaskQuerier interface {
	ListIssueProjectIDs(ctx context.Context, ids []pgtype.UUID) ([]db.ListIssueProjectIDsRow, error)
	ListAutopilotRunProjectIDs(ctx context.Context, ids []pgtype.UUID) ([]db.ListAutopilotRunProjectIDsRow, error)
}

// quickCreateContext mirrors the project references in service.QuickCreateContext.
type quickCreateContext struct {
	Type          string `json:"type"`
	ProjectID     string `json:"project_id"`
	ParentIssueID string `json:"parent_issue_id"`
}

// TaskProjects returns the projects whose content a run reads. A viewer must
// see every project to read or stop the run. Chat routes check the creator.
func TaskProjects(ctx context.Context, q TaskQuerier, task db.AgentTaskQueue) ([]pgtype.UUID, error) {
	projects, err := TasksProjects(ctx, q, []db.AgentTaskQueue{task})
	if err != nil {
		return nil, err
	}
	return projects[0], nil
}

// TasksProjects resolves a page of runs in at most two queries, including
// quick-create parents and run-only autopilots. Results follow the input order.
func TasksProjects(ctx context.Context, q TaskQuerier, tasks []db.AgentTaskQueue) ([][]pgtype.UUID, error) {
	projects := make([][]pgtype.UUID, len(tasks))
	issues := make([]pgtype.UUID, len(tasks))
	runs := make([]pgtype.UUID, len(tasks))
	issueSet := map[pgtype.UUID]struct{}{}
	runSet := map[pgtype.UUID]struct{}{}
	for i, task := range tasks {
		switch {
		case task.IssueID.Valid:
			issues[i] = task.IssueID
		case task.ChatSessionID.Valid:
			continue
		case task.AutopilotRunID.Valid:
			runs[i] = task.AutopilotRunID
			runSet[task.AutopilotRunID] = struct{}{}
		default:
			var qc quickCreateContext
			if len(task.Context) == 0 || json.Unmarshal(task.Context, &qc) != nil || qc.Type != "quick_create" {
				continue
			}
			if id, err := util.ParseUUID(qc.ProjectID); err == nil {
				projects[i] = append(projects[i], id)
			}
			if id, err := util.ParseUUID(qc.ParentIssueID); err == nil {
				issues[i] = id
			}
		}
		if issues[i].Valid {
			issueSet[issues[i]] = struct{}{}
		}
	}
	issueProjects := map[pgtype.UUID]pgtype.UUID{}
	if len(issueSet) > 0 {
		ids := make([]pgtype.UUID, 0, len(issueSet))
		for id := range issueSet {
			ids = append(ids, id)
		}
		rows, err := q.ListIssueProjectIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			issueProjects[row.ID] = row.ProjectID
		}
	}
	runProjects := map[pgtype.UUID]pgtype.UUID{}
	if len(runSet) > 0 {
		ids := make([]pgtype.UUID, 0, len(runSet))
		for id := range runSet {
			ids = append(ids, id)
		}
		rows, err := q.ListAutopilotRunProjectIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			runProjects[row.ID] = row.ProjectID
		}
	}
	for i := range tasks {
		if projectID := issueProjects[issues[i]]; projectID.Valid {
			projects[i] = append(projects[i], projectID)
		}
		if projectID := runProjects[runs[i]]; projectID.Valid {
			projects[i] = append(projects[i], projectID)
		}
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
