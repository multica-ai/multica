package dingtalk

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
		RobotCode:          "appkey-1",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealed),
	})

	creds, err := decodeCredentials(raw, box.Open)
	if err != nil {
		t.Fatalf("decodeCredentials: %v", err)
	}
	if creds.AppKey != "appkey-1" || creds.RobotCode != "appkey-1" || creds.AppSecret != "the-app-secret" {
		t.Errorf("creds = %+v, want appkey-1 / the-app-secret", creds)
	}
}

func TestDecodeCredentials_RobotCodeDefaultsToAppID(t *testing.T) {
	box := testBox(t)
	sealed, _ := box.Seal([]byte("s"))
	raw, _ := json.Marshal(installConfig{
		AppID:              "appkey-2",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealed),
	})
	creds, err := decodeCredentials(raw, box.Open)
	if err != nil {
		t.Fatalf("decodeCredentials: %v", err)
	}
	if creds.RobotCode != "appkey-2" {
		t.Errorf("robotCode = %q, want fallback to app_id appkey-2", creds.RobotCode)
	}
}

func TestDecodeCredentials_EmptyConfig(t *testing.T) {
	if _, err := decodeCredentials(nil, nil); err == nil {
		t.Error("expected an error for empty config")
	}
}
