package telegram

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool backs every test that exercises reply-delivery ownership.
//
// Those tests run the real queries rather than a hand-written stand-in on
// purpose. The first version of this feature shipped with an in-memory fake of
// the ownership table, and the fake quietly disagreed with the SQL about one
// column — enough for the whole suite to pass while the final answer never
// reached Telegram. A fake of a state machine is a second implementation of
// it, and the two drift.
//
// Without a database the suite exits green rather than red, the same contract
// the other DB-backed packages here follow.
var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping telegram delivery tests: could not connect: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping telegram delivery tests: database not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}
	testPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}
