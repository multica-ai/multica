package handler

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestProvisionMembersRevocationMatchesInvitationRecipientAndCommittedWrites(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)
	for _, status := range []string{"created", "added", "already_member"} {
		t.Run(status, func(t *testing.T) {
			for _, linked := range []bool{false, true} {
				if status == "created" && linked {
					continue
				}
				label := "unlinked"
				if linked {
					label = "linked"
				}
				t.Run(label, func(t *testing.T) {
					email := "provision-revocation-" + status + "-" + label + "@example.test"
					var userID string
					if status != "created" {
						userID = dbfx.User(t, "Original recipient", email)
					} else {
						var count int
						dbfx.QueryRow(t, `SELECT count(*) FROM "user" WHERE email=$1`, email).Scan(&count)
						if count != 0 {
							t.Fatal("new-account fixture email already exists")
						}
						dbfx.Cleanup(t, `DELETE FROM "user" WHERE email=$1`, email)
					}
					dbfx.Cleanup(t, `DELETE FROM member WHERE workspace_id=$1 AND user_id IN (SELECT id FROM "user" WHERE email=$2)`, testWorkspaceID, email)
					if status == "already_member" {
						dbfx.Insert(t, "member", testutil.Cols{"workspace_id": testWorkspaceID, "user_id": userID, "role": "admin"})
					}
					cols := testutil.Cols{
						"workspace_id":  testWorkspaceID,
						"inviter_id":    testUserID,
						"invitee_email": email,
						"role":          "member",
						"expires_at":    testutil.Raw("now() + interval '1 day'"),
					}
					var wantRecipient any
					if linked {
						cols["invitee_user_id"] = userID
						wantRecipient = userID
					}
					invitationID := dbfx.Insert(t, "workspace_invitation", cols)

					h := *testHandler
					h.Bus = events.New()
					var published []events.Event
					var committedAtPublish []bool
					h.Bus.SubscribeAll(func(e events.Event) {
						published = append(published, e)
						var committed bool
						dbfx.QueryRow(t, `SELECT NOT EXISTS (SELECT 1 FROM workspace_invitation WHERE id=$1) AND EXISTS (SELECT 1 FROM member m JOIN "user" u ON u.id=m.user_id WHERE m.workspace_id=$2 AND u.email=$3)`, invitationID, testWorkspaceID, email).Scan(&committed)
						committedAtPublish = append(committedAtPublish, committed)
					})
					name := "Replacement"
					var out ProvisionMembersResponse
					testutil.Call(t, h.ProvisionMembers, provisionMembersRequest([]ProvisionMemberEntry{{Email: email, Role: "member", Name: &name}})).Want(200).JSON(&out)
					if len(out.Results) != 1 || out.Results[0].Status != status {
						t.Fatalf("outcome: %+v", out)
					}
					wantEvents := 2
					if status == "already_member" {
						wantEvents = 1
						if out.Results[0].Role != "admin" {
							t.Fatalf("existing role overwritten: %+v", out.Results[0])
						}
					}
					if len(published) != wantEvents {
						t.Fatalf("events=%+v, want %d", published, wantEvents)
					}
					for i, committed := range committedAtPublish {
						if !committed {
							t.Fatalf("event %d preceded committed reconciliation", i)
						}
					}
					if status != "already_member" && published[0].Type != protocol.EventMemberAdded {
						t.Fatalf("missing member-added event: %+v", published)
					}
					ev := published[len(published)-1]
					if ev.Type != protocol.EventInvitationRevoked || ev.WorkspaceID != testWorkspaceID || ev.ActorType != "member" || ev.ActorID != testUserID {
						t.Fatalf("revocation envelope: %+v", ev)
					}
					encoded, err := json.Marshal(ev.Payload)
					if err != nil {
						t.Fatal(err)
					}
					var payload map[string]any
					if err := json.Unmarshal(encoded, &payload); err != nil {
						t.Fatal(err)
					}
					want := map[string]any{"invitation_id": invitationID, "invitee_email": email, "invitee_user_id": wantRecipient}
					if !reflect.DeepEqual(payload, want) {
						t.Fatalf("revocation payload=%v, want %v", payload, want)
					}
					var invitationExists bool
					dbfx.QueryRow(t, `SELECT EXISTS (SELECT 1 FROM workspace_invitation WHERE id=$1)`, invitationID).Scan(&invitationExists)
					if invitationExists {
						t.Fatal("invitation row still exists")
					}
				})
			}
		})
	}
}
