package projectplan

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	CoverageNoTasksYet           = "no_tasks_yet"
	CoverageCoveredNoActiveTasks = "covered_no_active_tasks"
	CoverageNotStarted           = "not_started"
	CoverageInProgress           = "in_progress"
	CoverageComplete             = "complete"
)

// Reader builds the read model shared by all three Plan views. Its query count
// is fixed with respect to the number of phases and parts.
type Reader struct {
	queries *db.Queries
}

func NewReader(queries *db.Queries) *Reader {
	return &Reader{queries: queries}
}

type Overview struct {
	Plan           Plan            `json:"plan"`
	Rollup         Rollup          `json:"rollup"`
	Phases         []Phase         `json:"phases"`
	Dependencies   []Dependency    `json:"dependencies"`
	UncoveredParts []UncoveredPart `json:"uncovered_parts"`
}

type Plan struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	ProjectID     string          `json:"project_id"`
	Version       int32           `json:"version"`
	Kind          string          `json:"kind"`
	Origin        string          `json:"origin"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	Attributes    json.RawMessage `json:"attributes"`
	SourceIssueID *string         `json:"source_issue_id"`
	Superseded    bool            `json:"superseded"`
	SupersededAt  *string         `json:"superseded_at"`
	CreatedByType string          `json:"created_by_type"`
	CreatedByID   string          `json:"created_by_id"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

type Rollup struct {
	TasksDone         int64 `json:"tasks_done"`
	TasksTotal        int64 `json:"tasks_total"`
	Percent           int32 `json:"percent"`
	PartsCovered      int64 `json:"parts_covered"`
	PartsTotal        int64 `json:"parts_total"`
	PartsWithoutTasks int64 `json:"parts_without_tasks"`
}

type Phase struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Attributes  json.RawMessage `json:"attributes"`
	Position    int32           `json:"position"`
	Rollup      TaskRollup      `json:"rollup"`
	Parts       []Part          `json:"parts"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

type Part struct {
	ID                 string          `json:"id"`
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	AcceptanceCriteria string          `json:"acceptance_criteria"`
	Attributes         json.RawMessage `json:"attributes"`
	Position           int32           `json:"position"`
	CoverageState      string          `json:"coverage_state"`
	Rollup             TaskRollup      `json:"rollup"`
	Issues             []IssueDetail   `json:"issues"`
	CreatedAt          string          `json:"created_at"`
	UpdatedAt          string          `json:"updated_at"`
}

type TaskRollup struct {
	TasksDone  int64 `json:"tasks_done"`
	TasksTotal int64 `json:"tasks_total"`
	Percent    int32 `json:"percent"`
}

type IssueDetail struct {
	ID             *string `json:"id"`
	Number         int32   `json:"number"`
	Identifier     string  `json:"identifier"`
	Title          string  `json:"title"`
	Status         string  `json:"status"`
	StatusCategory string  `json:"status_category"`
	AssigneeType   *string `json:"assignee_type"`
	AssigneeID     *string `json:"assignee_id"`
	Deleted        bool    `json:"deleted"`
}

type Dependency struct {
	ID       string         `json:"id"`
	Blocked  DependencyNode `json:"blocked"`
	Blocking DependencyNode `json:"blocking"`
}

type DependencyNode struct {
	Type       string  `json:"type"`
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	PhaseID    *string `json:"phase_id,omitempty"`
	PhaseTitle *string `json:"phase_title,omitempty"`
	Missing    bool    `json:"missing"`
}

type UncoveredPart struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Position   int32  `json:"position"`
	PhaseID    string `json:"phase_id"`
	PhaseTitle string `json:"phase_title"`
}

func (r *Reader) ReadActive(
	ctx context.Context,
	workspaceID pgtype.UUID,
	projectID pgtype.UUID,
) (Overview, error) {
	plan, err := r.queries.GetActiveProjectPlanForRead(ctx, db.GetActiveProjectPlanForReadParams{
		ProjectID: projectID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return Overview{}, fmt.Errorf("get active project plan: %w", err)
	}
	return r.readOverview(ctx, plan)
}

func (r *Reader) Read(
	ctx context.Context,
	workspaceID pgtype.UUID,
	projectID pgtype.UUID,
	planID pgtype.UUID,
) (Overview, error) {
	plan, err := r.queries.GetProjectPlanForRead(ctx, db.GetProjectPlanForReadParams{
		ID: planID, ProjectID: projectID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return Overview{}, fmt.Errorf("get project plan: %w", err)
	}
	return r.readOverview(ctx, plan)
}

func (r *Reader) readOverview(ctx context.Context, plan db.ProjectPlan) (Overview, error) {
	rollupRows, err := r.queries.ListProjectPlanRollups(ctx, db.ListProjectPlanRollupsParams{
		ProjectPlanID: plan.ID, WorkspaceID: plan.WorkspaceID, ProjectID: plan.ProjectID,
	})
	if err != nil {
		return Overview{}, fmt.Errorf("list project plan rollups: %w", err)
	}
	issueRows, err := r.queries.ListProjectPlanIssueDetails(ctx, db.ListProjectPlanIssueDetailsParams{
		WorkspaceID: plan.WorkspaceID, ProjectID: plan.ProjectID, ProjectPlanID: plan.ID,
	})
	if err != nil {
		return Overview{}, fmt.Errorf("list project plan issue details: %w", err)
	}
	dependencyRows, err := r.queries.ListProjectPlanDependenciesForRead(ctx, plan.ID)
	if err != nil {
		return Overview{}, fmt.Errorf("list project plan dependencies: %w", err)
	}
	uncoveredRows, err := r.queries.ListProjectPlanUncoveredParts(ctx, plan.ID)
	if err != nil {
		return Overview{}, fmt.Errorf("list uncovered project plan parts: %w", err)
	}

	overview := Overview{
		Plan:           planResponse(plan),
		Phases:         make([]Phase, 0),
		Dependencies:   make([]Dependency, 0, len(dependencyRows)),
		UncoveredParts: make([]UncoveredPart, 0, len(uncoveredRows)),
	}
	phaseIndexes := make(map[string]int, len(rollupRows))
	partLocations := make(map[string][2]int, len(rollupRows))

	for _, row := range rollupRows {
		phaseID := uuidString(row.PhaseID)
		phaseIndex, exists := phaseIndexes[phaseID]
		if !exists {
			phaseIndex = len(overview.Phases)
			phaseIndexes[phaseID] = phaseIndex
			overview.Phases = append(overview.Phases, Phase{
				ID: phaseID, Title: row.PhaseTitle, Description: row.PhaseDescription,
				Attributes: rawJSON(row.PhaseAttributes), Position: row.PhasePosition,
				Rollup: TaskRollup{
					TasksDone: row.PhaseTasksDone, TasksTotal: row.PhaseTasksTotal,
					Percent: percent(row.PhaseTasksDone, row.PhaseTasksTotal),
				},
				Parts: make([]Part, 0), CreatedAt: timestamp(row.PhaseCreatedAt),
				UpdatedAt: timestamp(row.PhaseUpdatedAt),
			})
		}
		if !row.PartID.Valid {
			continue
		}
		partID := uuidString(row.PartID)
		partIndex := len(overview.Phases[phaseIndex].Parts)
		overview.Phases[phaseIndex].Parts = append(overview.Phases[phaseIndex].Parts, Part{
			ID: partID, Title: row.PartTitle.String, Description: row.PartDescription.String,
			AcceptanceCriteria: row.PartAcceptanceCriteria.String,
			Attributes:         rawJSON(row.PartAttributes), Position: row.PartPosition.Int32,
			CoverageState: coverageState(
				row.PartMembershipRows, row.PartTasksTotal, row.PartTasksDone, row.PartTasksStarted,
			),
			Rollup: TaskRollup{
				TasksDone: row.PartTasksDone, TasksTotal: row.PartTasksTotal,
				Percent: percent(row.PartTasksDone, row.PartTasksTotal),
			},
			Issues: make([]IssueDetail, 0), CreatedAt: timestamp(row.PartCreatedAt),
			UpdatedAt: timestamp(row.PartUpdatedAt),
		})
		partLocations[partID] = [2]int{phaseIndex, partIndex}
	}

	if len(rollupRows) > 0 {
		row := rollupRows[0]
		overview.Rollup = Rollup{
			TasksDone: row.PlanTasksDone, TasksTotal: row.PlanTasksTotal,
			Percent:      percent(row.PlanTasksDone, row.PlanTasksTotal),
			PartsCovered: row.PlanPartsCovered, PartsTotal: row.PlanPartsTotal,
			PartsWithoutTasks: row.PlanPartsWithoutTasks,
		}
	}

	for _, row := range issueRows {
		location, exists := partLocations[uuidString(row.ProjectPlanPartID)]
		if !exists {
			continue
		}
		deleted := row.IssueDeleted
		status := row.IssueStatus.String
		if deleted {
			status = "deleted"
		}
		detail := IssueDetail{
			ID: uuidPtr(row.IssueID), Number: row.IssueNumber,
			Identifier: issueIdentifier(row.IssuePrefix, row.IssueNumber),
			Title:      row.IssueTitle, Status: status,
			StatusCategory: row.IssueStatusCategory,
			AssigneeType:   textPtr(row.AssigneeType), AssigneeID: uuidPtr(row.AssigneeID),
			Deleted: deleted,
		}
		phaseIndex, partIndex := location[0], location[1]
		overview.Phases[phaseIndex].Parts[partIndex].Issues = append(
			overview.Phases[phaseIndex].Parts[partIndex].Issues,
			detail,
		)
	}

	phaseNodes := make(map[string]DependencyNode, len(overview.Phases))
	partNodes := make(map[string]DependencyNode, len(partLocations))
	for _, phase := range overview.Phases {
		phaseNodes[phase.ID] = DependencyNode{Type: "phase", ID: phase.ID, Title: phase.Title}
		for _, part := range phase.Parts {
			phaseID, phaseTitle := phase.ID, phase.Title
			partNodes[part.ID] = DependencyNode{
				Type: "part", ID: part.ID, Title: part.Title,
				PhaseID: &phaseID, PhaseTitle: &phaseTitle,
			}
		}
	}
	for _, row := range dependencyRows {
		overview.Dependencies = append(overview.Dependencies, Dependency{
			ID: uuidString(row.ID),
			Blocked: dependencyNode(
				row.BlockedPhaseID, row.BlockedPartID, phaseNodes, partNodes,
			),
			Blocking: dependencyNode(
				row.BlockingPhaseID, row.BlockingPartID, phaseNodes, partNodes,
			),
		})
	}
	for _, row := range uncoveredRows {
		overview.UncoveredParts = append(overview.UncoveredParts, UncoveredPart{
			ID: uuidString(row.PartID), Title: row.PartTitle, Position: row.PartPosition,
			PhaseID: uuidString(row.PhaseID), PhaseTitle: row.PhaseTitle,
		})
	}
	return overview, nil
}

func planResponse(plan db.ProjectPlan) Plan {
	return Plan{
		ID: uuidString(plan.ID), WorkspaceID: uuidString(plan.WorkspaceID),
		ProjectID: uuidString(plan.ProjectID), Version: plan.Version, Kind: plan.Kind,
		Origin: plan.Origin, Title: plan.Title, Description: plan.Description,
		Attributes: rawJSON(plan.Attributes), SourceIssueID: uuidPtr(plan.SourceIssueID),
		Superseded: plan.SupersededAt.Valid, SupersededAt: timestampPtr(plan.SupersededAt),
		CreatedByType: plan.CreatedByType, CreatedByID: uuidString(plan.CreatedByID),
		CreatedAt: timestamp(plan.CreatedAt), UpdatedAt: timestamp(plan.UpdatedAt),
	}
}

func coverageState(memberships, total, done, started int64) string {
	switch {
	case memberships == 0:
		return CoverageNoTasksYet
	case total == 0:
		return CoverageCoveredNoActiveTasks
	case done == total:
		return CoverageComplete
	case started > 0:
		return CoverageInProgress
	default:
		return CoverageNotStarted
	}
}

func dependencyNode(
	phaseID pgtype.UUID,
	partID pgtype.UUID,
	phases map[string]DependencyNode,
	parts map[string]DependencyNode,
) DependencyNode {
	if phaseID.Valid {
		id := uuidString(phaseID)
		if node, ok := phases[id]; ok {
			return node
		}
		return DependencyNode{Type: "phase", ID: id, Missing: true}
	}
	id := uuidString(partID)
	if node, ok := parts[id]; ok {
		return node
	}
	return DependencyNode{Type: "part", ID: id, Missing: true}
}

func percent(done, total int64) int32 {
	if total == 0 {
		return 0
	}
	return int32(math.Round(float64(done) * 100 / float64(total)))
}

func issueIdentifier(prefix string, number int32) string {
	if prefix == "" {
		return "#" + strconv.Itoa(int(number))
	}
	return prefix + "-" + strconv.Itoa(int(number))
}

func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func uuidString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return uuid.UUID(value.Bytes).String()
}

func uuidPtr(value pgtype.UUID) *string {
	if !value.Valid {
		return nil
	}
	result := uuidString(value)
	return &result
}

func textPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func timestamp(value pgtype.Timestamptz) string {
	if !value.Valid {
		return ""
	}
	return value.Time.UTC().Format(time.RFC3339Nano)
}

func timestampPtr(value pgtype.Timestamptz) *string {
	if !value.Valid {
		return nil
	}
	result := timestamp(value)
	return &result
}
