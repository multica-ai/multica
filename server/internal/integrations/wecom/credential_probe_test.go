package wecom

// credential_probe_test.go — idx_channel_installation_type_appid is UNIQUE on
// (channel_type, config->>'app_id') with no workspace in it, so a bot id's
// routing slot is global. ReclaimDeadChannelInstallationByAppID then
// hard-deletes whoever holds it, plus every binding beneath. Bot ids are not
// secret. The only thing that can separate "the rightful owner rebinding"
// from "anyone who typed that id" is proof they hold the secret.
//
// These are the socket-level guards: what the probe does with the verdict WeCom
// sends back. The guards on WHEN the probe is allowed to run at all are DB-backed
// and live in installation_probe_db_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

func testUUID(b byte) pgtype.UUID { return pgtype.UUID{Bytes: [16]byte{b}, Valid: true} }

// fakeProbe stands in for the handshake. Counters are mutex-guarded because
// the concurrency guards in installation_probe_db_test.go call it from several
// goroutines under -race.
type fakeProbe struct {
	mu     sync.Mutex
	err    error
	calls  int
	gotBot string
	gotSec string

	// entered is closed on the first call and release, if non-nil, is waited
	// on before returning — the seam that lets a test hold one install inside
	// the probe while another one races it.
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *fakeProbe) Probe(_ context.Context, botID, secret string) error {
	f.mu.Lock()
	f.calls++
	f.gotBot, f.gotSec = botID, secret
	err := f.err
	entered, release := f.entered, f.release
	f.mu.Unlock()

	if entered != nil {
		f.once.Do(func() { close(entered) })
	}
	if release != nil {
		<-release
	}
	return err
}

func (f *fakeProbe) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// ---- the verdict split ----

// ackConn is a wsConn that answers the subscribe frame with a fixed
// (errcode, errmsg) pair, echoing the caller's internally generated req_id.
type ackConn struct {
	mu      sync.Mutex
	errCode int
	errMsg  string
	ack     []byte
	acked   bool
	closed  bool
}

func (c *ackConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil || env.Cmd != cmdSubscribe {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ack, _ = json.Marshal(frameEnvelope{
		Headers: frameHeaders{ReqID: env.Headers.ReqID},
		ErrCode: c.errCode,
		ErrMsg:  c.errMsg,
	})
	return nil
}

func (c *ackConn) ReadMessage() (int, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ack != nil && !c.acked {
		c.acked = true
		return websocket.TextMessage, c.ack, nil
	}
	return 0, nil, errors.New("no more frames")
}

func (c *ackConn) SetReadDeadline(time.Time) error  { return nil }
func (c *ackConn) SetWriteDeadline(time.Time) error { return nil }
func (c *ackConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

type ackDialer struct{ conn wsConn }

func (d ackDialer) DialContext(context.Context, string, http.Header) (wsConn, *http.Response, error) {
	return d.conn, nil, nil
}

// Only the codes WeCom documents as a refusal of the pair may be reported to
// the admin as a wrong credential. Everything else non-zero fails closed as
// unverifiable: WeCom guarantees nothing about non-zero codes except that they
// are not success, the subscribe path is rate-limited, and telling an admin to
// rotate a long-connection secret that was fine destroys it — it cannot be
// recovered.
func TestProbeClassifiesSubscribeErrCodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		errCode int
		errMsg  string
		want    error // nil means the probe must succeed
	}{
		{name: "success", errCode: 0, errMsg: "ok", want: nil},
		{name: "invalid secret is a rejection", errCode: 40001, errMsg: "invalid secret", want: ErrCredentialsRejected},
		{name: "invalid corpid is a rejection", errCode: 40013, errMsg: "invalid corpid", want: ErrCredentialsRejected},
		{name: "rate limited is not a rejection", errCode: 45009, errMsg: "api freq out of limit", want: ErrCredentialsUnverifiable},
		{name: "concurrency limited is not a rejection", errCode: 45033, errMsg: "api concurrency out of limit", want: ErrCredentialsUnverifiable},
		{name: "system error is not a rejection", errCode: -1, errMsg: "system busy", want: ErrCredentialsUnverifiable},
		{name: "unknown code is not a rejection", errCode: 999999, errMsg: "something new", want: ErrCredentialsUnverifiable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn := &ackConn{errCode: tc.errCode, errMsg: tc.errMsg}
			p := NewHandshakeProbe(ackDialer{conn: conn}, "wss://example.test/ws")

			err := p.Probe(context.Background(), "bot-1", "secret-1")

			if tc.want == nil {
				if err != nil {
					t.Fatalf("errcode %d: Probe = %v, want nil", tc.errCode, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("errcode %d: Probe = %v, want %v", tc.errCode, err, tc.want)
			}
			// The opposite verdict must not also match, or the split this file
			// exists for is not a split.
			other := ErrCredentialsRejected
			if tc.want == ErrCredentialsRejected {
				other = ErrCredentialsUnverifiable
			}
			if errors.Is(err, other) {
				t.Fatalf("errcode %d: Probe = %v, which also matches %v", tc.errCode, err, other)
			}
		})
	}
}

// Whatever the verdict, the raw code and message travel with the error so the
// handler's log line names the code an operator would need to widen the
// whitelist.
func TestProbeCarriesRawErrCodeOnUnverifiable(t *testing.T) {
	t.Parallel()

	conn := &ackConn{errCode: 45009, errMsg: "api freq out of limit"}
	p := NewHandshakeProbe(ackDialer{conn: conn}, "wss://example.test/ws")

	err := p.Probe(context.Background(), "bot-1", "secret-1")
	if err == nil {
		t.Fatal("Probe = nil, want an unverifiable error")
	}
	if got := err.Error(); !strings.Contains(got, "45009") || !strings.Contains(got, "api freq out of limit") {
		t.Fatalf("error %q does not carry the raw errcode and errmsg", got)
	}
}

// ---- the probe cannot be turned off ----

// Proof of control gates a statement that hard-deletes another workspace's
// installation and every binding under it. A service that can be built without
// a probe reopens exactly that path on the next wiring mistake, so nil is a
// construction error rather than a fail-open mode.
func TestNewInstallationServiceRefusesNilProbe(t *testing.T) {
	t.Parallel()

	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	if _, err := NewInstallationService(nil, nil, box, WithCredentialProbe(nil)); !errors.Is(err, ErrProbeRequired) {
		t.Fatalf("NewInstallationService with an explicit nil probe = %v, want ErrProbeRequired", err)
	}
}

// Omitting the option is not a way to omit the check: the constructor supplies
// the real handshake probe.
func TestNewInstallationServiceDefaultsToTheRealProbe(t *testing.T) {
	t.Parallel()

	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	svc, err := NewInstallationService(nil, nil, box)
	if err != nil {
		t.Fatalf("NewInstallationService: %v", err)
	}
	if svc.probe == nil {
		t.Fatal("probe is nil — an install would reach the reclaim with an unproven credential")
	}
	if _, ok := svc.probe.(*handshakeProbe); !ok {
		t.Fatalf("probe is %T, want the real *handshakeProbe", svc.probe)
	}
}

// A service assembled by struct literal (a test helper, a future caller that
// skips the constructor) must still refuse rather than silently install
// unverified credentials.
func TestUpsertRefusesWithoutAProbe(t *testing.T) {
	t.Parallel()

	svc := &InstallationService{}
	_, err := svc.Upsert(context.Background(), InstallationParams{
		WorkspaceID:     testUUID(1),
		AgentID:         testUUID(2),
		InstallerUserID: testUUID(3),
		BotID:           "somebody-elses-bot",
		Secret:          "a-guess",
	})
	if !errors.Is(err, ErrProbeRequired) {
		t.Fatalf("Upsert without a probe = %v, want ErrProbeRequired", err)
	}
}
