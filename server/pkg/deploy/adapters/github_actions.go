// github_actions.go — registers GitHub Actions as the default adapter
// kind. The actual webhook receiver for GitHub events lives at
// /api/integrations/github/webhook (handler.HandleGitHubWebhook) and
// covers far more than deploys (PR events, reviews, checks, push). We
// don't reroute that flow through the deploy.Adapter interface.
//
// This adapter therefore exists primarily so:
//   1. Get("github_actions") returns a non-nil adapter — keeps the
//      registry total over the canonical adapter set.
//   2. The /api/deploy/adapters listing renders "github_actions" as a
//      first-class option in the env-config dropdown.
//
// All Adapter methods return ErrPollNotSupported / ErrRollbackNotSupported
// or a no-op success — the GitHub Actions deploy lifecycle is driven
// from the workspace-level webhook flow, not from the per-env deploy
// adapter receiver.
package adapters

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica/server/pkg/deploy"
)

type githubActionsAdapter struct{}

func (a *githubActionsAdapter) Name() string         { return "github_actions" }
func (a *githubActionsAdapter) SupportsPoll() bool   { return false }
func (a *githubActionsAdapter) SupportsRollback() bool { return false }

// VerifySignature returns nil — the GitHub Actions adapter never
// receives webhooks via the multi-adapter receiver. If the deploy
// adapter route is hit for an env with this kind, the receiver checks
// the kind first and short-circuits with a 400 explaining the user
// should configure their GitHub webhook at the workspace level instead.
func (a *githubActionsAdapter) VerifySignature(env *deploy.Environment, headers http.Header, body []byte) error {
	_ = env
	_ = headers
	_ = body
	return deploy.ErrSignatureInvalid
}

func (a *githubActionsAdapter) OnWebhook(ctx context.Context, env *deploy.Environment, raw json.RawMessage) (*deploy.DeployEvent, error) {
	_ = ctx
	_ = env
	_ = raw
	// Should never reach here in practice — the receiver checks kind
	// before dispatching. Returning ErrIrrelevantPayload keeps the
	// outcome harmless if it ever does.
	return nil, deploy.ErrIrrelevantPayload
}

func (a *githubActionsAdapter) PollCurrent(ctx context.Context, env *deploy.Environment) (*deploy.DeployState, error) {
	_ = ctx
	_ = env
	return nil, deploy.ErrPollNotSupported
}

func (a *githubActionsAdapter) Rollback(ctx context.Context, env *deploy.Environment, targetSHA string) error {
	_ = ctx
	_ = env
	_ = targetSHA
	return deploy.ErrRollbackNotSupported
}

func init() {
	deploy.Register(&githubActionsAdapter{})
}
