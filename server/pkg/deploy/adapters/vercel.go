package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/deploy"
)

// vercelAdapter implements the Vercel webhook + REST API contract. The
// adapter listens on `deployment.created/succeeded/error/canceled`,
// verifies the request via the workspace's stored secret using
// HMAC-SHA1 (Vercel's choice, not ours — see x-vercel-signature header),
// and supports both polling current deploy state and promoting a prior
// deployment for rollback.
type vercelAdapter struct{}

// vercelConfig is the schema persisted (encrypted) in
// deploy_adapter_config.config_encrypted for this adapter.
type vercelConfig struct {
	TeamID    string `json:"team_id"`
	ProjectID string `json:"project_id"`
	Token     string `json:"token"`
}

// vercelWebhookPayload covers the fields we care about across every
// deployment.* event Vercel emits. Vercel keeps the wire shape stable
// across these events; the per-event difference is in `type` plus
// whether `payload.deployment.error` is populated.
type vercelWebhookPayload struct {
	Type    string `json:"type"`
	Payload struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
		Deployment struct {
			ID  string `json:"id"`
			URL string `json:"url"`
			Meta struct {
				GitHubCommitSHA string `json:"githubCommitSha"`
				GitHubCommitRef string `json:"githubCommitRef"`
				GitlabCommitSHA string `json:"gitlabCommitSha"`
				GitlabCommitRef string `json:"gitlabCommitRef"`
				BitbucketSHA    string `json:"bitbucketCommitSha"`
				BitbucketRef    string `json:"bitbucketCommitRef"`
			} `json:"meta"`
		} `json:"deployment"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"payload"`
	CreatedAt int64 `json:"createdAt"`
}

func (a *vercelAdapter) Name() string         { return "vercel" }
func (a *vercelAdapter) SupportsPoll() bool   { return true }
func (a *vercelAdapter) SupportsRollback() bool { return true }

// VerifySignature: x-vercel-signature is "<hex>" where hex is the
// HMAC-SHA1 of the raw body using the shared secret. Wrapping any
// failure in deploy.ErrSignatureInvalid keeps the receiver's error map
// uniform across adapters.
func (a *vercelAdapter) VerifySignature(env *deploy.Environment, headers http.Header, body []byte) error {
	provided := headers.Get("x-vercel-signature")
	if provided == "" {
		return fmt.Errorf("%w: missing x-vercel-signature header", deploy.ErrSignatureInvalid)
	}
	if env.WebhookSecret == "" {
		return fmt.Errorf("%w: no webhook secret configured", deploy.ErrSignatureInvalid)
	}
	expected := hmacSHA1Hex(body, env.WebhookSecret)
	if !constantTimeEqualHex(expected, provided) {
		return deploy.ErrSignatureInvalid
	}
	return nil
}

// OnWebhook maps a deployment.* event to a deploy.DeployEvent. Returns
// (nil, deploy.ErrIrrelevantPayload) when the event's project doesn't
// match the env's stored config — Vercel scopes webhooks at the team
// level, so the same delivery hits every project under that team.
func (a *vercelAdapter) OnWebhook(ctx context.Context, env *deploy.Environment, raw json.RawMessage) (*deploy.DeployEvent, error) {
	cfg, err := decodeVercelConfig(env.Config)
	if err != nil {
		return nil, err
	}
	var payload vercelWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("vercel: parse payload: %w", err)
	}
	if cfg.ProjectID != "" && payload.Payload.Project.ID != "" &&
		cfg.ProjectID != payload.Payload.Project.ID {
		return nil, deploy.ErrIrrelevantPayload
	}

	status := vercelStatusFromType(payload.Type)
	if status == "" {
		// Unknown event type — treat as ignored rather than error so
		// Vercel adding a new event in the future doesn't 500 the receiver.
		return nil, deploy.ErrIrrelevantPayload
	}

	sha := firstNonEmpty(
		payload.Payload.Deployment.Meta.GitHubCommitSHA,
		payload.Payload.Deployment.Meta.GitlabCommitSHA,
		payload.Payload.Deployment.Meta.BitbucketSHA,
	)
	ref := firstNonEmpty(
		payload.Payload.Deployment.Meta.GitHubCommitRef,
		payload.Payload.Deployment.Meta.GitlabCommitRef,
		payload.Payload.Deployment.Meta.BitbucketRef,
		env.TargetBranch,
	)

	occurred := time.Unix(0, payload.CreatedAt*int64(time.Millisecond))
	if payload.CreatedAt == 0 {
		occurred = time.Now()
	}

	logURL := ""
	if payload.Payload.Deployment.URL != "" {
		// Vercel's deployment.url is hostname-only; render it as https.
		logURL = "https://" + payload.Payload.Deployment.URL
	}

	return &deploy.DeployEvent{
		Status:     status,
		SHA:        sha,
		Ref:        ref,
		LogURL:     logURL,
		ErrorMsg:   payload.Payload.Error.Message,
		OccurredAt: occurred,
	}, nil
}

// vercelStatusFromType collapses Vercel's event names into our enum.
func vercelStatusFromType(t string) string {
	switch t {
	case "deployment.created":
		return "pending"
	case "deployment-state-change":
		// State is in payload.deployment but isn't broken out here; the
		// generic "changed" event falls through to ignored. Specific
		// state events below handle it.
		return ""
	case "deployment.succeeded", "deployment-ready":
		return "succeeded"
	case "deployment.error", "deployment-error":
		return "failed"
	case "deployment.canceled", "deployment-canceled":
		// Cancelations are recorded as failed so the swimlane doesn't
		// claim the canceled deploy is the current SHA. Could carry a
		// dedicated status later if the UI ever differentiates.
		return "failed"
	default:
		return ""
	}
}

// PollCurrent hits the deployments listing endpoint and returns the
// latest READY deployment. Rate-limited at the http.Client.Timeout
// boundary; if Vercel is slow we fail closed and let the next tick try
// again.
func (a *vercelAdapter) PollCurrent(ctx context.Context, env *deploy.Environment) (*deploy.DeployState, error) {
	cfg, err := decodeVercelConfig(env.Config)
	if err != nil {
		return nil, err
	}
	if cfg.ProjectID == "" || cfg.Token == "" {
		return nil, errors.New("vercel: project_id and token required for poll")
	}
	url := fmt.Sprintf("https://api.vercel.com/v6/deployments?projectId=%s&state=READY&limit=1", cfg.ProjectID)
	if cfg.TeamID != "" {
		url += "&teamId=" + cfg.TeamID
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("vercel: build poll request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vercel: poll request: %w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("vercel: poll status %d: %s", resp.StatusCode, string(body))
	}
	var listResp struct {
		Deployments []struct {
			UID     string `json:"uid"`
			URL     string `json:"url"`
			Created int64  `json:"created"`
			Meta    struct {
				GitHubCommitSHA string `json:"githubCommitSha"`
			} `json:"meta"`
		} `json:"deployments"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, fmt.Errorf("vercel: parse poll response: %w", err)
	}
	if len(listResp.Deployments) == 0 {
		return nil, nil // not an error — just no deploys yet
	}
	d := listResp.Deployments[0]
	return &deploy.DeployState{
		CurrentSHA: d.Meta.GitHubCommitSHA,
		DeployedAt: time.Unix(0, d.Created*int64(time.Millisecond)),
		LogURL:     "https://" + d.URL,
	}, nil
}

// Rollback promotes a target deployment via the v13 promote endpoint.
// We first locate the deployment whose meta.githubCommitSha matches the
// requested target SHA, then issue the POST. Two round-trips because
// Vercel's promote takes a deployment UID, not a commit hash.
func (a *vercelAdapter) Rollback(ctx context.Context, env *deploy.Environment, targetSHA string) error {
	cfg, err := decodeVercelConfig(env.Config)
	if err != nil {
		return err
	}
	if targetSHA == "" {
		return errors.New("vercel: target_sha required")
	}
	uid, err := a.findDeploymentBySHA(ctx, cfg, targetSHA)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://api.vercel.com/v13/deployments/%s/promote", uid)
	if cfg.TeamID != "" {
		url += "?teamId=" + cfg.TeamID
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return fmt.Errorf("vercel: build rollback request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("vercel: rollback request: %w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("vercel: rollback status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// findDeploymentBySHA scans the recent READY deployments list for one
// whose meta.githubCommitSha matches. We cap at the first 50 because
// "rollback to something from 6 months ago" isn't a realistic use case
// and unbounded paging would be a rate-limit foot-gun.
func (a *vercelAdapter) findDeploymentBySHA(ctx context.Context, cfg vercelConfig, sha string) (string, error) {
	url := fmt.Sprintf("https://api.vercel.com/v6/deployments?projectId=%s&state=READY&limit=50", cfg.ProjectID)
	if cfg.TeamID != "" {
		url += "&teamId=" + cfg.TeamID
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("vercel: build find request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vercel: find request: %w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("vercel: find status %d: %s", resp.StatusCode, string(body))
	}
	var listResp struct {
		Deployments []struct {
			UID  string `json:"uid"`
			Meta struct {
				GitHubCommitSHA string `json:"githubCommitSha"`
			} `json:"meta"`
		} `json:"deployments"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		return "", fmt.Errorf("vercel: parse find response: %w", err)
	}
	for _, d := range listResp.Deployments {
		if strings.EqualFold(d.Meta.GitHubCommitSHA, sha) {
			return d.UID, nil
		}
	}
	return "", fmt.Errorf("vercel: no deployment found for sha %s", sha)
}

func decodeVercelConfig(raw json.RawMessage) (vercelConfig, error) {
	var cfg vercelConfig
	if len(raw) == 0 {
		return cfg, errors.New("vercel: missing adapter config")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("vercel: parse config: %w", err)
	}
	return cfg, nil
}

// firstNonEmpty returns the first non-empty string. Used for SHA / ref
// fallback chains where Vercel's payload may carry the value under any
// of several keys depending on the source repo type.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func init() {
	deploy.Register(&vercelAdapter{})
}
