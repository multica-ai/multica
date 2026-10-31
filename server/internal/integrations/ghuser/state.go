package ghuser

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// StateTTL bounds how long a connect link stays usable.
const StateTTL = 10 * time.Minute

// ErrInvalidState covers a malformed, tampered, or expired state. Callers do
// not learn which check failed.
var ErrInvalidState = errors.New("ghuser: invalid oauth state")

// State is what the OAuth callback recovers from the signed state parameter.
type State struct {
	UserID   string
	ReturnTo string
}

type stateClaims struct {
	UserID   string `json:"u"`
	ReturnTo string `json:"r"`
	Exp      int64  `json:"e"`
	Nonce    string `json:"n"`
}

// SignState binds the user and the return target into a signed, expiring
// token: "<base64url payload>.<base64url HMAC-SHA256>".
func (s *Service) SignState(userID, returnTo string) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	raw, err := json.Marshal(stateClaims{
		UserID:   userID,
		ReturnTo: returnTo,
		Exp:      s.now().Add(StateTTL).Unix(),
		Nonce:    hex.EncodeToString(nonce),
	})
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + s.signStatePayload(payload), nil
}

// VerifyState checks the signature before trusting the payload, then the
// expiry.
func (s *Service) VerifyState(token string) (State, error) {
	if !s.Configured() {
		return State{}, ErrNotConfigured
	}
	payload, sig, ok := strings.Cut(token, ".")
	if !ok || payload == "" || sig == "" {
		return State{}, ErrInvalidState
	}
	if !hmac.Equal([]byte(sig), []byte(s.signStatePayload(payload))) {
		return State{}, ErrInvalidState
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return State{}, ErrInvalidState
	}
	var claims stateClaims
	if err := json.Unmarshal(raw, &claims); err != nil || claims.UserID == "" {
		return State{}, ErrInvalidState
	}
	if s.now().Unix() > claims.Exp {
		return State{}, ErrInvalidState
	}
	return State{UserID: claims.UserID, ReturnTo: claims.ReturnTo}, nil
}

func (s *Service) signStatePayload(payload string) string {
	mac := hmac.New(sha256.New, s.stateKey)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
