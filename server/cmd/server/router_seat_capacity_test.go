package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/seatcapacity"
)

func TestSeatCapacityAssemblyUsesCloudURLAlone(t *testing.T) {
	executor := seatCapacityExecutor("https://cloud.internal")
	if executor == nil {
		t.Fatal("Cloud-connected router assembled a nil capacity executor")
	}
	if !seatcapacity.CanRunWorker(executor) {
		t.Fatal("Cloud URL did not assemble a recovery-capable capacity executor")
	}
}
