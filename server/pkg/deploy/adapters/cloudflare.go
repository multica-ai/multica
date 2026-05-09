package adapters

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/multica-ai/multica/server/pkg/deploy"
)

// cloudflareAdapter implements Cloudflare Pages webhook + REST. Pages
// uses a simpler signing scheme than Vercel: a shared secret is sent
// verbatim in the cf-webhook-auth header (no HMAC). We compare the
// header against the env's stored secret in constant time.
type cloudflareAdapter struct{}

type cloudflareConfig struct {
	AccountID   string `json:"account_id"`
	ProjectName string `json:"project_name"`
	APIToken    string `json:"api_token"`
}

// cloudflareWebhookPayload is the slice of the Pages "deployment_status"
// notification we care about. Pages sends a few different envelope
// shapes depending on which dashboard option the user picked; we cover
// both the canonical webhook builder and the older "notification"
// builder by reading the deployment fields from the union of the two.
type cloudflareWebhookPayload struct {
	// "deployment_status" or similar.
	Type string `json:"type"`
	Data struct {
		Project struct {
			Name string `json:"name"`
		} `json:"project"`
		Deployment struct {
			ID         string `json:"id"`
			ShortID    string `json:"short_id"`
			Project    string `json:"project_name"`
			Status     string `json:"latest_stage_status"` // "success"|"failure"|"active"|...
			DeployURL  string `json:"url"`
			ProdBranch string `json:"production_branch"`
			Created    string `json:"created_on"`
			Source     struct {
				Config struct {
					CommitHash string `json:"commit_hash"`
					Branch     string `json:"production_branch"`
				} `json:"config"`
			} `json:"source"`
		} `json:"deployment"`
	} `json:"data"`
	// Newer Pages dashboards send the deployment as a top-level object
	// without a `data.deployment` wrapper. We accept both shapes.
	Deployment *struct {
		ID         string `json:"id"`
		Project    string `json:"project_name"`
		Status     string `json:"latest_stage_status"`
		DeployURL  string `json:"url"`
		Created    string `json:"created_on"`
		ShortID    string `json:"short_id"`
		Source     struct {
			Config struct {
				CommitHash string `json:"commit_hash"`
				Branch     string `json:"production_branch"`
			} `json:"config"`
		} `json:"source"`
	} `json:"deployment,omitempty"`
}

func (a *cloudflareAdapter) Name() string         { return "cloudflare" }
func (a *cloudflareAdapter) SupportsPoll() bool   { return true }
func (a *cloudflareAdapter) SupportsRollback() bool { return true }

// VerifySignature: cf-webhook-auth is a plain shared secret. Constant-
// time compare to keep timing-side channels closed.
func (a *cloudflareAdapter) VerifySignature(env *deploy.Environment, headers http.Header, body []byte) error {
	if env.WebhookSecret == "" {
		return fmt.Errorf("%w: no webhook secret configured", deploy.ErrSignatureInvalid)
	}
	provided := headers.Get("cf-webhook-auth")
	if provided == "" {
		return fmt.Errorf("%w: missing cf-webhook-auth header", deploy.ErrSignatureInvalid)
	}
	if subtle.ConstantTimeCompare([]byte(provided), []byte(env.WebhookSecret)) != 1 {
		return deploy.ErrSignatureInvalid
	}
	_ = body // unused but kept for the interface; Cloudflare's signing isn't body-derived
	return nil
}

func (a *cloudflareAdapter) OnWebhook(ctx context.Context, env *deploy.Environment, raw json.RawMessage) (*deploy.DeployEvent, error) {
	cfg, err := decodeCloudflareConfig(env.Config)
	if err != nil {
		return nil, err
	}
	var payload cloudflareWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("cloudflare: parse payload: %w", err)
	}

	dep := payload.Data.Deployment
	if payload.Deployment != nil {
		dep.ID = payload.Deployment.ID
		dep.Project = payload.Deployment.Project
		dep.Status = payload.Deployment.Status
		dep.DeployURL = payload.Deployment.DeployURL
		dep.Source.Config.CommitHash = payload.Deployment.Source.Config.CommitHash
		dep.Source.Config.Branch = payload.Deployment.Source.Config.Branch
		dep.Created = payload.Deployment.Created
	}

	// Project-name match; if the env stored project_name, drop deliveries
	// for unrelated projects in the same Cloudflare account.
	if cfg.ProjectName != "" && dep.Project != "" && dep.Project != cfg.ProjectName {
		return nil, deploy.ErrIrrelevantPayload
	}

	status := cloudflareStatusFromString(dep.Status)
	if status == "" {
		return nil, deploy.ErrIrrelevantPayload
	}

	occurred := time.Now()
	if dep.Created != "" {
		if t, err := time.Parse(time.RFC3339, dep.Created); err == nil {
			occurred = t
		}
	}
	ref := dep.Source.Config.Branch
	if ref == "" {
		ref = env.TargetBranch
	}

	return &deploy.DeployEvent{
		Status:     status,
		SHA:        dep.Source.Config.CommitHash,
		Ref:        ref,
		LogURL:     dep.DeployURL,
		OccurredAt: occurred,
	}, nil
}

// cloudflareStatusFromString maps Pages' status strings to our enum.
// Pages distinguishes "queued"/"initializing"/"building"/"deploying"
// before settling into "success" or "failure"; we collapse the build
// stages into in_progress.
func cloudflareStatusFromString(s string) string {
	switch s {
	case "success":
		return "succeeded"
	case "failure", "failed":
		return "failed"
	case "queued", "initializing", "building", "deploying", "active":
		return "in_progress"
	case "canceled", "skipped":
		return "failed"
	default:
		return ""
	}
}

func (a *cloudflareAdapter) PollCurrent(ctx context.Context, env *deploy.Environment) (*deploy.DeployState, error) {
	cfg, err := decodeCloudflareConfig(env.Config)
	if err != nil {
		return nil, err
	}
	if cfg.AccountID == "" || cfg.ProjectName == "" || cfg.APIToken == "" {
		return nil, errors.New("cloudflare: account_id, project_name, and api_token required for poll")
	}
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/pages/projects/%s/deployments?per_page=1",
		cfg.AccountID, cfg.ProjectName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: build poll request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: poll request: %w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("cloudflare: poll status %d: %s", resp.StatusCode, string(body))
	}
	var listResp struct {
		Result []struct {
			ID        string `json:"id"`
			URL       string `json:"url"`
			CreatedOn string `json:"created_on"`
			Source    struct {
				Config struct {
					CommitHash string `json:"commit_hash"`
				} `json:"config"`
			} `json:"source"`
			LatestStage struct {
				Status string `json:"status"`
			} `json:"latest_stage"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, fmt.Errorf("cloudflare: parse poll response: %w", err)
	}
	if len(listResp.Result) == 0 {
		return nil, nil
	}
	d := listResp.Result[0]
	deployedAt := time.Now()
	if d.CreatedOn != "" {
		if t, err := time.Parse(time.RFC3339, d.CreatedOn); err == nil {
			deployedAt = t
		}
	}
	return &deploy.DeployState{
		CurrentSHA: d.Source.Config.CommitHash,
		DeployedAt: deployedAt,
		LogURL:     d.URL,
	}, nil
}

// Rollback retries a prior deployment by ID. We resolve the ID by
// scanning the deployments list for the matching commit_hash, then call
// the retry endpoint. Pages' retry on a prior deployment effectively
// promotes that artifact back to current.
func (a *cloudflareAdapter) Rollback(ctx context.Context, env *deploy.Environment, targetSHA string) error {
	cfg, err := decodeCloudflareConfig(env.Config)
	if err != nil {
		return err
	}
	if targetSHA == "" {
		return errors.New("cloudflare: target_sha required")
	}
	id, err := a.findDeploymentByCommitHash(ctx, cfg, targetSHA)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/pages/projects/%s/deployments/%s/retry",
		cfg.AccountID, cfg.ProjectName, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return fmt.Errorf("cloudflare: build rollback request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare: rollback request: %w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("cloudflare: rollback status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (a *cloudflareAdapter) findDeploymentByCommitHash(ctx context.Context, cfg cloudflareConfig, sha string) (string, error) {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/pages/projects/%s/deployments?per_page=50",
		cfg.AccountID, cfg.ProjectName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("cloudflare: build find request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("cloudflare: find request: %w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("cloudflare: find status %d: %s", resp.StatusCode, string(body))
	}
	var listResp struct {
		Result []struct {
			ID     string `json:"id"`
			Source struct {
				Config struct {
					CommitHash string `json:"commit_hash"`
				} `json:"config"`
			} `json:"source"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		return "", fmt.Errorf("cloudflare: parse find response: %w", err)
	}
	for _, d := range listResp.Result {
		if d.Source.Config.CommitHash == sha {
			return d.ID, nil
		}
	}
	return "", fmt.Errorf("cloudflare: no deployment found for sha %s", sha)
}

func decodeCloudflareConfig(raw json.RawMessage) (cloudflareConfig, error) {
	var cfg cloudflareConfig
	if len(raw) == 0 {
		return cfg, errors.New("cloudflare: missing adapter config")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("cloudflare: parse config: %w", err)
	}
	return cfg, nil
}

func init() {
	deploy.Register(&cloudflareAdapter{})
}
