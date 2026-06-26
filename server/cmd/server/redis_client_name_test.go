package main

import (
	"testing"

	"github.com/redis/go-redis/v9"
)

// TestNewNamedRedisClientClientName verifies the REDIS_DISABLE_CLIENT_NAME
// opt-out: managed Redis (GCP Memorystore, AWS ElastiCache restricted-command)
// blocks the CLIENT command, so go-redis's CLIENT SETNAME handshake — sent
// whenever ClientName != "" — must be suppressible by leaving ClientName empty.
// See #4627.
func TestNewNamedRedisClientClientName(t *testing.T) {
	base := &redis.Options{Addr: "localhost:6379"}

	// Default: a client name is set, so go-redis issues CLIENT SETNAME.
	t.Setenv("REDIS_DISABLE_CLIENT_NAME", "")
	if got := newNamedRedisClient(base, "realtime-read").Options().ClientName; got == "" {
		t.Fatalf("default: expected a non-empty client name, got empty")
	}

	// Opt-out keeps ClientName empty so no CLIENT SETNAME is sent.
	for _, v := range []string{"true", "1", "TRUE", " True "} {
		t.Setenv("REDIS_DISABLE_CLIENT_NAME", v)
		if got := newNamedRedisClient(base, "realtime-read").Options().ClientName; got != "" {
			t.Fatalf("REDIS_DISABLE_CLIENT_NAME=%q: expected empty client name, got %q", v, got)
		}
	}

	// Only true/1 disable; other values keep the default named behavior.
	for _, v := range []string{"false", "0", "no"} {
		t.Setenv("REDIS_DISABLE_CLIENT_NAME", v)
		if got := newNamedRedisClient(base, "realtime-read").Options().ClientName; got == "" {
			t.Fatalf("REDIS_DISABLE_CLIENT_NAME=%q: expected a non-empty client name, got empty", v)
		}
	}
}
