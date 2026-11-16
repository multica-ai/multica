package handler

import (
	"encoding/json"
	"strings"
)

// WebhookPayloadConditionFailure is a snapshot of the admission decision,
// independent of subsequent trigger edits. ActualValues are JSON previews.
type WebhookPayloadConditionFailure struct {
	FilterIndex    int                     `json:"filter_index"`
	ConditionIndex int                     `json:"condition_index"`
	Condition      WebhookPayloadCondition `json:"condition"`
	ActualValues   []string                `json:"actual_values"`
	Missing        bool                    `json:"missing"`
	Truncated      bool                    `json:"truncated"`
}

type webhookFilterDecision struct {
	Allowed    bool
	ReasonCode string
	Failure    *WebhookPayloadConditionFailure
}

func newWebhookPayloadConditionFailure(c WebhookPayloadCondition, payload any, filterIndex, conditionIndex int) *WebhookPayloadConditionFailure {
	values, found := resolveWebhookPayloadPath(payload, strings.Split(c.Path, "."))
	failure := &WebhookPayloadConditionFailure{
		FilterIndex: filterIndex, ConditionIndex: conditionIndex, Condition: c,
		ActualValues: []string{}, Missing: !found,
	}
	// Keep delivery list responses bounded even for wildcard paths or object
	// values. The detail endpoint still exposes the original body in full.
	const maxValues, maxPreviewRunes = 10, 256
	if len(values) > maxValues {
		failure.Truncated = true
		values = values[:maxValues]
	}
	for _, value := range values {
		encoded, _ := json.Marshal(value)
		preview := []rune(string(encoded))
		if len(preview) > maxPreviewRunes {
			preview = append(preview[:maxPreviewRunes-1], '…')
			failure.Truncated = true
		}
		failure.ActualValues = append(failure.ActualValues, string(preview))
	}
	return failure
}

func (d webhookFilterDecision) storedError() string {
	if d.Failure == nil {
		return d.ReasonCode
	}
	// The existing diagnostic text column holds the snapshot, avoiding a
	// migration. API converters expose it as a typed, optional field and keep
	// error human-readable for older clients.
	encoded, _ := json.Marshal(d.Failure)
	return string(encoded)
}

func (r *WebhookDeliveryResponse) decodePayloadFilterFailure() {
	if r.ReasonCode == nil || *r.ReasonCode != "payload_filtered" || r.Error == nil {
		return
	}
	var failure WebhookPayloadConditionFailure
	if json.Unmarshal([]byte(*r.Error), &failure) == nil && failure.Condition.Path != "" {
		r.PayloadConditionFailure = &failure
		message := "payload_filtered: " + failure.Condition.Path
		r.Error = &message
	}
}
