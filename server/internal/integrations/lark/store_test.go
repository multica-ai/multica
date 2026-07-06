package lark

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func uuidFrom(b byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	u.Valid = true
	return u
}

// sealedSecret returns an n-byte value standing in for a secretbox-sealed app
// secret. A real sealed Lark secret is ~72 bytes, which base64-encodes to >76
// chars and so triggers PostgreSQL's MIME line wrapping.
func sealedSecret(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

// mimeWrap reproduces PostgreSQL encode(...,'base64') line wrapping: a newline
// every 76 characters.
func mimeWrap(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i += 76 {
		end := i + 76
		if end > len(s) {
			end = len(s)
		}
		out.WriteString(s[i:end])
		if end < len(s) {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func TestInstallationConfigRoundTrip(t *testing.T) {
	secret := sealedSecret(72)
	in := Installation{
		ID:                 uuidFrom(0x11),
		WorkspaceID:        uuidFrom(0x22),
		AgentID:            uuidFrom(0x33),
		AppID:              "cli_app_123",
		AppSecretEncrypted: secret,
		TenantKey:          pgtype.Text{String: "tenant_xyz", Valid: true},
		BotOpenID:          "ou_bot_open",
		InstallerUserID:    uuidFrom(0x44),
		Region:             "lark",
		BotUnionID:         pgtype.Text{String: "on_union_999", Valid: true},
	}

	cfg, err := encodeInstallConfig(in)
	if err != nil {
		t.Fatalf("encodeInstallConfig: %v", err)
	}

	// The encoder must emit unwrapped base64 — embedded newlines are exactly
	// what breaks Go's json []byte decode path.
	if bytes.ContainsAny(cfg, "\n\r") {
		t.Fatalf("encoded config carries wrapped base64 / newlines: %s", cfg)
	}

	row := db.ChannelInstallation{
		ID:              in.ID,
		WorkspaceID:     in.WorkspaceID,
		AgentID:         in.AgentID,
		ChannelType:     "feishu",
		Config:          cfg,
		Status:          "active",
		InstallerUserID: in.InstallerUserID,
		WsLeaseToken:    pgtype.Text{String: "node-1-g3", Valid: true},
	}

	got, err := installationFromRow(row)
	if err != nil {
		t.Fatalf("installationFromRow: %v", err)
	}
	if !bytes.Equal(got.AppSecretEncrypted, secret) {
		t.Fatalf("secret round-trip mismatch:\n got %x\nwant %x", got.AppSecretEncrypted, secret)
	}
	if got.AppID != in.AppID || got.BotOpenID != in.BotOpenID || got.Region != in.Region {
		t.Fatalf("scalar config mismatch: %+v", got)
	}
	if got.TenantKey != in.TenantKey || got.BotUnionID != in.BotUnionID {
		t.Fatalf("optional config mismatch: tenant=%+v union=%+v", got.TenantKey, got.BotUnionID)
	}
	// Flat columns must come straight from the row, not the config.
	if got.Status != "active" || got.WsLeaseToken.String != "node-1-g3" {
		t.Fatalf("flat columns not preserved: status=%q lease=%q", got.Status, got.WsLeaseToken.String)
	}
	if got.ID != in.ID || got.WorkspaceID != in.WorkspaceID || got.InstallerUserID != in.InstallerUserID {
		t.Fatalf("flat id columns not preserved: %+v", got)
	}
}

// TestInstallationToleratesMimeWrappedSecret simulates the migration backfill:
// channel_installation.config.app_secret_encrypted holds base64 wrapped at 76
// chars (PostgreSQL encode default). Decoding must still recover the bytes.
func TestInstallationToleratesMimeWrappedSecret(t *testing.T) {
	secret := sealedSecret(72)
	wrapped := mimeWrap(base64.StdEncoding.EncodeToString(secret))
	if !strings.Contains(wrapped, "\n") {
		t.Fatal("test setup: expected wrapped base64 to contain a newline")
	}

	cfg, err := json.Marshal(map[string]string{
		"app_id":               "cli_app_123",
		"app_secret_encrypted": wrapped,
		"region":               "feishu",
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	got, err := installationFromRow(db.ChannelInstallation{Config: cfg, Status: "active"})
	if err != nil {
		t.Fatalf("installationFromRow with wrapped secret: %v", err)
	}
	if !bytes.Equal(got.AppSecretEncrypted, secret) {
		t.Fatalf("wrapped secret round-trip mismatch:\n got %x\nwant %x", got.AppSecretEncrypted, secret)
	}
}

func TestInstallConfigOmitsEmptyOptionalFields(t *testing.T) {
	cfg, err := encodeInstallConfig(Installation{
		AppID:     "cli_app_123",
		BotOpenID: "ou_bot",
		Region:    "feishu",
		// TenantKey, BotUnionID, AppSecretEncrypted all empty.
	})
	if err != nil {
		t.Fatalf("encodeInstallConfig: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(cfg, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"tenant_key", "bot_union_id", "app_secret_encrypted"} {
		if _, ok := raw[k]; ok {
			t.Fatalf("expected %q to be omitted, config=%s", k, cfg)
		}
	}
	// And they decode back to the zero/invalid pgtype values.
	got, err := installationFromRow(db.ChannelInstallation{Config: cfg})
	if err != nil {
		t.Fatalf("installationFromRow: %v", err)
	}
	if got.TenantKey.Valid || got.BotUnionID.Valid || got.AppSecretEncrypted != nil {
		t.Fatalf("empty optionals should decode invalid/nil: %+v", got)
	}
}

func TestUserBindingConfigRoundTrip(t *testing.T) {
	in := UserBinding{
		ID:             uuidFrom(0x55),
		WorkspaceID:    uuidFrom(0x22),
		MulticaUserID:  uuidFrom(0x66),
		InstallationID: uuidFrom(0x11),
		ChannelUserID:  "ou_sender",
		UnionID:        pgtype.Text{String: "on_union_777", Valid: true},
	}
	cfg, err := encodeBindingConfig(in)
	if err != nil {
		t.Fatalf("encodeBindingConfig: %v", err)
	}
	got, err := userBindingFromRow(db.ChannelUserBinding{
		ID:             in.ID,
		WorkspaceID:    in.WorkspaceID,
		MulticaUserID:  in.MulticaUserID,
		InstallationID: in.InstallationID,
		ChannelUserID:  in.ChannelUserID,
		Config:         cfg,
	})
	if err != nil {
		t.Fatalf("userBindingFromRow: %v", err)
	}
	if got.UnionID != in.UnionID || got.ChannelUserID != in.ChannelUserID || got.MulticaUserID != in.MulticaUserID {
		t.Fatalf("user binding round-trip mismatch: %+v", got)
	}
}

// An absent union_id must serialize to "{}" so the upsert's
// `config || jsonb_strip_nulls(EXCLUDED.config)` merge cannot wipe an
// already-stored union_id when this bind does not carry one.
func TestBindingConfigNullStrip(t *testing.T) {
	cfg, err := encodeBindingConfig(UserBinding{ChannelUserID: "ou_sender"})
	if err != nil {
		t.Fatalf("encodeBindingConfig: %v", err)
	}
	if got := strings.TrimSpace(string(cfg)); got != "{}" {
		t.Fatalf("expected empty union_id to encode as {}, got %q", got)
	}
}

func TestChatSessionBindingFromRow(t *testing.T) {
	row := db.ChannelChatSessionBinding{
		ID:             uuidFrom(0x77),
		ChatSessionID:  uuidFrom(0x88),
		InstallationID: uuidFrom(0x11),
		ChannelType:    "feishu",
		ChannelChatID:  "oc_chat_1",
		ChatType:       "group",
		LastMessageID:  pgtype.Text{String: "om_last", Valid: true},
		LastThreadID:   pgtype.Text{String: "omt_thread", Valid: true},
	}
	got := chatSessionBindingFromRow(row)
	if got.ChannelChatID != "oc_chat_1" || got.ChatType != "group" {
		t.Fatalf("chat fields mismatch: %+v", got)
	}
	if got.LastMessageID != row.LastMessageID || got.LastThreadID != row.LastThreadID {
		t.Fatalf("reply-target fields mismatch: %+v", got)
	}
	if got.ChatSessionID != row.ChatSessionID || got.InstallationID != row.InstallationID {
		t.Fatalf("id fields mismatch: %+v", got)
	}
}
