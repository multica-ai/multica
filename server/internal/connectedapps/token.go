package connectedapps

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Gateway token errors. The gateway answers all of them with the same 401.
var (
	ErrTokenMalformed = errors.New("connectedapps: token malformed")
	ErrTokenSignature = errors.New("connectedapps: token signature mismatch")
	ErrTokenExpired   = errors.New("connectedapps: token expired")
)

const tokenPrefix = "mca_"

type tokenClaims struct {
	TaskID string `json:"t"`
	Exp    int64  `json:"e"`
}

// DeriveTokenSecret derives the gateway signing key from a service secret, so
// a gateway token can never verify as a connect state or the other way round.
func DeriveTokenSecret(secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("multica-connected-apps-gateway-v1"))
	return mac.Sum(nil)
}

// SignTaskToken mints the bearer token a run presents to the gateway. It names
// only the task; everything else (agent, workspace, who triggered it) is read
// from the task row on each call, so the token is useless once the run ends.
func SignTaskToken(secret []byte, taskID string, exp time.Time) (string, error) {
	raw, err := json.Marshal(tokenClaims{TaskID: taskID, Exp: exp.Unix()})
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return tokenPrefix + payload + "." + signTokenPayload(secret, payload), nil
}

// VerifyTaskToken checks a gateway token and returns its task id.
func VerifyTaskToken(secret []byte, token string, now time.Time) (string, error) {
	body, ok := strings.CutPrefix(strings.TrimSpace(token), tokenPrefix)
	if !ok {
		return "", ErrTokenMalformed
	}
	payload, sig, found := strings.Cut(body, ".")
	if !found || payload == "" || sig == "" {
		return "", ErrTokenMalformed
	}
	if !hmac.Equal([]byte(sig), []byte(signTokenPayload(secret, payload))) {
		return "", ErrTokenSignature
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", ErrTokenMalformed
	}
	var claims tokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil || claims.TaskID == "" {
		return "", ErrTokenMalformed
	}
	if now.Unix() > claims.Exp {
		return "", ErrTokenExpired
	}
	return claims.TaskID, nil
}

func signTokenPayload(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
