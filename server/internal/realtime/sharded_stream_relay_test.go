package realtime

import (
	"encoding/json"
	"testing"
	"time"

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
