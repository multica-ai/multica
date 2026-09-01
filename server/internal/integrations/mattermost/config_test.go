package mattermost

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCanonicalServerURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "plain https", in: "https://mm.example.com", want: "https://mm.example.com"},
		{name: "trailing slash dropped", in: "https://mm.example.com/", want: "https://mm.example.com"},
		{name: "surrounding whitespace", in: "  https://mm.example.com \n", want: "https://mm.example.com"},
		{name: "host lower-cased", in: "https://MM.Example.COM", want: "https://mm.example.com"},
		{name: "scheme lower-cased", in: "HTTPS://mm.example.com", want: "https://mm.example.com"},
		{name: "port preserved", in: "http://localhost:8065", want: "http://localhost:8065"},
		{name: "sub-path preserved", in: "https://corp.example.com/mattermost/", want: "https://corp.example.com/mattermost"},
		{name: "query and fragment dropped", in: "https://mm.example.com/?a=b#c", want: "https://mm.example.com"},
		{name: "empty", in: "", wantErr: true},
		{name: "whitespace only", in: "   ", wantErr: true},
		{name: "no scheme", in: "mm.example.com", wantErr: true},
		{name: "no host", in: "https://", wantErr: true},
		{name: "wrong scheme", in: "ftp://mm.example.com", wantErr: true},
		// A javascript: or file: URL must not survive into a stored config.
		{name: "javascript scheme", in: "javascript:alert(1)", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := canonicalServerURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("canonicalServerURL(%q) = %q, want error", tc.in, got)
				}
				if !errors.Is(err, ErrInvalidServerURL) {
					t.Fatalf("error = %v, want ErrInvalidServerURL", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("canonicalServerURL(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("canonicalServerURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The routing key must distinguish the SAME bot user id on two different
// servers — that is the whole reason it is not a bare bot id like Telegram's.
func TestInstallationKeyDistinguishesServers(t *testing.T) {
	a := installationKey("https://a.example.com", "bot123")
	b := installationKey("https://b.example.com", "bot123")
	if a == b {
		t.Fatalf("same key %q for two different servers", a)
	}
	if got, want := a, "a.example.com:bot123"; got != want {
		t.Fatalf("installationKey = %q, want %q", got, want)
	}
	// The scheme is stripped so switching a deployment from http to https does
	// not orphan its installation.
	if installationKey("http://a.example.com", "bot123") != a {
		t.Fatal("scheme changed the routing key")
	}
	// A sub-path is part of the identity: two Mattermost instances behind one
	// hostname are two servers.
	if installationKey("https://a.example.com/mm", "bot123") == a {
		t.Fatal("sub-path did not change the routing key")
	}
}

func TestWebsocketURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://mm.example.com", "wss://mm.example.com/api/v4/websocket"},
		{"http://localhost:8065", "ws://localhost:8065/api/v4/websocket"},
		{"https://corp.example.com/mattermost", "wss://corp.example.com/mattermost/api/v4/websocket"},
	}
	for _, tc := range tests {
		if got := websocketURL(tc.in); got != tc.want {
			t.Errorf("websocketURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateAccessToken(t *testing.T) {
	if _, err := validateAccessToken(""); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("empty token error = %v, want ErrInvalidAccessToken", err)
	}
	if _, err := validateAccessToken("   "); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("blank token error = %v, want ErrInvalidAccessToken", err)
	}
	// A newline in a token would let the value break out of the Authorization
	// header into a forged one.
	if _, err := validateAccessToken("abc\r\nX-Admin: 1"); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("header-injecting token error = %v, want ErrInvalidAccessToken", err)
	}
	got, err := validateAccessToken("  n1p6r8mgkjbc7bdxr9wa9tbn3o  ")
	if err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if got != "n1p6r8mgkjbc7bdxr9wa9tbn3o" {
		t.Fatalf("token = %q, want it trimmed", got)
	}
}

func TestDecodeCredentials(t *testing.T) {
	cfg, err := json.Marshal(installConfig{
		AppID:                "mm.example.com:bot123",
		ServerURL:            "https://mm.example.com",
		BotUserID:            "bot123",
		BotUsername:          "multica",
		AccessTokenEncrypted: base64.StdEncoding.EncodeToString([]byte("sealed-token")),
	})
	if err != nil {
		t.Fatal(err)
	}

	// A nil Decrypter means "stored bytes are plaintext" (the test path).
	creds, err := decodeCredentials(cfg, nil)
	if err != nil {
		t.Fatalf("decodeCredentials: %v", err)
	}
	if creds.AccessToken != "sealed-token" {
		t.Errorf("AccessToken = %q, want %q", creds.AccessToken, "sealed-token")
	}
	if creds.ServerURL != "https://mm.example.com" || creds.BotUserID != "bot123" || creds.BotUsername != "multica" {
		t.Errorf("credentials = %+v, want the stored identity", creds)
	}

	// The injected Decrypter is what actually runs in production.
	creds, err = decodeCredentials(cfg, func(ct []byte) ([]byte, error) {
		return []byte(strings.ToUpper(string(ct))), nil
	})
	if err != nil {
		t.Fatalf("decodeCredentials with decrypter: %v", err)
	}
	if creds.AccessToken != "SEALED-TOKEN" {
		t.Errorf("AccessToken = %q, want the decrypter's output", creds.AccessToken)
	}

	if _, err := decodeCredentials(nil, nil); err == nil {
		t.Error("empty config accepted, want error")
	}
	if _, err := decodeCredentials(json.RawMessage("{not json"), nil); err == nil {
		t.Error("malformed config accepted, want error")
	}
	if _, err := decodeCredentials(cfg, func([]byte) ([]byte, error) {
		return nil, errors.New("key rotated")
	}); err == nil {
		t.Error("decrypt failure swallowed, want error")
	}
}

// DecodePublicConfig feeds the management API. It must never surface the
// token, and must still render a row when the blob cannot be parsed.
func TestDecodePublicConfig(t *testing.T) {
	cfg, err := json.Marshal(installConfig{
		ServerURL:            "https://mm.example.com",
		BotUserID:            "bot123",
		BotUsername:          "multica",
		AccessTokenEncrypted: "c2VjcmV0",
	})
	if err != nil {
		t.Fatal(err)
	}
	pub := DecodePublicConfig(cfg)
	if pub.ServerURL != "https://mm.example.com" || pub.BotUserID != "bot123" || pub.BotUsername != "multica" {
		t.Fatalf("PublicConfig = %+v, want the display fields", pub)
	}
	// The struct has no token field at all; assert the shape stays that way by
	// checking the marshalled form carries nothing secret.
	blob, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "c2VjcmV0") {
		t.Fatalf("PublicConfig leaked the token: %s", blob)
	}

	if got := DecodePublicConfig(json.RawMessage("{broken")); got != (PublicConfig{}) {
		t.Fatalf("malformed config = %+v, want zero value (row still renders)", got)
	}
}

func TestDecryptTokenToleratesWrappedBase64(t *testing.T) {
	// Some deployments store MIME-wrapped base64; the newlines must not break
	// the decode.
	raw := base64.StdEncoding.EncodeToString([]byte("token-value"))
	wrapped := raw[:4] + "\n" + raw[4:]
	got, err := decryptToken(wrapped, nil)
	if err != nil {
		t.Fatalf("decryptToken: %v", err)
	}
	if got != "token-value" {
		t.Fatalf("decryptToken = %q, want %q", got, "token-value")
	}
	if got, err := decryptToken("", nil); err != nil || got != "" {
		t.Fatalf("decryptToken(\"\") = %q, %v; want empty, nil", got, err)
	}
}
