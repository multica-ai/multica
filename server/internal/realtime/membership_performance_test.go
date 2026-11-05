package realtime

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"
)

// Regression adapted from the independent review probe: every identity is valid and
// every database query succeeds before the configured two-second deadline.
func TestMembershipBurstDoesNotRejectValidWriters(t *testing.T) {
	for _, tc := range []struct {
		writers int
		latency time.Duration
	}{
		{1024, 5 * time.Millisecond},
		{1024, 800 * time.Millisecond},
		{4096, 200 * time.Millisecond},
	} {
		t.Run(fmt.Sprintf("writers=%d/query=%s", tc.writers, tc.latency), func(t *testing.T) {
			hub := NewHub()
			var calls, active, peak, queryErrors atomic.Int32
			hub.SetMembershipChecker(membershipBatchOverride{
				batch: func(ctx context.Context, identities []MembershipIdentity) (map[MembershipIdentity]bool, error) {
					calls.Add(1)
					n := active.Add(1)
					defer active.Add(-1)
					for p := peak.Load(); n > p; p = peak.Load() {
						if peak.CompareAndSwap(p, n) {
							break
						}
					}
					timer := time.NewTimer(tc.latency)
					defer timer.Stop()
					select {
					case <-timer.C:
					case <-ctx.Done():
						queryErrors.Add(1)
						return nil, ctx.Err()
					}
					members := make(map[MembershipIdentity]bool, len(identities))
					for _, identity := range identities {
						members[identity] = true
					}
					return members, nil
				},
			})
			workerDone := make(chan struct{})
			hub.writeMembershipOnce.Do(func() { go func() { hub.runMembershipChecks(); close(workerDone) }() })
			start := make(chan struct{})
			latencies := make([]time.Duration, tc.writers)
			var denied atomic.Int32
			var wg sync.WaitGroup
			wg.Add(tc.writers)
			for i := 0; i < tc.writers; i++ {
				go func(i int) {
					defer wg.Done()
					<-start
					begin := time.Now()
					if !hub.authorizeWrites(MembershipIdentity{WorkspaceID: fmt.Sprintf("workspace-%d", i%32), UserID: fmt.Sprintf("user-%d", i)}) {
						denied.Add(1)
					}
					latencies[i] = time.Since(begin)
				}(i)
			}
			close(start)
			wg.Wait()
			close(hub.writeMembership)
			<-workerDone
			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			t.Logf("valid=%d denied=%d queries=%d query_errors=%d peak_query_concurrency=%d p50=%s p95=%s max=%s", tc.writers, denied.Load(), calls.Load(), queryErrors.Load(), peak.Load(), latencies[len(latencies)/2].Round(time.Millisecond), latencies[len(latencies)*95/100].Round(time.Millisecond), latencies[len(latencies)-1].Round(time.Millisecond))
			if queryErrors.Load() != 0 {
				t.Fatal("unexpected query error")
			}
			if denied.Load() != 0 {
				t.Fatal("queue rejected valid writers")
			}
			if peak.Load() > membershipWorkers || (tc.latency != 5*time.Millisecond && peak.Load() < 2) {
				t.Fatal("membership queries did not use bounded concurrency")
			}
		})
	}
}

// A caller that already exhausted its queue budget must do no database work.
func TestMembershipExpiredQueuedRequestDoesNotQuery(t *testing.T) {
	hub := NewHub()
	var calls atomic.Int32
	hub.SetMembershipChecker(membershipBatchOverride{
		batch: func(_ context.Context, identities []MembershipIdentity) (map[MembershipIdentity]bool, error) {
			calls.Add(1)
			return map[MembershipIdentity]bool{identities[0]: true}, nil
		},
	})
	// Hold the worker while a request exhausts its queue budget.
	hub.writeMembershipOnce.Do(func() {})
	if hub.authorizeWrites(MembershipIdentity{WorkspaceID: "workspace", UserID: "valid-user"}) {
		t.Fatal("expected queue timeout")
	}
	close(hub.writeMembership)
	hub.runMembershipChecks()
	t.Logf("database queries for already expired writer: %d", calls.Load())
	if calls.Load() != 0 {
		t.Fatal("expired caller triggered a database query")
	}
}

func TestMembershipCancelledRequestDoesNotEnterLiveCohort(t *testing.T) {
	hub := NewHub()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	dead := membershipWriteRequest{ctx: cancelled, identity: MembershipIdentity{UserID: "cancelled"}, result: make(chan bool, 1)}
	live := membershipWriteRequest{ctx: context.Background(), identity: MembershipIdentity{UserID: "current"}, result: make(chan bool, 1)}
	var calls atomic.Int32
	hub.SetMembershipChecker(membershipBatchOverride{batch: func(_ context.Context, identities []MembershipIdentity) (map[MembershipIdentity]bool, error) {
		calls.Add(1)
		if len(identities) != 1 || identities[0] != live.identity {
			return nil, fmt.Errorf("cancelled request entered the SQL cohort: %v", identities)
		}
		return map[MembershipIdentity]bool{live.identity: true}, nil
	}})
	hub.writeMembership <- dead
	hub.writeMembership <- live
	close(hub.writeMembership)
	hub.runMembershipChecks()
	if <-dead.result || !<-live.result || calls.Load() != 1 {
		t.Fatal("cancellation affected another writer or caused extra SQL")
	}
}

func TestMembershipQueryUsesRemainingCallerBudget(t *testing.T) {
	hub := NewHub()
	caller, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	deadline, _ := caller.Deadline()
	var observed time.Time
	request := membershipWriteRequest{ctx: caller, identity: MembershipIdentity{UserID: "current"}, result: make(chan bool, 1)}
	hub.SetMembershipChecker(membershipBatchOverride{batch: func(ctx context.Context, identities []MembershipIdentity) (map[MembershipIdentity]bool, error) {
		observed, _ = ctx.Deadline()
		cancel()
		return map[MembershipIdentity]bool{identities[0]: true}, nil
	}})
	hub.writeMembership <- request
	close(hub.writeMembership)
	hub.runMembershipChecks()
	if <-request.result || observed.IsZero() || observed.After(deadline) {
		t.Fatal("query outlived its callers or granted an expired authorization")
	}
}

// Use the actual shard reader with a mocked XREAD. A workspace with no local
// subscribers should not make another workspace wait for its removal lookup.
func TestMembershipRemovalWithoutSubscribersDoesNotQuery(t *testing.T) {
	hub := NewHub()
	const latency = 300 * time.Millisecond
	var calls atomic.Int32
	hub.SetMembershipChecker(membershipFunc(func(ctx context.Context, _, _ string) (bool, error) {
		calls.Add(1)
		timer := time.NewTimer(latency)
		defer timer.Stop()
		select {
		case <-timer.C:
			return false, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}))
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })
	relay := NewShardedStreamRelay(hub, rdb, rdb, ShardedStreamRelayConfig{})
	removedWorkspace := "workspace-with-no-local-clients"
	shard := relay.shardFor(ScopeWorkspace, removedWorkspace)
	var otherWorkspace string
	for i := 0; ; i++ {
		otherWorkspace = fmt.Sprintf("other-workspace-%d", i)
		if relay.shardFor(ScopeWorkspace, otherWorkspace) == shard {
			break
		}
	}
	client := attachRealtimeTestClient(hub, ScopeWorkspace, otherWorkspace)
	client.workspaceID = otherWorkspace
	stream, lastID := ShardedStreamKey(shard), "0-0"
	mock.ExpectXRead(&redis.XReadArgs{
		Streams: []string{stream, lastID}, Count: relay.config.ReadCount, Block: relay.config.ReadBlock,
	}).SetVal([]redis.XStream{{Stream: stream, Messages: []redis.XMessage{
		{ID: "1-0", Values: envelopeRedisValues(envelope{
			EventID: "removal-event", Scope: ScopeWorkspace, ScopeID: removedWorkspace,
			PayloadJSON: fmt.Sprintf(`{"type":"member:removed","payload":{"workspace_id":%q,"user_id":"removed-user"}}`, removedWorkspace),
		})},
		{ID: "2-0", Values: envelopeRedisValues(envelope{
			EventID: "unrelated-update", Scope: ScopeWorkspace, ScopeID: otherWorkspace,
			PayloadJSON: `{"type":"issue:updated"}`,
		})},
	}}})
	finished := make(chan struct{})
	begin := time.Now()
	go func() { relay.readShardOnce(context.Background(), shard, stream, &lastID); close(finished) }()
	select {
	case <-client.send:
		t.Logf("unrelated workspace enqueue delay=%s; removal queries with no local subscriber=%d", time.Since(begin).Round(time.Millisecond), calls.Load())
		if calls.Load() != 0 {
			t.Error("removal queried membership without local recipients")
		}
	case <-time.After(3 * time.Second):
		t.Error("unrelated workspace update was not delivered")
	}
	<-finished
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
