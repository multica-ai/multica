package taskpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	BrainMCPAcceptanceMode     = "brain_mcp_acceptance_v1"
	BrainMCPReadOnlyProvider   = "claude"
	FrozenCompanyEventsAfter   = int64(199900)
	FrozenCompanyEventsThrough = int64(199901)
	FrozenCompanyEventsLimit   = int64(1)
	BrainProjectID             = "brain"
	MulticaProjectID           = "multica"
)

var frozenBrainProjectIDs = [...]string{BrainProjectID, MulticaProjectID}

var frozenBrainContextQuestions = map[string]string{
	BrainProjectID:   "How do I retrieve evidence beyond the short search excerpt?",
	MulticaProjectID: "A worker completed its task. Has the owner accepted the issue?",
}

const BrainAuthorityQuestion = "Can a note become authoritative just by saying verified or moving into a truth folder?"

var brainMCPServerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
var brainMCPRevisionPattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

var brainMCPToolNames = [...]string{
	"brain_resolve",
	"brain_context",
	"brain_read",
	"brain_company_projects",
	"brain_projects",
	"company_events",
}

// Request is the minimal owner-authorized opt-in accepted on a one-task rerun.
// The tool set and provider are fixed by this server-side policy.
type Request struct {
	Mode       string   `json:"mode"`
	MCPServer  string   `json:"mcp_server"`
	ProjectIDs []string `json:"project_ids"`
}

type CompanyEventsSelector struct {
	AfterID   int64 `json:"afterId"`
	ThroughID int64 `json:"throughId"`
	Limit     int64 `json:"limit"`
}

// ExecutionPolicy is persisted with a single task and echoed in its claim.
type ExecutionPolicy struct {
	Mode                  string                `json:"mode"`
	Provider              string                `json:"provider"`
	MCPServer             string                `json:"mcp_server"`
	ProjectIDs            []string              `json:"project_ids"`
	Tools                 []string              `json:"tools"`
	CompanyEventsSelector CompanyEventsSelector `json:"company_events_selector"`
}

func NewBrainMCPAcceptance(request Request) (*ExecutionPolicy, error) {
	if request.Mode != BrainMCPAcceptanceMode {
		return nil, fmt.Errorf("unsupported task execution policy mode %q", request.Mode)
	}
	if !brainMCPServerNamePattern.MatchString(request.MCPServer) {
		return nil, errors.New("task execution policy requires a valid MCP server name")
	}
	if !equalStrings(request.ProjectIDs, frozenBrainProjectIDs[:]) {
		return nil, errors.New("task execution policy requires the exact frozen brain and multica project scope")
	}
	return &ExecutionPolicy{
		Mode:       BrainMCPAcceptanceMode,
		Provider:   BrainMCPReadOnlyProvider,
		MCPServer:  request.MCPServer,
		ProjectIDs: append([]string(nil), request.ProjectIDs...),
		Tools:      BrainMCPToolIDs(request.MCPServer),
		CompanyEventsSelector: CompanyEventsSelector{
			AfterID:   FrozenCompanyEventsAfter,
			ThroughID: FrozenCompanyEventsThrough,
			Limit:     FrozenCompanyEventsLimit,
		},
	}, nil
}

func Validate(policy *ExecutionPolicy) error {
	if policy == nil {
		return nil
	}
	if policy.Mode != BrainMCPAcceptanceMode {
		return fmt.Errorf("unsupported task execution policy mode %q", policy.Mode)
	}
	if policy.Provider != BrainMCPReadOnlyProvider {
		return fmt.Errorf("task execution policy requires provider %q", BrainMCPReadOnlyProvider)
	}
	if !brainMCPServerNamePattern.MatchString(policy.MCPServer) {
		return errors.New("task execution policy requires a valid MCP server name")
	}
	if !equalStrings(policy.ProjectIDs, frozenBrainProjectIDs[:]) {
		return errors.New("task execution policy requires the exact frozen brain and multica project scope")
	}
	expected := BrainMCPToolIDs(policy.MCPServer)
	if len(policy.Tools) != len(expected) {
		return errors.New("task execution policy must contain the exact Brain MCP tool allowlist")
	}
	got := make(map[string]struct{}, len(policy.Tools))
	for _, tool := range policy.Tools {
		if _, exists := got[tool]; exists {
			return errors.New("task execution policy contains a duplicate MCP tool")
		}
		got[tool] = struct{}{}
	}
	for _, tool := range expected {
		if _, ok := got[tool]; !ok {
			return errors.New("task execution policy must contain the exact Brain MCP tool allowlist")
		}
	}
	if policy.CompanyEventsSelector != (CompanyEventsSelector{
		AfterID:   FrozenCompanyEventsAfter,
		ThroughID: FrozenCompanyEventsThrough,
		Limit:     FrozenCompanyEventsLimit,
	}) {
		return errors.New("task execution policy must contain the frozen company_events selector")
	}
	return nil
}

// AllowsToolInput enforces exact, versioned acceptance selectors before Claude
// calls the MCP server. It does not by itself authorize brain_read documents;
// callers must also correlate those IDs and revisions to a verified context.
func AllowsToolInput(policy *ExecutionPolicy, tool string, input map[string]any) bool {
	if policy == nil || !IsBrainMCPReadOnlyTool(policy, "mcp__"+policy.MCPServer+"__"+tool) || input == nil {
		return false
	}
	switch tool {
	case "brain_resolve":
		identity, ok := input["identity"].(string)
		if len(input) != 1 || !ok {
			return false
		}
		return identity == "not-an-enrolled-project"
	case "brain_context":
		project, projectOK := input["project"].(string)
		question, questionOK := input["q"].(string)
		if len(input) != 2 || !projectOK || !questionOK || !containsString(policy.ProjectIDs, project) {
			return false
		}
		if project == BrainProjectID {
			return question == frozenBrainContextQuestions[BrainProjectID] ||
				question == BrainAuthorityQuestion
		}
		return question == frozenBrainContextQuestions[MulticaProjectID]
	case "brain_read":
		id, idOK := input["id"].(string)
		revision, revisionOK := input["revision"].(string)
		return len(input) == 2 && idOK && validBrainProjectID(id) &&
			revisionOK && brainMCPRevisionPattern.MatchString(revision)
	case "brain_company_projects", "brain_projects":
		return len(input) == 0
	case "company_events":
		return allowsCompanyEventsInput(policy, input)
	default:
		return false
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func allowsCompanyEventsInput(policy *ExecutionPolicy, input map[string]any) bool {
	if policy == nil || len(input) != 3 {
		return false
	}
	afterID, afterOK := input["afterId"].(float64)
	throughID, throughOK := input["throughId"].(float64)
	limit, limitOK := input["limit"].(float64)
	return afterOK && afterID == float64(policy.CompanyEventsSelector.AfterID) &&
		throughOK && throughID == float64(policy.CompanyEventsSelector.ThroughID) &&
		limitOK && limit == float64(policy.CompanyEventsSelector.Limit)
}

func validBrainProjectID(value string) bool {
	if value == "" || len(value) > 256 || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func BrainMCPToolIDs(serverName string) []string {
	tools := make([]string, len(brainMCPToolNames))
	for index, name := range brainMCPToolNames {
		tools[index] = "mcp__" + serverName + "__" + name
	}
	return tools
}

func IsBrainMCPReadOnlyTool(policy *ExecutionPolicy, tool string) bool {
	if policy == nil || Validate(policy) != nil {
		return false
	}
	for _, allowed := range policy.Tools {
		if tool == allowed {
			return true
		}
	}
	return false
}

// FromTaskContext reads the opt-in policy from agent_task_queue.context. Other
// context fields, such as head_sha, remain opaque to this package.
func FromTaskContext(raw []byte) (*ExecutionPolicy, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var context map[string]json.RawMessage
	if err := json.Unmarshal(raw, &context); err != nil {
		return nil, fmt.Errorf("parse task context: %w", err)
	}
	policyJSON, ok := context["execution_policy"]
	if !ok || bytes.Equal(bytes.TrimSpace(policyJSON), []byte("null")) {
		return nil, nil
	}
	var policy ExecutionPolicy
	if err := json.Unmarshal(policyJSON, &policy); err != nil {
		return nil, fmt.Errorf("parse task execution policy: %w", err)
	}
	if err := Validate(&policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

// FilterMCPConfig returns a fresh Claude MCP config containing only the
// policy's already-configured server. It never adds, edits, or mutates a
// shared agent/runtime configuration.
func FilterMCPConfig(raw json.RawMessage, policy *ExecutionPolicy) (json.RawMessage, error) {
	if err := Validate(policy); err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, errors.New("task execution policy is required")
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errors.New("task execution policy requires a managed MCP config")
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parse effective MCP config: %w", err)
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(config["mcpServers"], &servers); err != nil || servers == nil {
		return nil, errors.New("task execution policy requires a managed mcpServers map")
	}
	server, ok := servers[policy.MCPServer]
	if !ok || len(server) == 0 || string(server) == "null" {
		return nil, fmt.Errorf("authorized MCP server %q is not present in the effective config", policy.MCPServer)
	}
	filtered, err := json.Marshal(map[string]any{
		"mcpServers": map[string]json.RawMessage{policy.MCPServer: server},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal task MCP config: %w", err)
	}
	return filtered, nil
}
