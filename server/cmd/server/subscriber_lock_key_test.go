package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/util"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestLockSubscriberWrites_KeyedOnUUIDValueNotSpelling covers MUL-5483 review
// round 8, finding 1.
//
// The lock key used to be hashtext() over the caller's raw string. PostgreSQL
// treats 'AB…' and 'ab…' as the SAME uuid — every membership query and every
// issue_subscriber row matches either spelling — but hashtext does not, so an
// uppercase request took a DIFFERENT lock from the canonical holder and the two
// paths stopped excluding each other. The serialization boundary silently had a
// hole in it exactly when two clients disagreed about case.
//
// Casting to uuid before hashing makes the key a function of the VALUE. This
// test pins that: a competing transaction holds the lock under the canonical
// lowercase spelling, and an uppercase acquisition of the same UUID must block.
func TestLockSubscriberWrites_KeyedOnUUIDValueNotSpelling(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	// Holder takes the lock with the canonical (lowercase) spelling.
	holder, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer holder.Rollback(context.Background())

	if err := queries.WithTx(holder).LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
		UserID:      util.MustParseUUID(testUserID),
	}); err != nil {
		t.Fatalf("holder lock: %v", err)
	}

	// A second transaction asks for the same UUIDs spelled in UPPERCASE. This
	// is what a client sending a non-canonical UUID produces.
	upperWorkspace := strings.ToUpper(testWorkspaceID)
	upperUser := strings.ToUpper(testUserID)
	if upperWorkspace == testWorkspaceID || upperUser == testUserID {
		t.Fatal("fixture UUIDs have no letters to upper-case; this test cannot detect the bug")
	}

	blocked := make(chan error, 1)
	go func() {
		waiter, err := testPool.Begin(context.Background())
		if err != nil {
			blocked <- err
			return
		}
		defer waiter.Rollback(context.Background())
		blocked <- queries.WithTx(waiter).LockSubscriberWrites(
			context.Background(),
			db.LockSubscriberWritesParams{
				WorkspaceID: util.MustParseUUID(upperWorkspace),
				UserID:      util.MustParseUUID(upperUser),
			},
		)
	}()

	select {
	case err := <-blocked:
		t.Fatalf("an uppercase-spelled UUID acquired the lock while the canonical spelling held it "+
			"(err=%v): the two spellings are the same UUID everywhere else, so this reopens the race "+
			"the lock exists to close", err)
	case <-time.After(400 * time.Millisecond):
		// Correctly blocked.
	}

	if err := holder.Commit(context.Background()); err != nil {
		t.Fatalf("commit holder: %v", err)
	}
	select {
	case err := <-blocked:
		if err != nil {
			t.Fatalf("waiter failed after release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("waiter never acquired the lock after the holder committed")
	}
}
