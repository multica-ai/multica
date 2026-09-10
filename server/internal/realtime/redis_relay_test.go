package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"
)

func enabledTestRetentionConfig() StreamRetentionConfig {
	cfg := DefaultStreamRetentionConfig()
	cfg.StreamTTLEnabled = true
	return cfg
}

func TestNewRedisRelayWithClientsSeparatesBlockingReadPool(t *testing.T) {
	hub := NewHub()
	writeClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	readClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() {
		writeClient.Close()
		readClient.Close()
	})

	relay := NewRedisRelayWithClients(hub, writeClient, readClient)

	if relay.writeRDB != writeClient {
		t.Fatal("expected relay to use the write client for non-blocking Redis commands")
	}
	if relay.readRDB != readClient {
		t.Fatal("expected relay to reserve the read client for blocking XREADGROUP calls")
	}
}

func TestRedisRelayStopPreventsNewConsumers(t *testing.T) {
	hub := NewHub()
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { client.Close() })

	relay := NewRedisRelay(hub, client)
	relay.Stop()
	relay.startConsumer(context.Background(), ScopeWorkspace, "workspace-1")

	relay.mu.Lock()
	consumerCount := len(relay.consumers)
	relay.mu.Unlock()
	if consumerCount != 0 {
		t.Fatalf("expected no consumers after Stop, got %d", consumerCount)
	}
	relay.Wait()
}

func TestRedisRelaySweepUsesExactTimeTrimAndRepairsTTL(t *testing.T) {
	M.Reset()
	t.Cleanup(M.Reset)
	hub := NewHub()
	attachRealtimeTestClient(hub, ScopeWorkspace, "workspace-1")
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	retention := enabledTestRetentionConfig()
	retention.StreamMaxLen = 321
	retention.TrimHorizon = 20 * time.Minute
	retention.StreamTTL = 30 * time.Minute
	retention.TTLRefreshInterval = time.Minute
	retention.MaintenanceInterval = 2 * time.Minute
	relay := NewRedisRelayWithClientsAndConfig(hub, rdb, rdb, retention)
	now := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	relay.now = func() time.Time { return now }
	relay.ttl.now = relay.now
	stream := StreamKey(ScopeWorkspace, "workspace-1")

	mock.ExpectXTrimMinID(stream, streamMinID(now, retention.TrimHorizon)).SetVal(7)
	mock.ExpectPTTL(stream).SetVal(-1)
	mock.ExpectPExpire(stream, retention.StreamTTL).SetVal(true)
	mock.ExpectZRemRangeByScore(NodesKey(ScopeWorkspace, "workspace-1"), "-inf", fmt.Sprintf("%f", float64(now.Unix()))).SetVal(1)
	mock.ExpectScan(0, "ws:scope:*:stream", legacyStreamScanCount).SetVal(nil, 0)

	relay.sweepLegacyStreams(context.Background())

	if got := M.RedisRelayStreamTrimmedTotal.Load(); got != 7 {
		t.Fatalf("trimmed total = %d, want 7", got)
	}
	if relay.retention.StreamMaxLen != 321 || relay.retention.MaintenanceInterval != 2*time.Minute {
		t.Fatalf("legacy relay did not retain shared config: %+v", relay.retention)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisRelaySweepRepairsInactiveLegacyStreamsIncrementally(t *testing.T) {
	M.Reset()
	t.Cleanup(M.Reset)
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	retention := enabledTestRetentionConfig()
	relay := NewRedisRelayWithClientsAndConfig(NewHub(), rdb, rdb, retention)
	now := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	relay.now = func() time.Time { return now }
	relay.ttl.now = relay.now
	stream := StreamKey(ScopeWorkspace, "inactive-workspace")

	mock.ExpectScan(0, "ws:scope:*:stream", legacyStreamScanCount).SetVal([]string{stream}, 42)
	mock.ExpectXTrimMinID(stream, streamMinID(now, retention.TrimHorizon)).SetVal(3)
	mock.ExpectPTTL(stream).SetVal(-1)
	mock.ExpectPExpire(stream, retention.StreamTTL).SetVal(true)

	relay.sweepLegacyStreams(context.Background())

	if relay.legacyScanCursor != 42 {
		t.Fatalf("scan cursor = %d, want 42", relay.legacyScanCursor)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisRelayCountsTTLRepairFailure(t *testing.T) {
	M.Reset()
	t.Cleanup(M.Reset)
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	retention := enabledTestRetentionConfig()
	relay := NewRedisRelayWithClientsAndConfig(NewHub(), rdb, rdb, retention)
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	relay.now = func() time.Time { return now }
	relay.ttl.now = relay.now
	stream := StreamKey(ScopeWorkspace, "workspace-without-ttl")

	mock.ExpectScan(0, "ws:scope:*:stream", legacyStreamScanCount).SetVal([]string{stream}, 0)
	mock.ExpectXTrimMinID(stream, streamMinID(now, retention.TrimHorizon)).SetVal(0)
	mock.ExpectPTTL(stream).SetVal(-1)
	mock.ExpectPExpire(stream, retention.StreamTTL).SetErr(errors.New("PEXPIRE denied"))

	relay.sweepLegacyStreams(context.Background())

	if got := M.RedisRelayStreamsWithoutTTL.Load(); got != 1 {
		t.Fatalf("streams without TTL = %d, want 1", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisRelayCompleteScanPrunesMissingTTLForDeletedStreams(t *testing.T) {
	M.Reset()
	t.Cleanup(M.Reset)
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	retention := enabledTestRetentionConfig()
	relay := NewRedisRelayWithClientsAndConfig(NewHub(), rdb, rdb, retention)
	relay.streamsWithoutTTL[StreamKey(ScopeWorkspace, "deleted")] = struct{}{}
	M.SetRedisStreamsWithoutTTL("legacy", 1)

	mock.ExpectScan(0, "ws:scope:*:stream", legacyStreamScanCount).SetVal(nil, 0)
	relay.sweepLegacyStreams(context.Background())

	if got := M.RedisRelayStreamsWithoutTTL.Load(); got != 0 {
		t.Fatalf("streams without TTL after full scan = %d, want 0", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisRelayEnsureConsumerGroupIgnoresBusyGroup(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	relay := NewRedisRelay(NewHub(), rdb)

	mock.ExpectXGroupCreateMkStream("stream", "group", "$").SetErr(errors.New("BUSYGROUP Consumer Group name already exists"))
	if err := relay.ensureConsumerGroup(context.Background(), "stream", "group", "$"); err != nil {
		t.Fatalf("BUSYGROUP should be ignored: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDualWriteBroadcasterFansOutLocallyBeforePublishing(t *testing.T) {
	hub := NewHub()
	client := attachRealtimeTestClient(hub, ScopeWorkspace, "workspace-1")
	publisher := &localFirstPublisher{t: t, client: client}
	broadcaster := newDualWriteBroadcaster(hub, publisher)
	message := []byte(`{"type":"issue:updated"}`)

	broadcaster.BroadcastToScope(ScopeWorkspace, "workspace-1", message)

	if !publisher.called {
		t.Fatal("expected relay publish to be invoked")
	}
	if publisher.scopeType != ScopeWorkspace || publisher.scopeID != "workspace-1" {
		t.Fatalf("unexpected relay scope: %s/%s", publisher.scopeType, publisher.scopeID)
	}
	if string(publisher.frame) != string(message) {
		t.Fatalf("expected relay payload to remain unchanged, got %s", publisher.frame)
	}

	var localFrame map[string]any
	if err := json.Unmarshal(publisher.localFrame, &localFrame); err != nil {
		t.Fatalf("local frame is not JSON: %v", err)
	}
	if localFrame["event_id"] != publisher.eventID {
		t.Fatalf("expected local frame event_id %q, got %v", publisher.eventID, localFrame["event_id"])
	}

	hub.BroadcastToScopeDedup(ScopeWorkspace, "workspace-1", injectEventID(message, publisher.eventID), publisher.eventID)
	select {
	case duplicate := <-client.send:
		t.Fatalf("expected redis loopback to dedup, got duplicate %s", duplicate)
	case <-time.After(20 * time.Millisecond):
	}
}

func attachRealtimeTestClient(hub *Hub, scopeType, scopeID string) *Client {
	client := &Client{
		send:          make(chan []byte, 2),
		workspaceID:   "workspace-1",
		userID:        "user-1",
		subscriptions: map[scopeKey]bool{},
	}
	key := sk(scopeType, scopeID)
	client.subscriptions[key] = true

	hub.mu.Lock()
	hub.clients[client] = true
	hub.rooms[key] = map[*Client]bool{client: true}
	hub.mu.Unlock()

	return client
}

type localFirstPublisher struct {
	t      *testing.T
	client *Client

	called     bool
	scopeType  string
	scopeID    string
	exclude    string
	frame      []byte
	eventID    string
	localFrame []byte
}

func (p *localFirstPublisher) PublishWithID(scopeType, scopeID, exclude string, frame []byte, id string) error {
	p.called = true
	p.scopeType = scopeType
	p.scopeID = scopeID
	p.exclude = exclude
	p.frame = append([]byte(nil), frame...)
	p.eventID = id

	select {
	case p.localFrame = <-p.client.send:
	default:
		p.t.Fatal("expected local fanout to happen before relay publish")
	}
	return nil
}
