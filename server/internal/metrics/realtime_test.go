package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dwickyfp/wallts/server/internal/realtime"
)

func TestRealtimeCollectorExposesCounters(t *testing.T) {
	m := &realtime.Metrics{}
	m.ActiveConnections.Store(3)
	m.MessagesSentTotal.Store(11)
	m.RedisConnected.Store(true)
	m.RedisMirrorPrimaryErrors.Store(2)
	m.RedisMirrorSecondaryErrors.Store(5)

	registry := NewRegistry(RegistryOptions{Realtime: m})
	rec := httptest.NewRecorder()
	NewHandler(registry.Gatherer).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	for _, want := range []string{
		"wallts_realtime_active_connections 3",
		"wallts_realtime_messages_sent_total 11",
		"wallts_realtime_redis_connected 1",
		`wallts_realtime_redis_mirror_errors_total{target="primary"} 2`,
		`wallts_realtime_redis_mirror_errors_total{target="secondary"} 5`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\n%s", want, body)
		}
	}
}
