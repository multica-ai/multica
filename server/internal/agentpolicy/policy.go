package agentpolicy

import (
	"encoding/json"
	"strings"
)

const (
	ModeOperatorControlled      = "operator_controlled"
	ModeSupervisedCollaboration = "supervised_collaboration"
	ModeAutonomousSandbox       = "autonomous_sandbox"

	CommandIssueCreate         = "issue.create"
	CommandIssueUpdateStatus   = "issue.update.status"
	CommandIssueStatus         = "issue.status"
	CommandIssueUpdateAssignee = "issue.update.assignee"
	CommandIssueAssign         = "issue.assign"

	RawAgentMentionsDeny              = "deny"
	CollaborationRequestsAllowAudited = "allow_audited"
	CollaborationScopeSameIssue       = "same_issue"
)

// Policy is the Multica command policy embedded in agent.runtime_config.
type Policy struct {
	SchemaVersion     string              `json:"schema_version"`
	Mode              string              `json:"mode"`
	DenyCommands      []string            `json:"deny_commands"`
	DenyAgentMentions *bool               `json:"deny_agent_mentions"`
	AllowPlainComment *bool               `json:"allow_comment_without_agent_mentions"`
	Collaboration     CollaborationPolicy `json:"collaboration"`
}

// CollaborationPolicy describes bounded agent-to-agent discussion behavior.
// MHS-20 introduced the policy foundation; MHS-21 adds the server-validated
// collaboration_request primitive used instead of raw agent mentions.
type CollaborationPolicy struct {
	Enabled               *bool    `json:"enabled"`
	Scope                 string   `json:"scope"`
	AllowedAgentTargets   []string `json:"allowed_agent_targets"`
	RawAgentMentions      string   `json:"raw_agent_mentions"`
	CollaborationRequests string   `json:"collaboration_requests"`
	MaxTurns              int      `json:"max_turns"`
	MaxDepth              int      `json:"max_depth"`
	TTLMinutes            int      `json:"ttl_minutes"`
	PreventSelfHandoff    *bool    `json:"prevent_self_handoff"`
	PreventCycles         *bool    `json:"prevent_cycles"`
}

type runtimeConfig struct {
	MulticaPolicy *Policy `json:"multica_policy"`
}

// FromRuntimeConfig extracts the Multica command policy from agent.runtime_config.
// Invalid or empty JSON is treated as no policy so legacy agents keep working.
func FromRuntimeConfig(raw []byte) Policy {
	if len(raw) == 0 {
		return Policy{}
	}
	var cfg runtimeConfig
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg.MulticaPolicy == nil {
		return Policy{}
	}
	return *cfg.MulticaPolicy
}

func (p Policy) IsOperatorControlled() bool {
	return p.Mode == ModeOperatorControlled
}

func (p Policy) IsSupervisedCollaboration() bool {
	return p.Mode == ModeSupervisedCollaboration
}

func (p Policy) IsAutonomousSandbox() bool {
	return p.Mode == ModeAutonomousSandbox
}

func (p Policy) DeniesCommand(command string) bool {
	for _, denied := range p.DenyCommands {
		if denied == command {
			return true
		}
	}
	if !p.IsOperatorControlled() && !p.IsSupervisedCollaboration() {
		return false
	}
	// MHS-20 keeps supervised_collaboration lifecycle mutations controlled until
	// a proposal/approval path exists. Agents may discuss in comments, but direct
	// issue creation, status changes, and assignee changes remain server-denied.
	switch command {
	case CommandIssueCreate,
		CommandIssueUpdateStatus,
		CommandIssueStatus,
		CommandIssueUpdateAssignee,
		CommandIssueAssign:
		return true
	default:
		return false
	}
}

func (p Policy) DeniesAnyCommand(commands ...string) bool {
	for _, command := range commands {
		if p.DeniesCommand(command) {
			return true
		}
	}
	return false
}

func (p Policy) DeniesAgentMentionsByDefault() bool {
	if p.IsOperatorControlled() || p.IsSupervisedCollaboration() {
		return true
	}
	if p.DenyAgentMentions != nil {
		return *p.DenyAgentMentions
	}
	return false
}

func (p Policy) AllowsAuditedCollaborationRequests() bool {
	if !p.IsSupervisedCollaboration() {
		return false
	}
	if p.Collaboration.Enabled != nil && !*p.Collaboration.Enabled {
		return false
	}
	scope := strings.TrimSpace(p.Collaboration.Scope)
	if scope != "" && scope != CollaborationScopeSameIssue {
		return false
	}
	return p.Collaboration.CollaborationRequests == CollaborationRequestsAllowAudited
}

func (p Policy) DeniesRawAgentMentions() bool {
	if p.IsOperatorControlled() || p.IsSupervisedCollaboration() {
		return true
	}
	return p.Collaboration.RawAgentMentions == RawAgentMentionsDeny
}

func (p Policy) PreventsSelfHandoff() bool {
	if !p.IsSupervisedCollaboration() {
		return false
	}
	if p.Collaboration.PreventSelfHandoff != nil {
		return *p.Collaboration.PreventSelfHandoff
	}
	return true
}

func (p Policy) PreventsCycles() bool {
	if !p.IsSupervisedCollaboration() {
		return false
	}
	if p.Collaboration.PreventCycles != nil {
		return *p.Collaboration.PreventCycles
	}
	return true
}

func (p Policy) MaxCollaborationTurns() int {
	if p.Collaboration.MaxTurns > 0 {
		return p.Collaboration.MaxTurns
	}
	return 2
}

func (p Policy) MaxCollaborationDepth() int {
	if p.Collaboration.MaxDepth > 0 {
		return p.Collaboration.MaxDepth
	}
	return 1
}

func (p Policy) CollaborationTTLMinutes() int {
	if p.Collaboration.TTLMinutes > 0 {
		return p.Collaboration.TTLMinutes
	}
	return 120
}

func (p Policy) AllowsTargetAgent(targetName, targetID string) bool {
	if !p.AllowsAuditedCollaborationRequests() {
		return false
	}
	if len(p.Collaboration.AllowedAgentTargets) == 0 {
		return true
	}
	targetName = strings.ToLower(strings.TrimSpace(targetName))
	targetID = strings.ToLower(strings.TrimSpace(targetID))
	for _, allowed := range p.Collaboration.AllowedAgentTargets {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed != "" && (allowed == targetName || allowed == targetID) {
			return true
		}
	}
	return false
}
