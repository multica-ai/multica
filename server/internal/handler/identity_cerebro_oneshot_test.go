package handler

// CEREBRO-PATCH(auth-identity-oneshot-test): FIR-2523 test pinning new-user detection (isNew) that gates the auto-provisioning hook.

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// TestAutoProvision_OneShotOnUserCreation documents the behavioral edge that
// the FIR-2523 auto-membership hook fires only on the user-creation edge.
//
// In VerifyCode / GoogleLogin the hook is guarded by `if isNew`
// (auth.go:413 / 588). findOrCreateUser returns isNew=true ONLY on the call
// that inserts the user row. Every subsequent login for the same email
// returns isNew=false, so provisionMembershipForNewUser is never invoked
// again. There is no self-healing retry: a user whose first login predates
// the workspace's signup-domain config (or whose first provisioning was
// skipped/failed) stays un-provisioned forever until an admin invites them
// manually or re-toggles the setting.
//
// This test pins that behavior so a future "re-provision returning users"
// fix is a deliberate, visible change.
func TestAutoProvision_OneShotOnUserCreation(t *testing.T) {
	if testHandler == nil {
		t.Skip("skip: no test handler / database")
	}
	ctx := context.Background()
	email := fmt.Sprintf("oneshot-%d@firtal.com", os.Getpid())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, email)
	})

	// First login: the user does not exist -> isNew must be true, which is
	// the ONLY condition under which the provisioning hook runs.
	_, isNew1, err := testHandler.findOrCreateUser(ctx, email)
	if err != nil {
		t.Fatalf("first findOrCreateUser: %v", err)
	}
	if !isNew1 {
		t.Fatalf("first login: expected isNew=true (hook would fire), got false")
	}

	// Second login for the SAME email: user now exists -> isNew=false, so the
	// `if isNew` guard skips provisioning. This is the gap: a returning user
	// is never re-provisioned, even if the workspace signup domains were
	// configured only after the first login.
	_, isNew2, err := testHandler.findOrCreateUser(ctx, email)
	if err != nil {
		t.Fatalf("second findOrCreateUser: %v", err)
	}
	if isNew2 {
		t.Fatalf("second login: expected isNew=false (hook is skipped), got true")
	}
}
