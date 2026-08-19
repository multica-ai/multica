package main

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/tagaccess"
)

func tagProjectionAccessFromEnv(db *pgxpool.Pool) (*tagaccess.AuthenticatedAccess, error) {
	keyID := strings.TrimSpace(os.Getenv("TAG_PROJECTION_HMAC_KEY_ID"))
	encodedKey := strings.TrimSpace(os.Getenv("TAG_PROJECTION_HMAC_KEY_BASE64"))
	if keyID == "" && encodedKey == "" {
		return nil, nil
	}
	if keyID == "" || encodedKey == "" {
		return nil, errors.New("Tag projection ingress authentication configuration is incomplete")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(encodedKey)
	if err != nil || len(key) < sha256.Size {
		return nil, errors.New("Tag projection ingress authentication key is invalid")
	}
	return tagaccess.NewAuthenticatedAccess(
		tagaccess.NewPostgresStore(db),
		tagaccess.SystemClock{},
		map[string][]byte{keyID: key},
		nil,
	)
}
