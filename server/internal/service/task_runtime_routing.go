package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RuntimeRouting is server-owned execution evidence, never request context.
// A root captures the workspace routes once; descendants copy the same bytes.
type RuntimeRouting struct {
	Version         int                     `json:"version"`
	ExecutionUserID string                  `json:"execution_user_id"`
	Routes          map[string]RuntimeRoute `json:"routes"`
}

type RuntimeRoute struct {
	RuntimeID      string `json:"runtime_id"`
	RuntimeOwnerID string `json:"runtime_owner_id"`
	Provider       string `json:"provider"`
	Source         string `json:"source"`
	// Nil identifies snapshots created before model freezing was introduced.
	Model *string `json:"model,omitempty"`
}

var ErrTaskRuntimeUnavailable = errors.New("selected task runtime is unavailable or not authorized")
var ErrTaskRuntimeOffline = fmt.Errorf("%w: selected personal runtime is offline", ErrTaskRuntimeUnavailable)

// ParseRuntimeRouting distinguishes old tasks from invalid modern evidence.
// Individual routes are checked when selected so an unrelated deleted machine
// cannot prevent the rest of the workspace from starting tasks.
func ParseRuntimeRouting(raw []byte) (*RuntimeRouting, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var routing RuntimeRouting
	if err := json.Unmarshal(raw, &routing); err != nil {
		return nil, fmt.Errorf("invalid runtime routing: %w", err)
	}
	if routing.Version != 1 || routing.Routes == nil {
		return nil, fmt.Errorf("invalid runtime routing version or routes")
	}
	if id, err := util.ParseUUID(routing.ExecutionUserID); err != nil || !id.Valid {
		return nil, fmt.Errorf("invalid runtime execution user")
	}
	return &routing, nil
}

func (r *RuntimeRouting) Route(agentID string) (RuntimeRoute, error) {
	if r == nil {
		return RuntimeRoute{}, fmt.Errorf("%w: missing execution snapshot", ErrTaskRuntimeUnavailable)
	}
	route, ok := r.Routes[agentID]
	if !ok {
		return route, fmt.Errorf("%w: agent was not present when this execution started", ErrTaskRuntimeUnavailable)
	}
	for _, value := range []string{route.RuntimeID, route.RuntimeOwnerID} {
		if id, err := util.ParseUUID(value); err != nil || !id.Valid {
			return route, fmt.Errorf("%w: saved runtime no longer exists", ErrTaskRuntimeUnavailable)
		}
	}
	if route.Provider == "" || (route.Source != "personal" && route.Source != "default") {
		return route, fmt.Errorf("%w: invalid saved runtime route", ErrTaskRuntimeUnavailable)
	}
	return route, nil
}

type taskRuntimeSelection struct {
	RuntimeID       pgtype.UUID
	RuntimeOwnerID  pgtype.UUID
	Routing         []byte
	ExecutionUserID pgtype.UUID
	Source          string
}

// resolveTaskRuntime uses an explicit human for new roots. A parent is trusted
// only after its agent/workspace is checked; no client-supplied routing is read.
func (s *TaskService) resolveTaskRuntime(ctx context.Context, q *db.Queries, agent db.Agent, executionUserID, parentTaskID pgtype.UUID) (taskRuntimeSelection, error) {
	if agent.Kind == "system" {
		return taskRuntimeSelection{RuntimeID: agent.RuntimeID, ExecutionUserID: executionUserID}, nil
	}
	var raw []byte
	if parentTaskID.Valid {
		parent, err := q.GetAgentTask(ctx, parentTaskID)
		if err != nil {
			return taskRuntimeSelection{}, fmt.Errorf("load runtime routing parent: %w", err)
		}
		parentAgent, err := q.GetAgent(ctx, parent.AgentID)
		if err != nil || parentAgent.WorkspaceID != agent.WorkspaceID {
			return taskRuntimeSelection{}, fmt.Errorf("%w: foreign runtime routing parent", ErrTaskRuntimeUnavailable)
		}
		if len(parent.RuntimeRouting) > 0 && string(parent.RuntimeRouting) != "null" {
			raw = parent.RuntimeRouting
		} else {
			// Pre-feature chains retain the old default binding and cannot start
			// reading a user's new preferences halfway through the execution.
			return taskRuntimeSelection{RuntimeID: agent.RuntimeID, ExecutionUserID: executionUserID}, nil
		}
	} else {
		if !executionUserID.Valid {
			// Unattributed legacy/channel work may use its existing default, but
			// an audit fallback must never authorize a personal-machine route.
			return taskRuntimeSelection{RuntimeID: agent.RuntimeID}, nil
		}
		var err error
		raw, err = s.captureRuntimeRouting(ctx, q, agent.WorkspaceID, executionUserID)
		if err != nil {
			return taskRuntimeSelection{}, err
		}
	}
	routing, err := ParseRuntimeRouting(raw)
	if err != nil {
		return taskRuntimeSelection{}, fmt.Errorf("%w: %v", ErrTaskRuntimeUnavailable, err)
	}
	route, err := routing.Route(util.UUIDToString(agent.ID))
	if err != nil {
		return taskRuntimeSelection{}, err
	}
	runtimeID, err := util.ParseUUID(route.RuntimeID)
	if err != nil {
		return taskRuntimeSelection{}, err
	}
	allowed, err := q.IsTaskRuntimeAllowed(ctx, db.IsTaskRuntimeAllowedParams{AgentID: agent.ID, RuntimeID: runtimeID, RuntimeRouting: raw})
	if err != nil {
		return taskRuntimeSelection{}, fmt.Errorf("check task runtime: %w", err)
	}
	if !allowed {
		return taskRuntimeSelection{}, fmt.Errorf("%w: check agent access and the selected runtime's sharing settings", ErrTaskRuntimeUnavailable)
	}
	if route.Source == "personal" {
		runtime, err := q.GetAgentRuntime(ctx, runtimeID)
		if err != nil {
			return taskRuntimeSelection{}, fmt.Errorf("load selected runtime: %w", err)
		}
		if runtime.Status != "online" {
			return taskRuntimeSelection{}, fmt.Errorf("%w: selected personal runtime is %s", ErrTaskRuntimeOffline, runtime.Status)
		}
	}
	executionUserID, err = util.ParseUUID(routing.ExecutionUserID)
	if err != nil {
		return taskRuntimeSelection{}, err
	}
	runtimeOwnerID, err := util.ParseUUID(route.RuntimeOwnerID)
	if err != nil {
		return taskRuntimeSelection{}, err
	}
	return taskRuntimeSelection{RuntimeID: runtimeID, RuntimeOwnerID: runtimeOwnerID, Routing: raw, ExecutionUserID: executionUserID, Source: route.Source}, nil
}

func (s *TaskService) captureRuntimeRouting(ctx context.Context, q *db.Queries, workspaceID, userID pgtype.UUID) ([]byte, error) {
	candidates, err := q.ListRuntimeRoutingCandidates(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("load default runtime routes: %w", err)
	}
	preferences, err := q.ListAgentRuntimePreferences(ctx, db.ListAgentRuntimePreferencesParams{WorkspaceID: workspaceID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("load personal runtime routes: %w", err)
	}
	routing := RuntimeRouting{Version: 1, ExecutionUserID: util.UUIDToString(userID), Routes: make(map[string]RuntimeRoute, len(candidates))}
	for _, candidate := range candidates {
		routing.Routes[util.UUIDToString(candidate.AgentID)] = RuntimeRoute{RuntimeID: util.UUIDToString(candidate.RuntimeID), RuntimeOwnerID: util.UUIDToString(candidate.RuntimeOwnerID), Provider: candidate.Provider, Source: "default", Model: &candidate.Model.String}
	}
	for _, pref := range preferences {
		agentID := util.UUIDToString(pref.AgentID)
		route := RuntimeRoute{RuntimeID: util.UUIDToString(pref.RuntimeID), Source: "personal"}
		runtime, err := q.GetAgentRuntime(ctx, pref.RuntimeID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("load personal runtime: %w", err)
		}
		// Keep invalid preferences as invalid routes. Never silently replace
		// a deleted/changed personal machine with the agent owner's machine.
		if err == nil && runtime.WorkspaceID == workspaceID && runtime.OwnerID == userID {
			route.RuntimeOwnerID = util.UUIDToString(runtime.OwnerID)
			route.Provider = runtime.Provider
			model := ""
			// Personal runtimes never inherit the shared agent's model.
			if pref.ModelMode == "custom" {
				model = pref.Model
			}
			route.Model = &model
		}
		routing.Routes[agentID] = route
	}
	return json.Marshal(routing)
}

// EffectiveRuntimeForUser is shared by UI preflight and automated admission.
// Enqueue still resolves/validates its own immutable snapshot.
func (s *TaskService) EffectiveRuntimeForUser(ctx context.Context, agent db.Agent, userID pgtype.UUID) (pgtype.UUID, error) {
	return s.RuntimeForExecution(ctx, agent, userID, pgtype.UUID{})
}

// RuntimeForExecution preflights a trusted request principal and optional parent
// task. The handler resolves these from authentication, never a request payload.
func (s *TaskService) RuntimeForExecution(ctx context.Context, agent db.Agent, userID, sourceTaskID pgtype.UUID) (pgtype.UUID, error) {
	selection, err := s.resolveTaskRuntime(ctx, s.Queries, agent, userID, sourceTaskID)
	return selection.RuntimeID, err
}

func (s *TaskService) runtimeForIssueTask(ctx context.Context, agent db.Agent, issue db.Issue, attr attribution.Result, actorUserID, rerunOfTaskID, triggerCommentID pgtype.UUID) (taskRuntimeSelection, error) {
	parentID := attr.DelegatedFromTaskID
	executionUserID := attr.UserID
	if actorUserID.Valid || rerunOfTaskID.Valid {
		parentID = pgtype.UUID{}
	} else if !parentID.Valid && triggerCommentID.Valid {
		comment, err := s.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: triggerCommentID, WorkspaceID: issue.WorkspaceID})
		if err != nil {
			return taskRuntimeSelection{}, fmt.Errorf("load execution trigger: %w", err)
		}
		if comment.AuthorType == "system" && comment.SourceTaskID.Valid {
			parentID = comment.SourceTaskID
		}
	}
	if !actorUserID.Valid && !executionUserID.Valid && !rerunOfTaskID.Valid && !parentID.Valid && issue.OriginType.Valid && issue.OriginType.String == "autopilot" && issue.OriginID.Valid {
		if run, err := s.Queries.GetAutopilotRunByIssue(ctx, issue.ID); err == nil && run.TaskID.Valid {
			parentID = run.TaskID
		}
	}
	return s.resolveTaskRuntime(ctx, s.Queries, agent, executionUserID, parentID)
}

// runtimeOverlayOwner identifies whose connected accounts a prepared overlay uses.
func runtimeOverlayOwner(agent db.Agent, selection taskRuntimeSelection) pgtype.UUID {
	if selection.Source == "personal" || (selection.Source != "" && selection.RuntimeOwnerID.Valid && selection.RuntimeOwnerID != agent.OwnerID) {
		return selection.ExecutionUserID
	}
	return agent.OwnerID
}

func (s *TaskService) buildRoutedRuntimeMCPOverlay(ctx context.Context, originatorUserID pgtype.UUID, agent db.Agent, selection taskRuntimeSelection) runtimeMCPOverlayData {
	owner := runtimeOverlayOwner(agent, selection)
	if owner != agent.OwnerID {
		agent.OwnerID = owner
		originatorUserID = selection.ExecutionUserID
	}
	return s.buildRuntimeMCPOverlay(ctx, originatorUserID, agent)
}

func (s *TaskService) buildTaskRuntimeMCPOverlay(ctx context.Context, task db.AgentTaskQueue, agent db.Agent) runtimeMCPOverlayData {
	selection := taskRuntimeSelection{}
	if routing, err := ParseRuntimeRouting(task.RuntimeRouting); err == nil && routing != nil {
		if route, err := routing.Route(util.UUIDToString(task.AgentID)); err == nil {
			selection.Source = route.Source
			selection.RuntimeOwnerID, _ = util.ParseUUID(route.RuntimeOwnerID)
			selection.ExecutionUserID, _ = util.ParseUUID(routing.ExecutionUserID)
		}
	}
	return s.buildRoutedRuntimeMCPOverlay(ctx, task.OriginatorUserID, agent, selection)
}

// attributionForTrustedParent carries an authenticated task's delegation into
// assign/promote of an existing issue, without rewriting the issue's provenance.
// Callers may supply only a task ID established by task-token authentication.
func (s *TaskService) attributionForTrustedParent(ctx context.Context, issue db.Issue, parentID pgtype.UUID) (attribution.Result, error) {
	parent, err := s.Queries.GetAgentTask(ctx, parentID)
	if err != nil {
		return attribution.Result{}, fmt.Errorf("load acting task: %w", err)
	}
	parentAgent, err := s.Queries.GetAgent(ctx, parent.AgentID)
	if err != nil || parentAgent.WorkspaceID != issue.WorkspaceID {
		return attribution.Result{}, fmt.Errorf("%w: foreign acting task", ErrTaskRuntimeUnavailable)
	}
	return attribution.ClassifyDirect(attribution.DirectFacts{
		IssueID: issue.ID, OriginType: "agent_create", OriginTaskID: parent.ID,
		OriginOriginator: parent.OriginatorUserID, OriginAccountable: parent.AccountableUserID,
	}), nil
}
