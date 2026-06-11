package octo

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/im"
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
	return f.insts, nil
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
	mu   sync.Mutex
	msgs []InboundMessage
}

func (f *fakeHubDispatch) Handle(ctx context.Context, msg InboundMessage) (DispatchResult, error) {
	f.mu.Lock()
	f.msgs = append(f.msgs, msg)
	f.mu.Unlock()
	return DispatchResult{Outcome: OutcomeIngested}, nil
}
func (f *fakeHubDispatch) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs)
}

func hubInst() db.OctoInstallation {
	return db.OctoInstallation{ID: validUUID(0xAB), RobotID: "robot_hub", Status: "active"}
}

func TestHub_AcquiresLeaseRunsConnectorDispatches(t *testing.T) {
	q := &fakeHubQueries{insts: []db.OctoInstallation{hubInst()}}
	disp := &fakeHubDispatch{}

	emitted := make(chan struct{})
	factory := func(inst db.OctoInstallation) (Connector, error) {
		return connectorFunc(func(ctx context.Context, inst db.OctoInstallation, onMessage func(im.BotMessage)) error {
			onMessage(im.BotMessage{
				MessageID:   "m1",
				FromUID:     "uid1",
				ChannelID:   "ch1",
				ChannelType: im.ChannelDM,
				Payload:     im.MessagePayload{Type: im.MsgText, Content: "hi"},
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
		return connectorFunc(func(ctx context.Context, inst db.OctoInstallation, onMessage func(im.BotMessage)) error {
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
type connectorFunc func(ctx context.Context, inst db.OctoInstallation, onMessage func(im.BotMessage)) error

func (f connectorFunc) Run(ctx context.Context, inst db.OctoInstallation, onMessage func(im.BotMessage)) error {
	return f(ctx, inst, onMessage)
}
