package dbstartup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSettingsFromEnv(t *testing.T) {
	t.Setenv(StartupTimeoutEnv, "45s")
	t.Setenv(ConnectTimeoutEnv, "2s")

	got := SettingsFromEnv()
	if got.StartupTimeout != 45*time.Second {
		t.Fatalf("StartupTimeout = %s, want 45s", got.StartupTimeout)
	}
	if got.ConnectTimeout != 2*time.Second {
		t.Fatalf("ConnectTimeout = %s, want 2s", got.ConnectTimeout)
	}
}

func TestSettingsFromEnvAllowsFailFastButKeepsConnectionTimeoutBounded(t *testing.T) {
	t.Setenv(StartupTimeoutEnv, "0")
	t.Setenv(ConnectTimeoutEnv, "0")

	got := SettingsFromEnv()
	if got.StartupTimeout != 0 {
		t.Fatalf("StartupTimeout = %s, want 0", got.StartupTimeout)
	}
	if got.ConnectTimeout != DefaultConnectTimeout {
		t.Fatalf("ConnectTimeout = %s, want %s", got.ConnectTimeout, DefaultConnectTimeout)
	}
}

func TestSettingsFromEnvSharesEntrypointStartupBudget(t *testing.T) {
	current := time.Unix(2_000_000_000, 0)
	t.Setenv(StartupTimeoutEnv, "3m")
	t.Setenv(startupStartedAtEnv, fmt.Sprintf("%d", current.Unix()))

	settings := settingsFromEnv(func() time.Time { return current })
	if got := settings.RetryOptions().Timeout; got != 3*time.Minute {
		t.Fatalf("initial retry timeout = %s, want 3m", got)
	}

	current = current.Add(70 * time.Second)
	if got := settings.RetryOptions().Timeout; got != 110*time.Second {
		t.Fatalf("remaining retry timeout = %s, want 1m50s", got)
	}

	current = current.Add(2 * time.Minute)
	if got := settings.RetryOptions().Timeout; got != 0 {
		t.Fatalf("expired retry timeout = %s, want 0", got)
	}
}

func TestParsePoolConfigAppliesConnectTimeout(t *testing.T) {
	cfg, err := ParsePoolConfig("postgres://user:pass@localhost:5432/db?sslmode=disable", 7*time.Second)
	if err != nil {
		t.Fatalf("ParsePoolConfig: %v", err)
	}
	if cfg.ConnConfig.ConnectTimeout != 7*time.Second {
		t.Fatalf("ConnectTimeout = %s, want 7s", cfg.ConnConfig.ConnectTimeout)
	}
}

func TestRetryRecoversWithCappedExponentialBackoff(t *testing.T) {
	var attempts int
	var delays []time.Duration
	err := Retry(context.Background(), RetryOptions{
		Timeout:        time.Minute,
		InitialBackoff: time.Second,
		MaxBackoff:     5 * time.Second,
		Jitter:         func(delay time.Duration) time.Duration { return delay },
		Sleep: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
	}, func(context.Context) error {
		attempts++
		if attempts < 5 {
			return errors.New("database unavailable")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if attempts != 5 {
		t.Fatalf("attempts = %d, want 5", attempts)
	}
	wantDelays := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second}
	if !reflect.DeepEqual(delays, wantDelays) {
		t.Fatalf("delays = %v, want %v", delays, wantDelays)
	}
}

func TestRetryStopsWhenParentIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var attempts int
	err := Retry(ctx, RetryOptions{
		Timeout: time.Minute,
		Jitter:  func(delay time.Duration) time.Duration { return delay },
		Sleep: func(context.Context, time.Duration) error {
			cancel()
			return context.Canceled
		},
	}, func(context.Context) error {
		attempts++
		return errors.New("database unavailable")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Retry error = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryStopsAtTimeout(t *testing.T) {
	var attempts int
	err := Retry(context.Background(), RetryOptions{
		Timeout: 5 * time.Millisecond,
		Jitter:  func(delay time.Duration) time.Duration { return delay },
	}, func(context.Context) error {
		attempts++
		return errors.New("database unavailable")
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Retry error = %v, want context.DeadlineExceeded", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryTimeoutPreservesLastError(t *testing.T) {
	wantErr := &pgconn.PgError{Code: "57P03", Message: "database is starting"}
	err := Retry(context.Background(), RetryOptions{
		Timeout: time.Minute,
		Sleep: func(context.Context, time.Duration) error {
			return context.DeadlineExceeded
		},
	}, func(context.Context) error {
		return wantErr
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Retry error = %v, want context.DeadlineExceeded", err)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Retry error = %v, want wrapped last error %v", err, wantErr)
	}
}

func TestRetryWithZeroTimeoutAttemptsOnce(t *testing.T) {
	wantErr := errors.New("database unavailable")
	var attempts int
	err := Retry(context.Background(), RetryOptions{}, func(context.Context) error {
		attempts++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Retry error = %v, want %v", err, wantErr)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryStopsImmediatelyForPermanentError(t *testing.T) {
	wantErr := &pgconn.PgError{Code: "42601", Message: "syntax error"}
	var attempts int
	err := Retry(context.Background(), RetryOptions{
		Timeout:     time.Minute,
		ShouldRetry: IsTransientDatabaseError,
	}, func(context.Context) error {
		attempts++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Retry error = %v, want %v", err, wantErr)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryCanLeaveLongOperationOnParentContext(t *testing.T) {
	err := Retry(context.Background(), RetryOptions{
		Timeout:                   time.Minute,
		AllowOperationPastTimeout: true,
	}, func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); ok {
			return errors.New("operation unexpectedly inherited retry deadline")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
}

func TestIsTransientDatabaseError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "connection exception", err: &pgconn.PgError{Code: "08006"}, want: true},
		{name: "database starting", err: &pgconn.PgError{Code: "57P03"}, want: true},
		{name: "too many connections", err: &pgconn.PgError{Code: "53300"}, want: true},
		{name: "syntax error", err: &pgconn.PgError{Code: "42601"}, want: false},
		{name: "connection deadline", err: context.DeadlineExceeded, want: true},
		{name: "cancelled", err: context.Canceled, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTransientDatabaseError(tt.err); got != tt.want {
				t.Fatalf("IsTransientDatabaseError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestRetryRetriesConnectionHandshakeTimeout(t *testing.T) {
	address := startStalledDatabaseListener(t)
	pool, err := NewPool(
		context.Background(),
		fmt.Sprintf("postgres://user:pass@%s/db?sslmode=disable", address),
		30*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	var attempts int
	err = Retry(context.Background(), RetryOptions{
		Timeout:        time.Second,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		Jitter:         func(delay time.Duration) time.Duration { return delay },
		ShouldRetry:    IsTransientDatabaseError,
	}, func(ctx context.Context) error {
		attempts++
		return pool.Ping(ctx)
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Retry error = %v, want context.DeadlineExceeded", err)
	}
	if attempts < 2 {
		t.Fatalf("attempts = %d, want at least 2 for a connection handshake timeout", attempts)
	}
}

func TestIsTransientDatabaseErrorFromRefusedConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	pool, err := NewPool(
		context.Background(),
		fmt.Sprintf("postgres://user:pass@%s/db?sslmode=disable", address),
		100*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	err = pool.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping unexpectedly succeeded")
	}
	if !IsTransientDatabaseError(err) {
		t.Fatalf("IsTransientDatabaseError(%v) = false, want true", err)
	}
}

func startStalledDatabaseListener(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var (
		mu          sync.Mutex
		connections []net.Conn
	)
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
		}
	}()

	t.Cleanup(func() {
		_ = listener.Close()
		<-acceptDone
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range connections {
			_ = conn.Close()
		}
	})

	return listener.Addr().String()
}
