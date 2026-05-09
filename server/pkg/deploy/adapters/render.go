package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/deploy"
)

// renderAdapter implements Render.com's Deploy webhook + REST API.
// Render uses HMAC-SHA256 over the raw body, formatted as a hex string
// in the x-webhook-signature header (no algorithm prefix).
type renderAdapter struct{}

type renderConfig struct {
	ServiceID string `json:"service_id"`
	APIKey    string `json:"api_key"`
}

// renderWebhookPayload covers the Render deploy event shape. Render's
// body is small — a deploy_id + service_id + status — and we resolve
// the SHA from a follow-up REST call when it's not present in the
// payload.
type renderWebhookPayload struct {
	Type    string `json:"type"` // "deploy"
	Data    struct {
		ServiceID string `json:"serviceId"`
		DeployID  string `json:"deployId"`
		Status    string `json:"status"` // "live"|"build_failed"|"update_failed"|"canceled"|...
	} `json:"data"`
	Timestamp string `json:"timestamp"`
}

func (a *renderAdapter) Name() string         { return "render" }
func (a *renderAdapter) SupportsPoll() bool   { return true }
func (a *renderAdapter) SupportsRollback() bool { return false }

func (a *renderAdapter) VerifySignature(env *deploy.Environment, headers http.Header, body []byte) error {
	provided := headers.Get("x-webhook-signature")
	if provided == "" {
		return fmt.Errorf("%w: missing x-webhook-signature header", deploy.ErrSignatureInvalid)
	}
	if env.WebhookSecret == "" {
		return fmt.Errorf("%w: no webhook secret configured", deploy.ErrSignatureInvalid)
	}
	expected := hmacSHA256Hex(body, env.WebhookSecret)
	if !constantTimeEqualHex(expected, strings.TrimPrefix(provided, "sha256=")) {
		return deploy.ErrSignatureInvalid
	}
	return nil
}

func (a *renderAdapter) OnWebhook(ctx context.Context, env *deploy.Environment, raw json.RawMessage) (*deploy.DeployEvent, error) {
	cfg, err := decodeRenderConfig(env.Config)
	if err != nil {
		return nil, err
	}
	var payload renderWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("render: parse payload: %w", err)
	}
	if cfg.ServiceID != "" && payload.Data.ServiceID != "" &&
		cfg.ServiceID != payload.Data.ServiceID {
		return nil, deploy.ErrIrrelevantPayload
	}

	status := renderStatusFromString(payload.Data.Status)
	if status == "" {
		return nil, deploy.ErrIrrelevantPayload
	}
	occurred := time.Now()
	if payload.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, payload.Timestamp); err == nil {
			occurred = t
		}
	}

	// Render's webhook payload doesn't carry the commit SHA directly.
	// We fetch the deploy via the REST API to fill it in. Best-effort:
	// a missing SHA still produces a useful event (the receiver records
	// a row with empty SHA) — better than dropping the delivery.
	var sha string
	if payload.Data.DeployID != "" && cfg.APIKey != "" {
		sha, _ = a.fetchDeploySHA(ctx, cfg, payload.Data.DeployID)
	}

	return &deploy.DeployEvent{
		Status:     status,
		SHA:        sha,
		Ref:        env.TargetBranch,
		LogURL:     fmt.Sprintf("https://dashboard.render.com/web/%s/deploys/%s", cfg.ServiceID, payload.Data.DeployID),
		OccurredAt: occurred,
	}, nil
}

// renderStatusFromString maps Render's lifecycle to our enum.
func renderStatusFromString(s string) string {
	switch s {
	case "live":
		return "succeeded"
	case "build_failed", "update_failed":
		return "failed"
	case "canceled", "deactivated":
		return "failed"
	case "build_in_progress", "update_in_progress", "queued":
		return "in_progress"
	case "created":
		return "pending"
	default:
		return ""
	}
}

func (a *renderAdapter) fetchDeploySHA(ctx context.Context, cfg renderConfig, deployID string) (string, error) {
	url := fmt.Sprintf("https://api.render.com/v1/services/%s/deploys/%s", cfg.ServiceID, deployID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	body, err := readBody(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("render: fetch deploy status %d: %s", resp.StatusCode, string(body))
	}
	var detail struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &detail); err != nil {
		return "", err
	}
	return detail.Commit.ID, nil
}

func (a *renderAdapter) PollCurrent(ctx context.Context, env *deploy.Environment) (*deploy.DeployState, error) {
	cfg, err := decodeRenderConfig(env.Config)
	if err != nil {
		return nil, err
	}
	if cfg.ServiceID == "" || cfg.APIKey == "" {
		return nil, errors.New("render: service_id and api_key required for poll")
	}
	url := fmt.Sprintf("https://api.render.com/v1/services/%s/deploys?limit=1", cfg.ServiceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("render: build poll request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("render: poll request: %w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("render: poll status %d: %s", resp.StatusCode, string(body))
	}
	// Render's list endpoint returns an array of {deploy: {...}} objects
	// when called via the v1 listing, or a flat array when called via
	// some legacy endpoints. We accept either.
	var listResp []struct {
		Deploy struct {
			ID        string `json:"id"`
			Status    string `json:"status"`
			CreatedAt string `json:"createdAt"`
			Commit    struct {
				ID string `json:"id"`
			} `json:"commit"`
		} `json:"deploy"`
	}
	if err := json.Unmarshal(body, &listResp); err == nil && len(listResp) > 0 {
		d := listResp[0].Deploy
		deployedAt := time.Now()
		if d.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, d.CreatedAt); err == nil {
				deployedAt = t
			}
		}
		return &deploy.DeployState{
			CurrentSHA: d.Commit.ID,
			DeployedAt: deployedAt,
			LogURL:     fmt.Sprintf("https://dashboard.render.com/web/%s/deploys/%s", cfg.ServiceID, d.ID),
		}, nil
	}
	return nil, nil
}

func (a *renderAdapter) Rollback(ctx context.Context, env *deploy.Environment, targetSHA string) error {
	_ = ctx
	_ = env
	_ = targetSHA
	return deploy.ErrRollbackNotSupported
}

func decodeRenderConfig(raw json.RawMessage) (renderConfig, error) {
	var cfg renderConfig
	if len(raw) == 0 {
		return cfg, errors.New("render: missing adapter config")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("render: parse config: %w", err)
	}
	return cfg, nil
}

func init() {
	deploy.Register(&renderAdapter{})
}
