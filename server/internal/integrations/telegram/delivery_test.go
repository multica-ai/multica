package telegram

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

func TestDeliveryCallContextBudget(t *testing.T) {
	for _, tc := range []struct {
		name          string
		leaseTTL      time.Duration
		parentTimeout time.Duration
		want          time.Duration
	}{
		{name: "production", want: 20 * time.Second},
		{name: "short_lease", leaseTTL: 600 * time.Millisecond, want: 200 * time.Millisecond},
		{name: "earlier_parent", leaseTTL: 600 * time.Millisecond, parentTimeout: 50 * time.Millisecond, want: 50 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				parent := context.Background()
				if tc.parentTimeout > 0 {
					var cancel context.CancelFunc
					parent, cancel = context.WithTimeout(parent, tc.parentTimeout)
					defer cancel()
				}
				o := &Outbound{leaseTTL: tc.leaseTTL}
				started := time.Now()
				ctx, cancel := o.callContext(parent)
				defer cancel()
				if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(started.Add(tc.want)) {
					t.Fatalf("call deadline = %v (present=%v), want %v", deadline, ok, started.Add(tc.want))
				}

				time.Sleep(tc.want - time.Nanosecond)
				synctest.Wait()
				if err := ctx.Err(); err != nil {
					t.Fatalf("call canceled before its deadline: %v", err)
				}
				time.Sleep(time.Nanosecond)
				synctest.Wait()
				if err := ctx.Err(); err != context.DeadlineExceeded {
					t.Fatalf("call error at its deadline = %v, want DeadlineExceeded", err)
				}
			})
		})
	}

	t.Run("parent_cancel", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			parent, cancelParent := context.WithCancel(context.Background())
			defer cancelParent()
			o := &Outbound{}
			ctx, cancel := o.callContext(parent)
			defer cancel()
			cancelParent()
			synctest.Wait()
			if err := ctx.Err(); err != context.Canceled {
				t.Fatalf("call error after parent cancellation = %v, want Canceled", err)
			}
		})
	})
}
