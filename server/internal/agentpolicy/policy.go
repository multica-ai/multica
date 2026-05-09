package agentpolicy

import "encoding/json"

const (
	ModeOperatorControlled = "operator_controlled"

	CommandIssueCreate         = "issue.create"
	CommandIssueUpdateStatus   = "issue.update.status"
	CommandIssueStatus         = "issue.status"
	CommandIssueUpdateAssignee = "issue.update.assignee"
	CommandIssueAssign         = "issue.assign"
)

// Policy is the Multica command policy embedded in agent.runtime_config.
type Policy struct {
	Mode              string   `json:"mode"`
	DenyCommands      []string `json:"deny_commands"`
	DenyAgentMentions *bool    `json:"deny_agent_mentions"`
	AllowPlainComment *bool    `json:"allow_comment_without_agent_mentions"`
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

func (p Policy) DeniesCommand(command string) bool {
	for _, denied := range p.DenyCommands {
		if denied == command {
			return true
		}
	}
	if !p.IsOperatorControlled() {
		return false
	}
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
	if p.IsOperatorControlled() {
		return true
	}
	if p.DenyAgentMentions != nil {
		return *p.DenyAgentMentions
	}
	return false
}
