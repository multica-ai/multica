package main

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/tagaccess"
	"github.com/redis/go-redis/v9"
)

type tagProjectionAuthentication struct {
	keyID      string
	key        []byte
	configured bool
}

func tagProjectionAuthenticationFromEnv() (tagProjectionAuthentication, error) {
	keyID := strings.TrimSpace(os.Getenv("TAG_PROJECTION_HMAC_KEY_ID"))
	encodedKey := strings.TrimSpace(os.Getenv("TAG_PROJECTION_HMAC_KEY_BASE64"))
	if keyID == "" && encodedKey == "" {
		return tagProjectionAuthentication{}, nil
	}
	if keyID == "" || encodedKey == "" {
		return tagProjectionAuthentication{}, errors.New("Tag projection ingress authentication configuration is incomplete")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(encodedKey)
	if err != nil || len(key) < sha256.Size {
		return tagProjectionAuthentication{}, errors.New("Tag projection ingress authentication key is invalid")
	}
	return tagProjectionAuthentication{keyID: keyID, key: key, configured: true}, nil
}

func tagProjectionAccessFromEnv(db *pgxpool.Pool, closePort tagaccess.ConnectionClosePort) (*tagaccess.AuthenticatedAccess, error) {
	authentication, err := tagProjectionAuthenticationFromEnv()
	if err != nil {
		return nil, err
	}
	if !authentication.configured {
		return nil, nil
	}
	if closePort == nil {
		return nil, errors.New("Tag projection ingress requires the realtime connection-close port")
	}
	return tagaccess.NewAuthenticatedAccess(
		tagaccess.NewPostgresStore(db),
		tagaccess.SystemClock{},
		map[string][]byte{authentication.keyID: authentication.key},
		closePort,
	)
}

func tagHTTPAssertionVerifierFromEnv() (*tagaccess.HTTPAssertionVerifier, error) {
	return tagGatewayAssertionVerifierFromEnv(false)
}

func tagWebSocketAssertionVerifierFromEnv() (*tagaccess.HTTPAssertionVerifier, error) {
	return tagGatewayAssertionVerifierFromEnv(true)
}

func tagGatewayAssertionVerifierFromEnv(webSocket bool) (*tagaccess.HTTPAssertionVerifier, error) {
	keyID := strings.TrimSpace(os.Getenv("TAG_GATEWAY_ASSERTION_HMAC_KEY_ID"))
	encodedKey := strings.TrimSpace(os.Getenv("TAG_GATEWAY_ASSERTION_HMAC_KEY_BASE64"))
	if keyID == "" && encodedKey == "" {
		return nil, nil
	}
	if keyID == "" || encodedKey == "" {
		return nil, errors.New("Tag Gateway assertion authentication configuration is incomplete")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(encodedKey)
	if err != nil || len(key) < sha256.Size {
		return nil, errors.New("Tag Gateway assertion authentication key is invalid")
	}
	if webSocket {
		return tagaccess.NewWebSocketAssertionVerifier(map[string][]byte{keyID: key}, tagaccess.SystemClock{})
	}
	return tagaccess.NewHTTPAssertionVerifier(map[string][]byte{keyID: key}, tagaccess.SystemClock{})
}

// tagRevocationRedisOptionsFromEnv composes revocation onto the existing relay
// endpoint by default. A non-empty URL is an optional credential/topology
// hardening override; malformed overrides fail closed.
func tagRevocationRedisOptionsFromEnv(relay *redis.Options) (*redis.Options, bool, error) {
	raw := strings.TrimSpace(os.Getenv("TAG_REVOCATION_REDIS_URL"))
	if raw == "" {
		if relay == nil {
			return nil, false, errors.New("Tag realtime revocation requires the existing realtime relay Redis")
		}
		return relay, false, nil
	}
	opts, err := redis.ParseURL(raw)
	if err != nil {
		return nil, false, errors.New("Tag realtime revocation Redis override is invalid")
	}
	return opts, true, nil
}
