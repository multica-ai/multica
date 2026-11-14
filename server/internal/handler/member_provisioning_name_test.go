package handler

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestProvisionMembersOptionalNameAndPreservation(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)
	email := "provision-name@example.test"
	cleanupProvisionedEmails(t, email)
	call := func(name any, role string) ProvisionMemberResult {
		t.Helper()
		req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{"entries": []any{map[string]any{"email": email, "name": name, "role": role}}}), "id", testWorkspaceID)
		var out ProvisionMembersResponse
		testutil.Call(t, testHandler.ProvisionMembers, req).Want(200).JSON(&out)
		return out.Results[0]
	}
	if got := call("  Display 名  ", "member"); got.Status != "created" {
		t.Fatalf("create: %+v", got)
	}
	var name string
	var before time.Time
	dbfx.QueryRow(t, `SELECT name, updated_at FROM "user" WHERE email=$1`, email).Scan(&name, &before)
	if name != "Display 名" {
		t.Fatalf("name=%q", name)
	}
	if got := call("Replacement", "admin"); got.Status != "already_member" || got.Role != "member" {
		t.Fatalf("retry: %+v", got)
	}
	var after time.Time
	var role string
	dbfx.QueryRow(t, `SELECT u.name, u.updated_at, m.role FROM "user" u JOIN member m ON u.id=m.user_id WHERE email=$1 AND workspace_id=$2`, email, testWorkspaceID).Scan(&name, &after, &role)
	if name != "Display 名" || !before.Equal(after) || role != "member" {
		t.Fatalf("mutated existing user: %q %s %s %q", name, before, after, role)
	}
}

func TestProvisionMembersExistingUserAddedWithoutProfileMutation(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)
	email := "provision-added@example.test"
	userID := dbfx.User(t, "Original", email)
	dbfx.Cleanup(t, `DELETE FROM member WHERE user_id=$1`, userID)
	var before time.Time
	dbfx.QueryRow(t, `SELECT updated_at FROM "user" WHERE id=$1`, userID).Scan(&before)
	req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{"entries": []any{map[string]any{"email": email, "name": "Replacement"}}}), "id", testWorkspaceID)
	var out ProvisionMembersResponse
	testutil.Call(t, testHandler.ProvisionMembers, req).Want(200).JSON(&out)
	if out.Results[0].Status != "added" {
		t.Fatalf("result=%+v", out)
	}
	var name string
	var after time.Time
	var onboardingRecorded bool
	dbfx.QueryRow(t, `SELECT name, updated_at, onboarded_at IS NOT NULL AND onboarded_at = updated_at FROM "user" WHERE id=$1`, userID).Scan(&name, &after, &onboardingRecorded)
	if name != "Original" || !onboardingRecorded || after.Before(before) {
		t.Fatal("existing name changed or attachment onboarding bookkeeping is incorrect")
	}
}

func TestProvisionMembersNameValidation(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)
	email := "invalid-name@example.test"
	cleanupProvisionedEmails(t, email)
	for _, name := range []any{"", " \t ", 42} {
		t.Run(fmt.Sprintf("%v", name), func(t *testing.T) {
			req := withURLParam(newRequest(http.MethodPost, "/", map[string]any{"entries": []any{map[string]any{"email": email, "name": name}}}), "id", testWorkspaceID)
			var out ProvisionMembersResponse
			if _, ok := name.(string); !ok {
				testutil.Call(t, testHandler.ProvisionMembers, req).Want(400)
				return
			}
			testutil.Call(t, testHandler.ProvisionMembers, req).Want(200).JSON(&out)
			if out.Results[0].Status != "invalid" {
				t.Fatalf("accepted invalid name: %+v", out)
			}
		})
	}
}
