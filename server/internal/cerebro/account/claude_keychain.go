package account

// FIR-4520: naming for the macOS keychain item Claude Code stores its OAuth
// credentials in.
//
// Claude Code looks the item up with an account name (the OS username) and a
// service name that carries a CLAUDE_CONFIG_DIR-derived suffix:
//
//	security find-generic-password -a <account> -w -s <service>
//
// Looking it up by service alone matches whichever item the keychain returns
// first. On a Mac that also carries an item written by an older Claude Code
// (account name "Claude Code", never refreshed since), that is the stale one,
// so the daemon reads an expired token forever and the account's 5h/7d usage
// windows stay empty.
//
// Both helpers mirror Claude Code's own logic exactly — a different service
// or account name silently reads the wrong item rather than failing loudly.

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// ClaudeKeychainServiceBase is the service name used when CLAUDE_CONFIG_DIR is
// unset, i.e. the default ~/.claude profile.
const ClaudeKeychainServiceBase = "Claude Code-credentials"

// ClaudeKeychainFallbackAccount is what Claude Code falls back to when the OS
// username is unavailable or carries characters it refuses to pass to
// `security`.
const ClaudeKeychainFallbackAccount = "claude-code-user"

// claudeKeychainAccountPattern is Claude Code's own allowlist for the account
// name it passes to `security`.
var claudeKeychainAccountPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// ClaudeKeychainService returns the keychain service name for a given
// CLAUDE_CONFIG_DIR value. An empty configDir means the default profile and
// carries no suffix; any other profile appends the first 8 hex characters of
// the NFC-normalized directory's SHA-256, which is how Claude Code keeps
// several logins apart on one machine.
func ClaudeKeychainService(configDir string) string {
	configDir = strings.TrimSpace(configDir)
	if configDir == "" {
		return ClaudeKeychainServiceBase
	}
	sum := sha256.Sum256([]byte(norm.NFC.String(configDir)))
	return ClaudeKeychainServiceBase + "-" + hex.EncodeToString(sum[:])[:8]
}

// ClaudeKeychainAccount returns the keychain account name for the given OS
// username.
func ClaudeKeychainAccount(username string) string {
	username = strings.TrimSpace(username)
	if !claudeKeychainAccountPattern.MatchString(username) {
		return ClaudeKeychainFallbackAccount
	}
	return username
}
