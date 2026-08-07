package outbox

// metrics_test.go — the swappable sink. The ref exists because adapters are
// wired before the Prometheus registry is built, so the contract that matters
// is that an unset ref is harmless and a swap is safe under concurrent use.

import (
	"sync"
	"testing"
)

func TestMetricsRef_UnsetRefDiscardsSilently(t *testing.T) {
	t.Parallel()
	ref := NewMetricsRef()
	// This is the boot window: adapters are already recording, the registry
	// does not exist yet. It must not panic.
	ref.RecordEnqueued(testChannelType, EnqueuePathRealtime, "chat_done")
	ref.RecordDelivery(testChannelType, OutcomeSent)
	ref.RecordReconcileRaceLost(testChannelType)
}

func TestMetricsRef_NilRefIsUsable(t *testing.T) {
	t.Parallel()
	var ref *MetricsRef
	ref.Set(newRecordingMetrics())
	ref.RecordDelivery(testChannelType, OutcomeSent)
}

func TestMetricsRef_ForwardsAfterSet(t *testing.T) {
	t.Parallel()
	ref := NewMetricsRef()
	sink := newRecordingMetrics()
	ref.Set(sink)

	ref.RecordEnqueued(testChannelType, EnqueuePathRealtime, "chat_done")
	ref.RecordDelivery(testChannelType, OutcomeSent)
	ref.RecordReconcileRaceLost(testChannelType)

	if sink.enqueued != 1 {
		t.Errorf("enqueued = %d, want 1", sink.enqueued)
	}
	if sink.delivery[OutcomeSent] != 1 {
		t.Errorf("sent = %d, want 1", sink.delivery[OutcomeSent])
	}
	if sink.raceLosses != 1 {
		t.Errorf("race losses = %d, want 1", sink.raceLosses)
	}
}

func TestMetricsRef_SetNilRevertsToDiscarding(t *testing.T) {
	t.Parallel()
	ref := NewMetricsRef()
	sink := newRecordingMetrics()
	ref.Set(sink)
	ref.Set(nil)
	ref.RecordDelivery(testChannelType, OutcomeSent)
	if sink.delivery[OutcomeSent] != 0 {
		t.Error("a nil Set must stop forwarding")
	}
}

// main.go swaps the sink while adapters are already running, so the swap races
// live observations by construction.
func TestMetricsRef_ConcurrentSetAndRecord(t *testing.T) {
	t.Parallel()
	ref := NewMetricsRef()
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(2)
		go func() { defer wg.Done(); ref.Set(newRecordingMetrics()) }()
		go func() { defer wg.Done(); ref.RecordDelivery(testChannelType, OutcomeSent) }()
	}
	wg.Wait()
}

func TestNoopMetrics_AcceptsEverything(t *testing.T) {
	t.Parallel()
	m := NoopMetrics()
	m.RecordEnqueued("", "", "")
	m.RecordDelivery("", "")
	m.RecordReconcileRaceLost("")
}
