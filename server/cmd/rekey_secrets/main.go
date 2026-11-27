// rekey_secrets re-seals integration secrets stored under a replaced key
// (MULTICA_*_SECRET_KEY_PREVIOUS) with the current MULTICA_*_SECRET_KEY.
//
// Without --apply it only reports, and exits non-zero while any value is
// still sealed under a previous key or opens under no key. Run it with the
// same key env as the servers, after every server runs with both keys.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/secretrekey"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

const advisoryLockName = "rekey_secrets"

func main() {
	logger.Init()
	if err := run(); err != nil {
		slog.Error("rekey secrets failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	apply := flag.Bool("apply", false, "re-seal values found under a previous key (default: report only)")
	flag.Parse()

	boxes, err := loadBoxes(secretrekey.Targets)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire advisory-lock connection: %w", err)
	}
	defer lockConn.Release()
	var locked bool
	if err := lockConn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1, 0))`, advisoryLockName).Scan(&locked); err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("another rekey_secrets run holds the lock")
	}
	defer func() {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, advisoryLockName)
	}()

	reports, runErr := secretrekey.Run(ctx, pool, secretrekey.Targets, boxes, *apply)
	var previous, unreadable int
	for _, r := range reports {
		if r.Skipped != "" {
			slog.Info("skipped", "target", r.Target.Name(), "reason", r.Skipped)
			continue
		}
		previous += r.Previous
		unreadable += r.Unreadable
		attrs := []any{"target", r.Target.Name(), "current", r.Current, "unreadable", r.Unreadable}
		if *apply {
			attrs = append(attrs, "resealed", r.Previous)
		} else {
			attrs = append(attrs, "pending", r.Previous)
		}
		if r.Unreadable > 0 {
			attrs = append(attrs, "unreadable_keys", formatKeys(r.UnreadableKeys))
		}
		slog.Info("scanned", attrs...)
	}
	if runErr != nil {
		return runErr
	}
	if *apply {
		slog.Info("rekey complete", "resealed", previous, "unreadable", unreadable)
		previous = 0
	} else {
		slog.Info("rekey report", "pending", previous, "unreadable", unreadable)
	}
	if previous > 0 || unreadable > 0 {
		return fmt.Errorf("%d values still sealed under a previous key, %d open under no configured key", previous, unreadable)
	}
	return nil
}

// loadBoxes builds one Box per key env that is set. An unset env skips its
// targets; a set but invalid current or previous key is an error.
func loadBoxes(targets []secretrekey.Target) (map[string]*secretbox.Box, error) {
	boxes := map[string]*secretbox.Box{}
	for _, t := range targets {
		if _, done := boxes[t.KeyEnv]; done || os.Getenv(t.KeyEnv) == "" {
			continue
		}
		key, err := secretbox.LoadKey(t.KeyEnv)
		if err != nil {
			return nil, err
		}
		prev, err := secretbox.LoadPreviousKey(t.KeyEnv)
		if err != nil {
			return nil, err
		}
		var previous [][]byte
		if prev != nil {
			previous = append(previous, prev)
		}
		box, err := secretbox.New(key, previous...)
		if err != nil {
			return nil, err
		}
		boxes[t.KeyEnv] = box
	}
	return boxes, nil
}

func formatKeys(keys [][]string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = strings.Join(k, "/")
	}
	return strings.Join(parts, ",")
}
