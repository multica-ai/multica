package handler

import (
	"net/http"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// A webhook token IS a trigger: whoever holds it fires the autopilot through
// the public ingress route, outside the permission system. That is why
// GetAutopilot redacts it for non-writers — but `autopilot:updated` fans out to
// the WHOLE workspace room, so publishing the live trigger response handed every
// member exactly what the read path had just refused them, through a push no
// write gate ever sees.
//
// The leak predates MUL-7108 but is the same authorization bypass, so it is
// closed with it. The fix follows the rule broadcastAgentResponse already
// established for agents: the HTTP caller keeps the live value, the broadcast
// copy carries none.
func TestAutopilotTriggerBroadcastsCarryNoWebhookCredential(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newActingFixture(t, "broadcast redaction")
	ws := "?workspace_id=" + testWorkspaceID
	// The workspace owner is a writer here, so every call below is authorized:
	// what is under test is what the FANOUT carries, not who may make the call.
	writer := actingCaller{authUserID: testUserID}

	// The bus is synchronous, so the event for a request has been recorded by
	// the time the handler returns. Filtering on this autopilot keeps the
	// assertion deterministic even if another test publishes on the same topic.
	var mu sync.Mutex
	var broadcasts []AutopilotTriggerResponse
	testHandler.Bus.Subscribe(protocol.EventAutopilotUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok || payload["autopilot_id"] != fx.autopilotID {
			return
		}
		trigger, ok := payload["trigger"].(AutopilotTriggerResponse)
		if !ok {
			return
		}
		mu.Lock()
		broadcasts = append(broadcasts, trigger)
		mu.Unlock()
	})
	lastBroadcast := func(t *testing.T) AutopilotTriggerResponse {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if len(broadcasts) == 0 {
			t.Fatal("no autopilot:updated event was published; the UI would never refetch")
		}
		return broadcasts[len(broadcasts)-1]
	}

	cases := []struct {
		name    string
		request func() *http.Request
		handler func(w http.ResponseWriter, r *http.Request)
		want    int
	}{
		{
			name:    "create-webhook-trigger",
			want:    http.StatusCreated,
			handler: testHandler.CreateAutopilotTrigger,
			request: func() *http.Request {
				return withURLParams(writer.request("POST", "/api/autopilots/"+fx.autopilotID+"/triggers"+ws,
					map[string]any{"kind": "webhook", "label": "ci"}), "id", fx.autopilotID)
			},
		},
		{
			name:    "update-trigger",
			want:    http.StatusOK,
			handler: testHandler.UpdateAutopilotTrigger,
			request: func() *http.Request {
				return withURLParams(writer.request("PATCH",
					"/api/autopilots/"+fx.autopilotID+"/triggers/"+fx.webhookTriggerID+ws,
					map[string]any{"enabled": false}),
					"id", fx.autopilotID, "triggerId", fx.webhookTriggerID)
			},
		},
		{
			name:    "rotate-webhook-token",
			want:    http.StatusOK,
			handler: testHandler.RotateAutopilotTriggerWebhookToken,
			request: func() *http.Request {
				return withURLParams(writer.request("POST",
					"/api/autopilots/"+fx.autopilotID+"/triggers/"+fx.webhookTriggerID+"/rotate-token"+ws, nil),
					"id", fx.autopilotID, "triggerId", fx.webhookTriggerID)
			},
		},
		{
			name:    "set-signing-secret",
			want:    http.StatusOK,
			handler: testHandler.SetAutopilotTriggerSigningSecret,
			request: func() *http.Request {
				return withURLParams(writer.request("PUT",
					"/api/autopilots/"+fx.autopilotID+"/triggers/"+fx.webhookTriggerID+"/signing-secret"+ws,
					map[string]any{"signing_secret": "0123456789abcdef"}),
					"id", fx.autopilotID, "triggerId", fx.webhookTriggerID)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var resp AutopilotTriggerResponse
			testutil.Call(t, tc.handler, tc.request()).Want(tc.want).JSON(&resp)
			// The authorized caller still gets the live credential; without
			// this the redaction assertion below would pass on an endpoint
			// that had simply stopped issuing tokens.
			if resp.WebhookToken == nil || *resp.WebhookToken == "" {
				t.Fatalf("HTTP response carries no webhook token; the broadcast assertion would prove nothing")
			}

			event := lastBroadcast(t)
			if event.WebhookToken != nil || event.WebhookPath != nil || event.WebhookURL != nil {
				t.Errorf("broadcast leaked the webhook credential: token=%v path=%v url=%v — every workspace member receives this event",
					derefStr(event.WebhookToken), derefStr(event.WebhookPath), derefStr(event.WebhookURL))
			}
			// The event must still identify what changed: clients react by
			// refetching through the authenticated detail endpoint, which is
			// what makes carrying no secret free.
			if event.ID == "" {
				t.Error("broadcast carries no trigger id; clients cannot tell what to refetch")
			}
		})
	}
}
