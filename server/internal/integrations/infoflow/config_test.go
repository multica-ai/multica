package infoflow

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestDecodeCredentials_RoundTrip(t *testing.T) {
	box := testBox(t)
	sealed, err := box.Seal([]byte("the-app-secret"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	raw, _ := json.Marshal(installConfig{
		AppID:              "appkey-1",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealed),
	})

	creds, err := decodeCredentials(raw, box.Open)
	if err != nil {
		t.Fatalf("decodeCredentials: %v", err)
	}
	if creds.AppKey != "appkey-1" || creds.AppSecret != "the-app-secret" {
		t.Errorf("creds = %+v, want appkey-1 / the-app-secret", creds)
	}
}

func TestDecodeCredentials_Errors(t *testing.T) {
	box := testBox(t)
	sealed, _ := box.Seal([]byte("s"))
	valid := mustJSON(t, installConfig{
		AppID:              "appkey-2",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealed),
	})

	tests := []struct {
		name    string
		raw     json.RawMessage
		decrypt Decrypter
	}{
		{name: "empty config", raw: nil, decrypt: box.Open},
		{name: "nil decrypt", raw: valid, decrypt: nil},
		{name: "missing app id", raw: mustJSON(t, installConfig{AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealed)}), decrypt: box.Open},
		{name: "bad base64", raw: mustJSON(t, installConfig{AppID: "a", AppSecretEncrypted: "not-base64!!"}), decrypt: box.Open},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeCredentials(tc.raw, tc.decrypt); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
