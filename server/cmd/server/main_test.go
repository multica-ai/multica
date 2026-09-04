package main

import (
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisClientName(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		suffix   string
		want     string
	}{
		{"empty_suffix_returns_existing", "multica-api:store", "", "multica-api:store"},
		{"empty_existing_uses_default_prefix", "", "store", "multica-api:store"},
		{"both_set_joins_with_colon", "custom", "store", "custom:store"},
		{"empty_both_returns_empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redisClientName(tt.existing, tt.suffix)
			if got != tt.want {
				t.Errorf("redisClientName(%q, %q) = %q, want %q", tt.existing, tt.suffix, got, tt.want)
			}
		})
	}
}

func TestChannelLeaseRedisURLFromEnvPrefersDedicatedInstance(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://shared:6379/0")
	t.Setenv("CHANNEL_WS_LEASE_REDIS_URL", "redis://leases:6379/0")
	if got := channelLeaseRedisURLFromEnv(); got != "redis://leases:6379/0" {
		t.Fatalf("channel lease Redis URL = %q", got)
	}
}

func TestChannelLeaseRedisURLFromEnvFallsBackToSharedRedis(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://shared:6379/0")
	t.Setenv("CHANNEL_WS_LEASE_REDIS_URL", "")
	if got := channelLeaseRedisURLFromEnv(); got != "redis://shared:6379/0" {
		t.Fatalf("channel lease Redis URL = %q", got)
	}
}

func TestRealtimeRelayRedisURLFromEnvPrefersDedicatedInstance(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://shared:6379/0")
	t.Setenv("REALTIME_RELAY_REDIS_URL", " redis://relay:6379/0 ")
	if got := realtimeRelayRedisURLFromEnv(); got != "redis://relay:6379/0" {
		t.Fatalf("realtime relay Redis URL = %q", got)
	}
}

func TestRealtimeRelayRedisURLFromEnvFallsBackToSharedRedis(t *testing.T) {
	t.Setenv("REDIS_URL", " redis://shared:6379/0 ")
	t.Setenv("REALTIME_RELAY_REDIS_URL", "")
	if got := realtimeRelayRedisURLFromEnv(); got != "redis://shared:6379/0" {
		t.Fatalf("realtime relay Redis URL = %q", got)
	}
}

func TestShardedRelayConfigFromEnvDerivesSafeRetention(t *testing.T) {
	t.Setenv("REALTIME_RELAY_STREAM_MAXLEN", "")
	t.Setenv("REALTIME_RELAY_REPLAY_GRACE", "20m")
	t.Setenv("REALTIME_RELAY_TRIM_HORIZON", "")
	t.Setenv("REALTIME_RELAY_STREAM_TTL", "")

	cfg := shardedRelayConfigFromEnv()
	if cfg.StreamMaxLen != 2000 {
		t.Fatalf("stream max len = %d, want 2000", cfg.StreamMaxLen)
	}
	if cfg.TrimHorizon != 40*time.Minute {
		t.Fatalf("trim horizon = %s, want 40m", cfg.TrimHorizon)
	}
	if cfg.StreamTTL != 60*time.Minute {
		t.Fatalf("stream TTL = %s, want 60m", cfg.StreamTTL)
	}
	if cfg.StreamTTLEnabled {
		t.Fatal("stream TTL must remain disabled until the staged rollout flag is enabled")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("derived retention config is invalid: %v", err)
	}
}

func TestShardedRelayConfigFromEnvEnablesTTLExplicitly(t *testing.T) {
	t.Setenv("REALTIME_RELAY_STREAM_TTL_ENABLED", "true")

	cfg := shardedRelayConfigFromEnv()
	if !cfg.StreamTTLEnabled {
		t.Fatal("stream TTL flag was not applied")
	}
	retention := cfg.RetentionConfig()
	if !retention.StreamTTLEnabled || retention.StreamTTL != cfg.StreamTTL {
		t.Fatalf("legacy retention config diverged from sharded config: %+v", retention)
	}
}

func TestShardedRelayConfigFromEnvNormalizesUnsafeOverrides(t *testing.T) {
	t.Setenv("REALTIME_RELAY_REPLAY_GRACE", "5m")
	t.Setenv("REALTIME_RELAY_TRIM_HORIZON", "4m")
	t.Setenv("REALTIME_RELAY_STREAM_TTL", "3m")

	cfg := shardedRelayConfigFromEnv()
	if cfg.TrimHorizon != 10*time.Minute {
		t.Fatalf("trim horizon = %s, want 10m", cfg.TrimHorizon)
	}
	if cfg.StreamTTL != 15*time.Minute {
		t.Fatalf("stream TTL = %s, want 15m", cfg.StreamTTL)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("normalized retention config is invalid: %v", err)
	}
}

func TestNewNamedRedisClient_SetsClientName(t *testing.T) {
	t.Setenv("REDIS_DISABLE_CLIENT_NAME", "")
	base := &redis.Options{Addr: "localhost:6379"}
	client := newNamedRedisClient(base, "store")
	defer client.Close()

	opts := client.Options()
	if opts.ClientName != "multica-api:store" {
		t.Errorf("ClientName = %q, want %q", opts.ClientName, "multica-api:store")
	}
}

func TestNewNamedRedisClient_DisableClientName(t *testing.T) {
	t.Setenv("REDIS_DISABLE_CLIENT_NAME", "true")
	base := &redis.Options{Addr: "localhost:6379"}
	client := newNamedRedisClient(base, "store")
	defer client.Close()

	opts := client.Options()
	if opts.ClientName != "" {
		t.Errorf("ClientName = %q, want empty when REDIS_DISABLE_CLIENT_NAME=true", opts.ClientName)
	}
}

func TestNewNamedRedisClient_DisableClientName_ClearsPreExistingName(t *testing.T) {
	t.Setenv("REDIS_DISABLE_CLIENT_NAME", "true")
	// Simulate REDIS_URL with ?client_name=foo — ParseURL sets ClientName.
	base := &redis.Options{Addr: "localhost:6379", ClientName: "foo"}
	client := newNamedRedisClient(base, "store")
	defer client.Close()

	opts := client.Options()
	if opts.ClientName != "" {
		t.Errorf("ClientName = %q, want empty: REDIS_DISABLE_CLIENT_NAME must clear pre-existing name from URL", opts.ClientName)
	}
}

func TestNewNamedRedisClient_DisableClientName_InvalidValue(t *testing.T) {
	t.Setenv("REDIS_DISABLE_CLIENT_NAME", "not-a-bool")
	base := &redis.Options{Addr: "localhost:6379"}
	client := newNamedRedisClient(base, "store")
	defer client.Close()

	opts := client.Options()
	// Invalid value falls back to default (false), so ClientName IS set
	if opts.ClientName != "multica-api:store" {
		t.Errorf("ClientName = %q, want %q (invalid env should fall back to naming enabled)", opts.ClientName, "multica-api:store")
	}
}

// TestNormalizeServerVersion covers the router-config wiring path (not just
// a hand-set handler.Config field): an unstamped "dev" build must not leak
// into /api/config's server_version, or the Help popover would render
// "Server version dev" instead of hiding the row.
func TestNormalizeServerVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"unstamped_dev_default_becomes_empty", "dev", ""},
		{"already_empty_stays_empty", "", ""},
		{"stamped_release_tag_passes_through", "v0.4.0", "v0.4.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeServerVersion(tt.in); got != tt.want {
				t.Errorf("normalizeServerVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		def   bool
		want  bool
	}{
		{"empty_returns_default_false", "TEST_ENV_BOOL_1", "", false, false},
		{"empty_returns_default_true", "TEST_ENV_BOOL_2", "", true, true},
		{"true_string", "TEST_ENV_BOOL_3", "true", false, true},
		{"false_string", "TEST_ENV_BOOL_4", "false", true, false},
		{"one_is_true", "TEST_ENV_BOOL_5", "1", false, true},
		{"zero_is_false", "TEST_ENV_BOOL_6", "0", true, false},
		{"invalid_returns_default", "TEST_ENV_BOOL_7", "maybe", false, false},
		{"invalid_returns_default_true", "TEST_ENV_BOOL_8", "maybe", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != "" {
				t.Setenv(tt.key, tt.value)
			} else {
				os.Unsetenv(tt.key)
			}
			got := envBool(tt.key, tt.def)
			if got != tt.want {
				t.Errorf("envBool(%q, %v) = %v, want %v", tt.key, tt.def, got, tt.want)
			}
		})
	}
}

func TestEnvNonNegativeDuration(t *testing.T) {
	tests := []struct {
		name  string
		value string
		def   time.Duration
		want  time.Duration
	}{
		{name: "unset returns default", def: 3 * time.Second, want: 3 * time.Second},
		{name: "empty returns default", value: "", def: 2 * time.Second, want: 2 * time.Second},
		{name: "bare zero disables hold", value: "0", def: time.Second, want: 0},
		{name: "zero duration disables hold", value: "0s", def: time.Second, want: 0},
		{name: "positive duration", value: "5m", want: 5 * time.Minute},
		{name: "invalid returns default", value: "later", def: 4 * time.Second, want: 4 * time.Second},
		{name: "negative returns default", value: "-1s", def: 4 * time.Second, want: 4 * time.Second},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_NON_NEGATIVE_DURATION_" + strconv.Itoa(i)
			if tt.name == "unset returns default" {
				os.Unsetenv(key)
			} else {
				t.Setenv(key, tt.value)
			}
			if got := envNonNegativeDuration(key, tt.def); got != tt.want {
				t.Fatalf("envNonNegativeDuration(%q, %s) = %s, want %s", key, tt.def, got, tt.want)
			}
		})
	}
}

func TestEnvNonNegativeInt(t *testing.T) {
	tests := []struct {
		name  string
		value string
		def   int
		want  int
	}{
		{name: "unset returns default", def: 10, want: 10},
		{name: "zero disables gate", value: "0", def: 10, want: 0},
		{name: "positive override", value: "25", def: 10, want: 25},
		{name: "invalid returns default", value: "many", def: 10, want: 10},
		{name: "negative returns default", value: "-1", def: 10, want: 10},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_NON_NEGATIVE_INT_" + strconv.Itoa(i)
			if tt.name == "unset returns default" {
				os.Unsetenv(key)
			} else {
				t.Setenv(key, tt.value)
			}
			if got := envNonNegativeInt(key, tt.def); got != tt.want {
				t.Fatalf("envNonNegativeInt(%q, %d) = %d, want %d", key, tt.def, got, tt.want)
			}
		})
	}
}

func TestHoldBeforeShutdown(t *testing.T) {
	const hold = 10 * time.Millisecond
	started := time.Now()
	holdBeforeShutdown(syscall.SIGTERM, nil, hold)
	if elapsed := time.Since(started); elapsed < hold {
		t.Fatalf("holdBeforeShutdown returned after %s, before configured hold %s", elapsed, hold)
	}
}

func TestHoldBeforeShutdownInterruptedBySecondSignal(t *testing.T) {
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGINT
	done := make(chan struct{})

	go func() {
		holdBeforeShutdown(syscall.SIGTERM, signals, time.Minute)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("holdBeforeShutdown did not return after a second signal")
	}
	if len(signals) != 0 {
		t.Fatal("holdBeforeShutdown did not consume the second signal")
	}
}

func TestHoldBeforeShutdownDisabled(t *testing.T) {
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGINT
	holdBeforeShutdown(syscall.SIGTERM, signals, 0)
	if len(signals) != 1 {
		t.Fatal("disabled hold should not consume another signal")
	}
}

func TestJWTSecretBootError(t *testing.T) {
	strong := "a1b2c3d4e5f60718293a4b5c6d7e8f9012a3b4c5d6e7f8091a2b3c4d5e6f70819"
	tests := []struct {
		name      string
		jwtSecret string
		appEnv    string
		wantErr   bool
	}{
		{"production_with_empty_secret_is_rejected", "", "production", true},
		{"production_with_code_default_is_rejected", "multica-dev-secret-change-in-production", "production", true},
		{"production_with_compose_default_is_rejected", "change-me-in-production", "production", true},
		{"production_with_uppercase_env_is_rejected", "change-me-in-production", "PRODUCTION", true},
		{"production_with_whitespace_env_is_rejected", "change-me-in-production", " production ", true},
		{"production_with_strong_secret_is_accepted", strong, "production", false},
		{"non_production_with_empty_secret_is_allowed", "", "", false},
		{"non_production_with_weak_secret_is_allowed", "change-me-in-production", "development", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := jwtSecretBootError(tt.jwtSecret, tt.appEnv)
			if tt.wantErr && err == nil {
				t.Fatalf("jwtSecretBootError(%q, %q) = nil, want error", tt.jwtSecret, tt.appEnv)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("jwtSecretBootError(%q, %q) = %v, want nil", tt.jwtSecret, tt.appEnv, err)
			}
		})
	}
}

// TestNewMainHTTPServerTimeouts pins the production timeout defaults on the
// public HTTP server. These are safety settings, not tuning: removing them,
// resetting them to zero, or making ReadTimeout/WriteTimeout non-zero would
// silently reintroduce the Slowloris exposure or start killing uploads and
// long-lived WebSocket connections mid-stream — none of which the rest of the
// suite would catch.
func TestNewMainHTTPServerTimeouts(t *testing.T) {
	srv := newMainHTTPServer(":8080", nil)

	if got, want := srv.ReadHeaderTimeout, 5*time.Second; got != want {
		t.Errorf("ReadHeaderTimeout = %v, want %v", got, want)
	}
	if got, want := srv.IdleTimeout, 120*time.Second; got != want {
		t.Errorf("IdleTimeout = %v, want %v", got, want)
	}
	// Zero is intentional: WebSocket upgrades and large uploads share this
	// listener and must not be bounded by a whole-request deadline.
	if got := srv.ReadTimeout; got != 0 {
		t.Errorf("ReadTimeout = %v, want 0", got)
	}
	if got := srv.WriteTimeout; got != 0 {
		t.Errorf("WriteTimeout = %v, want 0", got)
	}
}
