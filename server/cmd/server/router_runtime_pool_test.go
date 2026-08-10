package main

import (
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/runtimepool"
	"github.com/redis/go-redis/v9"
)

// Break caught: constructing the scheduler before Router swaps the default
// Noop liveness store for Redis leaves placement reading a different liveness
// authority from heartbeat and stale-runtime handling.
func TestRuntimePoolWiringUsesFinalLiveness(t *testing.T) {
	tests := []struct {
		name string
		rdb  *redis.Client
	}{
		{name: "noop"},
		{name: "redis", rdb: redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.rdb != nil {
				t.Cleanup(func() { _ = test.rdb.Close() })
			}
			_, h := NewRouterWithOptions(
				nil,
				realtime.NewHub(),
				events.New(),
				analytics.NoopClient{},
				test.rdb,
				RouterOptions{},
			)

			scheduler, ok := h.TaskService.RuntimePool.(*runtimepool.Scheduler)
			if !ok {
				t.Fatalf("TaskService.RuntimePool = %T; want exactly one *runtimepool.Scheduler", h.TaskService.RuntimePool)
			}
			gotType, gotPointer := schedulerLivenessIdentity(t, scheduler)
			wantType, wantPointer := livenessIdentity(reflect.ValueOf(h.LivenessStore))
			if gotType != wantType || gotPointer != wantPointer {
				t.Fatalf("scheduler liveness = (%v,%x); Handler final liveness = (%v,%x)", gotType, gotPointer, wantType, wantPointer)
			}
		})
	}
}

func schedulerLivenessIdentity(t *testing.T, scheduler *runtimepool.Scheduler) (reflect.Type, uintptr) {
	t.Helper()
	field := reflect.ValueOf(scheduler).Elem().FieldByName("liveness")
	if !field.IsValid() {
		t.Fatal("runtimepool.Scheduler liveness field not found")
	}
	return livenessIdentity(field)
}

func livenessIdentity(value reflect.Value) (reflect.Type, uintptr) {
	for value.Kind() == reflect.Interface {
		value = value.Elem()
	}
	if value.Kind() == reflect.Pointer {
		return value.Type(), value.Pointer()
	}
	return value.Type(), 0
}
