package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWebhookEventAllowedByTriggerScope_GitLabCIStatusFields(t *testing.T) {
	for _, tc := range []struct {
		name, event, payload string
		allowed              bool
	}{
		{"pipeline failed", "Pipeline Hook", `{"object_kind":"pipeline","object_attributes":{"status":"failed"}}`, true},
		{"job failed", "Job Hook", `{"object_kind":"build","build_status":"failed"}`, true},
		{"pipeline missing status", "Pipeline Hook", `{"object_kind":"pipeline","object_attributes":{}}`, false},
		{"pipeline null status", "Pipeline Hook", `{"object_kind":"pipeline","object_attributes":{"status":null}}`, false},
		{"pipeline non-string status", "Pipeline Hook", `{"object_kind":"pipeline","object_attributes":{"status":42}}`, false},
		{"pipeline malformed attributes", "Pipeline Hook", `{"object_kind":"pipeline","object_attributes":[]}`, false},
		{"job missing status", "Job Hook", `{"object_kind":"build"}`, false},
		{"job null status", "Job Hook", `{"object_kind":"build","build_status":null}`, false},
		{"job non-string status", "Job Hook", `{"object_kind":"build","build_status":false}`, false},
		{"job does not use pipeline status", "Job Hook", `{"object_kind":"build","object_attributes":{"status":"failed"}}`, false},
		{"pipeline does not use job status", "Pipeline Hook", `{"object_kind":"pipeline","build_status":"failed"}`, false},
		{"MR ignores CI fields", "Merge Request Hook", `{"object_kind":"merge_request","object_attributes":{"action":"open","state":"opened","status":"failed"},"build_status":"failed"}`, false},
		{"issue ignores CI fields", "Issue Hook", `{"object_kind":"issue","object_attributes":{"action":"open","state":"opened","status":"failed"},"build_status":"failed"}`, false},
		{"note ignores CI fields", "Note Hook", `{"object_kind":"note","object_attributes":{"action":"create","status":"failed"},"build_status":"failed"}`, false},
		{"missing kind ignores CI fields", "Pipeline Hook", `{"object_attributes":{"status":"failed"},"build_status":"failed"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := make(http.Header)
			headers.Set("X-Gitlab-Event", tc.event)
			envelope, err := normalizeWebhookPayload([]byte(tc.payload), headers)
			if err != nil {
				t.Fatal(err)
			}
			filters, err := json.Marshal([]WebhookEventFilter{{Event: tc.event, Actions: []string{"failed"}}})
			if err != nil {
				t.Fatal(err)
			}
			decision := evaluateWebhookTriggerScope(filters, envelope)
			if decision.Allowed != tc.allowed || (!tc.allowed && decision.ReasonCode != "event_filtered") {
				t.Fatalf("decision=%+v, want allowed=%v", decision, tc.allowed)
			}
		})
	}
}

func gitLabCIWebhookBody(event, status string) []byte {
	if event == "Pipeline Hook" {
		return []byte(fmt.Sprintf(`{"object_kind":"pipeline","object_attributes":{"id":31,"ref":"main","status":%q},"project":{"id":1},"builds":[{"id":1977,"status":"failed"}]}`, status))
	}
	return []byte(fmt.Sprintf(`{"object_kind":"build","build_id":1977,"build_name":"test","build_stage":"test","build_status":%q,"pipeline_id":31,"project_id":1,"commit":{"id":31,"status":"failed"}}`, status))
}

func gitLabCIWebhookFixture(t *testing.T, event string, actions []string) (string, AutopilotTriggerResponse) {
	t.Helper()
	agentID := dbfx.Agent(t, "GitLab CI webhook agent", testRuntimeID)
	apID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id": testWorkspaceID, "title": "GitLab CI webhook",
		"assignee_id": agentID, "status": "active", "execution_mode": "run_only",
		"created_by_type": "member", "created_by_id": testUserID,
	})
	trig := createWebhookTriggerWithFilters(t, apID, []WebhookEventFilter{{Event: event, Actions: actions}})
	dbfx.Cleanup(t, `DELETE FROM autopilot_trigger WHERE autopilot_id = $1`, apID)
	dbfx.Cleanup(t, `DELETE FROM webhook_delivery WHERE autopilot_id = $1`, apID)
	dbfx.Cleanup(t, `DELETE FROM autopilot_run WHERE autopilot_id = $1`, apID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
	setSigningSecretViaHandler(t, apID, trig.ID, testSigningSecret)
	return apID, trig
}

func TestWebhookGitLabCI_IngressAndRecoveredWorker(t *testing.T) {
	for _, event := range []string{"Pipeline Hook", "Job Hook"} {
		for _, worker := range []bool{false, true} {
			for _, tc := range []struct {
				name, status string
				actions      []string
				allowed      bool
			}{
				{"failed matches", "failed", []string{"failed"}, true},
				{"success filtered", "success", []string{"failed"}, false},
				{"running filtered", "running", []string{"failed"}, false},
				{"empty actions", "success", nil, true},
			} {
				t.Run(fmt.Sprintf("%s/worker=%v/%s", event, worker, tc.name), func(t *testing.T) {
					ctx := context.Background()
					apID, trig := gitLabCIWebhookFixture(t, event, tc.actions)
					body := gitLabCIWebhookBody(event, tc.status)
					var deliveryID string
					if worker {
						// Recover a verified delivery before ingress admitted a run.
						headers := make(http.Header)
						headers.Set("X-Gitlab-Event", event)
						row, _, err := testHandler.persistInboundDelivery(httptest.NewRequest(http.MethodPost, "/webhook", nil), persistDeliveryInput{
							WorkspaceID: parseUUID(testWorkspaceID), AutopilotID: parseUUID(apID), TriggerID: parseUUID(trig.ID),
							Provider: "generic", Event: "gitlab." + event, SignatureStatus: sigStatusValid,
							RawBody: body, SelectedHeaders: selectedHeadersJSON(headers),
						})
						if err != nil {
							t.Fatal(err)
						}
						deliveryID = uuidToString(row.ID)
					} else {
						w := postWebhook(t, *trig.WebhookToken, body, map[string]string{
							"X-Gitlab-Event": event, "X-Hub-Signature-256": signBody(testSigningSecret, body),
						})
						if tc.allowed {
							deliveryID = requireAcceptedWebhookResponse(t, w)
						} else {
							var resp map[string]any
							if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
								t.Fatal(err)
							}
							if w.Code != http.StatusOK || resp["status"] != "ignored" || resp["reason"] != "event_filtered" || resp["run_id"] != nil {
								t.Fatalf("unexpected ingress response: %d %v", w.Code, resp)
							}
							deliveryID, _ = resp["delivery_id"].(string)
						}
					}
					row := processQueuedWebhookDelivery(t, deliveryID)
					wantStatus, wantRuns := deliveryStatusIgnored, 0
					if tc.allowed {
						wantStatus, wantRuns = deliveryStatusDispatched, 1
					}
					if row.Status != wantStatus || row.AutopilotRunID.Valid != tc.allowed || row.Event != "gitlab."+event || row.SignatureStatus != sigStatusValid {
						t.Fatalf("delivery status=%s run=%v event=%q signature=%s", row.Status, row.AutopilotRunID.Valid, row.Event, row.SignatureStatus)
					}
					if !tc.allowed && (!row.Error.Valid || row.Error.String != "event_filtered") {
						t.Fatalf("filtered delivery error=%v", row.Error)
					}
					runs, err := testHandler.Queries.ListAutopilotRuns(ctx, db.ListAutopilotRunsParams{AutopilotID: parseUUID(apID), Limit: 10})
					if err != nil || len(runs) != wantRuns {
						t.Fatalf("runs=%d, want %d, err=%v", len(runs), wantRuns, err)
					}
				})
			}
		}
	}
}

func TestWebhookGitLabCI_SignatureBeforeFiltering(t *testing.T) {
	for _, event := range []string{"Pipeline Hook", "Job Hook"} {
		for _, status := range []string{"failed", "success"} {
			for _, signature := range []string{"", "sha256=deadbeef"} {
				t.Run(fmt.Sprintf("%s/%s/signature=%q", event, status, signature), func(t *testing.T) {
					apID, trig := gitLabCIWebhookFixture(t, event, []string{"failed"})
					w := postWebhook(t, *trig.WebhookToken, gitLabCIWebhookBody(event, status), map[string]string{
						"X-Gitlab-Event": event, "X-Hub-Signature-256": signature,
					})
					var resp map[string]any
					if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
						t.Fatal(err)
					}
					wantReason := "invalid_signature"
					if signature == "" {
						wantReason = "missing_signature"
					}
					if w.Code != http.StatusUnauthorized || resp["status"] != "rejected" || resp["reason"] != wantReason || resp["run_id"] != nil {
						t.Fatalf("unexpected signature response: %d %v", w.Code, resp)
					}
					deliveryID, _ := resp["delivery_id"].(string)
					row := processQueuedWebhookDelivery(t, deliveryID)
					if row.Status != deliveryStatusRejected || row.AutopilotRunID.Valid {
						t.Fatalf("rejected delivery status=%s run=%v", row.Status, row.AutopilotRunID.Valid)
					}
					runs, err := testHandler.Queries.ListAutopilotRuns(context.Background(), db.ListAutopilotRunsParams{AutopilotID: parseUUID(apID), Limit: 10})
					if err != nil || len(runs) != 0 {
						t.Fatalf("rejected signature created %d runs, err=%v", len(runs), err)
					}
				})
			}
		}
	}
}
