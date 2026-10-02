package qoderruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

func (b *Bridge) prepare(ctx context.Context, t *claimedTask) (*run, error) {
	r := &run{TaskID: t.ID, Phase: "prepared", Resources: []map[string]any{}}
	if t.RuntimeID != b.state.RuntimeID || t.WorkspaceID != b.cfg.WorkspaceID {
		return nil, fmt.Errorf("claim identity does not match bridge")
	}
	if t.IssueID == "" || t.ChatSessionID != "" || t.IsLeaderTask {
		return nil, fmt.Errorf("Qoder bridge currently supports issue runs only")
	}
	if !strings.HasPrefix(t.AuthToken, "mat_") {
		return nil, fmt.Errorf("claim is missing a task-scoped credential")
	}
	if len(t.RemoteMCPConnections) > 0 || len(t.ConnectedApps) > 0 || len(t.PluginHookTools) > 0 {
		return nil, fmt.Errorf("Multica connected apps and runtime MCP tools are not supported by the Qoder bridge")
	}
	if t.Agent != nil && (hasCustomSkills(t.Agent.Skills) || len(t.Agent.SkillRefs) > 0 || len(t.Agent.CustomEnv) > 0 || len(t.Agent.CustomArgs) > 0 || len(t.Agent.McpConfig) > 0 && string(t.Agent.McpConfig) != "null" && string(t.Agent.McpConfig) != "{}" || t.Agent.Model != "" || t.Agent.ThinkingLevel != "" || t.Agent.ServiceTier != "") {
		return nil, fmt.Errorf("configure skills, tools, credentials and model on the Qoder Agent; Multica execution overrides are not supported by this bridge")
	}
	if t.Agent == nil {
		return nil, fmt.Errorf("associate a QCA Agent in the Multica agent runtime configuration")
	}
	var binding struct {
		AgentID string `json:"qoder_agent_id"`
	}
	if err := json.Unmarshal(t.Agent.RuntimeConfig, &binding); err != nil || !ValidAgentID(binding.AgentID) {
		return nil, fmt.Errorf("associate a QCA Agent in the Multica agent runtime configuration")
	}
	r.AgentID = binding.AgentID
	taskAPI := *b.multica
	taskAPI.token = t.AuthToken
	var issue struct {
		Title       string            `json:"title"`
		Description string            `json:"description"`
		Attachments []json.RawMessage `json:"attachments"`
	}
	if err := taskAPI.call(ctx, http.MethodGet, "/api/issues/"+url.PathEscape(t.IssueID), nil, &issue); err != nil {
		return nil, fmt.Errorf("read issue: %w", err)
	}
	if len(issue.Attachments) > 0 {
		return nil, fmt.Errorf("issue attachments are not supported by the Qoder bridge")
	}
	var p strings.Builder
	p.WriteString("Complete the following Multica issue in your Qoder environment. Return a final summary including code changes, tests and branch/PR/MR links when available. This bridge does not expose a local Multica daemon.\n")
	if t.PriorSessionID != "" {
		p.WriteString("This is a new Qoder session; previous remote conversation state is not loaded.\n")
	}
	fmt.Fprintf(&p, "\nRun: %s\nIssue: %s\n\n%s\n\n%s\n", t.ID, t.IssueIdentifier, issue.Title, issue.Description)
	if t.Agent != nil {
		fmt.Fprintf(&p, "\nAgent instructions:\n%s\n", t.Agent.Instructions)
	}
	fmt.Fprintf(&p, "\nWorkspace context:\n%s\n\nProject:\n%s\n%s\n", t.WorkspaceContext, t.ProjectTitle, t.ProjectDescription)
	if t.HandoffNote != "" {
		fmt.Fprintf(&p, "\nHandoff:\n%s\n", t.HandoffNote)
	}
	for _, c := range t.CoalescedComments {
		fmt.Fprintf(&p, "\nComment by %s:\n%s\n", c.AuthorName, c.Content)
	}
	if t.TriggerCommentContent != "" {
		fmt.Fprintf(&p, "\nTriggering comment by %s:\n%s\n", t.TriggerAuthorName, t.TriggerCommentContent)
	}
	for _, res := range t.ProjectResources {
		if res.ResourceType == "local_directory" {
			return nil, fmt.Errorf("local directory resources cannot be executed by Qoder Cloud Agent")
		}
		if res.ResourceType != "github_repo" {
			continue
		}
		var ref struct {
			URL string `json:"url"`
			Ref string `json:"ref"`
		}
		if err := json.Unmarshal(res.ResourceRef, &ref); err != nil {
			return nil, fmt.Errorf("invalid repository resource")
		}
		u, err := url.Parse(ref.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("PoC repository resources must use an HTTPS github.com URL without credentials")
		}
		resource := map[string]any{"type": "github_repository", "url": ref.URL}
		if strings.HasPrefix(ref.Ref, "refs/tags/") || regexp.MustCompile(`^[a-fA-F0-9]{40}([a-fA-F0-9]{24})?$`).MatchString(ref.Ref) {
			return nil, fmt.Errorf("Qoder bridge PoC supports branch refs only")
		}
		ref.Ref = strings.TrimPrefix(ref.Ref, "refs/heads/")
		if ref.Ref != "" {
			resource["checkout"] = map[string]string{"type": "branch", "name": ref.Ref}
		}
		r.Resources = append(r.Resources, resource)
		if len(r.Resources) > 1 {
			return nil, fmt.Errorf("Qoder bridge PoC supports one project repository per run")
		}
	}
	// Workspace repositories are informational; only explicitly bound project repositories are mounted.
	for _, repo := range t.Repos {
		fmt.Fprintf(&p, "\nAvailable repository: %s (%s)\n", repo.URL, repo.Description)
	}
	r.Prompt = p.String()
	return r, nil
}

// Built-in platform skills have no ID and assume local CLI tools. Do not inject
// those tools into a managed Qoder session; custom skills must be configured there.
func hasCustomSkills(skills []claimedSkill) bool {
	for _, s := range skills {
		if s.ID != "" {
			return true
		}
	}
	return false
}

// ValidAgentID validates the public QCA identifier without accepting URL fragments.
func ValidAgentID(id string) bool {
	return regexp.MustCompile(`^agent_[a-zA-Z0-9_-]{1,128}$`).MatchString(id)
}
