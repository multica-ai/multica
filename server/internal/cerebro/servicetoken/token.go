package servicetoken

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateServiceToken creates a new service token: "msv_" + 40 random hex
// chars, mirroring auth.GeneratePATToken / GenerateAgentTaskToken. Only the
// SHA-256 hash of the returned string is ever persisted (see auth.HashToken);
// the raw value is shown to the caller exactly once.
func GenerateServiceToken() (string, error) {
	b := make([]byte, 20) // 20 bytes = 40 hex chars
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate service token: %w", err)
	}
	return Prefix + hex.EncodeToString(b), nil
}

// tokenPrefix returns the stored, non-secret display prefix for a raw token
// (e.g. "msv_1a2b3c4d5e"), matching the PAT handler's 12-char convention.
func tokenPrefix(raw string) string {
	if len(raw) > 12 {
		return raw[:12]
	}
	return raw
}
