package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func validInstallationRaw(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(installationConfig{
		AppID:           "bot-123",
		SecretEncrypted: "c2VhbGVk", // arbitrary; decrypt is stubbed in tests
		Locale:          "zh-CN",
	})
	if err != nil {
		t.Fatalf("marshal installationConfig: %v", err)
	}
	return raw
}

func identityDecrypt(sealed string) ([]byte, error) {
	return []byte("plain-" + sealed), nil
}

func TestNewFactory_MissingInstallationID(t *testing.T) {
	factory := NewFactory(ChannelDeps{Decrypt: identityDecrypt})
	_, err := factory(channel.Config{
		Type: TypeWecom,
		Raw:  validInstallationRaw(t),
	})
	if err == nil {
		t.Fatal("expected error for missing InstallationID, got nil")
	}
}

func TestNewFactory_BadRawJSON(t *testing.T) {
	factory := NewFactory(ChannelDeps{Decrypt: identityDecrypt})
	_, err := factory(channel.Config{
		Type:           TypeWecom,
		Raw:            json.RawMessage(`{not valid json`),
		InstallationID: util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
	})
	if err == nil {
		t.Fatal("expected error for malformed Raw JSON, got nil")
	}
}

func TestNewFactory_MissingAppID(t *testing.T) {
	factory := NewFactory(ChannelDeps{Decrypt: identityDecrypt})
	raw, _ := json.Marshal(installationConfig{SecretEncrypted: "c2VhbGVk"})
	_, err := factory(channel.Config{
		Type:           TypeWecom,
		Raw:            raw,
		InstallationID: util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
	})
	if err == nil {
		t.Fatal("expected error for missing app_id, got nil")
	}
}

func TestNewFactory_MissingSecretEncrypted(t *testing.T) {
	factory := NewFactory(ChannelDeps{Decrypt: identityDecrypt})
	raw, _ := json.Marshal(installationConfig{AppID: "bot-123"})
	_, err := factory(channel.Config{
		Type:           TypeWecom,
		Raw:            raw,
		InstallationID: util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
	})
	if err == nil {
		t.Fatal("expected error for missing secret_encrypted, got nil")
	}
}

func TestNewFactory_DecryptFailure(t *testing.T) {
	wantErr := errors.New("boom: bad ciphertext")
	factory := NewFactory(ChannelDeps{
		Decrypt: func(string) ([]byte, error) { return nil, wantErr },
	})
	_, err := factory(channel.Config{
		Type:           TypeWecom,
		Raw:            validInstallationRaw(t),
		InstallationID: util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("factory err = %v, want wrapping %v", err, wantErr)
	}
}

func TestNewFactory_NilDecrypt(t *testing.T) {
	factory := NewFactory(ChannelDeps{})
	_, err := factory(channel.Config{
		Type:           TypeWecom,
		Raw:            validInstallationRaw(t),
		InstallationID: util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
	})
	if err == nil {
		t.Fatal("expected error when Decrypt is nil, got nil")
	}
}

func TestNewFactory_HappyPath(t *testing.T) {
	factory := NewFactory(ChannelDeps{Decrypt: identityDecrypt})
	ch, err := factory(channel.Config{
		Type:           TypeWecom,
		Raw:            validInstallationRaw(t),
		InstallationID: util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Type() != TypeWecom {
		t.Fatalf("Type() = %q, want %q", ch.Type(), TypeWecom)
	}
	got := ch.Capabilities()
	if !got.Has(channel.CapText) {
		t.Fatalf("Capabilities() = %v, want CapText set", got)
	}
	if got.Has(channel.CapMessageEdit) {
		t.Fatalf("Capabilities() = %v, want no streaming/message-edit capability", got)
	}
	// Capabilities must be a pure, stable declaration.
	if got2 := ch.Capabilities(); got2 != got {
		t.Fatalf("Capabilities() not stable: %v != %v", got2, got)
	}
}

func TestNewFactory_SharesRetryStateAcrossRebuilds(t *testing.T) {
	deps := ChannelDeps{Decrypt: identityDecrypt, Retries: NewRetryRegistry()}
	factory := NewFactory(deps)
	instID := util.MustParseUUID("11111111-1111-1111-1111-111111111111")

	ch1, err := factory(channel.Config{Type: TypeWecom, Raw: validInstallationRaw(t), InstallationID: instID})
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	wc1, ok := ch1.(*wecomChannel)
	if !ok {
		t.Fatalf("factory returned %T, want *wecomChannel", ch1)
	}
	wc1.retry.NoteAuthFail()
	wc1.retry.NoteAuthFail()

	// Simulate the Supervisor rebuilding the channel for the same
	// installation on the next reconnect attempt: the streak must survive.
	ch2, err := factory(channel.Config{Type: TypeWecom, Raw: validInstallationRaw(t), InstallationID: instID})
	if err != nil {
		t.Fatalf("second build: %v", err)
	}
	wc2 := ch2.(*wecomChannel)
	if wc2.retry.AuthStreak() != 2 {
		t.Fatalf("rebuilt channel auth streak = %d, want 2 (shared RetryState)", wc2.retry.AuthStreak())
	}
}

func TestNewFactory_SendReturnsNotWiredError(t *testing.T) {
	factory := NewFactory(ChannelDeps{Decrypt: identityDecrypt})
	ch, err := factory(channel.Config{
		Type:           TypeWecom,
		Raw:            validInstallationRaw(t),
		InstallationID: util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, sendErr := ch.Send(context.Background(), channel.OutboundMessage{ChatID: "c1", Text: "hi"})
	if sendErr == nil {
		t.Fatal("expected Send to return a not-wired error")
	}
}

func TestWecomChannel_DisconnectWithoutConnectIsNoop(t *testing.T) {
	factory := NewFactory(ChannelDeps{Decrypt: identityDecrypt})
	ch, err := factory(channel.Config{
		Type:           TypeWecom,
		Raw:            validInstallationRaw(t),
		InstallationID: util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ch.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect before Connect returned error: %v", err)
	}
	// Safe to call more than once.
	if err := ch.Disconnect(context.Background()); err != nil {
		t.Fatalf("second Disconnect returned error: %v", err)
	}
}

func TestWecomChannel_ConnectCancelledByCtxReturnsNil(t *testing.T) {
	factory := NewFactory(ChannelDeps{Decrypt: identityDecrypt, DialURL: "ws://127.0.0.1:0/does-not-matter"})
	ch, err := factory(channel.Config{
		Type:           TypeWecom,
		Raw:            validInstallationRaw(t),
		InstallationID: util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Dial will fail immediately against the bogus URL; Connect must
	// surface a dial error rather than hang, and must not panic.
	if err := ch.Connect(ctx); err == nil {
		t.Log("Connect returned nil on a pre-cancelled ctx with unreachable dial URL (acceptable)")
	}
}

// alwaysEmptyDBTX is a minimal db.DBTX fake whose QueryRow always scans as
// pgx.ErrNoRows, so ClaimChannelOutbound behaves exactly like an installation
// with no due queue rows: OutboxConsumer.Run keeps polling and only exits via
// ctx.Done().
type alwaysEmptyDBTX struct{}

func (alwaysEmptyDBTX) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (alwaysEmptyDBTX) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, pgx.ErrNoRows
}
func (alwaysEmptyDBTX) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	return alwaysEmptyRow{}
}

type alwaysEmptyRow struct{}

func (alwaysEmptyRow) Scan(...any) error { return pgx.ErrNoRows }

// TestWecomChannel_ConnectReturnsWhenConnRunExitsWhileOutboxRunning is the
// Critical Finding 1 regression test: when conn.Run returns (dial/subscribe
// failure here, standing in for disconnect/auth-fail) without the caller's
// ctx being cancelled, Connect must still return promptly instead of
// blocking forever on wg.Wait() for the outbox consumer goroutine.
func TestWecomChannel_ConnectReturnsWhenConnRunExitsWhileOutboxRunning(t *testing.T) {
	// The fake server closes the socket immediately without answering the
	// subscribe handshake, so conn.subscribe fails fast and conn.Run
	// returns a "short" error without ever needing ctx to be cancelled.
	srv := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		_ = c.Close()
	})

	factory := NewFactory(ChannelDeps{
		Decrypt: identityDecrypt,
		DialURL: srv.url,
		Outbox: OutboxDeps{
			Queries: db.New(alwaysEmptyDBTX{}),
		},
	})
	ch, err := factory(channel.Config{
		Type:           TypeWecom,
		Raw:            validInstallationRaw(t),
		InstallationID: util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
	})
	if err != nil {
		t.Fatalf("build channel: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- ch.Connect(context.Background()) }()

	select {
	case <-done:
		// Connect returned without the caller cancelling ctx: no deadlock.
	case <-time.After(5 * time.Second):
		t.Fatal("Connect deadlocked: outbox consumer kept wg.Wait() blocked after conn.Run returned")
	}
}

func TestNewRetryRegistry_GetOrCreate(t *testing.T) {
	reg := NewRetryRegistry()
	a := reg.Get("inst-1")
	b := reg.Get("inst-1")
	if a != b {
		t.Fatal("expected Get to return the same *RetryState for the same id")
	}
	c := reg.Get("inst-2")
	if c == a {
		t.Fatal("expected different installations to get distinct RetryState")
	}
}
