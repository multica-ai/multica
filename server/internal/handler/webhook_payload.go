package handler

import (
	"encoding/json"
	"strings"
)

// WebhookEnvelope is the canonical shape stored in autopilot_run.trigger_payload
// and surfaced to the agent. The handler normalises arbitrary JSON bodies into
// this shape so downstream consumers (run_only daemon prompt, create_issue
// description appendix) can rely on a stable schema regardless of which
// provider sent the webhook.
type WebhookEnvelope struct {
	Event         string          `json:"event"`
	EventPayload  json.RawMessage `json:"eventPayload"`
	Request       WebhookRequest  `json:"request"`
	PayloadPolicy string          `json:"payloadPolicy,omitempty"`
	EventActions  []string        `json:"eventActions,omitempty"`
}

type WebhookRequest struct {
	ReceivedAt  string `json:"receivedAt"`
	ContentType string `json:"contentType,omitempty"`
}

func validWebhookPayloadPolicy(policy string) bool {
	return policy == "redacted" || policy == "metadata_only"
}

// prepareWebhookPayload runs after signature verification and before any write.
// Metadata-only deliveries retain event/action metadata for scope rechecks, but
// never retain the source body. The policy travels with the durable envelope so
// changing the trigger later cannot resurrect a discarded payload.
func prepareWebhookPayload(envelope WebhookEnvelope, token, policy string) (WebhookEnvelope, error) {
	if policy == "metadata_only" || envelope.PayloadPolicy == "metadata_only" {
		if envelope.PayloadPolicy != "metadata_only" {
			envelope.EventActions = webhookActionCandidates("", envelope.EventPayload)
		}
		envelope.EventPayload = json.RawMessage("null")
		envelope.PayloadPolicy = "metadata_only"
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return WebhookEnvelope{}, err
	}
	safe, err := redactWebhookJSON(encoded, token)
	if err != nil {
		return WebhookEnvelope{}, err
	}
	var result WebhookEnvelope
	err = json.Unmarshal(safe, &result)
	return result, err
}

// webhookActionCandidates extracts possible action values from the event
// action suffix and from well-known payload fields.
func webhookActionCandidates(eventAction string, payload json.RawMessage) []string {
	seen := map[string]struct{}{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		seen[v] = struct{}{}
	}
	add(eventAction)
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err == nil {
		for _, key := range []string{"action", "state", "conclusion", "status"} {
			if v, ok := obj[key].(string); ok {
				add(v)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	return out
}
