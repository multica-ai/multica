package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWebhookPayloadFilter_EmptyWildcardAndMissingPaths(t *testing.T) {
	filters := []byte(`[{"event":"pull_request","conditions":[{"path":"pull_request.labels.*.name","op":"not_equals","value":"wip"}]}]`)
	for _, tc := range []struct {
		name, payload string
		allowed       bool
	}{
		{"empty labels", `{"pull_request":{"labels":[]}}`, true},
		{"other label", `{"pull_request":{"labels":[{"name":"bug"}]}}`, true},
		{"wip anywhere", `{"pull_request":{"labels":[{"name":"bug"},{"name":"wip"}]}}`, false},
		{"missing labels", `{"pull_request":{}}`, false},
		{"null labels", `{"pull_request":{"labels":null}}`, false},
		{"wrong array type", `{"pull_request":{"labels":{}}}`, false},
		{"missing leaf", `{"pull_request":{"labels":[{}]}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decision := evaluateWebhookTriggerScope(filters, pullRequestEnvelope(tc.payload))
			if decision.Allowed != tc.allowed {
				t.Fatalf("decision = %+v, want allowed=%v", decision, tc.allowed)
			}
		})
	}
}

func TestWebhookPayloadFilter_ReasonAndFirstFailure(t *testing.T) {
	filters := []byte(`[
		{"event":"issues","conditions":[{"path":"missing","op":"exists"}]},
		{"event":"pull_request","actions":["closed"],"conditions":[{"path":"missing","op":"exists"}]},
		{"event":"pull_request","conditions":[{"path":"draft","op":"equals","value":false},{"path":"base.ref","op":"equals","value":"main"},{"path":"sender","op":"equals","value":"alice"}]},
		{"event":"pull_request","conditions":[{"path":"override","op":"equals","value":true}]}
	]`)
	for _, tc := range []struct {
		name, payload, reason string
		allowed               bool
	}{
		{"first failed condition", `{"draft":false,"base":{"ref":"dev"},"sender":"bob"}`, "payload_filtered", false},
		{"later row accepts", `{"draft":false,"base":{"ref":"dev"},"override":true}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decision := evaluateWebhookTriggerScope(filters, pullRequestEnvelope(tc.payload))
			if decision.Allowed != tc.allowed || decision.ReasonCode != tc.reason {
				t.Fatalf("unexpected decision: %+v", decision)
			}
			if tc.allowed {
				if decision.Failure != nil {
					t.Fatal("allowed delivery must not retain a failed row's diagnostic")
				}
				return
			}
			failure := decision.Failure
			if failure == nil || failure.FilterIndex != 2 || failure.ConditionIndex != 1 || failure.Condition.Path != "base.ref" || !reflect.DeepEqual(failure.ActualValues, []string{`"dev"`}) {
				t.Fatalf("unexpected first failure: %+v", failure)
			}
		})
	}
	decision := evaluateWebhookTriggerScope(filters, WebhookEnvelope{Event: "github.push"})
	if decision.ReasonCode != "event_filtered" || decision.Failure != nil {
		t.Fatalf("event mismatch: %+v", decision)
	}
	decision = evaluateWebhookTriggerScope([]byte(`[{"event":"pull_request","actions":["closed"]}]`), pullRequestEnvelope(`{}`))
	if decision.ReasonCode != "event_filtered" || decision.Failure != nil {
		t.Fatalf("action mismatch: %+v", decision)
	}
}

func TestWebhookPayloadFilter_DiagnosticBounds(t *testing.T) {
	var values []any
	for range 11 {
		values = append(values, strings.Repeat("界", 300))
	}
	failure := newWebhookPayloadConditionFailure(WebhookPayloadCondition{Path: "items.*", Op: payloadOpEquals, Value: "x"}, map[string]any{"items": values}, 0, 0)
	if len(failure.ActualValues) != 10 || !failure.Truncated || len([]rune(failure.ActualValues[0])) != 256 {
		t.Fatalf("diagnostic was not bounded: %+v", failure)
	}
	missing := newWebhookPayloadConditionFailure(WebhookPayloadCondition{Path: "missing", Op: payloadOpExists}, nil, 0, 0)
	if !missing.Missing || len(missing.ActualValues) != 0 {
		t.Fatalf("missing diagnostic: %+v", missing)
	}
	null := newWebhookPayloadConditionFailure(WebhookPayloadCondition{Path: "value", Op: payloadOpExists}, map[string]any{"value": nil}, 0, 0)
	if null.Missing || !reflect.DeepEqual(null.ActualValues, []string{"null"}) {
		t.Fatalf("null diagnostic: %+v", null)
	}
}

func TestWebhookPayloadFilter_PersistsIngressAndWorkerDecisions(t *testing.T) {
	for _, worker := range []bool{false, true} {
		name := "ingress"
		if worker {
			name = "recovered worker"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			agentID := createWebhookTestAgent(t, "Payload diagnostic agent")
			apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
			filters := []WebhookEventFilter{{Event: "pull_request", Conditions: []WebhookPayloadCondition{{Path: "pull_request.base.ref", Op: payloadOpEquals, Value: "main"}}}}
			trig := createWebhookTriggerWithFilters(t, apID, filters)
			var deliveryID string
			if worker {
				row, _, err := testHandler.persistInboundDelivery(httptest.NewRequest(http.MethodPost, "/webhook", nil), persistDeliveryInput{
					WorkspaceID: parseUUID(testWorkspaceID), AutopilotID: parseUUID(apID), TriggerID: parseUUID(trig.ID),
					Provider: "github", Event: "github.pull_request.opened", SignatureStatus: sigStatusNotRequired,
					RawBody:         []byte(`{"action":"opened","pull_request":{"base":{"ref":"dev"}}}`),
					SelectedHeaders: selectedHeadersJSON(http.Header{"X-Github-Event": []string{"pull_request"}}),
				})
				if err != nil {
					t.Fatal(err)
				}
				deliveryID = uuidToString(row.ID)
				row = processQueuedWebhookDelivery(t, deliveryID)
				if row.Status != deliveryStatusIgnored {
					t.Fatalf("worker status=%s", row.Status)
				}
			} else {
				w := postWebhook(t, *trig.WebhookToken, map[string]any{"action": "opened", "pull_request": map[string]any{"base": map[string]any{"ref": "dev"}}}, map[string]string{"X-GitHub-Event": "pull_request"})
				var resp map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatal(err)
				}
				if w.Code != http.StatusOK || resp["reason"] != "payload_filtered" || resp["run_id"] != nil {
					t.Fatalf("unexpected webhook response: %d %v", w.Code, resp)
				}
				deliveryID = resp["delivery_id"].(string)
			}
			// Change the rule after receipt: the diagnostic must remain the old
			// decision, rather than evaluating the new filter at read time.
			if _, err := testPool.Exec(ctx, `UPDATE autopilot_trigger SET event_filters='[]' WHERE id=$1`, trig.ID); err != nil {
				t.Fatal(err)
			}
			var list struct {
				Deliveries []WebhookDeliveryResponse `json:"deliveries"`
			}
			w := httptest.NewRecorder()
			req := withURLParam(newRequest("GET", "/api/autopilots/"+apID+"/deliveries", nil), "id", apID)
			testHandler.ListAutopilotDeliveries(w, req)
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != 200 || len(list.Deliveries) != 1 {
				t.Fatalf("list: %d %s", w.Code, w.Body.String())
			}
			detailRow, err := testHandler.Queries.GetWebhookDelivery(ctx, parseUUID(deliveryID))
			if err != nil {
				t.Fatal(err)
			}
			for _, resp := range []WebhookDeliveryResponse{list.Deliveries[0], deliveryToResponse(detailRow, true)} {
				if resp.ReasonCode == nil || *resp.ReasonCode != "payload_filtered" || resp.PayloadConditionFailure == nil {
					t.Fatalf("missing stored diagnostic: %+v", resp)
				}
				failure := resp.PayloadConditionFailure
				if failure.Condition.Value != "main" || !reflect.DeepEqual(failure.ActualValues, []string{`"dev"`}) {
					t.Fatalf("diagnostic changed with trigger: %+v", failure)
				}
			}
			runs, err := testHandler.Queries.ListAutopilotRuns(ctx, db.ListAutopilotRunsParams{AutopilotID: parseUUID(apID), Limit: 10})
			if err != nil || len(runs) != 0 {
				t.Fatalf("filtered delivery created runs: %v %v", runs, err)
			}
		})
	}
}
