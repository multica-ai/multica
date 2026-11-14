package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

func TestLarkInstallationSameSecondRebindRevision(t *testing.T) {
	firstBinding := time.Date(2026, 11, 2, 10, 0, 0, 123456000, time.UTC)
	secondBinding := firstBinding.Add(time.Microsecond)
	first := larkInstallationToResponse(lark.Installation{InstalledAt: pgtype.Timestamptz{Time: firstBinding, Valid: true}})
	second := larkInstallationToResponse(lark.Installation{InstalledAt: pgtype.Timestamptz{Time: secondBinding, Valid: true}})
	if first.InstalledAt == second.InstalledAt {
		t.Fatal("same-second rebinds share the send-observation cache revision")
	}
	for _, tc := range []struct {
		wire string
		want time.Time
	}{{first.InstalledAt, firstBinding}, {second.InstalledAt, secondBinding}} {
		parsed, err := time.Parse(time.RFC3339Nano, tc.wire)
		if err != nil || !parsed.Equal(tc.want) {
			t.Fatalf("installation timestamp lost precision: %q", tc.wire)
		}
	}
}

func TestLarkSendObservationAccessAndWire(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := lark.NewInstallationService(testHandler.Queries, box)
	if err != nil {
		t.Fatal(err)
	}
	h := *testHandler
	h.LarkInstallations = svc
	suffix := uuid.NewString()
	member := dbfx.User(t, "Observation member", "observation-member-"+suffix+"@example.invalid")
	admin := dbfx.User(t, "Observation admin", "observation-admin-"+suffix+"@example.invalid")
	dbfx.Member(t, testWorkspaceID, member, "member")
	dbfx.Member(t, testWorkspaceID, admin, "admin")
	agent := dbfx.Agent(t, "Observation agent", handlerTestRuntimeID(t))
	inst := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id": testWorkspaceID, "agent_id": agent, "channel_type": "feishu", "installer_user_id": member, "status": "active",
		"config": `{"app_id":"cli_observation","last_send_observation":{"observed_at":"2026-11-02T00:00:00.000000001Z","result":"rejected","reason":"missing_permission"}}`,
	})
	otherWS := dbfx.Workspace(t, "Other observation", "observation-other-"+suffix)
	dbfx.Member(t, otherWS, testUserID, "owner")
	for _, tc := range []struct {
		name, user, ws, source string
		status                 int
	}{
		{"owner", testUserID, testWorkspaceID, "", 200},
		{"admin", admin, testWorkspaceID, "", 200},
		{"member", member, testWorkspaceID, "", 403},
		{"cross workspace", testUserID, otherWS, "", 404},
		{"not a member", member, otherWS, "", 404},
		{"task owner identity", testUserID, testWorkspaceID, "task_token", 403},
		{"machine owner identity", testUserID, testWorkspaceID, "cloud_pat", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := newRequestAs(tc.user, http.MethodGet, "/api/workspaces/"+tc.ws+"/lark/installations/"+inst+"/send-observation", nil)
			req.Header.Set("X-Actor-Source", tc.source)
			req = withURLParams(req, "id", tc.ws, "installationId", inst)
			response := testutil.Call(t, h.GetLarkSendObservation, req).Want(tc.status)
			if tc.status == 200 {
				var out struct {
					Observation *lark.SendObservation `json:"observation"`
				}
				response.JSON(&out)
				if out.Observation == nil || out.Observation.Reason != "missing_permission" {
					t.Fatal("observation missing")
				}
				if response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("private observation can be cached")
				}
			} else if strings.Contains(response.Body.String(), "missing_permission") {
				t.Fatal("denied response leaked observation")
			}
		})
	}
	for _, user := range []string{testUserID, member} {
		req := withURLParams(newRequestAs(user, http.MethodGet, "/list", nil), "id", testWorkspaceID)
		response := testutil.Call(t, h.ListLarkInstallations, req).Want(200)
		if strings.Contains(response.Body.String(), `"observation"`) || strings.Contains(response.Body.String(), "missing_permission") {
			t.Fatal("member-visible list leaked send observations")
		}
	}
	if _, err := testPool.Exec(context.Background(), "UPDATE channel_installation SET config=config-'last_send_observation' WHERE id=$1", inst); err != nil {
		t.Fatal(err)
	}
	req := withURLParams(newRequestAs(testUserID, http.MethodGet, "/observation", nil), "id", testWorkspaceID, "installationId", inst)
	var empty map[string]json.RawMessage
	testutil.Call(t, h.GetLarkSendObservation, req).Want(200).JSON(&empty)
	if string(empty["observation"]) != "null" {
		t.Fatal("missing evidence not returned explicitly")
	}
}
