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
		return nil, fmt.Errorf("associate an agent from Qoder Cloud Agents in the Multica agent runtime configuration")
	}
	binding, err := ParseAgentConfig(t.Agent.RuntimeConfig)
	if err != nil {
		return nil, err
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
		r.PriorSessionID = t.PriorSessionID
		p.WriteString("This is a follow-up on the same issue. If the earlier conversation is not in your context, rely on the issue and comments below.\n")
	}
	fmt.Fprintf(&p, "\nRun: %s\nIssue: %s\n\n%s\n\n%s\n", t.ID, t.IssueIdentifier, issue.Title, issue.Description)
	if t.IssueIdentifier != "" {
		fmt.Fprintf(&p, "\nWhen you change code, push a new branch named %s (never the default branch), open a pull request whose title starts with \"%s: \", and put the pull request link in your final summary. Multica links the pull request to this issue through that identifier.\n", BranchName(t.IssueIdentifier, issue.Title), t.IssueIdentifier)
	}
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
			return nil, fmt.Errorf("local directory resources cannot be executed by Qoder Cloud Agents")
		}
		if res.ResourceType != "github_repo" {
			continue
		}
		var ref Repository
		if err := json.Unmarshal(res.ResourceRef, &ref); err != nil {
			return nil, fmt.Errorf("invalid repository resource")
		}
		resource, err := repositoryResource(ref)
		if err != nil {
			return nil, err
		}
		r.Resources = append(r.Resources, resource)
		if len(r.Resources) > 1 {
			return nil, fmt.Errorf("Qoder Cloud Agents supports one project repository per run")
		}
	}
	// Project repositories win; otherwise use the agent's default repository,
	// and finally the workspace's only repository when it has a credential.
	if len(r.Resources) == 0 && binding.Repository != nil && binding.Repository.URL != "" {
		resource, err := repositoryResource(*binding.Repository)
		if err != nil {
			return nil, err
		}
		r.Resources = append(r.Resources, resource)
	}
	if len(r.Resources) == 0 && len(t.Repos) == 1 {
		if resource, err := repositoryResource(Repository{URL: t.Repos[0].URL}); err == nil {
			if _, err := b.sessionResources([]map[string]any{resource}); err == nil {
				r.Resources = append(r.Resources, resource)
			}
		}
	}
	for _, repo := range t.Repos {
		fmt.Fprintf(&p, "\nAvailable repository: %s (%s)\n", repo.URL, repo.Description)
	}
	r.Prompt = p.String()
	return r, nil
}

// Repository is a GitHub repository and optional branch mounted into a session.
type Repository struct {
	URL string `json:"url"`
	Ref string `json:"ref,omitempty"`
}

// AgentConfig is the runtime_config of a Multica agent bound to Qoder.
type AgentConfig struct {
	AgentID    string      `json:"qoder_agent_id"`
	Repository *Repository `json:"repository,omitempty"`
}

// ParseAgentConfig validates the Qoder binding stored on a Multica agent.
func ParseAgentConfig(raw json.RawMessage) (AgentConfig, error) {
	var cfg AgentConfig
	if err := json.Unmarshal(raw, &cfg); err != nil || !ValidAgentID(cfg.AgentID) {
		return cfg, fmt.Errorf("associate an agent from Qoder Cloud Agents in the Multica agent runtime configuration")
	}
	if cfg.Repository != nil && cfg.Repository.URL != "" {
		if _, err := repositoryResource(*cfg.Repository); err != nil {
			return cfg, fmt.Errorf("default repository: %w", err)
		}
	}
	return cfg, nil
}

var commitRef = regexp.MustCompile(`^[a-fA-F0-9]{40}([a-fA-F0-9]{24})?$`)

func repositoryResource(ref Repository) (map[string]any, error) {
	u, err := url.Parse(ref.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("repositories must use an HTTPS github.com URL without credentials")
	}
	resource := map[string]any{"type": "github_repository", "url": ref.URL}
	if strings.HasPrefix(ref.Ref, "refs/tags/") || commitRef.MatchString(ref.Ref) {
		return nil, fmt.Errorf("Qoder Cloud Agents supports branch refs only")
	}
	if name := strings.TrimPrefix(ref.Ref, "refs/heads/"); name != "" {
		resource["checkout"] = map[string]string{"type": "branch", "name": name}
	}
	return resource, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// BranchName gives remote work a branch Multica's GitHub integration links back
// to the issue: the identifier must appear in the branch name.
func BranchName(identifier, title string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 40 {
		slug = slug[:41]
		if i := strings.LastIndex(slug, "-"); i > 0 {
			slug = slug[:i]
		} else {
			slug = slug[:40]
		}
	}
	branch := "multica/" + strings.ToLower(identifier)
	if slug != "" {
		branch += "-" + slug
	}
	return branch
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

// ValidAgentID validates the public Qoder Cloud Agents identifier without accepting URL fragments.
func ValidAgentID(id string) bool {
	return regexp.MustCompile(`^agent_[a-zA-Z0-9_-]{1,128}$`).MatchString(id)
}
