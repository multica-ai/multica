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

func TestShardedStreamRelayConfigDefaults(t *testing.T) {
	relay := NewShardedStreamRelay(NewHub(), nil, nil, ShardedStreamRelayConfig{})

	if relay.config.Shards != defaultShardedRelayShards {
		t.Fatalf("expected default shard count %d, got %d", defaultShardedRelayShards, relay.config.Shards)
	}
	if relay.config.StreamMaxLen != 2000 {
		t.Fatalf("expected default stream max len 2000, got %d", relay.config.StreamMaxLen)
	}
	if relay.config.ReadCount != defaultShardedRelayReadCount {
		t.Fatalf("expected default read count %d, got %d", defaultShardedRelayReadCount, relay.config.ReadCount)
	}
	if relay.config.ReadBlock != defaultShardedRelayReadBlock {
		t.Fatalf("expected default read block %s, got %s", defaultShardedRelayReadBlock, relay.config.ReadBlock)
	}
	if relay.config.ReplayGrace != defaultShardedRelayReplayGrace {
		t.Fatalf("expected default replay grace %s, got %s", defaultShardedRelayReplayGrace, relay.config.ReplayGrace)
	}
	if relay.config.TrimHorizon != defaultRelayStreamTrimHorizon {
		t.Fatalf("expected default trim horizon %s, got %s", defaultRelayStreamTrimHorizon, relay.config.TrimHorizon)
	}
	if relay.config.StreamTTL != defaultRelayStreamTTL {
		t.Fatalf("expected default stream TTL %s, got %s", defaultRelayStreamTTL, relay.config.StreamTTL)
	}
	if relay.config.StreamTTLEnabled {
		t.Fatal("stream TTL must default to disabled for staged rollout safety")
	}
	if err := relay.config.Validate(); err != nil {
		t.Fatalf("default config is invalid: %v", err)
	}
}

func TestShardedStreamRelayConfigNormalizesRetentionInvariants(t *testing.T) {
	cfg := ShardedStreamRelayConfig{
		ReplayGrace:         20 * time.Minute,
		TrimHorizon:         10 * time.Minute,
		StreamTTL:           15 * time.Minute,
		TTLRefreshInterval:  time.Hour,
		MaintenanceInterval: time.Hour,
	}.Normalized()

	if cfg.TrimHorizon != 40*time.Minute {
		t.Fatalf("trim horizon = %s, want 40m", cfg.TrimHorizon)
	}
	if cfg.StreamTTL != 60*time.Minute {
		t.Fatalf("stream TTL = %s, want 60m", cfg.StreamTTL)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("normalized config is invalid: %v", err)
	}
}

func TestShardedStreamRelayShardForScopeIsStableAndBounded(t *testing.T) {
	relay := NewShardedStreamRelay(NewHub(), nil, nil, ShardedStreamRelayConfig{Shards: 8})

	first := relay.shardFor(ScopeWorkspace, "workspace-1")
	second := relay.shardFor(ScopeWorkspace, "workspace-1")
	if first != second {
		t.Fatalf("expected stable shard selection, got %d then %d", first, second)
	}
	if first < 0 || first >= relay.config.Shards {
		t.Fatalf("shard %d out of range [0,%d)", first, relay.config.Shards)
	}
}

func TestShardedStreamRelayDeliverMessageUsesEnvelopeScope(t *testing.T) {
	hub := NewHub()
	client := attachRealtimeTestClient(hub, ScopeTask, "task-1")
	relay := NewShardedStreamRelay(hub, nil, nil, ShardedStreamRelayConfig{})
	ev := envelope{
		EventID:     "event-1",
		Scope:       ScopeTask,
		ScopeID:     "task-1",
		PayloadJSON: `{"type":"task:updated"}`,
	}

	relay.deliverMessage(redis.XMessage{Values: envelopeRedisValues(ev)})

	select {
	case raw := <-client.send:
		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("delivered frame is not JSON: %v", err)
		}
		if frame["event_id"] != ev.EventID {
			t.Fatalf("expected event_id %q, got %v", ev.EventID, frame["event_id"])
		}
	case <-time.After(time.Second):
		t.Fatal("expected sharded relay message to be delivered")
	}

	relay.deliverMessage(redis.XMessage{Values: envelopeRedisValues(ev)})
	select {
	case duplicate := <-client.send:
		t.Fatalf("expected duplicate event id to be deduped, got %s", duplicate)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestShardedStreamRelayReplayStartIDIsBounded(t *testing.T) {
	grace := 5 * time.Minute
	relay := NewShardedStreamRelay(NewHub(), nil, nil, ShardedStreamRelayConfig{
		ReplayGrace: grace,
	})

	before := time.Now().Add(-grace).UnixMilli()
	id := relay.replayStartID()
	after := time.Now().Add(-grace).UnixMilli()

	// The ID should be in the format "<millis>-0".
	var ms int64
	var seq int
	n, _ := fmt.Sscanf(id, "%d-%d", &ms, &seq)
	if n != 2 {
		t.Fatalf("replayStartID() = %q, want format <millis>-0", id)
	}
	if seq != 0 {
		t.Fatalf("replayStartID() sequence = %d, want 0", seq)
	}
	// The timestamp should be within [before, after] (inclusive).
	if ms < before || ms > after {
		t.Fatalf("replayStartID() timestamp %d outside expected window [%d, %d]", ms, before, after)
	}
}

func TestShardedStreamRelayReadShardOnceReplaysRetainedMessages(t *testing.T) {
	hub := NewHub()
	client := attachRealtimeTestClient(hub, ScopeTask, "task-replay")
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	grace := 5 * time.Minute
	relay := NewShardedStreamRelay(hub, rdb, rdb, ShardedStreamRelayConfig{
		Shards:      1,
		ReadCount:   2,
		ReadBlock:   time.Millisecond,
		ReplayGrace: grace,
	})
	stream := ShardedStreamKey(0)

	// The initial cursor must be a bounded time-window, not "$".
	lastID := relay.replayStartID()
	if lastID == "$" {
		t.Fatal("replayStartID() returned \"$\", want a bounded time-window cursor")
	}

	// Expect an XREAD starting from the bounded cursor (not "$").
	mock.ExpectXRead(&redis.XReadArgs{
		Streams: []string{stream, lastID},
		Count:   relay.config.ReadCount,
		Block:   relay.config.ReadBlock,
	}).SetVal([]redis.XStream{{
		Stream: stream,
		Messages: []redis.XMessage{{
			ID: "1710000000000-0",
			Values: envelopeRedisValues(envelope{
				EventID:     "event-replayed",
				Scope:       ScopeTask,
				ScopeID:     "task-replay",
				PayloadJSON: `{"type":"task:updated"}`,
			}),
		}},
	}})

	if !relay.readShardOnce(context.Background(), 0, stream, &lastID) {
		t.Fatal("expected shard reader to continue after a successful replay read")
	}
	if lastID != "1710000000000-0" {
		t.Fatalf("expected last ID to advance to %q, got %q", "1710000000000-0", lastID)
	}

	select {
	case raw := <-client.send:
		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("delivered frame is not JSON: %v", err)
		}
		if frame["event_id"] != "event-replayed" {
			t.Fatalf("expected replayed event_id %q, got %v", "event-replayed", frame["event_id"])
		}
	case <-time.After(time.Second):
		t.Fatal("expected retained stream message to be delivered on replay")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestShardedStreamRelayMaintenanceTrimsExactlyAndRepairsTTL(t *testing.T) {
	M.Reset()
	t.Cleanup(M.Reset)
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	now := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	relay := NewShardedStreamRelay(NewHub(), rdb, rdb, ShardedStreamRelayConfig{
		Shards:           1,
		ReplayGrace:      5 * time.Minute,
		TrimHorizon:      10 * time.Minute,
		StreamTTL:        15 * time.Minute,
		StreamTTLEnabled: true,
	})
	relay.now = func() time.Time { return now }
	relay.ttl.now = relay.now
	stream := ShardedStreamKey(0)
	minID := streamMinID(now, 10*time.Minute)

	mock.ExpectExists(stream).SetVal(1)
	mock.ExpectXTrimMinID(stream, minID).SetVal(17)
	mock.ExpectPTTL(stream).SetVal(-1)
	mock.ExpectPExpire(stream, 15*time.Minute).SetVal(true)
	mock.ExpectXLen(stream).SetVal(23)
	mock.ExpectMemoryUsage(stream).SetVal(4096)
	mock.ExpectInfo("memory").SetVal("used_memory:8192\r\nmaxmemory:65536\r\n")
	mock.ExpectInfo("stats").SetVal("evicted_keys:3\r\n")

	relay.maintainStreams(context.Background())

	if got := M.RedisRelayStreamTrimmedTotal.Load(); got != 17 {
		t.Fatalf("trimmed total = %d, want 17", got)
	}
	observation := M.RedisStreamObservations()[stream]
	if observation.Entries != 23 || observation.MemoryBytes != 4096 || observation.PTTLMillis != (15*time.Minute).Milliseconds() {
		t.Fatalf("unexpected observation: %+v", observation)
	}
	if got := M.RedisUsedMemoryBytes.Load(); got != 8192 {
		t.Fatalf("used memory = %d, want 8192", got)
	}
	if got := M.RedisMaxMemoryBytes.Load(); got != 65536 {
		t.Fatalf("max memory = %d, want 65536", got)
	}
	if got := M.RedisEvictedKeys.Load(); got != 3 {
		t.Fatalf("evicted keys = %d, want 3", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestShardedStreamRelayMaintenanceCountsTTLRepairFailure(t *testing.T) {
	M.Reset()
	t.Cleanup(M.Reset)
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	relay := NewShardedStreamRelay(NewHub(), rdb, rdb, ShardedStreamRelayConfig{
		Shards:           1,
		StreamTTLEnabled: true,
	})
	relay.now = func() time.Time { return now }
	relay.ttl.now = relay.now
	stream := ShardedStreamKey(0)

	mock.ExpectExists(stream).SetVal(1)
	mock.ExpectXTrimMinID(stream, streamMinID(now, relay.config.TrimHorizon)).SetVal(0)
	mock.ExpectPTTL(stream).SetVal(-1)
	mock.ExpectPExpire(stream, relay.config.StreamTTL).SetErr(errors.New("PEXPIRE denied"))
	mock.ExpectXLen(stream).SetVal(1)
	mock.ExpectMemoryUsage(stream).SetVal(1024)
	mock.ExpectInfo("memory").SetVal("used_memory:1024\r\nmaxmemory:2048\r\n")
	mock.ExpectInfo("stats").SetVal("evicted_keys:0\r\n")

	relay.maintainStreams(context.Background())

	if got := M.RedisRelayStreamsWithoutTTL.Load(); got != 1 {
		t.Fatalf("streams without TTL = %d, want 1", got)
	}
	if got := M.RedisRelayRetentionErrors.Load(); got != 1 {
		t.Fatalf("retention errors = %d, want 1", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestShardedStreamRelayMissingTransitionResetsGenerationOnce(t *testing.T) {
	M.Reset()
	t.Cleanup(M.Reset)
	relay := NewShardedStreamRelay(NewHub(), nil, nil, ShardedStreamRelayConfig{Shards: 1})

	relay.updateStreamPresence(0, true)
	relay.updateStreamPresence(0, false)
	relay.updateStreamPresence(0, false)

	if got := relay.streamGeneration[0].Load(); got != 1 {
		t.Fatalf("generation = %d, want 1", got)
	}
	if got := M.RedisRelayStreamMissingTotal.Load(); got != 1 {
		t.Fatalf("missing total = %d, want 1", got)
	}
}
