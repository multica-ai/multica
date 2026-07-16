package lark

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// recordingEnricher captures what the connector hands it and rewrites
// the body so the test can prove enrichment ran between decode and emit.
type recordingEnricher struct {
	mu    sync.Mutex
	msgs  []InboundMessage
	creds []InstallationCredentials
}

type deadlineEnricher struct {
	deadline chan time.Time
}

func (e *deadlineEnricher) Enrich(ctx context.Context, msg InboundMessage, _ InstallationCredentials) InboundMessage {
	if deadline, ok := ctx.Deadline(); ok {
		e.deadline <- deadline
	}
	<-ctx.Done()
	return msg
}

func (e *recordingEnricher) Enrich(ctx context.Context, msg InboundMessage, creds InstallationCredentials) InboundMessage {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.msgs = append(e.msgs, msg)
	e.creds = append(e.creds, creds)
	msg.Body = "ENRICHED:" + msg.Body
	return msg
}

// TestWSConnectorEnrichesBeforeEmit verifies the connector runs the
// Enricher on a decoded message — with the connection's resolved
// credentials — before emitting it to the dispatcher.
func TestWSConnectorEnrichesBeforeEmit(t *testing.T) {
	t.Parallel()
	conn := newFakeWSConn()
	decoder := FrameDecoderFunc(func(payload []byte, _ Installation) (InboundMessage, bool, error) {
		return InboundMessage{
			EventID:   string(payload),
			AppID:     "test_app",
			MessageID: "msg-" + string(payload),
			Body:      "raw-" + string(payload),
		}, true, nil
	})
	enr := &recordingEnricher{}

	c, err := NewWSLongConnConnector(WSConnectorConfig{
		Dialer: &fakeWSDialer{conn: conn},
		EndpointFetcher: EndpointFetcherFunc(func(context.Context, InstallationCredentials) (WSEndpoint, error) {
			return WSEndpoint{URL: "wss://test/ignored", ServiceID: 7, PingInterval: time.Hour}, nil
		}),
		FrameDecoder: decoder,
		Enricher:     enr,
		CredentialsProvider: CredentialsProviderFunc(func(context.Context, Installation) (InstallationCredentials, error) {
			return InstallationCredentials{AppID: "test_app", AppSecret: "secret"}, nil
		}),
		PingInterval:  time.Hour,
		ReadDeadline:  time.Second,
		WriteTimeout:  time.Second,
		EnrichTimeout: time.Second,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewWSLongConnConnector: %v", err)
	}

	var emitted []InboundMessage
	var emitMu sync.Mutex
	emit := func(_ context.Context, msg InboundMessage) (DispatchResult, error) {
		emitMu.Lock()
		emitted = append(emitted, msg)
		emitMu.Unlock()
		return DispatchResult{Outcome: OutcomeIngested}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, Installation{AppID: "test_app"}, emit) }()

	pushDataFrame(conn, []byte("evt-1"), "m1")

	deadline := time.After(2 * time.Second)
	for {
		emitMu.Lock()
		n := len(emitted)
		emitMu.Unlock()
		if n >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no emit within 2s")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}

	emitMu.Lock()
	defer emitMu.Unlock()
	if emitted[0].Body != "ENRICHED:raw-evt-1" {
		t.Errorf("emit body = %q; enricher did not run before emit", emitted[0].Body)
	}

	enr.mu.Lock()
	defer enr.mu.Unlock()
	if len(enr.msgs) != 1 || enr.msgs[0].Body != "raw-evt-1" {
		t.Errorf("enricher received %+v", enr.msgs)
	}
	if len(enr.creds) != 1 || enr.creds[0].AppID != "test_app" || enr.creds[0].AppSecret != "secret" {
		t.Errorf("enricher got wrong creds: %+v", enr.creds)
	}
}

func TestWSConnectorSharesAckBudgetAcrossEnrichmentAndDispatch(t *testing.T) {
	t.Parallel()
	conn := newFakeWSConn()
	decoder := FrameDecoderFunc(func(payload []byte, _ Installation) (InboundMessage, bool, error) {
		return InboundMessage{EventID: string(payload), MessageID: "msg-" + string(payload)}, true, nil
	})
	enr := &deadlineEnricher{deadline: make(chan time.Time, 1)}

	const (
		ackTimeout      = 180 * time.Millisecond
		ackWriteReserve = 40 * time.Millisecond
		enrichTimeout   = 60 * time.Millisecond
	)
	c, err := NewWSLongConnConnector(WSConnectorConfig{
		Dialer: &fakeWSDialer{conn: conn},
		EndpointFetcher: EndpointFetcherFunc(func(context.Context, InstallationCredentials) (WSEndpoint, error) {
			return WSEndpoint{URL: "wss://test/ignored", ServiceID: 7, PingInterval: time.Hour}, nil
		}),
		FrameDecoder: decoder,
		Enricher:     enr,
		CredentialsProvider: CredentialsProviderFunc(func(context.Context, Installation) (InstallationCredentials, error) {
			return InstallationCredentials{AppID: "test_app", AppSecret: "secret"}, nil
		}),
		PingInterval:    time.Hour,
		ReadDeadline:    time.Second,
		WriteTimeout:    time.Second,
		EnrichTimeout:   enrichTimeout,
		AckTimeout:      ackTimeout,
		AckWriteReserve: ackWriteReserve,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewWSLongConnConnector: %v", err)
	}

	emitDeadline := make(chan time.Time, 1)
	emit := func(ctx context.Context, _ InboundMessage) (DispatchResult, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("dispatch context has no shared ACK deadline")
		} else {
			emitDeadline <- deadline
		}
		// Model media consuming the rest of the shared processing budget and
		// failing open. The connector must still ACK 200 and keep the socket.
		<-ctx.Done()
		return DispatchResult{Outcome: OutcomeIngested}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, Installation{AppID: "test_app"}, emit) }()

	started := time.Now()
	pushDataFrame(conn, []byte("evt-budget"), "m-budget")

	enrichDeadline := <-enr.deadline
	dispatchDeadline := <-emitDeadline
	if !dispatchDeadline.After(enrichDeadline) {
		t.Fatalf("dispatch deadline %v must outlive enrichment deadline %v", dispatchDeadline, enrichDeadline)
	}
	if got := dispatchDeadline.Sub(enrichDeadline); got < 50*time.Millisecond || got > 100*time.Millisecond {
		t.Fatalf("deadline gap = %v; want enrichment's shorter cap within the shared budget", got)
	}

	deadline := time.After(time.Second)
	for {
		writes := conn.snapshot()
		if len(writes) > 0 {
			frame, unmarshalErr := UnmarshalFrame(writes[0])
			if unmarshalErr != nil {
				t.Fatalf("unmarshal ACK: %v", unmarshalErr)
			}
			if !contains(string(frame.Payload), `"code":200`) {
				t.Fatalf("ACK payload = %s; want code=200", frame.Payload)
			}
			break
		}
		select {
		case runErr := <-done:
			t.Fatalf("connector exited instead of ACKing and keeping the connection: %v", runErr)
		case <-deadline:
			t.Fatal("connector did not ACK within 1s")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if elapsed := time.Since(started); elapsed > ackTimeout+100*time.Millisecond {
		t.Fatalf("ACK took %v; shared budget is %v", elapsed, ackTimeout)
	}
	select {
	case runErr := <-done:
		t.Fatalf("connector exited after fail-open dispatch: %v", runErr)
	default:
	}

	cancel()
	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("Run returned error on cancellation: %v", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}
