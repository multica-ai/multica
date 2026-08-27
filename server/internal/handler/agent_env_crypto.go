package handler

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// envEncKeyEnv is the server environment variable that carries the
// AES-256 key used to encrypt agent custom_env at rest (GAP-10, Phase 1).
// It must decode to exactly 32 bytes, supplied as either a 64-char hex
// string or a base64 string (44 chars for 32 raw bytes). When unset the
// code degrades safely to storing/returning plaintext rather than refusing
// to serve — see encryptCustomEnv / decryptCustomEnv.
const envEncKeyEnv = "MULTICA_ENV_ENC_KEY"

// envEnvelope is the JSON envelope stored in the custom_env JSONB column
// when encryption is active. Existing plaintext rows — and the empty `{}`
// placeholder — carry no "enc" marker and are returned as-is on read, so
// no migration is required and old rows keep working.
type envEnvelope struct {
	Enc string `json:"enc"` // scheme tag; "v1" = AES-256-GCM
	N   string `json:"n"`   // base64 nonce
	C   string `json:"c"`   // base64 ciphertext (GCM tag appended)
}

// envSchemeV1 is the only scheme the reader accepts. An unknown scheme is
// treated as a non-envelope (plaintext) value so a future reader can still
// read today's plaintext rows.
const envSchemeV1 = "v1"

// envKeyOnce guards the one-time warning emitted when encryption is
// requested but no key is configured, so a mis-set deployment logs the
// degradation exactly once instead of per request.
var envKeyOnce sync.Once

// loadEnvEncKey resolves the AEAD key from MULTICA_ENV_ENC_KEY. It accepts
// either a 32-byte hex string or a 32-byte base64 string. It returns
// (nil, nil) when the variable is unset — callers then degrade to plaintext.
func loadEnvEncKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(envEncKeyEnv))
	if raw == "" {
		return nil, nil
	}
	if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, fmt.Errorf("%s must decode to exactly 32 bytes (hex or base64); got %q", envEncKeyEnv, raw)
}

// encryptCustomEnv wraps plaintext (a JSON-encoded custom_env map) in the
// v1 envelope when a key is configured, returning the envelope JSON. When
// the key is unset it logs a one-time warning and returns the plaintext
// unchanged so the system keeps running — secrets just aren't encrypted at
// rest, a clear, auditable degradation rather than a hard failure.
func encryptCustomEnv(plaintext []byte) ([]byte, error) {
	key, err := loadEnvEncKey()
	if err != nil {
		return nil, err
	}
	if key == nil {
		envKeyOnce.Do(func() {
			slog.Warn("MULTICA_ENV_ENC_KEY is unset; agent custom_env will be stored in plaintext (encryption-at-rest disabled)",
				"help", "set "+envEncKeyEnv+" to a 32-byte hex or base64 value")
		})
		return plaintext, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("agent env: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("agent env: new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("agent env: read nonce: %w", err)
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	env := envEnvelope{
		Enc: envSchemeV1,
		N:   base64.StdEncoding.EncodeToString(nonce),
		C:   base64.StdEncoding.EncodeToString(ct),
	}
	return json.Marshal(env)
}

// decryptCustomEnv reverses encryptCustomEnv. If the stored bytes are a v1
// envelope they are decrypted; otherwise (a plaintext map, empty `{}`, or
// any value lacking the "enc":"v1" marker) they are returned verbatim so
// both pre-encryption rows and the key-unset degradation path keep working.
func decryptCustomEnv(stored []byte) ([]byte, error) {
	if len(stored) == 0 {
		return stored, nil
	}
	var env envEnvelope
	if err := json.Unmarshal(stored, &env); err == nil && env.Enc == envSchemeV1 {
		key, err := loadEnvEncKey()
		if err != nil {
			return nil, err
		}
		if key == nil {
			return nil, fmt.Errorf("agent env: custom_env is encrypted but %s is unset; cannot decrypt", envEncKeyEnv)
		}
		nonce, err := base64.StdEncoding.DecodeString(env.N)
		if err != nil {
			return nil, fmt.Errorf("agent env: decode nonce: %w", err)
		}
		ct, err := base64.StdEncoding.DecodeString(env.C)
		if err != nil {
			return nil, fmt.Errorf("agent env: decode ciphertext: %w", err)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, fmt.Errorf("agent env: new cipher: %w", err)
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("agent env: new gcm: %w", err)
		}
		pt, err := gcm.Open(nil, nonce, ct, nil)
		if err != nil {
			return nil, fmt.Errorf("agent env: decrypt (wrong key or corrupted ciphertext): %w", err)
		}
		return pt, nil
	}
	return stored, nil
}
