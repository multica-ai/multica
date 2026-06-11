package lark

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeWSConn is a programmable WSConn driven by tests. ReadMessage
// blocks until either Push delivers a frame or Close is invoked —
// this is how we simulate the "blocked in TCP read" condition the
// watchdog has to break.
//
// Frames sent via WriteMessage are buffered into writes[] so tests
// can assert on outbound traffic (ACK, ping) without race conditions.
type fakeWSConn struct {
	mu     sync.Mutex
	writes [][]byte

	frames    chan []byte
	closeOnce sync.Once
	closed    chan struct{}
	writeErr  error // optional injection for WriteMessage
}

func newFakeWSConn() *fakeWSConn {
	return &fakeWSConn{
		frames: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

// Push enqueues a binary frame for the next ReadMessage call.
func (f *fakeWSConn) Push(b []byte) {
	select {
	case f.frames <- b:
	case <-f.closed:
	}
}

func (f *fakeWSConn) ReadMessage() (int, []byte, error) {
	select {
	case b, ok := <-f.frames:
		if !ok {
			return 0, nil, io.EOF
		}
		return websocket.BinaryMessage, b, nil
	case <-f.closed:
		return 0, nil, &websocket.CloseError{Code: websocket.CloseAbnormalClosure, Text: "fake closed"}
	}
}

func (f *fakeWSConn) WriteMessage(messageType int, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return f.writeErr
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	f.writes = append(f.writes, cp)
	return nil
}

func (f *fakeWSConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *fakeWSConn) SetWriteDeadline(t time.Time) error { return nil }

func (f *fakeWSConn) Close() error {
	f.closeOnce.Do(func() {
		close(f.closed)
	})
	return nil
}

func (f *fakeWSConn) snapshot() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.writes))
	for i, w := range f.writes {
		out[i] = append([]byte(nil), w...)
	}
	return out
}

// fakeWSDialer hands back a pre-built fakeWSConn so tests can drive
// frames + observe closes deterministically.
type fakeWSDialer struct {
	conn    *fakeWSConn
	dialErr error
}

func (d *fakeWSDialer) DialContext(ctx context.Context, urlStr string, h http.Header) (WSConn, *http.Response, error) {
	if d.dialErr != nil {
		return nil, nil, d.dialErr
	}
	return d.conn, nil, nil
}

// quietConnector wires a connector with a deterministic decoder + the
// fakeWSConn. Caller controls the decoder so each test can assert
// per-payload behaviour.
func quietConnector(t *testing.T, conn *fakeWSConn, decoder FrameDecoder, pingInterval time.Duration) *WSLongConnConnector {
	t.Helper()
	c, err := NewWSLongConnConnector(WSConnectorConfig{
		Dialer:          &fakeWSDialer{conn: conn},
		EndpointFetcher: EndpointFetcherFunc(func(context.Context, InstallationCredentials) (WSEndpoint, error) {
			return WSEndpoint{URL: "wss://test/ignored", ServiceID: 7, PingInterval: pingInterval}, nil
		}),
		FrameDecoder:    decoder,
		CredentialsProvider: CredentialsProviderFunc(func(context.Context, db.LarkInstallation) (InstallationCredentials, error) {
			return InstallationCredentials{AppID: "test_app", AppSecret: "secret"}, nil
		}),
		PingInterval: pingInterval,
		ReadDeadline: time.Second,
		WriteTimeout: time.Second,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewWSLongConnConnector: %v", err)
	}
	return c
}

// pushDataFrame writes a Lark long-conn data Frame envelope wrapping
// the supplied JSON payload onto the conn's read queue.
func pushDataFrame(conn *fakeWSConn, payload []byte, messageID string) {
	f := &Frame{
		Method:  FrameMethodData,
		Service: 7,
		Headers: []FrameHeader{
			{Key: FrameHeaderTypeKey, Value: FrameHeaderTypeEvent},
			{Key: FrameHeaderMessageIDKey, Value: messageID},
		},
		Payload: payload,
	}
	conn.Push(f.Marshal())
}

func TestWSConnectorRunReturnsOnCtxCancelEvenWhenReadIsBlocked(t *testing.T) {
	t.Parallel()
	conn := newFakeWSConn()
	decoder := FrameDecoderFunc(func([]byte, db.LarkInstallation) (InboundMessage, bool, error) {
		return InboundMessage{}, false, nil
	})
	c := quietConnector(t, conn, decoder, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, db.LarkInstallation{AppID: "test_app"}, func(context.Context, InboundMessage) (DispatchResult, error) {
			t.Errorf("emit unexpectedly called")
			return DispatchResult{}, nil
		})
	}()

	// Give the connector a moment to dial + park in ReadMessage.
	time.Sleep(20 * time.Millisecond)

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error on ctx cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancel — watchdog broken")
	}
}

// TestWSConnectorEmitsDecodedFramesAndAcks verifies that:
//   - data frames whose decoder returns ok=true reach emit;
//   - every data frame yields an ACK Frame written back to the conn;
//   - heartbeat / non-event payloads are dropped but still ACKed (so
//     the server stops resending them).
func TestWSConnectorEmitsDecodedFramesAndAcks(t *testing.T) {
	t.Parallel()
	conn := newFakeWSConn()
	decoder := FrameDecoderFunc(func(payload []byte, _ db.LarkInstallation) (InboundMessage, bool, error) {
		if string(payload) == "heartbeat" {
			return InboundMessage{}, false, nil
		}
		return InboundMessage{
			EventID:   string(payload),
			AppID:     "test_app",
			MessageID: "msg-" + string(payload),
		}, true, nil
	})
	c := quietConnector(t, conn, decoder, time.Hour) // disable ping cadence

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
	go func() {
		done <- c.Run(ctx, db.LarkInstallation{AppID: "test_app"}, emit)
	}()

	pushDataFrame(conn, []byte("evt-1"), "m1")
	pushDataFrame(conn, []byte("heartbeat"), "m2")
	pushDataFrame(conn, []byte("evt-2"), "m3")

	deadline := time.After(2 * time.Second)
	for {
		emitMu.Lock()
		n := len(emitted)
		emitMu.Unlock()
		if n >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("only %d emits in 2s", n)
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}

	if emitted[0].EventID != "evt-1" || emitted[1].EventID != "evt-2" {
		t.Errorf("emit ordering wrong: %+v", emitted)
	}

	// Every data frame should have produced an ACK frame on the wire
	// regardless of decode outcome (drop or emit).
	writes := conn.snapshot()
	if len(writes) < 3 {
		t.Fatalf("expected >=3 outbound ACK frames, got %d", len(writes))
	}
	for i, w := range writes[:3] {
		f, err := UnmarshalFrame(w)
		if err != nil {
			t.Fatalf("write[%d] unmarshal: %v", i, err)
		}
		if f.Method != FrameMethodData {
			t.Errorf("ack[%d] Method = %d; want Data", i, f.Method)
		}
		if f.HeaderValue(FrameHeaderTypeKey) != FrameHeaderTypeEvent {
			t.Errorf("ack[%d] type header = %q", i, f.HeaderValue(FrameHeaderTypeKey))
		}
		if len(f.Payload) == 0 || !contains(string(f.Payload), `"code":200`) {
			t.Errorf("ack[%d] payload missing code=200: %s", i, string(f.Payload))
		}
	}
}

func TestWSConnectorRespondsToServerPingWithPong(t *testing.T) {
	t.Parallel()
	conn := newFakeWSConn()
	decoder := FrameDecoderFunc(func([]byte, db.LarkInstallation) (InboundMessage, bool, error) {
		return InboundMessage{}, false, nil
	})
	c := quietConnector(t, conn, decoder, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, db.LarkInstallation{AppID: "test_app"}, func(context.Context, InboundMessage) (DispatchResult, error) {
			return DispatchResult{}, nil
		})
	}()

	// Server-initiated ping (control frame, type=ping).
	pingFrame := &Frame{
		Method:  FrameMethodControl,
		Service: 7,
		Headers: []FrameHeader{{Key: FrameHeaderTypeKey, Value: FrameHeaderTypePing}},
	}
	conn.Push(pingFrame.Marshal())

	// Allow the read loop to react.
	deadline := time.After(2 * time.Second)
	for {
		writes := conn.snapshot()
		if len(writes) >= 1 {
			f, err := UnmarshalFrame(writes[0])
			if err != nil {
				t.Fatalf("pong unmarshal: %v", err)
			}
			if f.HeaderValue(FrameHeaderTypeKey) != FrameHeaderTypePong {
				t.Errorf("response type = %q; want pong", f.HeaderValue(FrameHeaderTypeKey))
			}
			if f.Service != 7 {
				t.Errorf("response Service = %d; want 7", f.Service)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("connector did not pong in 2s")
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	<-done
}

func TestWSConnectorEmitInfraErrorSendsNackAndReturns(t *testing.T) {
	t.Parallel()
	conn := newFakeWSConn()
	decoder := FrameDecoderFunc(func([]byte, db.LarkInstallation) (InboundMessage, bool, error) {
		return InboundMessage{EventID: "x"}, true, nil
	})
	c := quietConnector(t, conn, decoder, time.Hour)

	infra := errors.New("dispatcher infra failure")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, db.LarkInstallation{AppID: "test_app"}, func(context.Context, InboundMessage) (DispatchResult, error) {
			return DispatchResult{}, infra
		})
	}()

	pushDataFrame(conn, []byte("triggers-infra"), "m-infra")

	select {
	case err := <-done:
		if err == nil || !errors.Is(err, infra) {
			t.Fatalf("expected Run to wrap infra error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after infra error")
	}

	// NACK should have been written on the way out (code=500).
	writes := conn.snapshot()
	found := false
	for _, w := range writes {
		f, err := UnmarshalFrame(w)
		if err != nil {
			continue
		}
		if f.Method == FrameMethodData && contains(string(f.Payload), `"code":500`) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected NACK (code=500) frame written on infra error path")
	}
}

func TestWSConnectorSendsAppLayerPings(t *testing.T) {
	t.Parallel()
	conn := newFakeWSConn()
	decoder := FrameDecoderFunc(func([]byte, db.LarkInstallation) (InboundMessage, bool, error) {
		return InboundMessage{}, false, nil
	})
	c := quietConnector(t, conn, decoder, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, db.LarkInstallation{AppID: "test_app"}, func(context.Context, InboundMessage) (DispatchResult, error) {
			return DispatchResult{}, nil
		})
	}()

	deadline := time.After(500 * time.Millisecond)
	for {
		writes := conn.snapshot()
		pings := 0
		for _, w := range writes {
			f, err := UnmarshalFrame(w)
			if err != nil {
				continue
			}
			if f.Method == FrameMethodControl && f.HeaderValue(FrameHeaderTypeKey) == FrameHeaderTypePing {
				pings++
			}
		}
		if pings >= 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected >=3 app-layer pings, got %d writes total", len(writes))
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	<-done
}

func TestWSConnectorDecoderErrorAcksAndContinues(t *testing.T) {
	t.Parallel()
	conn := newFakeWSConn()
	decodeCount := int32(0)
	decoder := FrameDecoderFunc(func(payload []byte, _ db.LarkInstallation) (InboundMessage, bool, error) {
		n := atomic.AddInt32(&decodeCount, 1)
		if n == 1 {
			return InboundMessage{}, false, errors.New("synthetic decode failure")
		}
		return InboundMessage{EventID: "good"}, true, nil
	})
	c := quietConnector(t, conn, decoder, time.Hour)

	emits := make(chan InboundMessage, 1)
	emit := func(_ context.Context, msg InboundMessage) (DispatchResult, error) {
		emits <- msg
		return DispatchResult{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, db.LarkInstallation{AppID: "test_app"}, emit)
	}()

	pushDataFrame(conn, []byte("bad"), "mb")
	pushDataFrame(conn, []byte("good"), "mg")

	select {
	case msg := <-emits:
		if msg.EventID != "good" {
			t.Errorf("emit EventID = %q, want good", msg.EventID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("connector exited after first decode error instead of dropping the frame")
	}

	cancel()
	<-done
}

func TestWSConnectorReadErrorReturnsToHub(t *testing.T) {
	t.Parallel()
	conn := newFakeWSConn()
	decoder := FrameDecoderFunc(func([]byte, db.LarkInstallation) (InboundMessage, bool, error) {
		return InboundMessage{}, false, nil
	})
	c := quietConnector(t, conn, decoder, time.Hour)

	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, db.LarkInstallation{AppID: "test_app"}, func(context.Context, InboundMessage) (DispatchResult, error) {
			return DispatchResult{}, nil
		})
	}()

	// Close out from under the read loop. Because ctx is NOT cancelled,
	// the connector should treat this as a connection failure and
	// return the wrapped error so the Hub steps up its backoff.
	_ = conn.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil on read error; expected wrapped err for Hub backoff")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after underlying conn closed")
	}
}

func TestWSConnectorRequiresAllDeps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  WSConnectorConfig
	}{
		{"no dialer", WSConnectorConfig{
			EndpointFetcher:     EndpointFetcherFunc(func(context.Context, InstallationCredentials) (WSEndpoint, error) { return WSEndpoint{}, nil }),
			FrameDecoder:        FrameDecoderFunc(func([]byte, db.LarkInstallation) (InboundMessage, bool, error) { return InboundMessage{}, false, nil }),
			CredentialsProvider: CredentialsProviderFunc(func(context.Context, db.LarkInstallation) (InstallationCredentials, error) { return InstallationCredentials{}, nil }),
		}},
		{"no endpoint fetcher", WSConnectorConfig{
			Dialer:              &fakeWSDialer{},
			FrameDecoder:        FrameDecoderFunc(func([]byte, db.LarkInstallation) (InboundMessage, bool, error) { return InboundMessage{}, false, nil }),
			CredentialsProvider: CredentialsProviderFunc(func(context.Context, db.LarkInstallation) (InstallationCredentials, error) { return InstallationCredentials{}, nil }),
		}},
		{"no decoder", WSConnectorConfig{
			Dialer:              &fakeWSDialer{},
			EndpointFetcher:     EndpointFetcherFunc(func(context.Context, InstallationCredentials) (WSEndpoint, error) { return WSEndpoint{}, nil }),
			CredentialsProvider: CredentialsProviderFunc(func(context.Context, db.LarkInstallation) (InstallationCredentials, error) { return InstallationCredentials{}, nil }),
		}},
		{"no credentials provider", WSConnectorConfig{
			Dialer:          &fakeWSDialer{},
			EndpointFetcher: EndpointFetcherFunc(func(context.Context, InstallationCredentials) (WSEndpoint, error) { return WSEndpoint{}, nil }),
			FrameDecoder:    FrameDecoderFunc(func([]byte, db.LarkInstallation) (InboundMessage, bool, error) { return InboundMessage{}, false, nil }),
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewWSLongConnConnector(tc.cfg); err == nil {
				t.Fatalf("expected error for missing dep")
			}
		})
	}
}

func TestWSConnectorDialErrorIsReturned(t *testing.T) {
	t.Parallel()
	dialErr := errors.New("dial blew up")
	c, err := NewWSLongConnConnector(WSConnectorConfig{
		Dialer:          &fakeWSDialer{dialErr: dialErr},
		EndpointFetcher: EndpointFetcherFunc(func(context.Context, InstallationCredentials) (WSEndpoint, error) { return WSEndpoint{URL: "wss://x", ServiceID: 1}, nil }),
		FrameDecoder:    FrameDecoderFunc(func([]byte, db.LarkInstallation) (InboundMessage, bool, error) { return InboundMessage{}, false, nil }),
		CredentialsProvider: CredentialsProviderFunc(func(context.Context, db.LarkInstallation) (InstallationCredentials, error) {
			return InstallationCredentials{AppID: "a"}, nil
		}),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	err = c.Run(context.Background(), db.LarkInstallation{}, func(context.Context, InboundMessage) (DispatchResult, error) {
		return DispatchResult{}, nil
	})
	if err == nil || !errors.Is(err, dialErr) {
		t.Fatalf("expected wrapped dial error, got %v", err)
	}
}

// pushChunkedDataFrame pushes a Lark long-conn data Frame carrying one
// chunk of a multi-frame event. Lark splits big events across N frames
// with sum/seq/message_id headers; the connector reassembles before
// invoking the decoder.
func pushChunkedDataFrame(conn *fakeWSConn, payload []byte, messageID string, sum, seq int) {
	f := &Frame{
		Method:  FrameMethodData,
		Service: 7,
		Headers: []FrameHeader{
			{Key: FrameHeaderTypeKey, Value: FrameHeaderTypeEvent},
			{Key: FrameHeaderMessageIDKey, Value: messageID},
			{Key: FrameHeaderSumKey, Value: strconv.Itoa(sum)},
			{Key: FrameHeaderSeqKey, Value: strconv.Itoa(seq)},
		},
		Payload: payload,
	}
	conn.Push(f.Marshal())
}

// TestWSConnectorReassemblesChunkedDataFrame verifies that:
//   - intermediate chunks (sum>1, seq<sum-1) are NOT ACKed and NOT
//     emitted — the SDK's combine() contract;
//   - the final chunk completes the buffer, the decoder receives the
//     concatenated payload, and ONE ACK is written for the whole event.
//
// This is the regression test for the prior MVP behaviour where every
// data frame was decoded individually and chunked events were silently
// truncated then ACKed (server would never retry).
func TestWSConnectorReassemblesChunkedDataFrame(t *testing.T) {
	t.Parallel()
	conn := newFakeWSConn()
	var decodedPayloads [][]byte
	var decodeMu sync.Mutex
	decoder := FrameDecoderFunc(func(payload []byte, _ db.LarkInstallation) (InboundMessage, bool, error) {
		decodeMu.Lock()
		decodedPayloads = append(decodedPayloads, append([]byte(nil), payload...))
		decodeMu.Unlock()
		return InboundMessage{EventID: string(payload)}, true, nil
	})
	c := quietConnector(t, conn, decoder, time.Hour) // disable ping cadence

	emits := make(chan InboundMessage, 4)
	emit := func(_ context.Context, msg InboundMessage) (DispatchResult, error) {
		emits <- msg
		return DispatchResult{Outcome: OutcomeIngested}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, db.LarkInstallation{AppID: "test_app"}, emit)
	}()

	// Three chunks of a single logical event "ABC".
	pushChunkedDataFrame(conn, []byte("A"), "om-multi", 3, 0)
	pushChunkedDataFrame(conn, []byte("B"), "om-multi", 3, 1)
	pushChunkedDataFrame(conn, []byte("C"), "om-multi", 3, 2)

	select {
	case msg := <-emits:
		if msg.EventID != "ABC" {
			t.Errorf("emit EventID = %q; want ABC", msg.EventID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reassembled event did not reach emit in 2s")
	}

	// Drain the read loop briefly to let any pending ACK write land.
	time.Sleep(50 * time.Millisecond)

	// Exactly one ACK should have been written — only the final chunk
	// triggers ACK; intermediate chunks are silently buffered.
	cancel()
	<-done

	writes := conn.snapshot()
	dataAcks := 0
	for _, w := range writes {
		f, err := UnmarshalFrame(w)
		if err != nil {
			continue
		}
		if f.Method == FrameMethodData && bytes.Contains(f.Payload, []byte(`"code":200`)) {
			dataAcks++
		}
	}
	if dataAcks != 1 {
		t.Errorf("data-frame ACK count = %d; want exactly 1 (only the final chunk should ACK)", dataAcks)
	}

	decodeMu.Lock()
	defer decodeMu.Unlock()
	if len(decodedPayloads) != 1 {
		t.Fatalf("decoder invoked %d times; want exactly 1 (reassembled payload)", len(decodedPayloads))
	}
	if string(decodedPayloads[0]) != "ABC" {
		t.Errorf("decoder saw payload = %q; want ABC", string(decodedPayloads[0]))
	}
}

func TestWSConnectorCredentialsErrorIsReturned(t *testing.T) {
	t.Parallel()
	credsErr := errors.New("decrypt failed")
	c, err := NewWSLongConnConnector(WSConnectorConfig{
		Dialer:          &fakeWSDialer{conn: newFakeWSConn()},
		EndpointFetcher: EndpointFetcherFunc(func(context.Context, InstallationCredentials) (WSEndpoint, error) { return WSEndpoint{URL: "wss://x"}, nil }),
		FrameDecoder:    FrameDecoderFunc(func([]byte, db.LarkInstallation) (InboundMessage, bool, error) { return InboundMessage{}, false, nil }),
		CredentialsProvider: CredentialsProviderFunc(func(context.Context, db.LarkInstallation) (InstallationCredentials, error) {
			return InstallationCredentials{}, credsErr
		}),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	err = c.Run(context.Background(), db.LarkInstallation{}, func(context.Context, InboundMessage) (DispatchResult, error) {
		return DispatchResult{}, nil
	})
	if err == nil || !errors.Is(err, credsErr) {
		t.Fatalf("expected wrapped credentials error, got %v", err)
	}
}
