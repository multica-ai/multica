package daemon

import (
	"testing"
	"time"
)

func TestSharedMemoryCaptureQueueShutdownFlushesPendingBatch(t *testing.T) {
	flushed := make(chan []sharedMemoryCaptureEvent, 1)
	queue := newSharedMemoryCaptureQueueWith(time.Hour, func(batch []sharedMemoryCaptureEvent) {
		flushed <- batch
	})
	events := []sharedMemoryCaptureEvent{
		{TaskID: "task-1"},
		{TaskID: "task-2"},
		{TaskID: "task-3"},
	}
	for _, event := range events {
		if !queue.enqueue(event) {
			t.Fatalf("queue rejected event %q before shutdown", event.TaskID)
		}
	}

	queue.shutdown()
	select {
	case batch := <-flushed:
		if len(batch) != len(events) {
			t.Fatalf("shutdown flushed %d events, want %d", len(batch), len(events))
		}
		for i, event := range events {
			if batch[i].TaskID != event.TaskID {
				t.Errorf("batch[%d].TaskID = %q, want %q", i, batch[i].TaskID, event.TaskID)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown returned without flushing the pending capture batch")
	}
	if queue.enqueue(sharedMemoryCaptureEvent{TaskID: "late"}) {
		t.Fatal("queue accepted an event after shutdown")
	}
}
