package workflows

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ActiveRuleContext struct {
	WorkspaceID string
	ProjectID   string
	AgentID     string
	Model       string
	IssueID     string
}

type ActiveHookRuleScope struct {
	Kind  HookScopeKind `json:"kind"`
	Value string        `json:"value"`
}

type ActiveHookRule struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	ContractRule    string              `json:"contract_rule"`
	ContractSatisfy string              `json:"contract_satisfy"`
	Events          []HookEventType     `json:"events"`
	Scope           ActiveHookRuleScope `json:"scope"`
}

type ActiveRuleService struct {
	repository    HookRepository
	serverEnabled bool
	flags         WorkspaceFlagResolver
}

type ActiveRuleContextResolver interface {
	Resolve(context.Context, string, string, string) (ActiveRuleContext, error)
}

type PostgresActiveRuleContextResolver struct{ queries *db.Queries }

func NewPostgresActiveRuleContextResolver(database db.DBTX) *PostgresActiveRuleContextResolver {
	return &PostgresActiveRuleContextResolver{queries: db.New(database)}
}

func (r *PostgresActiveRuleContextResolver) Resolve(ctx context.Context, workspaceID, agentID, issueID string) (ActiveRuleContext, error) {
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return ActiveRuleContext{}, err
	}
	agentUUID, err := util.ParseUUID(agentID)
	if err != nil {
		return ActiveRuleContext{}, err
	}
	issueUUID, err := util.ParseUUID(issueID)
	if err != nil {
		return ActiveRuleContext{}, err
	}
	agent, err := r.queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentUUID, WorkspaceID: workspaceUUID})
	if err != nil {
		return ActiveRuleContext{}, err
	}
	issue, err := r.queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueUUID, WorkspaceID: workspaceUUID})
	if err != nil {
		return ActiveRuleContext{}, err
	}
	projectID := ""
	if issue.ProjectID.Valid {
		projectID = uuid.UUID(issue.ProjectID.Bytes).String()
	}
	return ActiveRuleContext{
		WorkspaceID: workspaceID, ProjectID: projectID, AgentID: agentID, Model: agent.Model.String, IssueID: issueID,
	}, nil
}

func NewActiveRuleService(repository HookRepository) *ActiveRuleService {
	return &ActiveRuleService{repository: repository, serverEnabled: true}
}

func (s *ActiveRuleService) WithWorkspaceFlags(flags WorkspaceFlagResolver) *ActiveRuleService {
	s.flags = flags
	return s
}

func (s *ActiveRuleService) WithServerEnabled(enabled bool) *ActiveRuleService {
	s.serverEnabled = enabled
	return s
}

func (s *ActiveRuleService) List(ctx context.Context, scope ActiveRuleContext) ([]ActiveHookRule, error) {
	if s == nil || s.repository == nil || !s.serverEnabled || scope.WorkspaceID == "" {
		return []ActiveHookRule{}, nil
	}
	if s.flags != nil {
		enabled, err := s.flags.WorkflowHooksEnabledForWorkspace(ctx, scope.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if !enabled {
			return []ActiveHookRule{}, nil
		}
	}
	policies, err := s.repository.ListEffective(ctx, scope.WorkspaceID)
	if err != nil {
		return nil, err
	}
	event := HookEvent{
		WorkspaceID: scope.WorkspaceID,
		ProjectID:   scope.ProjectID,
		AgentID:     scope.AgentID,
		Model:       scope.Model,
		IssueID:     scope.IssueID,
	}
	rules := make([]ActiveHookRule, 0, len(policies))
	for _, policy := range policies {
		binding, ok := mostSpecificBinding(policy.Bindings, event)
		if !ok {
			continue
		}
		rules = append(rules, ActiveHookRule{
			ID: policy.ID, Name: policy.Name,
			ContractRule: policy.ContractRule, ContractSatisfy: policy.ContractSatisfy,
			Events: policy.Events,
			Scope:  ActiveHookRuleScope{Kind: binding.Kind, Value: binding.ID},
		})
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Name == rules[j].Name {
			return rules[i].ID < rules[j].ID
		}
		return rules[i].Name < rules[j].Name
	})
	return rules, nil
}
