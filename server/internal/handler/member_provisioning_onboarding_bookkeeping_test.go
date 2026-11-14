package handler

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestProvisionMembersExistingAttachmentOnboardsWithoutReplacingNameOrOriginalCompletion(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)
	for _, previouslyOnboarded := range []bool{false, true} {
		label := "first-completion"
		if previouslyOnboarded {
			label = "preserved-completion"
		}
		t.Run(label, func(t *testing.T) {
			email := "provision-onboarding-" + label + "@example.test"
			userID := dbfx.User(t, "Original", email)
			dbfx.Cleanup(t, `DELETE FROM member WHERE user_id=$1`, userID)
			dbfx.Exec(t, `UPDATE "user" SET updated_at='2001-01-01'::timestamptz, onboarded_at=NULL WHERE id=$1`, userID)
			if previouslyOnboarded {
				dbfx.Exec(t, `UPDATE "user" SET onboarded_at='2000-01-01'::timestamptz WHERE id=$1`, userID)
			}
			name := "Replacement"
			call := func(role string) ProvisionMemberResult {
				var out ProvisionMembersResponse
				testutil.Call(t, testHandler.ProvisionMembers, provisionMembersRequest([]ProvisionMemberEntry{{Email: email, Role: role, Name: &name}})).Want(200).JSON(&out)
				if len(out.Results) != 1 {
					t.Fatalf("outcome: %+v", out)
				}
				return out.Results[0]
			}
			if got := call("admin"); got.Status != "added" || got.Role != "admin" {
				t.Fatalf("attachment: %+v", got)
			}
			var gotName, role string
			var onboarded *time.Time
			var updated time.Time
			dbfx.QueryRow(t, `SELECT u.name, u.onboarded_at, u.updated_at, m.role FROM "user" u JOIN member m ON m.user_id=u.id WHERE u.id=$1 AND m.workspace_id=$2`, userID, testWorkspaceID).Scan(&gotName, &onboarded, &updated, &role)
			if gotName != "Original" || role != "admin" || onboarded == nil || !updated.After(time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)) {
				t.Fatalf("attachment bookkeeping: name=%q role=%q onboarded=%v updated=%v", gotName, role, onboarded, updated)
			}
			if previouslyOnboarded {
				if !onboarded.Equal(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
					t.Fatalf("original completion replaced: %v", onboarded)
				}
			} else if !onboarded.Equal(updated) {
				t.Fatalf("first completion and update differ: %v / %v", onboarded, updated)
			}
			snapshot := func() string {
				var state string
				dbfx.QueryRow(t, `SELECT jsonb_build_object('user', to_jsonb(u), 'member', to_jsonb(m))::text FROM "user" u JOIN member m ON m.user_id=u.id WHERE u.id=$1 AND m.workspace_id=$2`, userID, testWorkspaceID).Scan(&state)
				return state
			}
			before := snapshot()
			if got := call("member"); got.Status != "already_member" || got.Role != "admin" {
				t.Fatalf("retry: %+v", got)
			}
			if after := snapshot(); after != before {
				t.Fatalf("already-member retry mutated rows: before=%s after=%s", before, after)
			}
		})
	}
}

func TestProvisionMembersAlreadyMemberDoesNotCompleteOnboarding(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)
	email := "provision-already-member-not-onboarded@example.test"
	userID := dbfx.User(t, "Original", email)
	dbfx.Exec(t, `UPDATE "user" SET onboarded_at=NULL, updated_at='2001-01-01'::timestamptz WHERE id=$1`, userID)
	dbfx.Insert(t, "member", testutil.Cols{"workspace_id": testWorkspaceID, "user_id": userID, "role": "admin"})
	var before string
	dbfx.QueryRow(t, `SELECT to_jsonb(u)::text FROM "user" u WHERE id=$1`, userID).Scan(&before)
	name := "Replacement"
	var out ProvisionMembersResponse
	testutil.Call(t, testHandler.ProvisionMembers, provisionMembersRequest([]ProvisionMemberEntry{{Email: email, Role: "member", Name: &name}})).Want(200).JSON(&out)
	if len(out.Results) != 1 || out.Results[0].Status != "already_member" || out.Results[0].Role != "admin" {
		t.Fatalf("outcome: %+v", out)
	}
	var after string
	dbfx.QueryRow(t, `SELECT to_jsonb(u)::text FROM "user" u WHERE id=$1`, userID).Scan(&after)
	if after != before {
		t.Fatalf("already-member profile changed: before=%s after=%s", before, after)
	}
}
