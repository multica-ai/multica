package auth

import (
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestUserMayAuthenticate(t *testing.T) {
	t.Parallel()
	if !UserMayAuthenticate(db.User{AccountStatus: AccountStatusActive}) {
		t.Fatal("active should authenticate")
	}
	if !UserMayAuthenticate(db.User{AccountStatus: ""}) {
		t.Fatal("empty status should treat as active (pre-migration safety)")
	}
	if UserMayAuthenticate(db.User{AccountStatus: AccountStatusSuspended}) {
		t.Fatal("suspended must not authenticate")
	}
}
