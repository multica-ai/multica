package daemon

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/multica-ai/multica/server/internal/chatgpt"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// Credentials stay on the daemon host. Only model names and ordinary task
// results cross the server boundary; the agent receives callbacks, not a store.
type codexChatGPTManager interface {
	AccessToken(context.Context, string) (chatgpt.Token, error)
	AssertActive(context.Context, chatgpt.Token) error
	Models(context.Context, string) ([]chatgpt.Model, error)
}

type codexLocalAuth struct {
	plan     *agent.CodexChatGPTPlan
	ownerDir string
	manager  codexChatGPTManager
	clientID string
}

// Read the profile on each request: signing out must not switch a running daemon
// back to vendor auth, and an explicit account selection must take effect without
// sharing the old registration's thread or cached model catalog.
func (d *Daemon) codexAuthForProvider(provider string, customCommand bool) (codexLocalAuth, error) {
	if provider != "codex" || customCommand {
		return codexLocalAuth{}, nil
	}
	cfg, err := cli.LoadCLIConfigForProfile(d.cfg.Profile)
	if err != nil {
		return codexLocalAuth{}, fmt.Errorf("read local Codex authentication config: %w", err)
	}
	store, err := cli.ChatGPTStorePathForProfile(d.cfg.Profile)
	if err != nil {
		return codexLocalAuth{}, err
	}
	ownerDir := filepath.Join(filepath.Dir(store), "thread-owners")
	if cfg.Backends == nil || cfg.Backends.Codex == nil || cfg.Backends.Codex.AuthMode == "" {
		return codexLocalAuth{ownerDir: ownerDir}, nil
	}
	if cfg.Backends.Codex.AuthMode != cli.CodexAuthModeChatGPTPlan {
		return codexLocalAuth{}, fmt.Errorf("unsupported local Codex auth mode %q", cfg.Backends.Codex.AuthMode)
	}
	if cfg.Backends.Codex.ChatGPTClientID == "" {
		return codexLocalAuth{}, fmt.Errorf("ChatGPT plan has no selected registration; run multica chatgpt login")
	}
	auth := newCodexChatGPTAuth(chatgpt.NewManager(store), cfg.Backends.Codex.ChatGPTClientID, ownerDir)
	assertActive := auth.plan.AssertActive
	auth.plan.AssertActive = func(ctx context.Context, token agent.CodexChatGPTToken) error {
		current, err := cli.LoadCLIConfigForProfile(d.cfg.Profile)
		if err != nil {
			return fmt.Errorf("read local Codex authentication config: %w", err)
		}
		if current.Backends == nil || current.Backends.Codex == nil || current.Backends.Codex.AuthMode != cli.CodexAuthModeChatGPTPlan || current.Backends.Codex.ChatGPTClientID != token.ClientID {
			return fmt.Errorf("Codex authentication selection changed; start a new run with the selected account")
		}
		return assertActive(ctx, token)
	}
	return auth, nil
}

func newCodexChatGPTAuth(manager codexChatGPTManager, clientID, ownerDir string) codexLocalAuth {
	return codexLocalAuth{ownerDir: ownerDir, manager: manager, clientID: clientID, plan: &agent.CodexChatGPTPlan{
		AccessToken: func(ctx context.Context) (agent.CodexChatGPTToken, error) {
			token, err := manager.AccessToken(ctx, clientID)
			if err != nil {
				return agent.CodexChatGPTToken{}, err
			}
			return agent.CodexChatGPTToken{AccessToken: token.AccessToken, ClientID: token.ClientID, Subject: token.Subject, Scopes: token.Scopes}, nil
		},
		AssertActive: func(ctx context.Context, token agent.CodexChatGPTToken) error {
			return manager.AssertActive(ctx, chatgpt.Token{AccessToken: token.AccessToken, ClientID: token.ClientID, Subject: token.Subject, Scopes: token.Scopes})
		},
	}}
}

func (auth codexLocalAuth) catalog(ctx context.Context) (agent.Catalog, error) {
	models, err := auth.manager.Models(ctx, auth.clientID)
	if err != nil {
		return agent.Catalog{}, err
	}
	catalog := agent.Catalog{Models: make([]agent.Model, 0, len(models))}
	for i, model := range models {
		catalog.Models = append(catalog.Models, agent.Model{ID: model.Slug, Label: model.DisplayName, Provider: "codex", Default: i == 0})
	}
	return catalog, nil
}

func (auth codexLocalAuth) selectModel(ctx context.Context, selection taskModelSelection) (taskModelSelection, error) {
	catalog, err := auth.catalog(ctx)
	if err != nil {
		return selection, err
	}
	if len(catalog.Models) == 0 {
		return selection, fmt.Errorf("ChatGPT plan has no available models; check the selected account's plan access")
	}
	if selection.Model == "" {
		selection.Model = catalog.Models[0].ID
	}
	for _, model := range catalog.Models {
		if model.ID == selection.Model {
			// The public catalog has no reasoning or tier metadata. Preserve
			// explicit choices; do not apply bundled Codex capability guesses.
			return selection, nil
		}
	}
	return selection, fmt.Errorf("model %q is not available in the selected ChatGPT account; refresh the model list and choose an available model", selection.Model)
}
