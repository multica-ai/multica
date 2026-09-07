package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestRedisLeaseStoreRequiresSafeNamespace(t *testing.T) {
	rdb, _ := redismock.NewClientMock()
	for _, namespace := range []string{"", "prod:blue", "prod blue"} {
		if _, err := NewRedisLeaseStore(rdb, namespace); err == nil {
			t.Fatalf("namespace %q should be rejected", namespace)
		}
	}
	if _, err := NewRedisLeaseStore(rdb, "prod-blue_1.eu"); err != nil {
		t.Fatalf("valid namespace rejected: %v", err)
	}
}

func TestRedisLeaseStoreReadyPingsAndLoadsScripts(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	store, err := NewRedisLeaseStore(rdb, "prod")
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectPing().SetVal("PONG")
	mock.ExpectScriptLoad(redisTryAcquireLeaseSource).SetVal(redisTryAcquireLease.Hash())
	mock.ExpectScriptLoad(redisRenewLeaseSource).SetVal(redisRenewLease.Hash())
	mock.ExpectScriptLoad(redisReleaseLeaseSource).SetVal(redisReleaseLease.Hash())
	mock.Regexp().ExpectEvalSha(redisTryAcquireLease.Hash(), []string{`multica:channel-lease:v1:prod:__ready__:.+`}, "readiness-check", int64(5000)).SetVal(int64(1))
	mock.Regexp().ExpectEvalSha(redisRenewLease.Hash(), []string{`multica:channel-lease:v1:prod:__ready__:.+`}, "readiness-check", int64(5000)).SetVal(int64(1))
	mock.Regexp().ExpectEvalSha(redisReleaseLease.Hash(), []string{`multica:channel-lease:v1:prod:__ready__:.+`}, "readiness-check").SetVal(int64(1))
	if err := store.Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisLeaseStoreAtomicOperationsAreTokenFenced(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	store, err := NewRedisLeaseStore(rdb, "prod")
	if err != nil {
		t.Fatal(err)
	}
	id := uuidFromString(t, "12121212-1212-1212-1212-121212121212")
	key := "multica:channel-lease:v1:prod:12121212-1212-1212-1212-121212121212"
	arg := AcquireLeaseParams{ID: id, Token: "node-g1", TTL: 180 * time.Second}

	mock.ExpectEvalSha(redisTryAcquireLease.Hash(), []string{key}, "node-g1", int64(180000)).SetVal(int64(1))
	if err := store.TryAcquireWSLease(context.Background(), arg); err != nil {
		t.Fatalf("TryAcquireWSLease: %v", err)
	}
	mock.ExpectEvalSha(redisRenewLease.Hash(), []string{key}, "node-g1", int64(180000)).SetVal(int64(1))
	if err := store.RenewWSLease(context.Background(), arg); err != nil {
		t.Fatalf("RenewWSLease: %v", err)
	}
	mock.ExpectEvalSha(redisRenewLease.Hash(), []string{key}, "node-g1", int64(180000)).SetVal(int64(0))
	if err := store.RenewWSLease(context.Background(), arg); !errors.Is(err, ErrLeaseNotAcquired) {
		t.Fatalf("mismatched renewal error = %v, want ErrLeaseNotAcquired", err)
	}
	mock.ExpectEvalSha(redisReleaseLease.Hash(), []string{key}, "node-g1").SetVal(int64(0))
	if err := store.ReleaseWSLease(context.Background(), ReleaseLeaseParams{ID: id, Token: "node-g1"}); err != nil {
		t.Fatalf("stale release should be a fenced no-op: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisLeaseStoreListsOnlyHeldKeys(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	store, err := NewRedisLeaseStore(rdb, "prod")
	if err != nil {
		t.Fatal(err)
	}
	id1 := uuidFromString(t, "13131313-1313-1313-1313-131313131313")
	id2 := uuidFromString(t, "14141414-1414-1414-1414-141414141414")
	mock.ExpectMGet(store.key(id1), store.key(id2)).SetVal([]interface{}{"owner", nil})
	held, err := store.ListHeldWSLeases(context.Background(), []pgtype.UUID{id1, id2})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := held[uuidString(id1)]; !ok {
		t.Fatalf("first ID should be held: %#v", held)
	}
	if _, ok := held[uuidString(id2)]; ok {
		t.Fatalf("second ID should be absent: %#v", held)
	}
}
