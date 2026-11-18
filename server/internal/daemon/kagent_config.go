package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/kagent"
)

// This configuration is operator-owned. Agent runtime_config only opts into
// native-text; it cannot select an endpoint, namespace, harness or identity.
type kagentDaemonConfig struct {
	JournalDir          string             `json:"journal_dir"`
	KubernetesEndpoint  string             `json:"kubernetes_endpoint"`
	KubernetesTokenFile string             `json:"kubernetes_token_file"`
	A2AEndpoint         string             `json:"a2a_endpoint"`
	A2ATokenFile        string             `json:"a2a_token_file"`
	Namespace           string             `json:"namespace"`
	Harness             string             `json:"harness"`
	ModelConfig         string             `json:"model_config"`
	Agents              []kagentEnrollment `json:"agents"`
}

type kagentEnrollment struct {
	AgentID     string `json:"agent_id"`
	WorkspaceID string `json:"workspace_id"`
}

func loadKagentDaemonConfig() (kagentDaemonConfig, error) {
	var c kagentDaemonConfig
	if os.Getenv("MULTICA_KAGENT_ENABLED") != "1" {
		return c, fmt.Errorf("kagent: DEV provider disabled")
	}
	path := os.Getenv("MULTICA_KAGENT_CONFIG")
	if !filepath.IsAbs(path) {
		return c, fmt.Errorf("kagent: absolute MULTICA_KAGENT_CONFIG required")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 1<<20 {
		return c, fmt.Errorf("kagent: cannot read bounded daemon configuration")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF {
		return c, fmt.Errorf("kagent: invalid daemon configuration")
	}
	if !filepath.IsAbs(c.JournalDir) || !filepath.IsAbs(c.KubernetesTokenFile) || !filepath.IsAbs(c.A2ATokenFile) || c.Harness == "" || c.ModelConfig == "" || len(c.Agents) == 0 {
		return c, fmt.Errorf("kagent: persistent journal, credential files, approved harness/model and enrollments required")
	}
	if _, err := kagent.NewKubernetesStore(kagent.HTTPConfig{Endpoint: c.KubernetesEndpoint}, []string{c.Namespace}); err != nil {
		return c, err
	}
	if _, err := kagent.NewA2AClient(kagent.HTTPConfig{Endpoint: c.A2AEndpoint}); err != nil {
		return c, err
	}
	seen := map[string]bool{}
	for _, a := range c.Agents {
		if _, err := kagent.ResourceName(a.AgentID); err != nil {
			return c, err
		}
		if a.WorkspaceID == "" || seen[a.AgentID] {
			return c, fmt.Errorf("kagent: unique agent/workspace enrollments required")
		}
		seen[a.AgentID] = true
	}
	return c, nil
}

func kagentFileToken(path string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		raw, err := os.ReadFile(path)
		if err != nil || len(raw) > 64<<10 || strings.TrimSpace(string(raw)) == "" {
			return "", fmt.Errorf("kagent: credential file unavailable")
		}
		return strings.TrimSpace(string(raw)), nil
	}
}

func (c kagentDaemonConfig) backendConfig(task Task) (*agent.KagentConfig, error) {
	if !c.allows(task.AgentID, task.WorkspaceID) {
		return nil, fmt.Errorf("kagent: agent/workspace is not enrolled")
	}
	lifecycle, journal, err := c.lifecycle()
	if err != nil {
		return nil, err
	}
	client, err := kagent.NewA2AClient(kagent.HTTPConfig{Endpoint: c.A2AEndpoint, Token: kagentFileToken(c.A2ATokenFile)})
	if err != nil {
		return nil, err
	}
	// DispatchedAt identifies the server claim, and is stable for start retries.
	attempt, _ := json.Marshal([]any{task.ID, task.Attempt, task.DispatchedAt})
	return &agent.KagentConfig{Enabled: true, Lifecycle: lifecycle, Journal: journal, Client: client, AgentID: task.AgentID, WorkspaceID: task.WorkspaceID,
		ConversationID: "chat/" + task.ChatSessionID, AttemptID: string(attempt)}, nil
}

func (c kagentDaemonConfig) allows(agentID, workspaceID string) bool {
	for _, a := range c.Agents {
		if a.AgentID == agentID && a.WorkspaceID == workspaceID {
			return true
		}
	}
	return false
}

func (c kagentDaemonConfig) lifecycle() (*kagent.Lifecycle, *kagent.Journal, error) {
	journal, err := kagent.NewJournal(c.JournalDir)
	if err != nil {
		return nil, nil, err
	}
	store, err := kagent.NewKubernetesStore(kagent.HTTPConfig{Endpoint: c.KubernetesEndpoint, Token: kagentFileToken(c.KubernetesTokenFile)}, []string{c.Namespace})
	if err != nil {
		return nil, nil, err
	}
	reconciler, err := kagent.New(store, kagent.Config{AllowedNamespaces: []string{c.Namespace}})
	if err != nil {
		return nil, nil, err
	}
	lifecycle, err := kagent.NewLifecycle(reconciler, journal)
	return lifecycle, journal, err
}
