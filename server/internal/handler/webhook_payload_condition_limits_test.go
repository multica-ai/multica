package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateWebhookPayloadConditionLimits(t *testing.T) {
	for _, character := range []string{"a", "界", "😀"} {
		for _, length := range []int{256, 257} {
			c := WebhookPayloadCondition{Path: strings.Repeat(character, length), Op: payloadOpExists}
			if err := validateWebhookPayloadCondition(c); (err == nil) != (length == 256) {
				t.Errorf("path %q x %d: %v", character, length, err)
			}
		}
	}
	for _, count := range []int{100, 101} {
		values := make([]any, count)
		for i := range values {
			values[i] = float64(i)
		}
		c := WebhookPayloadCondition{Path: "number", Op: payloadOpIn, Values: values}
		if err := validateWebhookPayloadCondition(c); (err == nil) != (count == 100) {
			t.Errorf("in list with %d values: %v", count, err)
		}
		filters, err := json.Marshal([]WebhookEventFilter{{Event: "pull_request", Conditions: []WebhookPayloadCondition{c}}})
		if err != nil {
			t.Fatal(err)
		}
		decision := evaluateWebhookTriggerScope(filters, pullRequestEnvelope(`{"number":0}`))
		if decision.Allowed != (count == 100) {
			t.Errorf("stored list with %d values: %+v", count, decision)
		}
	}
}

func TestWebhookPayloadConditionLimits_RejectCreateAndUpdate(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Condition limit agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trigger := createWebhookTriggerWithFilters(t, apID, []WebhookEventFilter{{Event: "push"}})
	tooManyValues := make([]any, 101)
	for i := range tooManyValues {
		tooManyValues[i] = float64(i)
	}
	for _, condition := range []WebhookPayloadCondition{
		{Path: strings.Repeat("x", 257), Op: payloadOpExists},
		{Path: "number", Op: payloadOpIn, Values: tooManyValues},
	} {
		for _, method := range []string{http.MethodPost, http.MethodPatch} {
			body := map[string]any{"event_filters": []WebhookEventFilter{{Event: "pull_request", Conditions: []WebhookPayloadCondition{condition}}}}
			if method == http.MethodPost {
				body["kind"] = "webhook"
			}
			req := withURLParams(newRequest(method, "/api/autopilots/"+apID+"/triggers", body), "id", apID, "triggerId", trigger.ID)
			w := httptest.NewRecorder()
			if method == http.MethodPost {
				testHandler.CreateAutopilotTrigger(w, req)
			} else {
				testHandler.UpdateAutopilotTrigger(w, req)
			}
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "must not exceed") {
				t.Fatalf("%s oversized condition: %d %s", method, w.Code, w.Body.String())
			}
		}
	}
	stored, err := testHandler.Queries.GetAutopilotTrigger(context.Background(), parseUUID(trigger.ID))
	if err != nil {
		t.Fatal(err)
	}
	var filters []WebhookEventFilter
	if err := json.Unmarshal(stored.EventFilters, &filters); err != nil {
		t.Fatal(err)
	}
	if len(filters) != 1 || filters[0].Event != "push" || len(filters[0].Conditions) != 0 {
		t.Fatalf("rejected updates modified the saved filters: %+v", filters)
	}
}
