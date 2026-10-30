package main

import (
	"context"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

type channelLeaseTestStore struct{}

func (channelLeaseTestStore) ListActiveInstallations(context.Context) ([]engine.Installation, error) {
	return nil, nil
}
func (channelLeaseTestStore) ListHeldWSLeases(context.Context, []pgtype.UUID) (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}
func (channelLeaseTestStore) TryAcquireWSLease(context.Context, engine.AcquireLeaseParams) error {
	return nil
}
func (channelLeaseTestStore) RenewWSLease(context.Context, engine.AcquireLeaseParams) error {
	return nil
}
func (channelLeaseTestStore) ReleaseWSLease(context.Context, engine.ReleaseLeaseParams) error {
	return nil
}

func clearChannelLeaseTimingEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"CHANNEL_WS_LEASE_TTL",
		"CHANNEL_WS_LEASE_RENEW_INTERVAL",
		"CHANNEL_WS_LEASE_POLL_INTERVAL",
		"CHANNEL_WS_LEASE_ERROR_RETRY_INTERVAL",
		"CHANNEL_WS_LEASE_EXPIRY_SAFETY_MARGIN",
	} {
		t.Setenv(name, "")
	}
}

func TestChannelSupervisorConfigDefaults(t *testing.T) {
	clearChannelLeaseTimingEnv(t)
	cfg, err := channelSupervisorConfigFromEnv(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LeaseTTL != 180*time.Second || cfg.LeaseRenewInterval != 60*time.Second || cfg.PollInterval != 30*time.Second {
		t.Fatalf("unexpected defaults: ttl=%s renew=%s poll=%s", cfg.LeaseTTL, cfg.LeaseRenewInterval, cfg.PollInterval)
	}
}

func TestChannelSupervisorConfigRejectsUnsafeRelation(t *testing.T) {
	clearChannelLeaseTimingEnv(t)
	t.Setenv("CHANNEL_WS_LEASE_POLL_INTERVAL", "90s")
	if _, err := channelSupervisorConfigFromEnv(nil); err == nil {
		t.Fatalf("poll > renew should fail closed")
	}
}

func TestBuildChannelSupervisorRedisDoesNotFallbackWhenClientMissing(t *testing.T) {
	clearChannelLeaseTimingEnv(t)
	t.Setenv("CHANNEL_WS_LEASE_BACKEND", "redis")
	t.Setenv("CHANNEL_WS_LEASE_NAMESPACE", "prod")
	store := channelLeaseTestStore{}
	sup := buildChannelSupervisor(store, store, channel.NewRegistry(), nil, RouterOptions{})
	if sup != nil {
		t.Fatalf("Redis mode without a client must disable the supervisor, not fall back to PostgreSQL")
	}
}

func TestBuildChannelSupervisorRedisRequiresReadyBackend(t *testing.T) {
	clearChannelLeaseTimingEnv(t)
	t.Setenv("CHANNEL_WS_LEASE_BACKEND", "redis")
	t.Setenv("CHANNEL_WS_LEASE_NAMESPACE", "prod")
	rdb, mock := redismock.NewClientMock()
	mock.ExpectPing().SetErr(context.DeadlineExceeded)
	store := channelLeaseTestStore{}
	sup := buildChannelSupervisor(store, store, channel.NewRegistry(), nil, RouterOptions{ChannelLeaseRedis: rdb})
	if sup != nil {
		t.Fatalf("unready Redis backend must disable the supervisor")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
