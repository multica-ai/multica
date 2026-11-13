package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAutopilotTriggerResponseIncludesRecordedOwner(t *testing.T) {
	const ownerID = "11111111-1111-1111-1111-111111111111"
	cases := []struct {
		name      string
		kind      string
		ownerType pgtype.Text
		ownerID   pgtype.UUID
		wantType  any
		wantID    any
	}{
		{"member", "schedule", pgtype.Text{String: "member", Valid: true}, parseUUID(ownerID), "member", ownerID},
		{"agent", "webhook", pgtype.Text{String: "agent", Valid: true}, parseUUID(ownerID), "agent", ownerID},
		{"legacy null", "schedule", pgtype.Text{}, pgtype.UUID{}, nil, nil},
		{"missing type", "api", pgtype.Text{}, parseUUID(ownerID), nil, ownerID},
		{"missing id", "schedule", pgtype.Text{String: "member", Valid: true}, pgtype.UUID{}, "member", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(testHandler.triggerToResponse(db.AutopilotTrigger{
				Kind:            tc.kind,
				CreatedByType:   tc.ownerType,
				CreatedByID:     tc.ownerID,
				PublishedByType: pgtype.Text{String: "member", Valid: true},
				PublishedByID:   parseUUID(testUserID),
			}))
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(payload, &got); err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]any{"created_by_type": tc.wantType, "created_by_id": tc.wantID} {
				if value, present := got[key]; !present || value != want {
					t.Errorf("%s = %v (present=%t), want %v; response=%s", key, value, present, want, payload)
				}
			}
		})
	}
}

func TestCreateAutopilotTriggerReturnsOwner(t *testing.T) {
	for _, kind := range []string{"schedule", "webhook"} {
		t.Run(kind, func(t *testing.T) {
			agentID := dbfx.Agent(t, "Trigger owner", "")
			apID := dbfx.Insert(t, "autopilot", testutil.Cols{
				"workspace_id": testWorkspaceID, "title": "Trigger owner", "assignee_id": agentID,
				"created_by_type": "member", "created_by_id": testUserID,
			})
			dbfx.Cleanup(t, `DELETE FROM autopilot_rule_version WHERE autopilot_id = $1`, apID)
			dbfx.Cleanup(t, `DELETE FROM autopilot_trigger WHERE autopilot_id = $1`, apID)
			body := map[string]any{"kind": kind}
			if kind == "schedule" {
				body["cron_expression"] = "0 * * * *"
			}
			req := withURLParam(newRequest(http.MethodPost, "/api/autopilots/"+apID+"/triggers", body), "id", apID)
			var got map[string]any
			testutil.Call(t, testHandler.CreateAutopilotTrigger, req).Want(http.StatusCreated).JSON(&got)
			if got["created_by_type"] != "member" || got["created_by_id"] != testUserID {
				t.Errorf("create response owner = %v/%v, want member/%s", got["created_by_type"], got["created_by_id"], testUserID)
			}
		})
	}
}

func TestAutopilotTriggerOwnerSurvivesEditAndReadRedaction(t *testing.T) {
	creatorID := dbfx.User(t, "Trigger creator", "trigger-creator@multica.test")
	dbfx.Member(t, testWorkspaceID, creatorID, "member")
	readerID := dbfx.User(t, "Trigger reader", "trigger-reader@multica.test")
	dbfx.Member(t, testWorkspaceID, readerID, "member")
	agentID := dbfx.Agent(t, "Trigger owner", "")
	apID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id": testWorkspaceID, "title": "Trigger owner", "assignee_id": agentID,
		"created_by_type": "member", "created_by_id": testUserID,
	})
	dbfx.Cleanup(t, `DELETE FROM autopilot_rule_version WHERE autopilot_id = $1`, apID)
	const secret = "trigger-owner-webhook-secret"
	triggerID := dbfx.Insert(t, "autopilot_trigger", testutil.Cols{
		"autopilot_id": apID, "kind": "webhook", "enabled": true, "webhook_token": secret,
		"created_by_type": "member", "created_by_id": creatorID,
		"published_by_type": "member", "published_by_id": creatorID,
	})
	legacyID := dbfx.Insert(t, "autopilot_trigger", testutil.Cols{
		"autopilot_id": apID, "kind": "schedule", "cron_expression": "0 * * * *",
		"published_by_type": "member", "published_by_id": testUserID,
	})
	patch := withURLParams(newRequest(http.MethodPatch, "/api/autopilots/"+apID+"/triggers/"+triggerID,
		map[string]any{"enabled": false}), "id", apID, "triggerId", triggerID)
	var edited map[string]any
	testutil.Call(t, testHandler.UpdateAutopilotTrigger, patch).Want(http.StatusOK).JSON(&edited)
	if edited["created_by_type"] != "member" || edited["created_by_id"] != creatorID {
		t.Errorf("edit response owner = %v/%v, want original member/%s", edited["created_by_type"], edited["created_by_id"], creatorID)
	}
	var storedCreator, storedPublisher string
	dbfx.QueryRow(t, `SELECT created_by_id::text, published_by_id::text FROM autopilot_trigger WHERE id = $1`, triggerID).
		Scan(&storedCreator, &storedPublisher)
	if storedCreator != creatorID || storedPublisher != testUserID {
		t.Errorf("stored creator/publisher = %s/%s, want %s/%s", storedCreator, storedPublisher, creatorID, testUserID)
	}
	read := withURLParam(newRequestAsUser(readerID, http.MethodGet, "/api/autopilots/"+apID, nil), "id", apID)
	var detail struct {
		Autopilot map[string]any   `json:"autopilot"`
		Triggers  []map[string]any `json:"triggers"`
	}
	testutil.Call(t, testHandler.GetAutopilot, read).Want(http.StatusOK).JSON(&detail)
	if detail.Autopilot["can_write"] != false {
		t.Fatal("reader unexpectedly has write access")
	}
	if len(detail.Triggers) != 2 {
		t.Fatalf("got %d triggers, want 2", len(detail.Triggers))
	}
	for _, trigger := range detail.Triggers {
		if trigger["id"] == triggerID {
			if trigger["created_by_type"] != "member" || trigger["created_by_id"] != creatorID {
				t.Errorf("reader response lost recorded owner: %v", trigger)
			}
			for _, key := range []string{"webhook_token", "webhook_path", "webhook_url"} {
				if trigger[key] != nil {
					t.Errorf("reader received %s = %v", key, trigger[key])
				}
			}
		} else if trigger["id"] == legacyID {
			for _, key := range []string{"created_by_type", "created_by_id"} {
				if value, present := trigger[key]; !present || value != nil {
					t.Errorf("legacy %s = %v (present=%t), want explicit null", key, value, present)
				}
			}
		} else {
			t.Errorf("unexpected trigger: %v", trigger["id"])
		}
	}
}
