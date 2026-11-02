package lark

import (
	"context"
	"io"
	"log/slog"
	"strings"
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

func TestWSConnectorBoundsAndJoinsRealInlineContextReads(t *testing.T) {
	conn := newFakeWSConn()
	finished := make(chan struct{})
	client := &budgetEnricherClient{enricherFakeClient: newEnricherFake()}
	client.get = func(ctx context.Context, _ string) ([]LarkMessage, error) {
		<-ctx.Done()
		close(finished)
		return nil, ctx.Err()
	}
	c, err := NewWSLongConnConnector(WSConnectorConfig{
		Dialer: &fakeWSDialer{conn: conn},
		EndpointFetcher: EndpointFetcherFunc(func(context.Context, InstallationCredentials) (WSEndpoint, error) {
			return WSEndpoint{URL: "wss://test/ignored", ServiceID: 7, PingInterval: time.Hour}, nil
		}),
		CredentialsProvider: CredentialsProviderFunc(func(context.Context, Installation) (InstallationCredentials, error) {
			return InstallationCredentials{AppID: "test_app", AppSecret: "secret"}, nil
		}),
		FrameDecoder: FrameDecoderFunc(func([]byte, Installation) (InboundMessage, bool, error) {
			return InboundMessage{MessageID: "trigger", ParentID: "parent", Body: "inspect"}, true, nil
		}),
		Enricher:      NewInboundEnricher(client, InboundEnricherConfig{}),
		EnrichTimeout: 30 * time.Millisecond,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	emitted := make(chan InboundMessage, 1)
	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, Installation{AppID: "test_app"}, func(emitCtx context.Context, msg InboundMessage) (DispatchResult, error) {
			if emitCtx.Err() != nil {
				t.Error("expired enrichment context escaped into dispatch")
			}
			select {
			case <-finished:
			default:
				t.Error("context read was not joined before dispatch")
			}
			emitted <- msg
			return DispatchResult{Outcome: OutcomeIngested}, nil
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("connector did not stop")
		}
	})
	pushDataFrame(conn, []byte("event"), "inline-frame")
	select {
	case out := <-emitted:
		resolver := NewFeishuMediaResolver(nil, nil, nil, nil, newDiscardLogger())
		if !strings.Contains(out.Body, "unable to fetch") || resolver.HasMedia(channelMessageFromLark(out)) {
			t.Fatalf("failed metadata acquired a media fence or lost fallback: %+v", out)
		}
	case <-time.After(time.Second):
		t.Fatal("inline read exceeded its budget")
	}
	for deadline := time.Now().Add(time.Second); len(conn.snapshot()) == 0 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	frames := conn.snapshot()
	if len(frames) != 1 {
		t.Fatalf("ACK count=%d", len(frames))
	}
	ack, err := UnmarshalFrame(frames[0])
	if err != nil || !strings.Contains(string(ack.Payload), `"code":200`) {
		t.Fatalf("ACK=%+v err=%v", ack, err)
	}
}
