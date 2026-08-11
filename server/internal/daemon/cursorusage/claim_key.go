package cursorusage

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// OpaqueClaimKey returns a deterministic SHA-256 hex digest of raw.
// Daemon-local Cursor account ids and Dashboard occurrence fingerprints are
// hashed before upload/persist so the server can dedupe without retaining
// reverse-engineerable Cursor identity or event payloads.
func OpaqueClaimKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
