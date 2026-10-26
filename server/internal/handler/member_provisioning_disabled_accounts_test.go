package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type provisioningDisabledAccountTxCounter struct {
	delegate txStarter
	begins   int
}

func (c *provisioningDisabledAccountTxCounter) Begin(ctx context.Context) (pgx.Tx, error) {
	c.begins++
	return c.delegate.Begin(ctx)
}

func TestProvisionMembersDisabledAccountsLeaveDatabaseAndEventsUntouched(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)
	for _, tc := range []struct {
		name     string
		email    string
		id       string
		existing bool
		member   bool
	}{
		{name: "denylisted email before creation", email: "pdzzer68@embassybase.com"},
		{name: "denylisted email existing user", email: "pdzzer68@embassybase.com", existing: true},
		{name: "denylisted email existing member", email: "pdzzer68@embassybase.com", existing: true, member: true},
		{name: "denylisted ID existing user", email: "provision-disabled-id@example.test", id: "514492f7-b30f-4147-bd33-c0e8ce5d6d4f", existing: true},
		{name: "denylisted ID existing member", email: "provision-disabled-id@example.test", id: "514492f7-b30f-4147-bd33-c0e8ce5d6d4f", existing: true, member: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var count int
			dbfx.QueryRow(t, `SELECT count(*) FROM "user" WHERE email=$1 OR id::text=NULLIF($2,'')`, tc.email, tc.id).Scan(&count)
			if count != 0 {
				t.Fatal("disabled-account fixture collides with an existing row")
			}
			var userID string
			if tc.existing {
				cols := testutil.Cols{"name": "Original disabled user", "email": tc.email, "updated_at": testutil.Raw("'2001-01-01'::timestamptz")}
				if tc.id != "" {
					cols["id"] = tc.id
				}
				userID = dbfx.Insert(t, "user", cols)
			} else {
				dbfx.Cleanup(t, `DELETE FROM "user" WHERE email=$1`, tc.email)
			}
			dbfx.Cleanup(t, `DELETE FROM member WHERE workspace_id=$1 AND user_id IN (SELECT id FROM "user" WHERE email=$2)`, testWorkspaceID, tc.email)
			if tc.member {
				dbfx.Insert(t, "member", testutil.Cols{"workspace_id": testWorkspaceID, "user_id": userID, "role": "admin"})
			}
			invitationID := dbfx.Insert(t, "workspace_invitation", testutil.Cols{
				"workspace_id":  testWorkspaceID,
				"inviter_id":    testUserID,
				"invitee_email": tc.email,
				"role":          "member",
				"expires_at":    testutil.Raw("now() + interval '1 day'"),
			})
			if !auth.IsTemporarilyDisabledUser(userID, tc.email) {
				t.Fatal("fixture is not disabled by the existing auth policy")
			}
			snapshot := func() string {
				var state string
				dbfx.QueryRow(t, `SELECT jsonb_build_object('users', (SELECT COALESCE(jsonb_agg(to_jsonb(u)), '[]'::jsonb) FROM "user" u WHERE email=$1), 'members', (SELECT COALESCE(jsonb_agg(to_jsonb(m)), '[]'::jsonb) FROM member m JOIN "user" u ON u.id=m.user_id WHERE u.email=$1 AND m.workspace_id=$2), 'invitation', (SELECT to_jsonb(i) FROM workspace_invitation i WHERE id=$3))::text`, tc.email, testWorkspaceID, invitationID).Scan(&state)
				return state
			}
			before := snapshot()
			h := *testHandler
			h.Bus = events.New()
			var published []events.Event
			h.Bus.SubscribeAll(func(e events.Event) { published = append(published, e) })
			counter := &provisioningDisabledAccountTxCounter{delegate: h.TxStarter}
			h.TxStarter = counter
			name := "Replacement"
			var out ProvisionMembersResponse
			testutil.Call(t, h.ProvisionMembers, provisionMembersRequest([]ProvisionMemberEntry{{Email: tc.email, Role: "member", Name: &name}})).Want(200).JSON(&out)
			if len(out.Results) != 1 || out.Results[0].Status != "failed" || out.Results[0].Error != auth.TemporarilyDisabledUserError || out.Results[0].UserID != "" || out.Results[0].MemberID != "" || out.Summary.Failed != 1 || out.Summary.Created != 0 || out.Summary.Added != 0 || out.Summary.AlreadyMember != 0 {
				t.Errorf("disabled-account outcome: %+v", out)
			}
			if after := snapshot(); after != before {
				t.Errorf("disabled-account request changed rows: before=%s after=%s", before, after)
			}
			if len(published) != 0 {
				t.Errorf("disabled-account request published events: %+v", published)
			}
			if auth.IsTemporarilyDisabledUserEmail(tc.email) && counter.begins != 0 {
				t.Errorf("denylisted email began %d transactions", counter.begins)
			}
		})
	}
}
