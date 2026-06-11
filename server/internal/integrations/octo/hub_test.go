package octo

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/octo/transport"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeHubQueries struct {
	insts       []db.OctoInstallation
	mu          sync.Mutex
	acquired    int
	released    int
	leaseDenied bool
}

func (f *fakeHubQueries) ListActiveOctoInstallations(ctx context.Context) ([]db.OctoInstallation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]db.OctoInstallation, len(f.insts))
	copy(out, f.insts)
	return out, nil
}
func (f *fakeHubQueries) setInsts(insts []db.OctoInstallation) {
	f.mu.Lock()
	f.insts = insts
	f.mu.Unlock()
}
func (f *fakeHubQueries) AcquireOctoWSLease(ctx context.Context, arg db.AcquireOctoWSLeaseParams) (db.OctoInstallation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.leaseDenied {
		return db.OctoInstallation{}, pgx.ErrNoRows
	}
	f.acquired++
	return db.OctoInstallation{ID: arg.ID}, nil
}
func (f *fakeHubQueries) ReleaseOctoWSLease(ctx context.Context, arg db.ReleaseOctoWSLeaseParams) error {
	f.mu.Lock()
	f.released++
	f.mu.Unlock()
	return nil
}
func (f *fakeHubQueries) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acquired, f.released
}

type fakeHubDispatch struct {
	mu      sync.Mutex
	msgs    []InboundMessage
	outcome Outcome
}

func (f *fakeHubDispatch) Handle(ctx context.Context, msg InboundMessage) (DispatchResult, error) {
	f.mu.Lock()
	f.msgs = append(f.msgs, msg)
	oc := f.outcome
	f.mu.Unlock()
	if oc == "" {
		oc = OutcomeIngested
	}
	return DispatchResult{Outcome: oc, SenderUID: msg.SenderUID}, nil
}
func (f *fakeHubDispatch) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs)
}

// recordingReplier captures the outcomes the hub forwards to the replier.
type recordingReplier struct {
	mu       sync.Mutex
	outcomes []Outcome
}

func (r *recordingReplier) Reply(_ context.Context, _ db.OctoInstallation, _ InboundMessage, res DispatchResult) {
	r.mu.Lock()
	r.outcomes = append(r.outcomes, res.Outcome)
	r.mu.Unlock()
}
func (r *recordingReplier) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.outcomes)
}

func hubInst() db.OctoInstallation {
	return db.OctoInstallation{ID: validUUID(0xAB), RobotID: "robot_hub", Status: "active"}
}

func TestHub_AcquiresLeaseRunsConnectorDispatches(t *testing.T) {
	q := &fakeHubQueries{insts: []db.OctoInstallation{hubInst()}}
	disp := &fakeHubDispatch{}

	emitted := make(chan struct{})
	factory := func(inst db.OctoInstallation) (Connector, error) {
		return connectorFunc(func(ctx context.Context, inst db.OctoInstallation, onMessage func(transport.BotMessage)) error {
			onMessage(transport.BotMessage{
				MessageID:   "m1",
				FromUID:     "uid1",
				ChannelID:   "ch1",
				ChannelType: transport.ChannelDM,
				Payload:     transport.MessagePayload{Type: transport.MsgText, Content: "hi"},
			})
			close(emitted)
			<-ctx.Done()
			return ctx.Err()
		}), nil
	}

	hub := NewHub(q, factory, disp, HubConfig{
		LeaseTTL:           time.Second,
		LeaseRenewInterval: time.Second,
		SweepInterval:      time.Second,
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { hub.Run(ctx); close(done) }()

	select {
	case <-emitted:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("connector never emitted a message")
	}

	// Give the dispatch goroutine a beat to record.
	time.Sleep(50 * time.Millisecond)
	if disp.count() == 0 {
		cancel()
		t.Fatal("message was not dispatched")
	}
	if got := disp.msgs[0]; got.RobotID != "robot_hub" || got.Body != "hi" || !got.AddressedToBot {
		t.Errorf("dispatched msg = %+v", got)
	}

	acquired, _ := q.counts()
	if acquired == 0 {
		t.Error("lease was never acquired")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("hub did not shut down after ctx cancel")
	}
	if _, released := q.counts(); released == 0 {
		t.Error("lease was not released on shutdown")
	}
}

func TestHub_LeaseDeniedDoesNotRunConnector(t *testing.T) {
	q := &fakeHubQueries{insts: []db.OctoInstallation{hubInst()}, leaseDenied: true}
	disp := &fakeHubDispatch{}

	ran := make(chan struct{}, 1)
	factory := func(inst db.OctoInstallation) (Connector, error) {
		return connectorFunc(func(ctx context.Context, inst db.OctoInstallation, onMessage func(transport.BotMessage)) error {
			select {
			case ran <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return ctx.Err()
		}), nil
	}

	hub := NewHub(q, factory, disp, HubConfig{
		LeaseTTL:           time.Second,
		LeaseRenewInterval: 200 * time.Millisecond,
		SweepInterval:      time.Second,
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	defer cancel()

	select {
	case <-ran:
		t.Error("connector ran despite lease denial")
	case <-time.After(600 * time.Millisecond):
		// expected: lease denied, connector never started
	}
}

// connectorFunc adapts a function to the Connector interface.
type connectorFunc func(ctx context.Context, inst db.OctoInstallation, onMessage func(transport.BotMessage)) error

func (f connectorFunc) Run(ctx context.Context, inst db.OctoInstallation, onMessage func(transport.BotMessage)) error {
	return f(ctx, inst, onMessage)
}

// TestHub_InvokesReplierWithDispatchOutcome guards the regression that started
// this work: the hub used to discard the DispatchResult, so NeedsBinding (and
// the agent-unavailable outcomes) never produced an outbound reply. The hub
// must forward every dispatched message's outcome to the replier.
func TestHub_InvokesReplierWithDispatchOutcome(t *testing.T) {
	q := &fakeHubQueries{insts: []db.OctoInstallation{hubInst()}}
	disp := &fakeHubDispatch{outcome: OutcomeNeedsBinding}
	replier := &recordingReplier{}

	emitted := make(chan struct{})
	factory := func(inst db.OctoInstallation) (Connector, error) {
		return connectorFunc(func(ctx context.Context, inst db.OctoInstallation, onMessage func(transport.BotMessage)) error {
			onMessage(transport.BotMessage{
				MessageID:   "m1",
				FromUID:     "uid1",
				ChannelID:   "uid1",
				ChannelType: transport.ChannelDM,
				Payload:     transport.MessagePayload{Type: transport.MsgText, Content: "hi"},
			})
			close(emitted)
			<-ctx.Done()
			return ctx.Err()
		}), nil
	}

	hub := NewHub(q, factory, disp, HubConfig{
		LeaseTTL:           time.Second,
		LeaseRenewInterval: time.Second,
		SweepInterval:      time.Second,
	}, nil)
	hub.SetOutcomeReplier(replier)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	select {
	case <-emitted:
	case <-time.After(3 * time.Second):
		t.Fatal("connector never emitted a message")
	}
	time.Sleep(50 * time.Millisecond)

	if replier.count() == 0 {
		t.Fatal("replier was never invoked with the dispatch outcome")
	}
	if got := replier.outcomes[0]; got != OutcomeNeedsBinding {
		t.Errorf("replier got outcome %q, want %q", got, OutcomeNeedsBinding)
	}
}

// TestHub_WaitWithTimeout_ReleasesLeaseBeforeReturning guards the graceful
// shutdown fix: after the Run context is cancelled, WaitWithTimeout must block
// until the supervisor has released its lease and only then return true. This
// is the contract main.go relies on so the next replica can take over without
// waiting out the full LeaseTTL.
func TestHub_WaitWithTimeout_ReleasesLeaseBeforeReturning(t *testing.T) {
	q := &fakeHubQueries{insts: []db.OctoInstallation{hubInst()}}
	disp := &fakeHubDispatch{}

	running := make(chan struct{}, 1)
	factory := func(inst db.OctoInstallation) (Connector, error) {
		return connectorFunc(func(ctx context.Context, inst db.OctoInstallation, onMessage func(transport.BotMessage)) error {
			select {
			case running <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return ctx.Err()
		}), nil
	}

	hub := NewHub(q, factory, disp, HubConfig{
		LeaseTTL:           time.Second,
		LeaseRenewInterval: time.Second,
		SweepInterval:      time.Second,
		ShutdownTimeout:    3 * time.Second,
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	select {
	case <-running:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("connector never started")
	}

	// Cancel and join. WaitWithTimeout must return true (supervisor exited in
	// time) and the lease must already be released by then.
	cancel()
	if ok := hub.WaitWithTimeout(hub.ShutdownTimeout()); !ok {
		t.Fatal("WaitWithTimeout timed out; supervisor did not exit")
	}
	if _, released := q.counts(); released == 0 {
		t.Error("lease was not released by the time WaitWithTimeout returned")
	}
}

// TestHub_ShutdownTimeoutDefault confirms a zero ShutdownTimeout falls back to
// the default rather than 0 (which WaitWithTimeout would treat as unbounded).
func TestHub_ShutdownTimeoutDefault(t *testing.T) {
	hub := NewHub(&fakeHubQueries{}, nil, &fakeHubDispatch{}, HubConfig{}, nil)
	if got := hub.ShutdownTimeout(); got != defaultShutdownTimeout {
		t.Errorf("ShutdownTimeout() = %v, want default %v", got, defaultShutdownTimeout)
	}
}

// TestHub_ReconfigureRestartsSupervisor guards the reconfigure fix: when an
// installation's updated_at advances (token rotation / re-register), the hub
// must cancel the stale supervisor and restart it so the connector picks up the
// new config. A running connector holds an in-memory snapshot, so an in-place
// DB update alone never reaches the live connection.
func TestHub_ReconfigureRestartsSupervisor(t *testing.T) {
	t0 := time.Unix(1000, 0)
	inst := hubInst()
	inst.UpdatedAt = pgtype.Timestamptz{Time: t0, Valid: true}
	q := &fakeHubQueries{insts: []db.OctoInstallation{inst}}
	disp := &fakeHubDispatch{}

	type run struct{ updatedAt time.Time }
	runs := make(chan run, 4)
	factory := func(inst db.OctoInstallation) (Connector, error) {
		return connectorFunc(func(ctx context.Context, inst db.OctoInstallation, onMessage func(transport.BotMessage)) error {
			runs <- run{updatedAt: inst.UpdatedAt.Time}
			<-ctx.Done()
			return ctx.Err()
		}), nil
	}

	hub := NewHub(q, factory, disp, HubConfig{
		LeaseTTL:           time.Second,
		LeaseRenewInterval: time.Second,
		SweepInterval:      150 * time.Millisecond,
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	// First run uses the original config.
	select {
	case r := <-runs:
		if !r.updatedAt.Equal(t0) {
			t.Fatalf("first run updatedAt = %v, want %v", r.updatedAt, t0)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connector never started")
	}

	// Reconfigure: bump updated_at. The hub should cancel the stale supervisor
	// and a later sweep should restart the connector with the new config.
	t1 := time.Unix(2000, 0)
	reconf := hubInst()
	reconf.UpdatedAt = pgtype.Timestamptz{Time: t1, Valid: true}
	q.setInsts([]db.OctoInstallation{reconf})

	select {
	case r := <-runs:
		if !r.updatedAt.Equal(t1) {
			t.Fatalf("restarted run updatedAt = %v, want %v (new config not applied)", r.updatedAt, t1)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("connector was not restarted after reconfigure")
	}
}
