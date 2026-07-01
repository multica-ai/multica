package dingtalk

import (
	"encoding/base64"
	"encoding/json"
	"strings"
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

func TestDecodePublicConfig_OmitsSecret(t *testing.T) {
	raw, _ := json.Marshal(installConfig{
		AppID:              "appkey-3",
		RobotCode:          "appkey-3",
		AppSecretEncrypted: "ZW5jcnlwdGVk",
	})
	pub := DecodePublicConfig(raw)
	if pub.AppID != "appkey-3" || pub.RobotCode != "appkey-3" {
		t.Errorf("public config = %+v", pub)
	}
	// Re-marshal the public view and confirm no secret leaks through it.
	out, _ := json.Marshal(pub)
	if string(out) == "" || containsSecret(string(out)) {
		t.Errorf("public config marshal leaked a secret: %s", out)
	}
}

func containsSecret(s string) bool {
	for _, needle := range []string{"AppSecret", "app_secret", "ZW5jcnlwdGVk"} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
