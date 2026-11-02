package lark

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// recordingEnricher captures what the connector hands it and rewrites
// the body so the test can prove enrichment ran between decode and emit.
type recordingEnricher struct {
	mu    sync.Mutex
	msgs  []InboundMessage
	creds []InstallationCredentials
}

// The real connector ACKs without HTTP. A quote taking longer than the former
// two-second cap is then read and uploaded by the real deferred resolver.
func TestWSConnectorDefersSlowQuotedImageAfterAck(t *testing.T) {
	t.Parallel()
	var historyCalls, quoteCalls atomic.Int32
	httpClient := &http.Client{Transport: mediaRoundTripper(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			body = `{"code":0,"tenant_access_token":"test-token","expire":7200}`
		case "/open-apis/im/v1/messages":
			historyCalls.Add(1)
			if req.URL.Query().Get("page_size") != "10" || req.URL.Query().Get("container_id") != "group" {
				t.Errorf("changed history selection: %s", req.URL.RawQuery)
			}
			body = `{"code":0,"data":{"items":[]}}`
		case "/open-apis/im/v1/messages/parent":
			quoteCalls.Add(1)
			timer := time.NewTimer(2200 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
			body = `{"code":0,"data":{"items":[{"message_id":"parent","msg_type":"image","body":{"content":"{\"image_key\":\"img_quote\"}"},"sender":{"id":"source","sender_type":"user"}}]}}`
		case "/open-apis/contact/v3/users/batch":
			body = `{"code":0,"data":{"items":[]}}`
		case "/open-apis/im/v1/messages/parent/resources/img_quote":
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(strings.NewReader(string(pngBytes))), ContentLength: int64(len(pngBytes))}, nil
		default:
			t.Errorf("unexpected HTTP request: %s", req.URL.Path)
			return nil, context.Canceled
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	client := NewHTTPAPIClient(HTTPClientConfig{HTTPClient: httpClient, ResourceHTTPClient: httpClient})
	conn := newFakeWSConn()
	c, err := NewWSLongConnConnector(WSConnectorConfig{
		Dialer: &fakeWSDialer{conn: conn},
		EndpointFetcher: EndpointFetcherFunc(func(context.Context, InstallationCredentials) (WSEndpoint, error) {
			return WSEndpoint{URL: "wss://test/ignored", ServiceID: 7, PingInterval: time.Hour}, nil
		}),
		CredentialsProvider: CredentialsProviderFunc(func(context.Context, Installation) (InstallationCredentials, error) {
			return InstallationCredentials{AppID: "test_app", AppSecret: "test_secret"}, nil
		}),
		FrameDecoder: FrameDecoderFunc(func([]byte, Installation) (InboundMessage, bool, error) {
			return InboundMessage{MessageID: "trigger", ParentID: "parent", Body: "inspect", ChatID: "group", ChatType: ChatTypeGroup, AddressedToBot: true}, true, nil
		}),
		Enricher: NewInboundEnricher(client, InboundEnricherConfig{RecentContextSize: DefaultRecentContextSize}),
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.cfg.EnrichTimeout != 2*time.Second {
		t.Fatalf("production enrichment cap changed: %s", c.cfg.EnrichTimeout)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	emitted := make(chan InboundMessage, 1)
	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, Installation{AppID: "test_app"}, func(ctx context.Context, msg InboundMessage) (DispatchResult, error) {
			if ctx.Err() != nil {
				t.Error("expired enrichment context escaped into dispatch")
			}
			emitted <- msg
			return DispatchResult{Outcome: OutcomeIngested}, nil
		})
	}()
	pushDataFrame(conn, []byte("event"), "frame-1")
	deadline := time.Now().Add(3 * time.Second)
	for len(conn.snapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	writes := conn.snapshot()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connector did not stop")
	}
	if len(writes) != 1 {
		t.Fatalf("ACK window missed or duplicated: got %d frames", len(writes))
	}
	ack, err := UnmarshalFrame(writes[0])
	if err != nil || !strings.Contains(string(ack.Payload), `"code":200`) {
		t.Fatalf("expected success ACK: %v %v", ack, err)
	}
	msg := <-emitted
	if msg.DeferredContext == nil || historyCalls.Load() != 0 || quoteCalls.Load() != 0 {
		t.Fatalf("remote reads occurred before ACK or context was lost: %+v", msg)
	}
	storage := &fakeMediaStorage{}
	resolver := NewFeishuMediaResolver(client, fakeCreds{secret: "plain"}, storage, &fakeMediaLedger{}, newDiscardLogger())
	cm := channelMessageFromLark(msg)
	if !resolver.HasMedia(cm) {
		t.Fatal("deferred context did not hold the task behind the media fence")
	}
	resolved := resolver.ResolveMedia(t.Context(), testMediaInstallation(t), engine.ResolvedIdentity{},
		uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), cm)
	if len(resolved.MediaRefs) != 1 || !resolved.BodyResolved || !strings.Contains(resolved.Text, `message_id="parent"`) || len(storage.uploads) != 1 {
		t.Fatalf("slow quoted image lost in deferred resolver: %+v", resolved)
	}
	if historyCalls.Load() != 1 || quoteCalls.Load() != 1 {
		t.Fatalf("unexpected fetches: history=%d quote=%d", historyCalls.Load(), quoteCalls.Load())
	}
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
