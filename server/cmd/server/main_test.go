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

// namedClientOptions builds a single-node client through the factory and
// returns its resolved options. Cluster mode is off (REDIS_CLUSTER unset), so
// newNamedClient yields a *redis.Client whose ClientName reflects the naming
// logic under test.
func namedClientOptions(t *testing.T, base *redis.Options, suffix string) *redis.Options {
	t.Helper()
	client := newRedisClientFactory(base).newNamedClient(suffix)
	t.Cleanup(func() { _ = client.Close() })
	c, ok := client.(*redis.Client)
	if !ok {
		t.Fatalf("expected *redis.Client in single-node mode, got %T", client)
	}
	return c.Options()
}

func TestNewNamedRedisClient_SetsClientName(t *testing.T) {
	t.Setenv("REDIS_DISABLE_CLIENT_NAME", "")
	base := &redis.Options{Addr: "localhost:6379"}
	opts := namedClientOptions(t, base, "store")
	if opts.ClientName != "multica-api:store" {
		t.Errorf("ClientName = %q, want %q", opts.ClientName, "multica-api:store")
	}
}

func TestNewNamedRedisClient_DisableClientName(t *testing.T) {
	t.Setenv("REDIS_DISABLE_CLIENT_NAME", "true")
	base := &redis.Options{Addr: "localhost:6379"}
	opts := namedClientOptions(t, base, "store")
	if opts.ClientName != "" {
		t.Errorf("ClientName = %q, want empty when REDIS_DISABLE_CLIENT_NAME=true", opts.ClientName)
	}
}

func TestNewNamedRedisClient_DisableClientName_ClearsPreExistingName(t *testing.T) {
	t.Setenv("REDIS_DISABLE_CLIENT_NAME", "true")
	// Simulate REDIS_URL with ?client_name=foo — ParseURL sets ClientName.
	base := &redis.Options{Addr: "localhost:6379", ClientName: "foo"}
	opts := namedClientOptions(t, base, "store")
	if opts.ClientName != "" {
		t.Errorf("ClientName = %q, want empty: REDIS_DISABLE_CLIENT_NAME must clear pre-existing name from URL", opts.ClientName)
	}
}

func TestNewNamedRedisClient_DisableClientName_InvalidValue(t *testing.T) {
	t.Setenv("REDIS_DISABLE_CLIENT_NAME", "not-a-bool")
	base := &redis.Options{Addr: "localhost:6379"}
	opts := namedClientOptions(t, base, "store")
	// Invalid value falls back to default (false), so ClientName IS set
	if opts.ClientName != "multica-api:store" {
		t.Errorf("ClientName = %q, want %q (invalid env should fall back to naming enabled)", opts.ClientName, "multica-api:store")
	}
}

// TestRedisClientFactory_ClusterMode verifies REDIS_CLUSTER=true makes the
// factory produce a *redis.ClusterClient, and that REDIS_ADDRS seeds are
// merged with the base Addr. Single-node mode (the default) is covered by the
// naming tests above, which assert a *redis.Client comes back.
func TestRedisClientFactory_ClusterMode(t *testing.T) {
	t.Setenv("REDIS_CLUSTER", "true")
	t.Setenv("REDIS_ADDRS", "seed-2:6379, seed-3:6379")
	base := &redis.Options{Addr: "seed-1:6379"}

	factory := newRedisClientFactory(base)
	if !factory.cluster {
		t.Fatal("factory.cluster = false, want true when REDIS_CLUSTER=true")
	}
	if len(factory.extraAddrs) != 2 || factory.extraAddrs[0] != "seed-2:6379" || factory.extraAddrs[1] != "seed-3:6379" {
		t.Fatalf("extraAddrs = %v, want [seed-2:6379 seed-3:6379]", factory.extraAddrs)
	}

	client := factory.newNamedClient("store")
	t.Cleanup(func() { _ = client.Close() })
	if _, ok := client.(*redis.ClusterClient); !ok {
		t.Fatalf("expected *redis.ClusterClient in cluster mode, got %T", client)
	}
}

func TestRedisClientFactory_SingleNodeByDefault(t *testing.T) {
	t.Setenv("REDIS_CLUSTER", "")
	base := &redis.Options{Addr: "localhost:6379"}
	factory := newRedisClientFactory(base)
	if factory.cluster {
		t.Fatal("factory.cluster = true, want false when REDIS_CLUSTER is unset")
	}
	client := factory.newNamedClient("store")
	t.Cleanup(func() { _ = client.Close() })
	if _, ok := client.(*redis.Client); !ok {
		t.Fatalf("expected *redis.Client in single-node mode, got %T", client)
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
