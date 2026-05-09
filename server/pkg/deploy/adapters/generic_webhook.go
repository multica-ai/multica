package adapters

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/deploy"
	"github.com/tidwall/gjson"
)

// genericWebhookAdapter is the escape hatch for providers we don't have
// a first-party adapter for. The workspace owner configures a JSON
// path-mapping table that tells the adapter where to find the SHA, ref,
// status, etc. inside the provider's webhook body.
//
// Status mapping is shallow on purpose — we don't try to translate the
// provider's status string into our enum if the user provides one;
// callers can always pre-normalize their webhook payload (CI scripts
// often do this anyway) or use a downstream "post hook" middleware.
type genericWebhookAdapter struct{}

// genericConfig is the shape persisted in deploy_adapter_config.config_encrypted.
// Every path uses gjson syntax (dotted paths with array indices).
type genericConfig struct {
	// gjson paths to the relevant fields in the inbound payload.
	StatusPath string `json:"status_path"`
	SHAPath    string `json:"sha_path"`
	RefPath    string `json:"ref_path,omitempty"`
	LogURLPath string `json:"log_url_path,omitempty"`

	// Optional: a path whose extracted value must equal a configured
	// expected value, otherwise the payload is treated as irrelevant.
	// Used to scope a workspace-wide webhook URL down to a single
	// project / app / pipeline.
	IdentityPath  string `json:"identity_path,omitempty"`
	IdentityValue string `json:"identity_value,omitempty"`

	// Signature verification config. Algo is "hmac-sha256" or
	// "hmac-sha1"; SignatureHeader is the header that carries the hex
	// signature; SecretField — when set — names a config-side field on
	// THIS struct that holds the secret. We don't actually use it
	// because the secret comes from env.WebhookSecret, but it's part of
	// the published config schema for forward compatibility.
	SignatureHeader string `json:"signature_header,omitempty"`
	SignatureAlgo   string `json:"signature_algo,omitempty"` // "hmac-sha256" | "hmac-sha1" | "shared-secret"

	// StatusMap (optional) translates the provider's raw status string
	// to our enum. Without it, the raw value flows through as-is and
	// the receiver's normalization layer rejects unknown values. With
	// it, e.g. {"PASSED":"succeeded","FAILED":"failed"} lets a CI
	// system that doesn't speak our enum integrate cleanly.
	StatusMap map[string]string `json:"status_map,omitempty"`
}

func (a *genericWebhookAdapter) Name() string         { return "generic_webhook" }
func (a *genericWebhookAdapter) SupportsPoll() bool   { return false }
func (a *genericWebhookAdapter) SupportsRollback() bool { return false }

func (a *genericWebhookAdapter) VerifySignature(env *deploy.Environment, headers http.Header, body []byte) error {
	cfg, err := decodeGenericConfig(env.Config)
	if err != nil {
		return err
	}
	if cfg.SignatureHeader == "" {
		// Adapters that don't configure signing are accepted as long as
		// they have a webhook secret stored — we still require *some*
		// shared secret to land in the request, otherwise an open URL
		// would let anyone post deploys. We default to comparing the
		// secret against the X-Webhook-Secret header as a sane fallback.
		cfg.SignatureHeader = "X-Webhook-Secret"
		cfg.SignatureAlgo = "shared-secret"
	}
	provided := headers.Get(cfg.SignatureHeader)
	if provided == "" {
		return fmt.Errorf("%w: missing %s header", deploy.ErrSignatureInvalid, cfg.SignatureHeader)
	}
	if env.WebhookSecret == "" {
		return fmt.Errorf("%w: no webhook secret configured", deploy.ErrSignatureInvalid)
	}
	switch strings.ToLower(cfg.SignatureAlgo) {
	case "", "shared-secret":
		if subtle.ConstantTimeCompare([]byte(provided), []byte(env.WebhookSecret)) != 1 {
			return deploy.ErrSignatureInvalid
		}
	case "hmac-sha256":
		expected := hmacSHA256Hex(body, env.WebhookSecret)
		if !constantTimeEqualHex(expected, strings.TrimPrefix(provided, "sha256=")) {
			return deploy.ErrSignatureInvalid
		}
	case "hmac-sha1":
		expected := hmacSHA1Hex(body, env.WebhookSecret)
		if !constantTimeEqualHex(expected, strings.TrimPrefix(provided, "sha1=")) {
			return deploy.ErrSignatureInvalid
		}
	default:
		return fmt.Errorf("%w: unsupported algo %q", deploy.ErrSignatureInvalid, cfg.SignatureAlgo)
	}
	return nil
}

// OnWebhook walks the gjson paths and assembles a DeployEvent.
func (a *genericWebhookAdapter) OnWebhook(ctx context.Context, env *deploy.Environment, raw json.RawMessage) (*deploy.DeployEvent, error) {
	_ = ctx
	cfg, err := decodeGenericConfig(env.Config)
	if err != nil {
		return nil, err
	}
	if !gjson.ValidBytes(raw) {
		return nil, errors.New("generic_webhook: payload is not valid JSON")
	}

	if cfg.IdentityPath != "" {
		got := gjson.GetBytes(raw, cfg.IdentityPath).String()
		if got != cfg.IdentityValue {
			return nil, deploy.ErrIrrelevantPayload
		}
	}

	if cfg.StatusPath == "" || cfg.SHAPath == "" {
		return nil, errors.New("generic_webhook: status_path and sha_path are required")
	}

	rawStatus := gjson.GetBytes(raw, cfg.StatusPath).String()
	if rawStatus == "" {
		return nil, deploy.ErrIrrelevantPayload
	}
	status := rawStatus
	if mapped, ok := cfg.StatusMap[rawStatus]; ok {
		status = mapped
	}

	sha := gjson.GetBytes(raw, cfg.SHAPath).String()
	ref := ""
	if cfg.RefPath != "" {
		ref = gjson.GetBytes(raw, cfg.RefPath).String()
	}
	if ref == "" {
		ref = env.TargetBranch
	}
	logURL := ""
	if cfg.LogURLPath != "" {
		logURL = gjson.GetBytes(raw, cfg.LogURLPath).String()
	}

	return &deploy.DeployEvent{
		Status:     status,
		SHA:        sha,
		Ref:        ref,
		LogURL:     logURL,
		OccurredAt: time.Now(),
	}, nil
}

func (a *genericWebhookAdapter) PollCurrent(ctx context.Context, env *deploy.Environment) (*deploy.DeployState, error) {
	_ = ctx
	_ = env
	return nil, deploy.ErrPollNotSupported
}

func (a *genericWebhookAdapter) Rollback(ctx context.Context, env *deploy.Environment, targetSHA string) error {
	_ = ctx
	_ = env
	_ = targetSHA
	return deploy.ErrRollbackNotSupported
}

func decodeGenericConfig(raw json.RawMessage) (genericConfig, error) {
	var cfg genericConfig
	if len(raw) == 0 {
		return cfg, errors.New("generic_webhook: missing adapter config")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("generic_webhook: parse config: %w", err)
	}
	return cfg, nil
}

func init() {
	deploy.Register(&genericWebhookAdapter{})
}
