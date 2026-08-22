package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestRealtimeCollectorExposesCounters(t *testing.T) {
	m := &realtime.Metrics{}
	m.ActiveConnections.Store(3)
	m.MessagesSentTotal.Store(11)
	m.InboundTooLargeTotal.Store(7)
	m.RedisConnected.Store(true)
	m.RedisMirrorPrimaryErrors.Store(2)
	m.RedisMirrorSecondaryErrors.Store(5)
	m.RedisRelayStreamTrimmedTotal.Store(13)
	m.RedisRelayStreamMissingTotal.Store(1)
	m.RedisRelayStreamsWithoutTTL.Store(2)
	m.RedisUsedMemoryBytes.Store(4096)
	m.RedisMaxMemoryBytes.Store(8192)
	m.RedisEvictedKeys.Store(3)
	m.ObserveRedisStream("ws:relay:shard:0", 23, 2048, 60000)

	registry := NewRegistry(RegistryOptions{Realtime: m})
	rec := httptest.NewRecorder()
	NewHandler(registry.Gatherer).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	for _, want := range []string{
		"multica_realtime_active_connections 3",
		"multica_realtime_messages_sent_total 11",
		"multica_realtime_inbound_too_large_total 7",
		"multica_realtime_redis_connected 1",
		`multica_realtime_redis_mirror_errors_total{target="primary"} 2`,
		`multica_realtime_redis_mirror_errors_total{target="secondary"} 5`,
		"multica_realtime_redis_stream_trimmed_entries_total 13",
		"multica_realtime_redis_stream_missing_total 1",
		"multica_realtime_redis_streams_without_ttl 2",
		"multica_realtime_redis_used_memory_bytes 4096",
		"multica_realtime_redis_maxmemory_bytes 8192",
		"multica_realtime_redis_evicted_keys 3",
		`multica_realtime_redis_stream_entries{stream="ws:relay:shard:0"} 23`,
		`multica_realtime_redis_stream_memory_bytes{stream="ws:relay:shard:0"} 2048`,
		`multica_realtime_redis_stream_pttl_milliseconds{stream="ws:relay:shard:0"} 60000`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\n%s", want, body)
		}
	}
}
