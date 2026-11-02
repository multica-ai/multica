package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// Signed action tokens (spec 2.7, plan 9.11). A chat button carries one of these
// as its callback data: it names a proposal, an action and an expiry, and is
// signed with the server's key, so a press can be trusted without a session.
//
// Wire format, 38 bytes, base64url without padding (51 characters, inside
// Telegram's 64-byte callback_data limit):
//
//	[0]     format version (actionTokenVersion)
//	[1]     action code (approve, reject or snooze)
//	[2:18]  proposal id
//	[18:22] expiry, Unix seconds, big-endian
//	[22:38] HMAC-SHA256 over bytes [0:22], truncated to 128 bits
//
// Only the three decisions a button may carry are signable. Revise asks for a
// note, which a button cannot carry, so it is a link into the app instead.
const (
	actionTokenVersion  = 1
	actionTokenBodyLen  = 22
	actionTokenMACLen   = 16
	actionTokenLen      = actionTokenBodyLen + actionTokenMACLen
	actionTokenKeyLabel = "multica channel action token v1"
	// ActionTokenTTL is how long a button stays valid after the message is built.
	ActionTokenTTL = 24 * time.Hour
)

// ActionSnoozeDays is the fixed snooze a chat button applies.
const ActionSnoozeDays = 7

var (
	// ErrActionTokenInvalid: the token is malformed, or its signature does not
	// match. Nothing in it is trusted.
	ErrActionTokenInvalid = errors.New("action token is not valid")
	// ErrActionTokenExpired: the signature is good but the expiry has passed.
	ErrActionTokenExpired = errors.New("action token has expired")
)

var actionCodes = map[string]byte{
	ProposalDecisionApprove: 1,
	ProposalDecisionReject:  2,
	ProposalDecisionSnooze:  3,
}

// SignedAction is what a verified token says.
type SignedAction struct {
	ProposalID pgtype.UUID
	Action     string
	ExpiresAt  time.Time
}

// ActionSigner issues and verifies action tokens. The signing key is derived
// from the server secret (the JWT secret), with a fixed label, so the same
// secret is never used as a raw HMAC key for another purpose.
type ActionSigner struct {
	key []byte
	now func() time.Time
}

// NewActionSigner builds a signer from the server secret. An empty secret
// yields a signer whose tokens verify only against the same empty secret, so
// callers pass the configured secret, never a placeholder.
func NewActionSigner(secret []byte) *ActionSigner {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(actionTokenKeyLabel))
	return &ActionSigner{key: mac.Sum(nil), now: time.Now}
}

// Sign returns a token for the action on the proposal, valid for ActionTokenTTL
// from now. It refuses an action that is not signable.
func (s *ActionSigner) Sign(proposalID pgtype.UUID, action string) (string, error) {
	code, ok := actionCodes[action]
	if !ok {
		return "", errors.New("action token: action cannot be signed: " + action)
	}
	if !proposalID.Valid {
		return "", errors.New("action token: proposal id is required")
	}
	var buf [actionTokenLen]byte
	buf[0] = actionTokenVersion
	buf[1] = code
	copy(buf[2:18], proposalID.Bytes[:])
	expiry := s.now().Add(ActionTokenTTL).Unix()
	binary.BigEndian.PutUint32(buf[18:22], uint32(expiry))
	copy(buf[actionTokenBodyLen:], s.mac(buf[:actionTokenBodyLen]))
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// Verify checks the token's shape and signature, then its expiry. A tampered
// token is ErrActionTokenInvalid even when it is also past its expiry, so a
// forged expiry is never reported as a real one.
func (s *ActionSigner) Verify(raw string) (SignedAction, error) {
	buf, err := decodeActionToken(raw)
	if err != nil {
		return SignedAction{}, err
	}
	if !hmac.Equal(buf[actionTokenBodyLen:], s.mac(buf[:actionTokenBodyLen])) {
		return SignedAction{}, ErrActionTokenInvalid
	}
	action, ok := actionName(buf[1])
	if !ok {
		return SignedAction{}, ErrActionTokenInvalid
	}
	var id pgtype.UUID
	copy(id.Bytes[:], buf[2:18])
	id.Valid = true
	expiresAt := time.Unix(int64(binary.BigEndian.Uint32(buf[18:22])), 0)
	if !s.now().Before(expiresAt) {
		return SignedAction{ProposalID: id, Action: action, ExpiresAt: expiresAt}, ErrActionTokenExpired
	}
	return SignedAction{ProposalID: id, Action: action, ExpiresAt: expiresAt}, nil
}

// ActionTokenProposalID reads the proposal id out of a token without checking
// its signature. It only groups buttons on a message; every action goes through
// Verify first. The second result is false for anything that is not a token.
func ActionTokenProposalID(raw string) (string, bool) {
	buf, err := decodeActionToken(raw)
	if err != nil {
		return "", false
	}
	var id pgtype.UUID
	copy(id.Bytes[:], buf[2:18])
	id.Valid = true
	return util.UUIDToString(id), true
}

func (s *ActionSigner) mac(body []byte) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write(body)
	return m.Sum(nil)[:actionTokenMACLen]
}

func decodeActionToken(raw string) ([actionTokenLen]byte, error) {
	var buf [actionTokenLen]byte
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) != actionTokenLen || data[0] != actionTokenVersion {
		return buf, ErrActionTokenInvalid
	}
	copy(buf[:], data)
	return buf, nil
}

func actionName(code byte) (string, bool) {
	for name, c := range actionCodes {
		if c == code {
			return name, true
		}
	}
	return "", false
}
