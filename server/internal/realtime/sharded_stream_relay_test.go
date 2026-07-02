package realtime

import (
	"context"
	"encoding/json"
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
	if relay.config.StreamMaxLen != defaultShardedRelayStreamMaxLen {
		t.Fatalf("expected default stream max len %d, got %d", defaultShardedRelayStreamMaxLen, relay.config.StreamMaxLen)
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

func TestShardedRelayReplayStartIDUsesBoundedTimeWindow(t *testing.T) {
	now := time.UnixMilli(1710000005000)
	got := shardedRelayReplayStartID(now, 5*time.Second)
	if got != "1710000000000-0" {
		t.Fatalf("replay start ID = %q, want bounded time-window cursor", got)
	}

	got = shardedRelayReplayStartID(time.UnixMilli(1000), 5*time.Second)
	if got != "0-0" {
		t.Fatalf("replay start ID before Unix epoch = %q, want 0-0 floor", got)
	}
}

func TestShardedStreamRelayReadShardReplaysBoundedRetainedMessages(t *testing.T) {
	hub := NewHub()
	client := attachRealtimeTestClient(hub, ScopeTask, "task-replay")
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	relay := NewShardedStreamRelay(hub, rdb, rdb, ShardedStreamRelayConfig{
		Shards:    1,
		ReadCount: 2,
		ReadBlock: time.Millisecond,
	})
	stream := ShardedStreamKey(0)
	lastID := "1710000000000-0"
	msgID := "1710000001000-0"
	ev := envelope{
		EventID:     "event-replayed",
		Scope:       ScopeTask,
		ScopeID:     "task-replay",
		PayloadJSON: `{"type":"task:updated"}`,
	}

	mock.ExpectXRead(&redis.XReadArgs{
		Streams: []string{stream, lastID},
		Count:   relay.config.ReadCount,
		Block:   relay.config.ReadBlock,
	}).SetVal([]redis.XStream{{
		Stream: stream,
		Messages: []redis.XMessage{{
			ID:     msgID,
			Values: envelopeRedisValues(ev),
		}},
	}})

	if !relay.readShardOnce(context.Background(), 0, stream, &lastID) {
		t.Fatal("expected shard reader to continue after a successful replay read")
	}
	if lastID != msgID {
		t.Fatalf("expected last ID to advance to %q, got %q", msgID, lastID)
	}

	select {
	case raw := <-client.send:
		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("delivered frame is not JSON: %v", err)
		}
		if frame["event_id"] != ev.EventID {
			t.Fatalf("expected replayed event_id %q, got %v", ev.EventID, frame["event_id"])
		}
	case <-time.After(time.Second):
		t.Fatal("expected retained stream message to be delivered")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
