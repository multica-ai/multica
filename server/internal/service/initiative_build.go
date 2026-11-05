package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// The SDLC workflow the build stage uses. The quickstart creates it from
// SDLC_AUTO_WORKFLOW in packages/core/squads/templates.ts, through the workflow
// API, with the template squad's agents as handlers. The server cannot call that
// client code, so the workflow and its instructions are repeated here. Keep the
// two in step: a change to one is a change to both.
const (
	sdlcWorkflowName        = "SDLC Auto"
	sdlcWorkflowDescription = "Build, independent review, then QA, all by agents. A rejected step goes back to the developer."
	sdlcInProgressStatusKey = "in_progress"
	sdlcQAStatusKey         = "qa"
	sdlcQAStatusName        = "QA"
)

// sdlcRoleAgents names the SDLC agents the workflow hands to, as the quickstart
// names them (SQUAD_ROLES in templates.ts).
var sdlcRoleAgents = []struct{ role, name string }{
	{"developer", "SDLC Developer"},
	{"reviewer", "SDLC Code Reviewer"},
	{"qa", "SDLC QA Tester"},
}

// ErrBuildNeedsSDLCSquad: the build stage's owner is not an SDLC squad with the
// developer, reviewer and QA agents the build workflow hands to.
var ErrBuildNeedsSDLCSquad = errors.New("the build stage needs an owning squad with the SDLC developer, reviewer and QA agents")

// ErrStageOwnerRequired: the stage a launch or a build starts has no owning
// squad, so there is nobody to run its work.
var ErrStageOwnerRequired = errors.New("this stage has no owning squad")

// startBuild creates the initiative's build project on the SDLC workflow and the
// kickoff issue the developer agent starts from, inside the stage transition's
// transaction. It returns the project id, stored as build_project_id.
//
// The workflow is the workspace's "SDLC Auto" when that one already hands to this
// squad's developer, which is what the quickstart creates; otherwise a workflow
// named after the squad is created, because the handlers are the squad's agents
// and a workflow is shared by the whole workspace.
func (s *InitiativeService) startBuild(ctx context.Context, q *db.Queries, ini db.Initiative, source db.Issue, owner pgtype.UUID, actor InitiativeActor) (pgtype.UUID, error) {
	agents, err := s.sdlcBuildSetup(ctx, q, ini.WorkspaceID, owner)
	if err != nil {
		return pgtype.UUID{}, err
	}
	workflow, err := s.sdlcWorkflowFor(ctx, q, ini.WorkspaceID, owner, agents)
	if err != nil {
		return pgtype.UUID{}, err
	}

	project, err := q.CreateProject(ctx, db.CreateProjectParams{
		WorkspaceID: ini.WorkspaceID,
		Title:       fmt.Sprintf("INI-%d %s", ini.Seq, ini.Title),
		Status:      "planned",
		Priority:    "none",
		WorkflowID:  workflow.ID,
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("create build project: %w", err)
	}
	policy := ResolveIssueCountPolicy(ctx, s.Issues.Entitlements, ini.WorkspaceID)
	number, err := AllocateIssueNumber(ctx, q, ini.WorkspaceID, policy)
	if err != nil {
		return pgtype.UUID{}, err
	}
	position, err := s.minTopPosition(ctx, q, ini.WorkspaceID, "todo")
	if err != nil {
		return pgtype.UUID{}, err
	}
	developer := agents["developer"]
	kickoff, err := q.CreateIssueWithOrigin(ctx, db.CreateIssueWithOriginParams{
		ID:           dbid.NewV7(),
		WorkspaceID:  ini.WorkspaceID,
		Title:        ini.Title,
		Description:  pgtype.Text{String: fmt.Sprintf("Build for initiative INI-%d. Source issue: %s.", ini.Seq, source.Title), Valid: true},
		Status:       "todo",
		Priority:     "none",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   developer,
		CreatorType:  actor.Type,
		CreatorID:    actor.ID,
		Position:     position,
		Number:       number,
		ProjectID:    project.ID,
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("create build kickoff issue: %w", err)
	}
	s.wakeAgent(ctx, q, kickoff, developer, actor,
		fmt.Sprintf("The initiative INI-%d has reached its build stage. Build from this issue's workflow, starting with the developer step.", ini.Seq))
	return project.ID, nil
}

// EnsureSDLCWorkflow returns the SDLC Auto workflow that hands to the squad's
// agents, creating it (and the workspace's QA status) when it is missing. It is
// the same setup a build stage runs, so a project assigned by hand gets the
// workflow an initiative's build would.
func (s *InitiativeService) EnsureSDLCWorkflow(ctx context.Context, q *db.Queries, ws, squad pgtype.UUID) (db.IssueWorkflow, error) {
	agents, err := s.sdlcBuildSetup(ctx, q, ws, squad)
	if err != nil {
		return db.IssueWorkflow{}, err
	}
	return s.sdlcWorkflowFor(ctx, q, ws, squad, agents)
}

// sdlcBuildSetup checks the squad can run an SDLC build, seeds the statuses the
// workflow uses, and returns the squad's SDLC agents by role.
func (s *InitiativeService) sdlcBuildSetup(ctx context.Context, q *db.Queries, ws, squad pgtype.UUID) (map[string]pgtype.UUID, error) {
	if !squad.Valid {
		return nil, ErrBuildNeedsSDLCSquad
	}
	agents, err := s.sdlcAgentsOf(ctx, q, ws, squad)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, r := range sdlcRoleAgents {
		if _, ok := agents[r.role]; !ok {
			missing = append(missing, r.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: missing %s", ErrBuildNeedsSDLCSquad, strings.Join(missing, ", "))
	}
	if err := issuestatus.Ensure(ctx, q, ws); err != nil {
		return nil, fmt.Errorf("seed issue statuses: %w", err)
	}
	if err := ensureQAStatus(ctx, q, ws); err != nil {
		return nil, err
	}
	return agents, nil
}

// sdlcAgentsOf maps the SDLC roles to the squad's agents with those names. An
// archived agent does not count.
func (s *InitiativeService) sdlcAgentsOf(ctx context.Context, q *db.Queries, ws, squad pgtype.UUID) (map[string]pgtype.UUID, error) {
	members, err := q.ListSquadMembers(ctx, squad)
	if err != nil {
		return nil, fmt.Errorf("list squad members: %w", err)
	}
	byName := map[string]pgtype.UUID{}
	for _, m := range members {
		if m.MemberType != "agent" {
			continue
		}
		agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: m.MemberID, WorkspaceID: ws})
		if err != nil || agent.ArchivedAt.Valid {
			continue
		}
		byName[agent.Name] = agent.ID
	}
	out := map[string]pgtype.UUID{}
	for _, r := range sdlcRoleAgents {
		if id, ok := byName[r.name]; ok {
			out[r.role] = id
		}
	}
	return out, nil
}

// ensureQAStatus makes sure the workspace has the QA status the workflow's last
// step sits on. A status named QA of any key is used; otherwise one is created.
func ensureQAStatus(ctx context.Context, q *db.Queries, ws pgtype.UUID) error {
	statuses, err := q.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{WorkspaceID: ws})
	if err != nil {
		return fmt.Errorf("list issue statuses: %w", err)
	}
	for _, st := range statuses {
		if strings.EqualFold(st.Name, sdlcQAStatusName) {
			return nil
		}
	}
	if _, err := q.GetIssueStatusEntryByKey(ctx, db.GetIssueStatusEntryByKeyParams{WorkspaceID: ws, Key: sdlcQAStatusKey}); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load QA status: %w", err)
	}
	if _, err := q.CreateIssueStatusEntry(ctx, db.CreateIssueStatusEntryParams{
		WorkspaceID: ws, Key: sdlcQAStatusKey, Name: sdlcQAStatusName,
		Description: "Verifies the change on the running system.",
		Category:    issuestatus.CategoryStarted, Color: "#8b5cf6", Icon: "circle",
	}); err != nil {
		return fmt.Errorf("create QA status: %w", err)
	}
	return nil
}

// sdlcWorkflowFor returns the SDLC workflow that hands to this squad's agents,
// reusing one the quickstart or an earlier build made, or creating it.
func (s *InitiativeService) sdlcWorkflowFor(ctx context.Context, q *db.Queries, ws, squad pgtype.UUID, agents map[string]pgtype.UUID) (db.IssueWorkflow, error) {
	rows, err := q.ListIssueWorkflows(ctx, ws)
	if err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("list workflows: %w", err)
	}
	taken := false
	for _, wf := range rows {
		isSDLC := wf.Name == sdlcWorkflowName || strings.HasPrefix(wf.Name, sdlcWorkflowName+" (")
		if !isSDLC {
			continue
		}
		taken = taken || wf.Name == sdlcWorkflowName
		def, err := issueworkflow.Decode(wf)
		if err != nil {
			return db.IssueWorkflow{}, err
		}
		if step, ok := def.Step("todo"); ok && step.Handler.Type == issueworkflow.HandlerAgent &&
			step.Handler.ID == util.UUIDToString(agents["developer"]) {
			return wf, nil
		}
	}

	name := sdlcWorkflowName
	if taken {
		squadRow, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: squad, WorkspaceID: ws})
		if err != nil {
			return db.IssueWorkflow{}, fmt.Errorf("load build squad: %w", err)
		}
		name = fmt.Sprintf("%s (%s)", sdlcWorkflowName, squadRow.Name)
		if runes := []rune(name); len(runes) > issueworkflow.MaxNameRunes {
			name = string(runes[:issueworkflow.MaxNameRunes])
		}
	}
	encoded, err := issueworkflow.EncodeSteps(issueworkflow.Arrange(sdlcSteps(agents)))
	if err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("encode SDLC workflow: %w", err)
	}
	wf, err := q.CreateIssueWorkflow(ctx, db.CreateIssueWorkflowParams{
		WorkspaceID:      ws,
		Name:             name,
		Description:      sdlcWorkflowDescription,
		InitialStatusKey: issuestatus.Todo,
		Steps:            encoded,
		OwnerID:          pgtype.UUID{},
	})
	if err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("create SDLC workflow: %w", err)
	}
	return wf, nil
}

// sdlcSteps is SDLC_AUTO_WORKFLOW, the same shape as the Delivery workflow: the
// developer takes the issue and builds it in progress, the reviewer checks, and
// QA verifies. Rejected work goes back to In Progress. QA's step is the last
// before done.
func sdlcSteps(agents map[string]pgtype.UUID) []issueworkflow.Step {
	agent := func(role string) issueworkflow.Handler {
		return issueworkflow.Handler{Type: issueworkflow.HandlerAgent, ID: util.UUIDToString(agents[role])}
	}
	none := issueworkflow.Handler{Type: issueworkflow.HandlerNone}
	return []issueworkflow.Step{
		{StatusKey: issuestatus.Backlog, Handler: none},
		{
			StatusKey: issuestatus.Todo, Handler: agent("developer"),
			Instructions: "Pick up this issue from To Do. Read its spec, stories, and acceptance criteria, then move it to In Progress to start the build.",
		},
		{
			StatusKey: sdlcInProgressStatusKey, Handler: agent("developer"),
			BackStatusKey: issuestatus.Todo,
			Instructions:  "Build everything this issue asks for, story by story, following the story flow in your instructions. Hand it to review only when every story is implemented and verified on the running app. Record what you verified in the spec.",
		},
		{
			StatusKey: "in_review", Handler: agent("reviewer"),
			BackStatusKey: sdlcInProgressStatusKey,
			Instructions:  "Review the whole change for this issue against its acceptance criteria. Send it back to In Progress with the blocking findings, or pass it on to QA with your verdict.",
		},
		{
			StatusKey: sdlcQAStatusKey, Handler: agent("qa"),
			BackStatusKey: sdlcInProgressStatusKey,
			Instructions:  "Verify the change on the running system against the acceptance criteria. NO GO sends it back to In Progress with the defects. GO or CONDITIONAL GO finishes the issue.",
		},
		{StatusKey: issuestatus.Blocked, Handler: none},
	}
}

// wakeAgent posts a note on the issue that mentions the agent, so the mention path
// wakes it, as wakeOwner does for a squad's leader.
func (s *InitiativeService) wakeAgent(ctx context.Context, q *db.Queries, issue db.Issue, agentID pgtype.UUID, actor InitiativeActor, text string) {
	content := text
	if agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: issue.WorkspaceID}); err == nil {
		content = fmt.Sprintf("[@%s](mention://agent/%s) %s", agent.Name, util.UUIDToString(agent.ID), text)
	}
	if _, err := q.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  actor.Type,
		AuthorID:    actor.ID,
		Content:     content,
		Type:        "comment",
	}); err != nil {
		// The stage has moved; the developer still finds the kickoff on the board.
		slog.Warn("initiative: could not post the kickoff note", "issue_id", util.UUIDToString(issue.ID), "error", err)
	}
}

// minTopPosition places a new issue at the top of its column, as the issue
// service does.
func (s *InitiativeService) minTopPosition(ctx context.Context, q *db.Queries, ws pgtype.UUID, status string) (float64, error) {
	min, err := q.MinIssuePositionInStatus(ctx, db.MinIssuePositionInStatusParams{WorkspaceID: ws, Status: status})
	if err != nil {
		return 0, fmt.Errorf("query min issue position: %w", err)
	}
	return min - 1, nil
}
