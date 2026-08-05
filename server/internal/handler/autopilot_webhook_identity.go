package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	scopeClaimHeader    = "X-Multica-Scope-Claim"
	maxScopeClaimLength = 512
)

// WebhookEventIdentity is the agent-safe identity envelope persisted on
// autopilot_run.trigger_payload. It intentionally omits untrusted text such as
// issue/PR comment bodies.
type WebhookEventIdentity struct {
	DeliveryID  string `json:"deliveryId,omitempty"`
	Repository  string `json:"repository,omitempty"`
	PrURL       string `json:"prUrl,omitempty"`
	PrNumber    int    `json:"prNumber,omitempty"`
	HeadSHA     string `json:"headSha,omitempty"`
	SenderLogin string `json:"senderLogin,omitempty"`
	SenderType  string `json:"senderType,omitempty"`
	AppSlug     string `json:"appSlug,omitempty"`
}

// StoredWebhookEnvelope is the canonical trigger_payload shape for webhook runs.
// Raw provider payloads remain on webhook_delivery.raw_body for replay only.
type StoredWebhookEnvelope struct {
	Event      string               `json:"event"`
	Identity   WebhookEventIdentity `json:"identity"`
	Request    WebhookRequest       `json:"request"`
	ScopeClaim string               `json:"scopeClaim,omitempty"`
}

func extractScopeClaim(headers http.Header) (string, error) {
	claim := strings.TrimSpace(headers.Get(scopeClaimHeader))
	if claim == "" {
		return "", nil
	}
	if len(claim) > maxScopeClaimLength {
		return "", fmt.Errorf("scope claim exceeds %d bytes", maxScopeClaimLength)
	}
	return claim, nil
}

func buildStoredWebhookEnvelope(
	provider string,
	env WebhookEnvelope,
	scopeClaim, dedupeKey string,
	headers http.Header,
	rawBody []byte,
) StoredWebhookEnvelope {
	identity := extractWebhookIdentity(provider, headers, dedupeKey, env.EventPayload)
	if identity.DeliveryID == "" && dedupeKey != "" {
		identity.DeliveryID = dedupeKey
	}
	return StoredWebhookEnvelope{
		Event:      env.Event,
		Identity:   identity,
		Request:    env.Request,
		ScopeClaim: scopeClaim,
	}
}

func extractWebhookIdentity(provider string, headers http.Header, dedupeKey string, payload json.RawMessage) WebhookEventIdentity {
	id := WebhookEventIdentity{}
	if dedupeKey != "" {
		id.DeliveryID = dedupeKey
	}
	if v := strings.TrimSpace(headers.Get("X-GitHub-Delivery")); v != "" {
		id.DeliveryID = v
	}
	if provider == "github" || headers.Get("X-GitHub-Event") != "" {
		applyGitHubIdentity(&id, payload)
	}
	return id
}

func applyGitHubIdentity(id *WebhookEventIdentity, payload json.RawMessage) {
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		return
	}
	if repo, ok := obj["repository"].(map[string]any); ok {
		if full, ok := repo["full_name"].(string); ok {
			id.Repository = full
		}
	}
	if sender, ok := obj["sender"].(map[string]any); ok {
		if login, ok := sender["login"].(string); ok {
			id.SenderLogin = login
		}
		if typ, ok := sender["type"].(string); ok {
			id.SenderType = typ
		}
	}
	if installation, ok := obj["installation"].(map[string]any); ok {
		if app, ok := installation["app_slug"].(string); ok {
			id.AppSlug = app
		}
	}
	if pr, ok := obj["pull_request"].(map[string]any); ok {
		applyGitHubPullRequestIdentity(id, pr)
	} else if issue, ok := obj["issue"].(map[string]any); ok {
		applyGitHubPullRequestIdentity(id, issue["pull_request"])
	}
}

func applyGitHubPullRequestIdentity(id *WebhookEventIdentity, prAny any) {
	pr, ok := prAny.(map[string]any)
	if !ok {
		return
	}
	if url, ok := pr["html_url"].(string); ok && url != "" {
		id.PrURL = url
	}
	if num, ok := pr["number"].(float64); ok {
		id.PrNumber = int(num)
	}
	if head, ok := pr["head"].(map[string]any); ok {
		if sha, ok := head["sha"].(string); ok {
			id.HeadSHA = sha
		}
	}
}

func mergeStoredEnvelopeHeadSHA(existingPayload, newPayload []byte) ([]byte, bool, error) {
	var oldEnv, newEnv StoredWebhookEnvelope
	if err := json.Unmarshal(existingPayload, &oldEnv); err != nil {
		return nil, false, fmt.Errorf("decode existing envelope: %w", err)
	}
	if err := json.Unmarshal(newPayload, &newEnv); err != nil {
		return nil, false, fmt.Errorf("decode new envelope: %w", err)
	}
	changed := false
	if newEnv.Identity.HeadSHA != "" && newEnv.Identity.HeadSHA != oldEnv.Identity.HeadSHA {
		oldEnv.Identity.HeadSHA = newEnv.Identity.HeadSHA
		changed = true
	}
	if newEnv.Event != "" && newEnv.Event != oldEnv.Event {
		oldEnv.Event = newEnv.Event
		changed = true
	}
	if !changed {
		return nil, false, nil
	}
	out, err := json.Marshal(oldEnv)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}
