package service

import (
	"encoding/base64"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// tokenTestSigner returns a signer whose clock the test controls.
func tokenTestSigner(secret string, now time.Time) *ActionSigner {
	s := NewActionSigner([]byte(secret))
	s.now = func() time.Time { return now }
	return s
}

func tokenTestProposal(t *testing.T, seed byte) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	for i := range id.Bytes {
		id.Bytes[i] = seed + byte(i)
	}
	id.Valid = true
	return id
}

// mutateToken decodes a token, lets change edit the raw bytes, and encodes it
// again, so each test changes exactly one part.
func mutateToken(t *testing.T, raw string, change func([]byte)) string {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("token is not base64url: %v", err)
	}
	change(data)
	return base64.RawURLEncoding.EncodeToString(data)
}

func TestActionTokenRoundTripCarriesProposalActionAndExpiry(t *testing.T) {
	issued := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	s := tokenTestSigner("secret-one", issued)
	id := tokenTestProposal(t, 1)

	for _, action := range []string{ProposalDecisionApprove, ProposalDecisionReject, ProposalDecisionSnooze} {
		raw, err := s.Sign(id, action)
		if err != nil {
			t.Fatalf("Sign(%s): %v", action, err)
		}
		got, err := s.Verify(raw)
		if err != nil {
			t.Fatalf("Verify(%s): %v", action, err)
		}
		if got.Action != action || got.ProposalID != id {
			t.Fatalf("Verify(%s) = %+v", action, got)
		}
		if want := issued.Add(ActionTokenTTL); !got.ExpiresAt.Equal(want) {
			t.Fatalf("expiry = %v, want %v (24 hours after issue)", got.ExpiresAt, want)
		}
	}
}

func TestActionTokenIsURLSafeAndFitsTelegramCallbackData(t *testing.T) {
	s := tokenTestSigner("secret-one", time.Now())
	raw, err := s.Sign(tokenTestProposal(t, 9), ProposalDecisionApprove)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 64 {
		t.Fatalf("token is %d bytes; Telegram callback_data allows 64", len(raw))
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(raw) {
		t.Fatalf("token %q is not URL-safe", raw)
	}
}

func TestActionTokenExpires(t *testing.T) {
	issued := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	id := tokenTestProposal(t, 2)
	raw, err := tokenTestSigner("secret-one", issued).Sign(id, ProposalDecisionReject)
	if err != nil {
		t.Fatal(err)
	}

	verifier := tokenTestSigner("secret-one", issued.Add(ActionTokenTTL-time.Second))
	if _, err := verifier.Verify(raw); err != nil {
		t.Fatalf("a token one second before expiry must verify: %v", err)
	}

	for _, at := range []time.Time{issued.Add(ActionTokenTTL), issued.Add(ActionTokenTTL + time.Hour)} {
		got, err := tokenTestSigner("secret-one", at).Verify(raw)
		if !errors.Is(err, ErrActionTokenExpired) {
			t.Fatalf("at %v: err = %v, want ErrActionTokenExpired", at, err)
		}
		if got.ProposalID != id || got.Action != ProposalDecisionReject {
			t.Fatalf("an expired token still names its proposal and action, got %+v", got)
		}
	}
}

func TestActionTokenTamperingIsRefused(t *testing.T) {
	now := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	s := tokenTestSigner("secret-one", now)
	raw, err := s.Sign(tokenTestProposal(t, 3), ProposalDecisionApprove)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Sign(tokenTestProposal(t, 50), ProposalDecisionApprove)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"changed signature": mutateToken(t, raw, func(b []byte) { b[actionTokenLen-1] ^= 0x01 }),
		"changed proposal id": mutateToken(t, raw, func(b []byte) {
			b[2] ^= 0xff
		}),
		"changed action": mutateToken(t, raw, func(b []byte) { b[1] = actionCodes[ProposalDecisionReject] }),
		"extended expiry": mutateToken(t, raw, func(b []byte) {
			b[18], b[19], b[20], b[21] = 0x7f, 0xff, 0xff, 0xff
		}),
		"signature from another proposal": mutateToken(t, raw, func(b []byte) {
			copy(b[actionTokenBodyLen:], mustDecode(t, other)[actionTokenBodyLen:])
		}),
		"a token signed with another secret": mustSign(t, tokenTestSigner("secret-two", now), tokenTestProposal(t, 3), ProposalDecisionApprove),
	}
	for name, token := range cases {
		if _, err := s.Verify(token); !errors.Is(err, ErrActionTokenInvalid) {
			t.Errorf("%s: err = %v, want ErrActionTokenInvalid", name, err)
		}
	}
}

// A forged expiry on an old token is reported as tampering, not as expiry.
func TestActionTokenTamperedAndExpiredIsInvalid(t *testing.T) {
	issued := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	raw, err := tokenTestSigner("secret-one", issued).Sign(tokenTestProposal(t, 4), ProposalDecisionSnooze)
	if err != nil {
		t.Fatal(err)
	}
	tampered := mutateToken(t, raw, func(b []byte) { b[actionTokenBodyLen] ^= 0x10 })
	_, err = tokenTestSigner("secret-one", issued.Add(48*time.Hour)).Verify(tampered)
	if !errors.Is(err, ErrActionTokenInvalid) {
		t.Fatalf("err = %v, want ErrActionTokenInvalid", err)
	}
}

func TestActionTokenMalformedInputIsRefused(t *testing.T) {
	s := tokenTestSigner("secret-one", time.Now())
	valid, err := s.Sign(tokenTestProposal(t, 5), ProposalDecisionApprove)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"empty":        "",
		"garbage":      "not a token!",
		"short":        valid[:20],
		"long":         valid + "AAAA",
		"wrong format": mutateToken(t, valid, func(b []byte) { b[0] = 2 }),
		"padded":       valid + "=",
	}
	for name, token := range cases {
		if _, err := s.Verify(token); !errors.Is(err, ErrActionTokenInvalid) {
			t.Errorf("%s: err = %v, want ErrActionTokenInvalid", name, err)
		}
		if _, ok := ActionTokenProposalID(token); ok {
			t.Errorf("%s: ActionTokenProposalID accepted a malformed token", name)
		}
	}
}

func TestActionTokenRefusesActionsThatCannotBeSigned(t *testing.T) {
	s := tokenTestSigner("secret-one", time.Now())
	// Revise needs a note, which a button cannot carry, so it is never signed.
	if _, err := s.Sign(tokenTestProposal(t, 6), ProposalDecisionRevise); err == nil {
		t.Fatal("revise must not be signable")
	}
	if _, err := s.Sign(pgtype.UUID{}, ProposalDecisionApprove); err == nil {
		t.Fatal("a token needs a proposal id")
	}
}

func TestActionTokenProposalIDGroupsButDoesNotVerify(t *testing.T) {
	id := tokenTestProposal(t, 7)
	raw, err := tokenTestSigner("secret-one", time.Now()).Sign(id, ProposalDecisionReject)
	if err != nil {
		t.Fatal(err)
	}
	// A reader without the key still reads the proposal id to group the buttons
	// of one decision; the press itself is verified before anything changes.
	got, ok := ActionTokenProposalID(raw)
	if !ok || got == "" {
		t.Fatalf("ActionTokenProposalID = %q, %v", got, ok)
	}
	if _, err := tokenTestSigner("secret-two", time.Now()).Verify(raw); !errors.Is(err, ErrActionTokenInvalid) {
		t.Fatalf("the grouping read must not stand in for verification: %v", err)
	}
}

func mustDecode(t *testing.T, raw string) []byte {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustSign(t *testing.T, s *ActionSigner, id pgtype.UUID, action string) string {
	t.Helper()
	raw, err := s.Sign(id, action)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
