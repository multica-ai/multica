package redact

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestAuditJSONMasksSecretKeysAndTokenShapes(t *testing.T) {
	raw := json.RawMessage(`{"secret":"not logged","query":"latest","credentials":{"api_key":"sk-12345678901234567890"}}`)
	got := AuditJSON(raw)
	if bytes.Contains(got, []byte("not logged")) || bytes.Contains(got, []byte("sk-12345678901234567890")) {
		t.Fatalf("secret leaked: %s", got)
	}
	if !bytes.Contains(got, []byte("latest")) {
		t.Fatalf("non-secret argument dropped: %s", got)
	}
}
