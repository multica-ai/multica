package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/multica-ai/multica/server/internal/daemonws"
)

func TestDaemonWSCollectorSeparatesRuntimeGoneDelivery(t *testing.T) {
	m := &daemonws.Metrics{}
	m.WakeupDeliveredHit.Store(3)
	m.WakeupDeliveredMiss.Store(4)
	m.RuntimeGoneDeliveredHit.Store(5)
	m.RuntimeGoneDeliveredMiss.Store(6)

	err := testutil.CollectAndCompare(NewDaemonWSCollector(m), strings.NewReader(`
# HELP multica_daemonws_runtime_gone_delivered_total Total runtime-gone local delivery attempts.
# TYPE multica_daemonws_runtime_gone_delivered_total counter
multica_daemonws_runtime_gone_delivered_total{result="hit"} 5
multica_daemonws_runtime_gone_delivered_total{result="miss"} 6
# HELP multica_daemonws_wakeup_delivered_total Total daemon wakeup local delivery attempts.
# TYPE multica_daemonws_wakeup_delivered_total counter
multica_daemonws_wakeup_delivered_total{result="hit"} 3
multica_daemonws_wakeup_delivered_total{result="miss"} 4
`),
		"multica_daemonws_runtime_gone_delivered_total",
		"multica_daemonws_wakeup_delivered_total",
	)
	if err != nil {
		t.Fatalf("collect daemon websocket delivery metrics: %v", err)
	}
}
