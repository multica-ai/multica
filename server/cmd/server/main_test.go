package main

import (
	"os"
	"testing"

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
