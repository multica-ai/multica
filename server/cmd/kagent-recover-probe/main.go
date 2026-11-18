package main

// kagent-recover-probe replays a Multica Kagent lifecycle journal against the
// live cluster, reports every agent the journal durably owns, and — only when
// the operator explicitly confirms remote drain — deletes them with the same
// UID/resourceVersion preconditions the daemon uses.
//
// Usage:
//
//	MULTICA_KAGENT_RECOVER_JOURNAL_DIR=/path/to/journal \
//	  go run ./cmd/kagent-recover-probe            # report only
//	DRAIN_CONFIRMED=1 MULTICA_KAGENT_RECOVER_JOURNAL_DIR=... \
//	  go run ./cmd/kagent-recover-probe            # delete owned agents
//
// The drain assertion is the caller's: DRAIN_CONFIRMED=1 attests that every
// attempt in this journal reached an observed terminal state. It never clears
// journal records and never deletes a resource the journal does not own.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	k "github.com/multica-ai/multica/server/pkg/kagent"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func main() {
	dir := os.Getenv("MULTICA_KAGENT_RECOVER_JOURNAL_DIR")
	if dir == "" {
		panic("MULTICA_KAGENT_RECOVER_JOURNAL_DIR required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	store, e := k.NewKubernetesStore(k.HTTPConfig{AllowInsecureLoopback: true, Endpoint: "http://127.0.0.1:18001"}, []string{"kagent"})
	check(e)
	rec, e := k.New(store, k.Config{AllowedNamespaces: []string{"kagent"}})
	check(e)
	journal, e := k.NewJournal(dir)
	check(e)
	life, e := k.NewLifecycle(rec, journal)
	check(e)
	states, err := life.Recover(ctx)
	if err != nil {
		fmt.Println("recover=", err)
	}
	if len(states) == 0 {
		fmt.Println("owned_agents=0")
		return
	}
	for _, s := range states {
		fmt.Printf("owned agent=%s name=%s namespace=%s ready=%t revision=%s uid=%s rv=%s\n",
			s.AgentID, s.Name, s.Namespace, s.Ready, s.ObservedRevision, s.UID, s.ResourceVersion)
	}
	if os.Getenv("DRAIN_CONFIRMED") != "1" {
		fmt.Println("drain_confirmed=false; reporting only")
		return
	}
	var firstErr error
	for _, s := range states {
		for {
			err := life.Delete(ctx, s.AgentID, true)
			if err == nil {
				fmt.Printf("deleted agent=%s verified_absent=true\n", s.AgentID)
				break
			}
			if !errors.Is(err, k.ErrDeletionPending) {
				check(err)
			}
			select {
			case <-ctx.Done():
				panic("deletion confirmation timed out")
			case <-time.After(time.Second):
			}
		}
	}
	if firstErr != nil {
		panic(firstErr)
	}
}
