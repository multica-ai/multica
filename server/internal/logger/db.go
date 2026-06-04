package logger

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewDBPool creates a pgx pool with the application's SQL tracer attached.
func NewDBPool(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.Tracer = NewSQLTracer(ConfigFromEnv())
	return pgxpool.NewWithConfig(ctx, cfg)
}
