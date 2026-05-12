package db

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// TxStarter abstracts transaction creation and is satisfied by *pgxpool.Pool.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}
