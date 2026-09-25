package realtime

import (
	"context"
	"errors"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
)

func TestStreamTTLRefresherRateLimitsPublishPath(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	now := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	refresher := newStreamTTLRefresher(15*time.Minute, 30*time.Second)
	refresher.now = func() time.Time { return now }

	mock.ExpectPExpire("stream", 15*time.Minute).SetVal(true)
	if err := refresher.refreshIfDue(context.Background(), rdb, "stream"); err != nil {
		t.Fatal(err)
	}
	if err := refresher.refreshIfDue(context.Background(), rdb, "stream"); err != nil {
		t.Fatal(err)
	}

	now = now.Add(30 * time.Second)
	mock.ExpectPExpire("stream", 15*time.Minute).SetVal(true)
	if err := refresher.refreshIfDue(context.Background(), rdb, "stream"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStreamTTLRefresherRetriesAfterFailure(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	refresher := newStreamTTLRefresher(15*time.Minute, 30*time.Second)
	mock.ExpectPExpire("stream", 15*time.Minute).SetErr(errors.New("redis unavailable"))
	if err := refresher.refreshIfDue(context.Background(), rdb, "stream"); err == nil {
		t.Fatal("expected PEXPIRE failure")
	}
	mock.ExpectPExpire("stream", 15*time.Minute).SetVal(true)
	if err := refresher.refreshIfDue(context.Background(), rdb, "stream"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStreamTTLRefresherRepairsOnlyMissingTTL(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	refresher := newStreamTTLRefresher(15*time.Minute, 30*time.Second)
	mock.ExpectPTTL("stream").SetVal(-1)
	mock.ExpectPExpire("stream", 15*time.Minute).SetVal(true)
	ttl, err := refresher.repairMissingTTL(context.Background(), rdb, "stream")
	if err != nil {
		t.Fatal(err)
	}
	if ttl != 15*time.Minute {
		t.Fatalf("repaired TTL = %s, want 15m", ttl)
	}

	mock.ExpectPTTL("stream").SetVal(10 * time.Minute)
	ttl, err = refresher.repairMissingTTL(context.Background(), rdb, "stream")
	if err != nil {
		t.Fatal(err)
	}
	if ttl != 10*time.Minute {
		t.Fatalf("healthy TTL = %s, want 10m", ttl)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStreamTTLRefresherDisabledPersistsStreamForSafeRollback(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	refresher := newStreamTTLRefresher(15*time.Minute, 30*time.Second)
	mock.ExpectPTTL("stream").SetVal(10 * time.Minute)
	mock.ExpectPersist("stream").SetVal(true)
	ttl, err := refresher.reconcileTTL(context.Background(), rdb, "stream", false)
	if err != nil {
		t.Fatal(err)
	}
	if ttl != -1 {
		t.Fatalf("TTL after disabled reconciliation = %s, want -1", ttl)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisInfoInt64(t *testing.T) {
	info := "# Memory\r\nused_memory:2160328\r\nmaxmemory:2097152\r\n"
	if got, ok := redisInfoInt64(info, "used_memory"); !ok || got != 2160328 {
		t.Fatalf("used_memory = %d, %v", got, ok)
	}
	if _, ok := redisInfoInt64(info, "missing"); ok {
		t.Fatal("expected missing field not to parse")
	}
}
