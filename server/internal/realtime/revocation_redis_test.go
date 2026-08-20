package realtime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
	"github.com/multica-ai/multica/server/internal/tagaccess"
	"github.com/redis/go-redis/v9"
)

func TestRedisConnectionCloseBrokerDispatchWaitsForAllActiveInstances(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")
	broker := NewRedisConnectionCloseBroker(NewHub(), rdb, rdb, "instance-a", key)
	// Process clocks may differ; active-node expiry must use Redis TIME.
	command := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseWorkspaceProjection, DeliveryID: "delivery-1", CorrelationID: "correlation-1",
		WorkspaceID: "workspace-1", AuthorityVersion: 4, TargetDigest: "digest-1",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseWorkspace, WorkspaceID: "workspace-1"}},
	}
	commandID := connectionCloseCommandID(command)

	mock.ExpectTime().SetVal(now)
	mock.ExpectZRemRangeByScore(revocationNodesKey, "-inf", fmt.Sprintf("%d", now.UnixMilli())).SetVal(0)
	mock.ExpectZRangeByScore(revocationNodesKey, &redis.ZRangeBy{
		Min: fmt.Sprintf("%d", now.UnixMilli()+1), Max: "+inf",
	}).SetVal([]string{"instance-a", "instance-b"})
	payload := mustMarshalCloseCommand(t, command)
	mock.ExpectXAdd(&redis.XAddArgs{
		Stream: revocationCommandStream, MaxLen: revocationCommandStreamMaxLen, Approx: true,
		Values: []any{"command_id", commandID, "payload_json", payload, "command_mac", connectionCloseCommandMAC(key, commandID, []byte(payload))},
	}).SetVal("1-0")
	mock.ExpectHGetAll(revocationAckKey(commandID)).SetVal(map[string]string{
		"instance-a": connectionCloseAckValue(key, commandID, "instance-a"),
		"instance-b": connectionCloseAckValue(key, commandID, "instance-b"),
	})

	participants, err := broker.ActiveInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.Dispatch(context.Background(), command, participants); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisConnectionCloseBrokerHeartbeatPublishesRedisClockLease(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	redisNow := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")
	broker := NewRedisConnectionCloseBroker(NewHub(), rdb, rdb, "instance-a", key)
	mock.ExpectTime().SetVal(redisNow)
	mock.ExpectZAdd(revocationNodesKey, redis.Z{
		Score: float64(redisNow.Add(revocationNodeTTL).UnixMilli()), Member: "instance-a",
	}).SetVal(1)

	if err := broker.heartbeatOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !broker.LeaseValid() {
		t.Fatal("successful Redis-clock heartbeat did not activate the local serving lease")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisConnectionCloseBrokerCreatesDedicatedInstanceConsumerGroup(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	broker := NewRedisConnectionCloseBroker(
		NewHub(), rdb, rdb, "instance-a", []byte("0123456789abcdef0123456789abcdef"),
	)
	mock.ExpectXGroupCreateMkStream(revocationCommandStream, broker.consumerGroup(), "$").SetVal("OK")
	if err := broker.ensureConsumerGroup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisConnectionCloseBrokerMissingGroupPermanentlyFencesTagSockets(t *testing.T) {
	hub := NewHub()
	tagClient := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "tag-client", VIBESUserID: "user-1", WorkspaceID: "workspace-1", VIBESSessionID: "session-1",
	})
	nativeClient := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "native-client", WorkspaceID: "native-workspace",
	})
	broker := NewRedisConnectionCloseBroker(
		hub, nil, nil, "instance-a", []byte("0123456789abcdef0123456789abcdef"),
	)
	future := time.Now().Add(time.Hour)
	broker.leaseDeadline.Store(&future)
	if !broker.failClosedOnReadError(errors.New("NOGROUP No such key or consumer group")) {
		t.Fatal("missing consumer group was treated as transient")
	}
	if broker.LeaseValid() || !tagClient.revoked.Load() || hub.hasClient(tagClient) {
		t.Fatal("missing consumer group did not permanently fence Tag sockets")
	}
	if nativeClient.revoked.Load() || !hub.hasClient(nativeClient) {
		t.Fatal("missing Tag consumer group fenced an unrelated native socket")
	}
}

func TestRedisConnectionCloseBrokerDoesNotCompleteWithoutAnActiveInstance(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")
	broker := NewRedisConnectionCloseBroker(NewHub(), rdb, rdb, "instance-a", key)

	mock.ExpectTime().SetVal(now)
	mock.ExpectZRemRangeByScore(revocationNodesKey, "-inf", fmt.Sprintf("%d", now.UnixMilli())).SetVal(0)
	mock.ExpectZRangeByScore(revocationNodesKey, &redis.ZRangeBy{
		Min: fmt.Sprintf("%d", now.UnixMilli()+1), Max: "+inf",
	}).SetVal([]string{})
	if _, err := broker.ActiveInstances(context.Background()); err == nil {
		t.Fatal("Dispatch completed without any active instance acknowledgement target")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisConnectionCloseBrokerRequiresExplicitAckFromLiveNode(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")
	broker := NewRedisConnectionCloseBroker(NewHub(), rdb, rdb, "instance-a", key)
	command := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseWorkspaceProjection, DeliveryID: "delivery-expired", CorrelationID: "correlation-expired",
		WorkspaceID: "workspace-1", AuthorityVersion: 5, TargetDigest: "digest-expired",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseWorkspace, WorkspaceID: "workspace-1"}},
	}
	commandID := connectionCloseCommandID(command)
	payload := mustMarshalCloseCommand(t, command)
	mock.ExpectTime().SetVal(now)
	mock.ExpectZRemRangeByScore(revocationNodesKey, "-inf", fmt.Sprintf("%d", now.UnixMilli())).SetVal(0)
	mock.ExpectZRangeByScore(revocationNodesKey, &redis.ZRangeBy{
		Min: fmt.Sprintf("%d", now.UnixMilli()+1), Max: "+inf",
	}).SetVal([]string{"live-instance"})
	mock.ExpectXAdd(&redis.XAddArgs{
		Stream: revocationCommandStream, MaxLen: revocationCommandStreamMaxLen, Approx: true,
		Values: []any{"command_id", commandID, "payload_json", payload, "command_mac", connectionCloseCommandMAC(key, commandID, []byte(payload))},
	}).SetVal("2-0")
	mock.ExpectHGetAll(revocationAckKey(commandID)).SetVal(map[string]string{"live-instance": "closed"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	participants, err := broker.ActiveInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.Dispatch(ctx, command, participants); err == nil {
		t.Fatal("Dispatch completed for a live node without an authenticated close acknowledgement")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisConnectionCloseBrokerNeverSubstitutesLeaseExpiryForExactAck(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")
	broker := NewRedisConnectionCloseBroker(NewHub(), rdb, rdb, "instance-a", key)
	command := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseWorkspaceProjection, DeliveryID: "delivery-paused", CorrelationID: "correlation-paused",
		WorkspaceID: "workspace-1", AuthorityVersion: 5, TargetDigest: "digest-paused",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseWorkspace, WorkspaceID: "workspace-1"}},
	}
	commandID := connectionCloseCommandID(command)
	payload := mustMarshalCloseCommand(t, command)
	mock.ExpectTime().SetVal(now)
	mock.ExpectZRemRangeByScore(revocationNodesKey, "-inf", fmt.Sprintf("%d", now.UnixMilli())).SetVal(0)
	mock.ExpectZRangeByScore(revocationNodesKey, &redis.ZRangeBy{
		Min: fmt.Sprintf("%d", now.UnixMilli()+1), Max: "+inf",
	}).SetVal([]string{"paused-instance"})
	mock.ExpectXAdd(&redis.XAddArgs{
		Stream: revocationCommandStream, MaxLen: revocationCommandStreamMaxLen, Approx: true,
		Values: []any{"command_id", commandID, "payload_json", payload, "command_mac", connectionCloseCommandMAC(key, commandID, []byte(payload))},
	}).SetVal("3-0")
	mock.ExpectHGetAll(revocationAckKey(commandID)).SetVal(map[string]string{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	participants, err := broker.ActiveInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.Dispatch(ctx, command, participants); err == nil {
		t.Fatal("Dispatch completed without the active boot's authenticated close acknowledgement")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisConnectionCloseBrokerRetryCannotDropOriginalUnacknowledgedBoot(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	firstNow := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")
	broker := NewRedisConnectionCloseBroker(NewHub(), rdb, rdb, "instance-b", key)
	command := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseWorkspaceProjection, DeliveryID: "delivery-retry", CorrelationID: "correlation-retry",
		WorkspaceID: "workspace-1", AuthorityVersion: 6, TargetDigest: "digest-retry",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseWorkspace, WorkspaceID: "workspace-1"}},
	}
	commandID := connectionCloseCommandID(command)
	payload := mustMarshalCloseCommand(t, command)

	mock.ExpectTime().SetVal(firstNow)
	mock.ExpectZRemRangeByScore(revocationNodesKey, "-inf", fmt.Sprintf("%d", firstNow.UnixMilli())).SetVal(0)
	mock.ExpectZRangeByScore(revocationNodesKey, &redis.ZRangeBy{Min: fmt.Sprintf("%d", firstNow.UnixMilli()+1), Max: "+inf"}).SetVal([]string{"instance-a", "instance-b"})
	mock.ExpectXAdd(&redis.XAddArgs{
		Stream: revocationCommandStream, MaxLen: revocationCommandStreamMaxLen, Approx: true,
		Values: []any{"command_id", commandID, "payload_json", payload, "command_mac", connectionCloseCommandMAC(key, commandID, []byte(payload))},
	}).SetVal("4-0")
	mock.ExpectHGetAll(revocationAckKey(commandID)).SetVal(map[string]string{
		"instance-b": connectionCloseAckValue(key, commandID, "instance-b"),
	})
	firstCtx, firstCancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer firstCancel()
	participants, err := broker.ActiveInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.Dispatch(firstCtx, command, participants); err == nil {
		t.Fatal("first dispatch completed without instance-a acknowledgement")
	}

	mock.ExpectXAdd(&redis.XAddArgs{
		Stream: revocationCommandStream, MaxLen: revocationCommandStreamMaxLen, Approx: true,
		Values: []any{"command_id", commandID, "payload_json", payload, "command_mac", connectionCloseCommandMAC(key, commandID, []byte(payload))},
	}).SetVal("5-0")
	mock.ExpectHGetAll(revocationAckKey(commandID)).SetVal(map[string]string{
		"instance-b": connectionCloseAckValue(key, commandID, "instance-b"),
	})
	retryCtx, retryCancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer retryCancel()
	if err := broker.Dispatch(retryCtx, command, participants); err == nil {
		t.Fatal("retry completed after dropping original unacknowledged instance-a")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredRedisBrokerLeasePermanentlyFencesFramesAndRegistrations(t *testing.T) {
	rdb, _ := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	hub := NewHub()
	go hub.Run()
	key := []byte("0123456789abcdef0123456789abcdef")
	broker := NewRedisConnectionCloseBroker(hub, rdb, rdb, "instance-a", key)
	_ = NewConnectionCloseCoordinator(hub, broker, newMemoryCloseReceiptStore(), time.Now)
	client := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "paused-socket", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
		VIBESSessionID: "vibes-session-1", AuthorityVersion: 1,
	})
	expired := time.Now().Add(-time.Millisecond)
	broker.leaseDeadline.Store(&expired)

	if client.trySend([]byte(`{"type":"must-not-enqueue"}`)) {
		t.Fatal("socket enqueued a frame after its broker lease expired")
	}
	if broker.LeaseValid() {
		t.Fatal("expired broker lease remained valid")
	}
	future := time.Now().Add(time.Hour)
	broker.leaseDeadline.Store(&future)
	if broker.LeaseValid() {
		t.Fatal("expired broker lease was revived by a later deadline")
	}
	if err := broker.heartbeatOnce(context.Background()); err == nil {
		t.Fatal("expired broker boot renewed after it had permanently self-fenced")
	}
	late := &Client{
		hub: hub, send: make(chan []byte, 1), userID: "user-1", workspaceID: "workspace-1",
		metadata: ConnectionMetadata{ConnectionID: "late-socket", VIBESUserID: "user-1", WorkspaceID: "workspace-1", VIBESSessionID: "vibes-session-1", AuthorityVersion: 1},
	}
	if hub.registerClient(late) || !late.revoked.Load() {
		t.Fatal("expired broker boot accepted a new socket registration")
	}
}

func TestRedisConnectionCloseBrokerRejectsForgedCommand(t *testing.T) {
	rdb, _ := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	key := []byte("0123456789abcdef0123456789abcdef")
	hub := NewHub()
	client := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "target", VIBESUserID: "user-1", WorkspaceID: "workspace-1", AuthorityVersion: 1,
	})
	broker := NewRedisConnectionCloseBroker(hub, rdb, rdb, "instance-a", key)
	command := tagaccess.ConnectionCloseCommand{
		Source: tagaccess.ConnectionCloseIdentityRestriction, DeliveryID: "forged", CorrelationID: "forged-correlation",
		IdentityRestrictionVersion: 1, TargetDigest: "forged-digest",
		Targets: []tagaccess.ConnectionCloseTarget{{Scope: tagaccess.ConnectionCloseAccount, VIBESUserID: "user-1"}},
	}
	broker.handleCommand(context.Background(), redis.XMessage{ID: "3-0", Values: map[string]any{
		"command_id": connectionCloseCommandID(command), "payload_json": mustMarshalCloseCommand(t, command), "command_mac": "forged",
	}})
	if client.revoked.Load() {
		t.Fatal("unauthenticated Redis writer injected a connection-close command")
	}
}

func TestRedisConnectionCloseBrokerStopFencesSocketsBeforeUnregisteringInstance(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	key := []byte("0123456789abcdef0123456789abcdef")
	hub := NewHub()
	client := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "restart-target", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
		VIBESSessionID: "vibes-session-1", AuthorityVersion: 1,
	})
	native := attachedRevocationClient(hub, ConnectionMetadata{
		ConnectionID: "native-unaffected", VIBESUserID: "native-user", WorkspaceID: "workspace-native",
	})
	broker := NewRedisConnectionCloseBroker(hub, rdb, rdb, "instance-a", key)
	_, broker.cancel = context.WithCancel(context.Background())
	mock.ExpectZRem(revocationNodesKey, "instance-a").SetVal(1)

	broker.Stop()
	if !client.revoked.Load() || hub.hasClient(client) {
		t.Fatal("broker stop unregistered the instance before fencing its sockets")
	}
	if native.revoked.Load() || !hub.hasClient(native) {
		t.Fatal("Tag revocation broker outage fenced an unrelated native socket")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func mustMarshalCloseCommand(t *testing.T, command tagaccess.ConnectionCloseCommand) string {
	t.Helper()
	payload, err := marshalConnectionCloseCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
