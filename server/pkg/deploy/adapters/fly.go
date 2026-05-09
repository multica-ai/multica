// fly.go — Fly.io adapter.
//
// IMPORTANT: Fly.io does NOT expose a webhook for app deployments as of
// this writing. The adapter therefore only supports polling. Inbound
// webhook calls return a signature error so the receiver maps to 401
// rather than silently consuming the request.
//
// Rollback support is also out of scope for this phase — Fly's CLI
// (flyctl) is the standard rollback path, and shelling to flyctl from
// the server is a footgun we'd rather defer until there's a clean
// stable HTTP surface.
package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/multica-ai/multica/server/pkg/deploy"
)

type flyAdapter struct{}

type flyConfig struct {
	AppName  string `json:"app_name"`
	APIToken string `json:"api_token"`
}

func (a *flyAdapter) Name() string         { return "fly" }
func (a *flyAdapter) SupportsPoll() bool   { return true }
func (a *flyAdapter) SupportsRollback() bool { return false }

// VerifySignature: not supported by Fly. Return ErrSignatureInvalid so
// any inbound webhook fails the gate. The receiver writes a 401, which
// is the right outcome for a sender that shouldn't be sending us
// webhooks in the first place.
func (a *flyAdapter) VerifySignature(env *deploy.Environment, headers http.Header, body []byte) error {
	_ = env
	_ = headers
	_ = body
	return fmt.Errorf("%w: fly does not support webhooks", deploy.ErrSignatureInvalid)
}

func (a *flyAdapter) OnWebhook(ctx context.Context, env *deploy.Environment, raw json.RawMessage) (*deploy.DeployEvent, error) {
	_ = ctx
	_ = env
	_ = raw
	return nil, errors.New("fly: webhooks are not supported")
}

// PollCurrent reads the machines list and finds the most recent
// deployment status. Fly's REST API is straightforward; we look at
// every machine's `latest_deployment_status` and bubble up the latest
// SHA.
//
// The API returns machines in a consistent order; sorting by
// updated_at handles the case where multiple machines were redeployed
// in different generations.
func (a *flyAdapter) PollCurrent(ctx context.Context, env *deploy.Environment) (*deploy.DeployState, error) {
	cfg, err := decodeFlyConfig(env.Config)
	if err != nil {
		return nil, err
	}
	if cfg.AppName == "" || cfg.APIToken == "" {
		return nil, errors.New("fly: app_name and api_token required for poll")
	}
	url := fmt.Sprintf("https://api.machines.dev/v1/apps/%s/machines", cfg.AppName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fly: build poll request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fly: poll request: %w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("fly: poll status %d: %s", resp.StatusCode, string(body))
	}
	var machines []struct {
		ID        string `json:"id"`
		UpdatedAt string `json:"updated_at"`
		Config    struct {
			Image string `json:"image"`
			Env   struct {
				CommitSHA string `json:"COMMIT_SHA"`
			} `json:"env"`
		} `json:"config"`
		// Fly also surfaces deployment metadata under "image_ref" and
		// "events" — we deliberately read the env var because that's
		// the conventional way Multica/CI workflows pass the SHA.
	}
	if err := json.Unmarshal(body, &machines); err != nil {
		return nil, fmt.Errorf("fly: parse poll response: %w", err)
	}
	if len(machines) == 0 {
		return nil, nil
	}
	// Pick the most-recently-updated machine as the canonical signal.
	sort.Slice(machines, func(i, j int) bool {
		return machines[i].UpdatedAt > machines[j].UpdatedAt
	})
	m := machines[0]
	deployedAt := time.Now()
	if m.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, m.UpdatedAt); err == nil {
			deployedAt = t
		}
	}
	return &deploy.DeployState{
		CurrentSHA: m.Config.Env.CommitSHA,
		DeployedAt: deployedAt,
		LogURL:     fmt.Sprintf("https://fly.io/apps/%s", cfg.AppName),
	}, nil
}

func (a *flyAdapter) Rollback(ctx context.Context, env *deploy.Environment, targetSHA string) error {
	_ = ctx
	_ = env
	_ = targetSHA
	return deploy.ErrRollbackNotSupported
}

func decodeFlyConfig(raw json.RawMessage) (flyConfig, error) {
	var cfg flyConfig
	if len(raw) == 0 {
		return cfg, errors.New("fly: missing adapter config")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("fly: parse config: %w", err)
	}
	return cfg, nil
}

func init() {
	deploy.Register(&flyAdapter{})
}
