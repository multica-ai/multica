package main

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/seatcapacity"
)

func TestSeatCapacityAssemblyFailsClosedWithoutServiceToken(t *testing.T) {
	t.Setenv("MULTICA_SUBSCRIPTION_CAPACITY_SERVICE_TOKEN", "")
	t.Setenv("MULTICA_SUBSCRIPTION_CAPACITY_TIMEOUT", "3s")

	executor := seatCapacityExecutorFromEnv("https://cloud.internal")
	if executor == nil {
		t.Fatal("Cloud-connected router assembled a nil capacity executor")
	}
	if seatcapacity.CanRunWorker(executor) {
		t.Fatal("unavailable capacity executor was eligible for worker startup")
	}
	if _, err := executor.ReserveInvitation(
		context.Background(), uuid.New(), uuid.New(), time.Now().Add(time.Hour),
	); err == nil {
		t.Fatal("Cloud-connected admission succeeded without a capacity service token")
	}
}
