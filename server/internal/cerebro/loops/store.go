package loops

import "github.com/jackc/pgx/v5/pgxpool"

// Store persists Chain v2 phase, block, and step state.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}
