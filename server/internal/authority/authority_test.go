package authority

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestWriteReceiptSignVerifyBindsWrite(t *testing.T) {
	stmt, pub, priv := testStatement(t)
	digest := sha256.Sum256([]byte(`{"nonce":"request-nonce"}`))
	receiptStmt := WriteReceiptStatement{
		Protocol:      WriteReceiptProtocolVersion,
		Operation:     "issue.upsert-external",
		RequestSHA256: fmt.Sprintf("%x", digest),
		ResourceID:    "11111111-1111-1111-1111-111111111111",
		WorkspaceID:   "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		Nonce:         stmt.Nonce,
		AuthorityID:   stmt.AuthorityID,
		DBIdentity:    stmt.DBIdentity,
		IssuedAt:      stmt.IssuedAt,
		ServerCommit:  stmt.ServerCommit,
	}
	receipt, err := SignWriteReceipt(priv, receiptStmt)
	if err != nil {
		t.Fatalf("SignWriteReceipt: %v", err)
	}
	pin := Pin{ServerURL: "https://api.multica.test", PublicKey: EncodePublicKey(pub), AuthorityID: stmt.AuthorityID, DBIdentity: stmt.DBIdentity}
	if err := VerifyWriteReceipt(receipt, pin, pin.ServerURL, stmt.IssuedAt.Add(time.Second), 2*time.Minute, 30*time.Second); err != nil {
		t.Fatalf("VerifyWriteReceipt: %v", err)
	}
	expected := WriteReceiptExpectation{Operation: receipt.Operation, RequestSHA256: receipt.RequestSHA256, ResourceID: receipt.ResourceID, WorkspaceID: receipt.WorkspaceID, Nonce: receipt.Nonce}
	if err := VerifyBoundWriteReceipt(receipt, expected, pin, pin.ServerURL, stmt.IssuedAt.Add(time.Second), 2*time.Minute, 30*time.Second); err != nil {
		t.Fatalf("VerifyBoundWriteReceipt: %v", err)
	}
	for name, mismatch := range map[string]WriteReceiptExpectation{
		"operation": {Operation: "other", RequestSHA256: expected.RequestSHA256, ResourceID: expected.ResourceID, WorkspaceID: expected.WorkspaceID, Nonce: expected.Nonce},
		"digest":    {Operation: expected.Operation, RequestSHA256: strings.Repeat("0", 64), ResourceID: expected.ResourceID, WorkspaceID: expected.WorkspaceID, Nonce: expected.Nonce},
		"resource":  {Operation: expected.Operation, RequestSHA256: expected.RequestSHA256, ResourceID: "22222222-2222-2222-2222-222222222222", WorkspaceID: expected.WorkspaceID, Nonce: expected.Nonce},
		"workspace": {Operation: expected.Operation, RequestSHA256: expected.RequestSHA256, ResourceID: expected.ResourceID, WorkspaceID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", Nonce: expected.Nonce},
		"nonce":     {Operation: expected.Operation, RequestSHA256: expected.RequestSHA256, ResourceID: expected.ResourceID, WorkspaceID: expected.WorkspaceID, Nonce: base64.RawURLEncoding.EncodeToString([]byte("12345678901234567890123456789012"))},
	} {
		t.Run("expected_"+name, func(t *testing.T) {
			if VerifyBoundWriteReceipt(receipt, mismatch, pin, pin.ServerURL, stmt.IssuedAt.Add(time.Second), 2*time.Minute, 30*time.Second) == nil {
				t.Fatal("accepted mismatched write expectation")
			}
		})
	}

	mutations := map[string]func(WriteReceipt) WriteReceipt{
		"operation": func(r WriteReceipt) WriteReceipt { r.Operation = "issue.delete"; return r },
		"digest":    func(r WriteReceipt) WriteReceipt { r.RequestSHA256 = strings.Repeat("0", 64); return r },
		"resource":  func(r WriteReceipt) WriteReceipt { r.ResourceID = "22222222-2222-2222-2222-222222222222"; return r },
		"workspace": func(r WriteReceipt) WriteReceipt { r.WorkspaceID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"; return r },
		"nonce": func(r WriteReceipt) WriteReceipt {
			r.Nonce = base64.RawURLEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
			return r
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			if err := VerifyWriteReceipt(mutate(receipt), pin, pin.ServerURL, stmt.IssuedAt.Add(time.Second), 2*time.Minute, 30*time.Second); err == nil {
				t.Fatal("VerifyWriteReceipt accepted tampering")
			}
		})
	}
}

func TestAuthorityRejectsUnknownOrBlankServerCommit(t *testing.T) {
	stmt, _, priv := testStatement(t)
	for _, commit := range []string{"", "   ", "unknown"} {
		stmt.ServerCommit = commit
		if _, err := Sign(priv, stmt); err == nil {
			t.Fatalf("Sign accepted server commit %q", commit)
		}
	}
}

func testStatement(t *testing.T) (Statement, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return Statement{
		Protocol:    ProtocolVersion,
		Nonce:       base64.RawURLEncoding.EncodeToString(make([]byte, 32)),
		AuthorityID: "local-dev-authority",
		DBIdentity: DBIdentity{
			SystemIdentifier: "7420934553282556881",
			DatabaseOID:      16384,
			DatabaseName:     "multica_test",
		},
		IssuedAt:     time.Date(2026, 7, 13, 12, 0, 0, 123, time.UTC),
		ServerCommit: "37b0a6e72e45b0336d7d444485f6fd39165cc9a9",
	}, pub, priv
}

func TestSignVerifyRoundTrip(t *testing.T) {
	stmt, pub, priv := testStatement(t)
	att, err := Sign(priv, stmt)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	pin := Pin{
		ServerURL:   "https://api.multica.test",
		PublicKey:   EncodePublicKey(pub),
		AuthorityID: stmt.AuthorityID,
		DBIdentity:  stmt.DBIdentity,
	}
	if err := Verify(att, pin, "https://api.multica.test/", stmt.IssuedAt.Add(30*time.Second), 2*time.Minute, 30*time.Second); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestVerifyRejectsTamperedSignedFields(t *testing.T) {
	stmt, pub, priv := testStatement(t)
	att, err := Sign(priv, stmt)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	pin := Pin{
		ServerURL:   "https://api.multica.test",
		PublicKey:   EncodePublicKey(pub),
		AuthorityID: stmt.AuthorityID,
		DBIdentity:  stmt.DBIdentity,
	}
	now := stmt.IssuedAt.Add(30 * time.Second)

	cases := map[string]func(Attestation) Attestation{
		"protocol": func(a Attestation) Attestation {
			a.Protocol = "multica-authority-attestation-v2"
			return a
		},
		"nonce": func(a Attestation) Attestation {
			a.Nonce = base64.RawURLEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
			return a
		},
		"authority_id": func(a Attestation) Attestation {
			a.AuthorityID = "other-authority"
			return a
		},
		"db_identity": func(a Attestation) Attestation {
			a.DBIdentity.DatabaseName = "copied_backend"
			return a
		},
		"issued_at": func(a Attestation) Attestation {
			a.IssuedAt = stmt.IssuedAt.Add(time.Second).Format(time.RFC3339Nano)
			return a
		},
		"server_commit": func(a Attestation) Attestation {
			a.ServerCommit = "ffffffffffffffffffffffffffffffffffffffff"
			return a
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Verify(mutate(att), pin, pin.ServerURL, now, 2*time.Minute, 30*time.Second); err == nil {
				t.Fatal("Verify succeeded for tampered attestation")
			}
		})
	}
}

func TestVerifyRejectsWrongPinsAndFreshness(t *testing.T) {
	stmt, pub, priv := testStatement(t)
	att, err := Sign(priv, stmt)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	pin := Pin{
		ServerURL:   "https://api.multica.test",
		PublicKey:   EncodePublicKey(pub),
		AuthorityID: stmt.AuthorityID,
		DBIdentity:  stmt.DBIdentity,
	}

	_, wrongPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate wrong key: %v", err)
	}
	wrongPub := wrongPriv.Public().(ed25519.PublicKey)

	cases := map[string]Pin{
		"wrong_public_key": func() Pin {
			p := pin
			p.PublicKey = EncodePublicKey(wrongPub)
			return p
		}(),
		"wrong_authority": func() Pin {
			p := pin
			p.AuthorityID = "wrong-authority"
			return p
		}(),
		"wrong_db": func() Pin {
			p := pin
			p.DBIdentity.DatabaseOID = 16385
			return p
		}(),
		"wrong_server_url": func() Pin {
			p := pin
			p.ServerURL = "https://other.multica.test"
			return p
		}(),
	}
	for name, badPin := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Verify(att, badPin, "https://api.multica.test", stmt.IssuedAt.Add(10*time.Second), 2*time.Minute, 30*time.Second); err == nil {
				t.Fatal("Verify succeeded with wrong pin")
			}
		})
	}

	if err := Verify(att, pin, pin.ServerURL, stmt.IssuedAt.Add(3*time.Minute), 2*time.Minute, 30*time.Second); err == nil {
		t.Fatal("Verify succeeded for stale attestation")
	}
	if err := Verify(att, pin, pin.ServerURL, stmt.IssuedAt.Add(-31*time.Second), 2*time.Minute, 30*time.Second); err == nil {
		t.Fatal("Verify succeeded for future attestation")
	}
}

func TestVerifyRejectsNonCanonicalIssuedAtWireText(t *testing.T) {
	stmt, pub, priv := testStatement(t)
	att, err := Sign(priv, stmt)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	pin := Pin{ServerURL: "https://api.multica.test", PublicKey: EncodePublicKey(pub), AuthorityID: stmt.AuthorityID, DBIdentity: stmt.DBIdentity}
	now := stmt.IssuedAt.Add(30 * time.Second)

	for name, issuedAt := range map[string]string{
		"equivalent_offset":  stmt.IssuedAt.In(time.FixedZone("offset", 60*60)).Format(time.RFC3339Nano),
		"numeric_utc_offset": strings.TrimSuffix(att.IssuedAt, "Z") + "+00:00",
	} {
		t.Run(name, func(t *testing.T) {
			mutated := att
			mutated.IssuedAt = issuedAt
			if err := Verify(mutated, pin, pin.ServerURL, now, 2*time.Minute, 30*time.Second); err == nil {
				t.Fatalf("Verify accepted non-canonical issued_at %q", issuedAt)
			}
		})
	}
}

func TestVerifyRejectsNonCanonicalSignatureBase64URL(t *testing.T) {
	stmt, pub, priv := testStatement(t)
	att, err := Sign(priv, stmt)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	pin := Pin{ServerURL: "https://api.multica.test", PublicKey: EncodePublicKey(pub), AuthorityID: stmt.AuthorityID, DBIdentity: stmt.DBIdentity}
	now := stmt.IssuedAt.Add(30 * time.Second)

	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := strings.IndexByte(alphabet, att.Signature[len(att.Signature)-1])
	if last < 0 || last&3 != 0 {
		t.Fatalf("unexpected canonical signature suffix %q", att.Signature[len(att.Signature)-1:])
	}
	trailingBits := att.Signature[:len(att.Signature)-1] + string(alphabet[last|1])

	for name, signature := range map[string]string{
		"ignored_crlf":  att.Signature + "\r\n",
		"trailing_bits": trailingBits,
	} {
		t.Run(name, func(t *testing.T) {
			mutated := att
			mutated.Signature = signature
			if err := Verify(mutated, pin, pin.ServerURL, now, 2*time.Minute, 30*time.Second); err == nil {
				t.Fatalf("Verify accepted non-canonical signature %q", signature)
			}
		})
	}
}

func TestValidateNonceRequiresStrictCanonicalBase64URL32Bytes(t *testing.T) {
	valid := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if _, err := ValidateNonce(valid); err != nil {
		t.Fatalf("ValidateNonce(valid): %v", err)
	}

	for _, nonce := range []string{
		valid + "=",
		"+/" + valid[2:],
		base64.RawURLEncoding.EncodeToString(make([]byte, 31)),
		base64.RawURLEncoding.EncodeToString(make([]byte, 33)),
		"not base64url",
	} {
		if _, err := ValidateNonce(nonce); err == nil {
			t.Fatalf("ValidateNonce(%q) succeeded", nonce)
		}
	}
}

func TestWriteReceiptV1RoundTripPreservesLegacyJSONShape(t *testing.T) {
	stmt, pub, priv := testStatement(t)
	digest := sha256.Sum256([]byte(`{"nonce":"legacy"}`))
	receipt, err := SignWriteReceipt(priv, WriteReceiptStatement{
		Protocol:      WriteReceiptProtocolV1,
		Operation:     OperationIssueUpsertExternal,
		RequestSHA256: fmt.Sprintf("%x", digest),
		ResourceID:    "11111111-1111-1111-1111-111111111111",
		Nonce:         stmt.Nonce,
		AuthorityID:   stmt.AuthorityID,
		DBIdentity:    stmt.DBIdentity,
		IssuedAt:      stmt.IssuedAt,
		ServerCommit:  stmt.ServerCommit,
	})
	if err != nil {
		t.Fatalf("SignWriteReceipt(v1): %v", err)
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "workspace_id") {
		t.Fatalf("v1 receipt added a field unknown to strict legacy clients: %s", raw)
	}
	pin := Pin{ServerURL: "https://api.multica.test", PublicKey: EncodePublicKey(pub), AuthorityID: stmt.AuthorityID, DBIdentity: stmt.DBIdentity}
	expected := WriteReceiptExpectation{
		Protocol:      WriteReceiptProtocolV1,
		Operation:     receipt.Operation,
		RequestSHA256: receipt.RequestSHA256,
		ResourceID:    receipt.ResourceID,
		Nonce:         receipt.Nonce,
	}
	if err := VerifyBoundWriteReceipt(receipt, expected, pin, pin.ServerURL, stmt.IssuedAt.Add(time.Second), 2*time.Minute, 30*time.Second); err != nil {
		t.Fatalf("VerifyBoundWriteReceipt(v1): %v", err)
	}
	expected.Protocol = WriteReceiptProtocolV2
	if err := VerifyBoundWriteReceipt(receipt, expected, pin, pin.ServerURL, stmt.IssuedAt.Add(time.Second), 2*time.Minute, 30*time.Second); err == nil {
		t.Fatal("v2 expectation accepted downgraded v1 receipt")
	}
}
