package telegram

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestParseTelegramBotID(t *testing.T) {
	id, err := parseTelegramBotID("123456789:AAExampleSecretToken")
	if err != nil || id != "123456789" {
		t.Fatalf("got (%q, %v), want (\"123456789\", nil)", id, err)
	}
	if _, err := parseTelegramBotID("nocolon"); err == nil {
		t.Fatal("expected error for token without ':'")
	}
	if _, err := parseTelegramBotID(":AA"); err == nil {
		t.Fatal("expected error for empty bot id")
	}
}

func TestDecodeCredentialsDecryptsBotToken(t *testing.T) {
	// nil Decrypter treats stored bytes as plaintext; the stored value is
	// base64(plaintext) to mirror the at-rest encoding.
	raw, _ := json.Marshal(installConfig{
		AppID:             "123456789",
		BotUsername:       "acme_tasks_bot",
		BotTokenEncrypted: base64Std("123456789:AAsecret"),
	})
	creds, err := decodeCredentials(raw, nil)
	if err != nil {
		t.Fatalf("decodeCredentials: %v", err)
	}
	if creds.BotID != "123456789" || creds.BotUsername != "acme_tasks_bot" || creds.BotToken != "123456789:AAsecret" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
}

func base64Std(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
