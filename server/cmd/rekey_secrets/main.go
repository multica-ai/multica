// rekey_secrets re-seals integration secrets stored under a replaced key
// (MULTICA_*_SECRET_KEY_PREVIOUS) with the current MULTICA_*_SECRET_KEY.
//
// --integrations names what to scan (comma-separated, or "all"); each named
// integration's current key must be set. Without --apply it only reports,
// and exits non-zero while any value is still sealed under a previous key or
// opens under no key. DATABASE_URL is required. Run it with the same key env
// as the servers, after every server runs with both keys.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
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
	integrations := flag.String("integrations", "", "comma-separated integrations to scan, or all: "+integrationNames())
	flag.Parse()

	// No local default: a report against the wrong database would pass the
	// verify gate for a deployment it never scanned.
	// A blank value would let pgx fall back to PGHOST/PGDATABASE.
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL must be set to the target deployment's database")
	}
	targets, keyEnvs, err := secretrekey.Select(strings.Split(*integrations, ","))
	if err != nil {
		return fmt.Errorf("--integrations: %w", err)
	}
	boxes, err := loadBoxes(keyEnvs)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	// The lock holds its own connection outside the pool, so the scan never
	// waits on it even with pool_max_conns=1.
	lockConn, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
	if err != nil {
		return fmt.Errorf("open advisory-lock connection: %w", err)
	}
	defer func() { _ = lockConn.Close(context.Background()) }()
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

	reports, runErr := secretrekey.Run(ctx, pool, targets, boxes, *apply)
	var previous, unreadable, scanned int
	for _, r := range reports {
		if r.Skipped != "" {
			slog.Info("skipped", "target", r.Target.Name(), "reason", r.Skipped)
			continue
		}
		scanned++
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
	if scanned == 0 {
		return fmt.Errorf("no target was scanned")
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

// loadBoxes builds one Box per selected key env. A missing or invalid
// current key, or an invalid previous key, is an error.
func loadBoxes(keyEnvs []string) (map[string]*secretbox.Box, error) {
	boxes := map[string]*secretbox.Box{}
	for _, env := range keyEnvs {
		key, err := secretbox.LoadKey(env)
		if err != nil {
			return nil, err
		}
		prev, err := secretbox.LoadPreviousKey(env)
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
		boxes[env] = box
	}
	return boxes, nil
}

func integrationNames() string {
	names := make([]string, 0, len(secretrekey.Integrations))
	for name := range secretrekey.Integrations {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func formatKeys(keys [][]string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = strings.Join(k, "/")
	}
	return strings.Join(parts, ",")
}
