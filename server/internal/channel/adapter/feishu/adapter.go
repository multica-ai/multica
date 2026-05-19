package feishu

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

// channelName is the registry key feishu adapters register under. Kept as a
// package-level constant rather than a config field because it is also the
// value the dispatcher routes on (see DESIGN §4.1) — making it configurable
// would let two operators register conflicting "feishu" adapters.
const channelName = "feishu"

// Config carries the static configuration the adapter needs at construction
// time. Network credentials (AppID / AppSecret / EncryptKey / VerifyToken)
// are read from a channel connection row in production and passed through
// here so the adapter stays credential-aware but transport-agnostic. Local
// development may bootstrap that row from FEISHU_* environment variables,
// but the adapter does not read environment state directly.
type Config struct {
	// AppID is the Feishu open-platform application id ("cli_…").
	AppID string
	// AppSecret is the matching app secret used to sign OpenAPI requests.
	// Stored as a plain string (not a *secret.Token) for symmetry with the
	// rest of the server's config; the wiring site is responsible for
	// reading it from a secret manager.
	AppSecret string
	// EncryptKey enables Feishu's webhook payload encryption. Optional —
	// when empty the SDK falls back to an unencrypted channel.
	EncryptKey string
	// VerifyToken is passed to the Feishu SDK event dispatcher so inbound
	// events can be verified consistently with the app's event configuration.
	VerifyToken string
}

// Adapter implements port.Channel for Feishu / Lark. The struct is small by
// design: it holds the seam Client, a fan-out channel to downstream
// consumers, and the lifecycle bookkeeping needed to satisfy the Channel
// contract's Disconnect-closes-Events guarantee.
type Adapter struct {
	cfg    Config
	client Client

	// out is the platform-neutral fan-out channel. It is a separate
	// channel from the one Client.Subscribe() returns so the adapter can
	// drop / reshape events without leaking SDK lifecycle (e.g. Stop()
	// closing Subscribe's chan must not crash downstream consumers
	// mid-iteration).
	out chan port.InboundEvent

	mu      sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc
	pumpWG  sync.WaitGroup
}

// NewAdapter constructs an Adapter ready to be Connect()ed. The Client
// argument is the SDK seam — production callers pass a real SDK-backed
// Client (wired in T7), tests pass a fakeFeishuClient that implements the
// same interface.
//
// The adapter only validates the seam (a non-nil Client) — credential
// validation is the concrete Client's job, because what counts as "valid"
// is platform-specific and may change with SDK versions.
func NewAdapter(client Client, cfg Config) *Adapter {
	if client == nil {
		// Fail-fast: a nil Client would only surface as a nil-pointer
		// deref deep inside the pump goroutine. Panicking at construction
		// time gives the caller a clean stack trace pointing at the
		// wiring bug.
		panic("feishu.NewAdapter: client must not be nil")
	}
	return &Adapter{
		cfg:    cfg,
		client: client,
		out:    make(chan port.InboundEvent, 16),
	}
}

// Name implements port.Channel.
func (a *Adapter) Name() string { return channelName }

// Connect implements port.Channel. It boots the SDK seam (Client.Start) and
// kicks off a goroutine that pumps RawEvents → InboundEvents into the fan-
// out channel. Connect is safe to call concurrently; the second call returns
// nil without re-starting.
//
// After a Disconnect, calling Connect again re-initialises the pump and
// creates a fresh fan-out channel so downstream consumers can re-attach.
// This is the reconnect path required by PRD AC2.1 (TC-adapt-3).
func (a *Adapter) Connect(ctx context.Context) error {
	a.mu.Lock()
	// Reconnect path takes precedence over the "already started" guard:
	// after Disconnect, started is still true but stopped is also true.
	// We must reset both flags and build a fresh channel before the pump
	// starts sending, otherwise Connect returns early without calling
	// client.Start and the first replayed event hits a "send on closed
	// channel" panic.
	if a.stopped {
		a.stopped = false
		a.started = false
		a.out = make(chan port.InboundEvent, 16)
	}
	if a.started {
		a.mu.Unlock()
		return nil
	}

	if err := a.client.Start(ctx); err != nil {
		a.mu.Unlock()
		return fmt.Errorf("feishu: start client: %w", err)
	}
	a.started = true

	pumpCtx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.pumpWG.Add(1)
	a.mu.Unlock()

	go a.pump(pumpCtx)
	return nil
}

// Disconnect implements port.Channel. After it returns, Events()' channel is
// closed. It is safe to call multiple times — only the first call performs
// the teardown; subsequent calls return nil immediately.
//
// Teardown order is load-bearing — do NOT reorder these steps:
//
//  1. Cancel the pump's context. The pump's outer select wakes and exits
//     before reading another RawEvent.
//  2. Stop the seam Client. Stop closes the Subscribe() channel, which is
//     the upstream of the pump's read loop.
//  3. Wait on pumpWG. The goroutine has now either exited via the context
//     branch or via the closed-channel branch.
//  4. Close `out`. By step 3 the pump can no longer send into it, so this
//     close is race-free.
//
// Reordering 3 and 4 would re-introduce the classic "send on closed channel"
// panic the WaitGroup is here to prevent (the pump might still be inside
// `case a.out <- ev:` when close runs).
func (a *Adapter) Disconnect(ctx context.Context) error {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return nil
	}
	a.stopped = true
	cancel := a.cancel
	started := a.started
	a.mu.Unlock()

	// Step 1: wake the pump goroutine so it does not read one more event
	// from Subscribe before noticing the shutdown.
	if cancel != nil {
		cancel()
	}
	// Step 2: tear down the upstream. Stop closes Subscribe()'s channel,
	// giving the pump a second exit path if it was already blocked on a
	// receive when we cancelled.
	var stopErr error
	if started {
		stopErr = a.client.Stop(ctx)
	}

	// Step 3: join. Once Wait returns, the pump goroutine is guaranteed
	// to have exited and cannot send into `out` again.
	a.pumpWG.Wait()
	// Step 4: now safe to close the fan-out channel.
	close(a.out)
	return stopErr
}

// Events implements port.Channel.
func (a *Adapter) Events() <-chan port.InboundEvent { return a.out }

// Send implements port.Channel for plain-text outbound messages. Routes to
// sendText so the per-message-type logic (and Retryable judgement) stays in
// send.go.
func (a *Adapter) Send(ctx context.Context, msg port.OutboundMessage) (port.SendResult, error) {
	return a.sendText(ctx, msg)
}

// SendCard implements port.Channel. It delegates to sendCard, which renders
// the platform-neutral title/body payload into Feishu's interactive-card
// schema before calling the OpenAPI.
func (a *Adapter) SendCard(ctx context.Context, msg port.OutboundCardMessage) (port.SendResult, error) {
	return a.sendCard(ctx, msg)
}

// GetChatInfo implements port.Channel. The adapter projects the SDK's chat
// metadata into the platform-neutral port.ChatInfo so callers never see a
// Feishu-shaped response.
func (a *Adapter) GetChatInfo(ctx context.Context, chatID string) (port.ChatInfo, error) {
	resp, err := a.client.GetChatInfo(ctx, chatID)
	if err != nil {
		return port.ChatInfo{}, fmt.Errorf("feishu: get chat info: %w", err)
	}
	return port.ChatInfo{
		ID:   resp.ID,
		Name: resp.Name,
		Type: mapChatType(resp.Type),
	}, nil
}

// GetUserInfo implements port.Channel.
func (a *Adapter) GetUserInfo(ctx context.Context, userID string) (port.UserInfo, error) {
	resp, err := a.client.GetUserInfo(ctx, userID)
	if err != nil {
		return port.UserInfo{}, fmt.Errorf("feishu: get user info: %w", err)
	}
	return port.UserInfo{
		ID:   resp.OpenID,
		Name: resp.Name,
	}, nil
}

// pump is the inbound goroutine: read RawEvents from the seam Client,
// normalise them into port.InboundEvents, and fan them out on `out`.
//
// Lifecycle:
//   - The goroutine exits when either pumpCtx is cancelled (Disconnect
//     called) or the Client's Subscribe channel is closed (the SDK shut
//     itself down).
//   - We deliberately do NOT close `out` here. Disconnect owns that
//     responsibility (after pumpWG.Wait), to avoid a "send on closed
//     channel" race if Stop closes Subscribe before the pump notices the
//     ctx cancellation.
//   - Malformed events are logged (slog.Error) and dropped — T5 Obs-1.
//     A future task can add a Prometheus metric counter.
func (a *Adapter) pump(pumpCtx context.Context) {
	defer a.pumpWG.Done()
	src := a.client.Subscribe()
	for {
		select {
		case <-pumpCtx.Done():
			return
		case raw, ok := <-src:
			if !ok {
				return
			}
			ev, emit, err := normaliseEvent(channelName, a.client.BotUserID(), raw)
			if err != nil {
				// T5 Obs-1: log malformed events instead of silently dropping.
				slog.Error("feishu: malformed event dropped",
					"event_id", raw.EventID,
					"event_type", raw.EventType,
					"error", err,
				)
				continue
			}
			if !emit {
				continue
			}
			select {
			case <-pumpCtx.Done():
				return
			case a.out <- ev:
			}
		}
	}
}

// Compile-time assertion: the adapter satisfies port.Channel. Keeps method
// signatures in sync with the interface — drift here surfaces as a build
// error at this single line rather than at every (registry, dispatcher,
// test) call site.
var _ port.Channel = (*Adapter)(nil)
