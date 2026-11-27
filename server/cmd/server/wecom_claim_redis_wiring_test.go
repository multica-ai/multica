package main

import (
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// The WeCom claim store is the one Redis user here whose callers spend budgets
// they have promised to keep: DedupeStore.ClaimBudget sizes the dispatcher's
// outcome grace, and a shutdown drain gives its whole sequence of round trips
// one DrainBudget. Neither is real unless the client applies those deadlines
// to the wire, and go-redis does not by default — it waits out ReadTimeout and
// ignores the context (baseClient.context).
//
// So the claim store gets its own client with the flag set, and the shared
// relay client keeps the default: turning it on there would change the timeout
// behaviour of every realtime publish that runs through it.
//
// This pins the two constructors, not main()'s choice between them; that the
// dedupe store is handed the right one is guarded at runtime instead, by the
// warning NewRedisDedupe logs for a client that ignores deadlines.
func TestClaimRedisClientHonoursContextDeadlines(t *testing.T) {
	base := &redis.UniversalOptions{Addrs: []string{"localhost:6379"}, ClientName: "multica", ReadTimeout: 3 * time.Second}

	claim := newClaimRedisClient(base, "wecom-claim")
	defer claim.Close()
	claimOptions := claim.(*redis.Client).Options()
	if !claimOptions.ContextTimeoutEnabled {
		t.Fatal("the claim client ignores context deadlines: ClaimBudget and the drain budget " +
			"would be numbers nothing enforces")
	}

	// The client it is derived from keeps go-redis's default, so this stays a
	// scoped change rather than a fleet-wide one.
	shared := newNamedRedisClient(base, "realtime-write")
	defer shared.Close()
	sharedOptions := shared.(*redis.Client).Options()
	if sharedOptions.ContextTimeoutEnabled {
		t.Fatal("the shared relay client now enforces context deadlines too: that is a wider " +
			"blast radius than the claim store asked for")
	}

	// Everything else about the client is still the deployment's.
	if got := claimOptions.ReadTimeout; got != base.ReadTimeout {
		t.Fatalf("ReadTimeout = %v, want the configured %v", got, base.ReadTimeout)
	}
	if got := claimOptions.ClientName; got != "multica:wecom-claim" {
		t.Fatalf("ClientName = %q, want the suffixed name", got)
	}
	if base.ContextTimeoutEnabled {
		t.Fatal("newClaimRedisClient mutated the options it was handed")
	}
}
