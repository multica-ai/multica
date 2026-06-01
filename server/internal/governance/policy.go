package governance

import "sort"

type Role string

const (
	RoleManagementTeam  Role = "management_team"
	RoleHRExecutive     Role = "hr_executive"
	RoleProcessExpert   Role = "process_expert"
	RolePromptEngineer  Role = "prompt_engineer"
	RoleSupervisorAudit Role = "supervisor_audit"
)

type Strategy string

const (
	StrategyAutomatic        Strategy = "automatic"
	StrategyApprovalRequired Strategy = "approval_required"
	StrategyDenied           Strategy = "denied"
)

type Domain string

const (
	DomainWorkspace    Domain = "workspace"
	DomainProject      Domain = "project"
	DomainAgent        Domain = "agent"
	DomainAutopilot    Domain = "autopilot"
	DomainSkillSquad   Domain = "skill_squad"
	DomainIssueMetadata Domain = "issue_metadata"
)

type Action struct {
	ID          string   `json:"id"`
	Domain      Domain   `json:"domain"`
	Strategy    Strategy `json:"strategy"`
	Description string   `json:"description"`
	Audit       bool     `json:"audit"`
}

type RoleTemplate struct {
	ID          Role     `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Domains     []Domain `json:"domains"`
}

type Decision struct {
	ActionID       string   `json:"action_id"`
	Domain         Domain   `json:"domain"`
	Strategy       Strategy `json:"strategy"`
	Allowed        bool     `json:"allowed"`
	RequiresApproval bool   `json:"requires_approval"`
	Reason         string   `json:"reason"`
	Audit          bool     `json:"audit"`
}

type Context struct {
	WorkspaceRole string
	Approved      bool
}

func RoleTemplates() []RoleTemplate {
	return []RoleTemplate{
		{
			ID:          RoleManagementTeam,
			Name:        "Management Team",
			Description: "Coordinates cross-project governance, organization rules, and operational experiments.",
			Domains:     []Domain{DomainWorkspace, DomainProject, DomainAgent, DomainAutopilot, DomainSkillSquad, DomainIssueMetadata},
		},
		{
			ID:          RoleHRExecutive,
			Name:        "HR Executive",
			Description: "Creates, updates, archives, restores, and evaluates agent roles within approved boundaries.",
			Domains:     []Domain{DomainAgent, DomainSkillSquad, DomainIssueMetadata},
		},
		{
			ID:          RoleProcessExpert,
			Name:        "Process Expert",
			Description: "Improves workflow, issue hygiene, project rules, and autopilot operating cadence.",
			Domains:     []Domain{DomainProject, DomainAutopilot, DomainIssueMetadata},
		},
		{
			ID:          RolePromptEngineer,
			Name:        "Prompt Engineer",
			Description: "Updates non-sensitive agent descriptions, instructions, and workflow prompts.",
			Domains:     []Domain{DomainAgent, DomainSkillSquad, DomainIssueMetadata},
		},
		{
			ID:          RoleSupervisorAudit,
			Name:        "Supervisor / Audit",
			Description: "Reviews governance decisions, approvals, and audit trails without expanding permissions.",
			Domains:     []Domain{DomainWorkspace, DomainProject, DomainAgent, DomainAutopilot, DomainSkillSquad, DomainIssueMetadata},
		},
	}
}

func Actions() []Action {
	return []Action{
		{ID: "issue.status.remediate", Domain: DomainIssueMetadata, Strategy: StrategyAutomatic, Description: "Repair issue status, labels, assignee, or metadata when the requested change is reversible and non-sensitive.", Audit: false},
		{ID: "issue.metadata.update_non_sensitive", Domain: DomainIssueMetadata, Strategy: StrategyAutomatic, Description: "Update non-sensitive issue metadata that documents PRs, blockers, deploy URLs, or current decisions.", Audit: false},
		{ID: "project.description.update", Domain: DomainProject, Strategy: StrategyAutomatic, Description: "Update non-sensitive project descriptions, resources, or operating notes.", Audit: false},
		{ID: "autopilot.pause_resume", Domain: DomainAutopilot, Strategy: StrategyAutomatic, Description: "Pause or resume an existing autopilot without changing triggers, webhooks, or secrets.", Audit: true},
		{ID: "agent.description.update", Domain: DomainAgent, Strategy: StrategyAutomatic, Description: "Update non-sensitive agent name, description, instructions, or prompt guidance.", Audit: true},
		{ID: "agent.create", Domain: DomainAgent, Strategy: StrategyApprovalRequired, Description: "Create a new agent or digital employee.", Audit: true},
		{ID: "agent.archive_restore", Domain: DomainAgent, Strategy: StrategyApprovalRequired, Description: "Archive or restore an agent.", Audit: true},
		{ID: "agent.env.update", Domain: DomainAgent, Strategy: StrategyApprovalRequired, Description: "Update custom environment variables or secret-like agent configuration.", Audit: true},
		{ID: "agent.permissions.update", Domain: DomainAgent, Strategy: StrategyApprovalRequired, Description: "Change an agent's workspace/project authorization boundary.", Audit: true},
		{ID: "autopilot.delete", Domain: DomainAutopilot, Strategy: StrategyApprovalRequired, Description: "Delete an autopilot or remove a trigger.", Audit: true},
		{ID: "autopilot.webhook.rotate", Domain: DomainAutopilot, Strategy: StrategyApprovalRequired, Description: "Rotate webhook URLs, tokens, or signing secrets.", Audit: true},
		{ID: "workspace.rules.update_cross_project", Domain: DomainWorkspace, Strategy: StrategyApprovalRequired, Description: "Update workspace-level rules, prompt runtime context, or cross-project governance defaults.", Audit: true},
		{ID: "workspace.metadata.update", Domain: DomainWorkspace, Strategy: StrategyApprovalRequired, Description: "Update workspace metadata, context, or organization-wide settings.", Audit: true},
		{ID: "project.delete", Domain: DomainProject, Strategy: StrategyApprovalRequired, Description: "Delete a project or destructive project-scoped configuration.", Audit: true},
		{ID: "squad.delete", Domain: DomainSkillSquad, Strategy: StrategyApprovalRequired, Description: "Delete a squad or remove its governance ownership.", Audit: true},
		{ID: "skill.delete", Domain: DomainSkillSquad, Strategy: StrategyApprovalRequired, Description: "Delete a skill used by agents or workflow automation.", Audit: true},
		{ID: "workspace.owner_grant", Domain: DomainWorkspace, Strategy: StrategyDenied, Description: "Grant workspace owner/admin authority to the actor itself or bypass owner approval.", Audit: true},
		{ID: "secret.reveal", Domain: DomainWorkspace, Strategy: StrategyDenied, Description: "Reveal, post, or exfiltrate secret values, tokens, private keys, or credentials.", Audit: true},
		{ID: "production.deploy", Domain: DomainWorkspace, Strategy: StrategyDenied, Description: "Deploy, publish, upload firmware, charge billing, or mutate production external systems without human control.", Audit: true},
	}
}

func ActionByID(actionID string) (Action, bool) {
	for _, action := range Actions() {
		if action.ID == actionID {
			return action, true
		}
	}
	return Action{}, false
}

func Evaluate(action Action, ctx Context) Decision {
	decision := Decision{
		ActionID: action.ID,
		Domain:   action.Domain,
		Strategy: action.Strategy,
		Audit:    action.Audit,
	}
	switch action.Strategy {
	case StrategyAutomatic:
		if isWorkspaceMember(ctx.WorkspaceRole) {
			decision.Allowed = true
			decision.Reason = "automatic_action_allowed"
		} else {
			decision.Strategy = StrategyDenied
			decision.Reason = "not_workspace_member"
		}
	case StrategyApprovalRequired:
		decision.RequiresApproval = true
		if !isWorkspaceAdminLike(ctx.WorkspaceRole) {
			decision.Reason = "requires_workspace_admin_or_owner"
		} else if !ctx.Approved {
			decision.Reason = "approval_required"
		} else {
			decision.Allowed = true
			decision.RequiresApproval = false
			decision.Reason = "approved_action_allowed"
		}
	case StrategyDenied:
		decision.Reason = "hard_boundary"
	default:
		decision.Strategy = StrategyDenied
		decision.Reason = "unknown_strategy"
	}
	return decision
}

func EvaluateAll(ctx Context) []Decision {
	actions := Actions()
	decisions := make([]Decision, 0, len(actions))
	for _, action := range actions {
		decisions = append(decisions, Evaluate(action, ctx))
	}
	sort.Slice(decisions, func(i, j int) bool {
		if decisions[i].Domain == decisions[j].Domain {
			return decisions[i].ActionID < decisions[j].ActionID
		}
		return decisions[i].Domain < decisions[j].Domain
	})
	return decisions
}

func isWorkspaceMember(role string) bool {
	return role == "owner" || role == "admin" || role == "member"
}

func isWorkspaceAdminLike(role string) bool {
	return role == "owner" || role == "admin"
}
