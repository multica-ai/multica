package dbstartup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	StartupTimeoutEnv = "MULTICA_DATABASE_STARTUP_TIMEOUT"
	ConnectTimeoutEnv = "MULTICA_DATABASE_CONNECT_TIMEOUT"

	DefaultStartupTimeout = 3 * time.Minute
	DefaultConnectTimeout = 5 * time.Second
	defaultInitialBackoff = time.Second
	defaultMaxBackoff     = 30 * time.Second
)

// Settings bounds database startup retries and individual connection attempts.
type Settings struct {
	StartupTimeout time.Duration
	ConnectTimeout time.Duration
}

// SettingsFromEnv reads the shared startup settings used by the migrator and
// API server. A zero startup timeout preserves an explicit fail-fast option.
func SettingsFromEnv() Settings {
	return Settings{
		StartupTimeout: envDuration(StartupTimeoutEnv, DefaultStartupTimeout, true),
		ConnectTimeout: envDuration(ConnectTimeoutEnv, DefaultConnectTimeout, false),
	}
}

// ParsePoolConfig applies the same bounded connection timeout to every startup
// pool while leaving each caller free to configure its own pool sizing.
func ParsePoolConfig(databaseURL string, connectTimeout time.Duration) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if connectTimeout <= 0 {
		connectTimeout = DefaultConnectTimeout
	}
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	return cfg, nil
}

// NewPool creates a startup pool with the shared connection timeout.
func NewPool(ctx context.Context, databaseURL string, connectTimeout time.Duration) (*pgxpool.Pool, error) {
	cfg, err := ParsePoolConfig(databaseURL, connectTimeout)
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}

// RetryEvent describes a failed attempt before the next backoff begins.
type RetryEvent struct {
	Attempt int
	Delay   time.Duration
	Err     error
}

// RetryOptions controls a bounded exponential-backoff loop. Jitter and Sleep
// are injectable so tests can cover the full sequence without wall-clock waits.
type RetryOptions struct {
	Timeout        time.Duration
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Jitter         func(time.Duration) time.Duration
	Sleep          func(context.Context, time.Duration) error
	ShouldRetry    func(error) bool
	// AllowOperationPastTimeout keeps a long-running operation on the parent
	// context while the timeout still bounds subsequent retries and backoffs.
	AllowOperationPastTimeout bool
	OnRetry                   func(RetryEvent)
}

// RetryOptions returns production defaults for database startup retries.
func (s Settings) RetryOptions() RetryOptions {
	return RetryOptions{
		Timeout:        s.StartupTimeout,
		InitialBackoff: defaultInitialBackoff,
		MaxBackoff:     defaultMaxBackoff,
	}
}

// Retry runs operation immediately and then retries transient failures with
// jittered exponential backoff until it succeeds, the parent is cancelled, or
// the configured startup budget expires. Timeout zero performs one attempt.
func Retry(ctx context.Context, opts RetryOptions, operation func(context.Context) error) error {
	if operation == nil {
		return errors.New("database startup operation is nil")
	}
	if opts.Timeout <= 0 {
		return operation(ctx)
	}

	retryCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	backoff := opts.InitialBackoff
	if backoff <= 0 {
		backoff = defaultInitialBackoff
	}
	maxBackoff := opts.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = defaultMaxBackoff
	}
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	jitter := opts.Jitter
	if jitter == nil {
		jitter = jitterDelay
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = sleepContext
	}

	var lastErr error
	for attempt := 1; ; attempt++ {
		if err := retryCtx.Err(); err != nil {
			return retryStopped(attempt-1, err, lastErr)
		}

		operationCtx := retryCtx
		if opts.AllowOperationPastTimeout {
			operationCtx = ctx
		}
		err := operation(operationCtx)
		if err == nil {
			return nil
		}
		lastErr = err
		if err := retryCtx.Err(); err != nil {
			return retryStopped(attempt, err, lastErr)
		}
		if opts.ShouldRetry != nil && !opts.ShouldRetry(err) {
			return err
		}

		delay := jitter(backoff)
		if delay < 0 {
			delay = 0
		}
		if opts.OnRetry != nil {
			opts.OnRetry(RetryEvent{Attempt: attempt, Delay: delay, Err: err})
		}
		if err := sleep(retryCtx, delay); err != nil {
			cause := retryCtx.Err()
			if cause == nil {
				cause = err
			}
			return retryStopped(attempt, cause, lastErr)
		}

		if backoff < maxBackoff {
			backoff *= 2
			if backoff <= 0 || backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func retryStopped(attempts int, cause, lastErr error) error {
	if lastErr == nil {
		return fmt.Errorf("database startup stopped before an attempt completed: %w", cause)
	}
	return fmt.Errorf("database startup failed after %d attempt(s): %w (last error: %v)", attempts, cause, lastErr)
}

func jitterDelay(delay time.Duration) time.Duration {
	// Keep each delay within 80-100% so concurrent pods do not reconnect in
	// lockstep and the configured maximum remains a hard upper bound.
	return time.Duration(float64(delay) * (0.8 + rand.Float64()*0.2))
}

// IsTransientDatabaseError limits whole-migration retries to connection and
// availability failures. SQL and migration-definition errors fail immediately
// instead of consuming the startup retry budget.
func IsTransientDatabaseError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return strings.HasPrefix(pgErr.Code, "08") ||
			pgErr.Code == "57P01" || // admin_shutdown
			pgErr.Code == "57P02" || // crash_shutdown
			pgErr.Code == "57P03" || // cannot_connect_now
			pgErr.Code == "53300" // too_many_connections
	}

	var connectErr *pgconn.ConnectError
	if errors.As(err, &connectErr) {
		return true
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) {
		return true
	}
	return pgconn.SafeToRetry(err) || pgconn.Timeout(err)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func envDuration(name string, def time.Duration, allowZero bool) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	value, err := time.ParseDuration(raw)
	valid := err == nil && (value > 0 || allowZero && value == 0)
	if !valid {
		slog.Warn("invalid env var, using default",
			"name", name,
			"value", raw,
			"default", def.String(),
			"error", err,
		)
		return def
	}
	return value
}
