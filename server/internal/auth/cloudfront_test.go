package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"
)

func decodeCloudFrontBase64(t *testing.T, encoded string) string {
	t.Helper()
	standard := strings.NewReplacer("-", "+", "_", "=", "~", "/").Replace(encoded)
	decoded, err := base64.StdEncoding.DecodeString(standard)
	if err != nil {
		t.Fatalf("decode CloudFront base64: %v", err)
	}
	return string(decoded)
}

func TestCloudFrontSignedURLWithContentDisposition(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer := &CloudFrontSigner{
		keyPairID:  "K123",
		privateKey: key,
	}

	got := signer.SignedURLWithContentDisposition(
		"https://static.example.test/uploads/report.md?existing=1",
		`attachment; filename="report.md"`,
		time.Unix(1893456000, 0),
	)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	q := u.Query()
	if got := q.Get("response-content-disposition"); got != `attachment; filename="report.md"` {
		t.Fatalf("response-content-disposition = %q", got)
	}
	if got := q.Get("Key-Pair-Id"); got != "K123" {
		t.Fatalf("Key-Pair-Id = %q", got)
	}
	if q.Get("Signature") == "" {
		t.Fatalf("missing Signature in %q", got)
	}

	policy := decodeCloudFrontBase64(t, q.Get("Policy"))
	if !strings.Contains(policy, "response-content-disposition=attachment%3B+filename%3D%22report.md%22") {
		t.Fatalf("policy did not include signed response-content-disposition: %s", policy)
	}
}
