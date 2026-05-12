// CEREBRO-PATCH(daemon-cerebro-account): cerebro-only daemon support for the
// JEH-997 account registration flow. Net-new fork file in the upstream-zone
// path — detects each provider's login identity on disk and reports it to
// the server via POST /api/daemon/runtimes/{id}/account on register and on
// every heartbeat tick (the endpoint is idempotent).
package daemon

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DetectAccountIdentity returns the stable login identity for the given
// agent provider, if it can be derived from the local auth-state on disk.
// An empty string with a nil error means "no auth-state found yet" — the
// caller should retry later rather than treating it as a failure. The
// `source` return is the absolute path that was consulted, for log
// attribution; empty if no file was checked.
//
// Currently implemented for the cerebro daemon's two first-class providers:
//
//   - "claude" reads ~/.claude.json and pulls oauthAccount.emailAddress.
//     Falls back to oauthAccount.accountUuid when the email is missing.
//   - "codex" reads ~/.codex/auth.json (or $CODEX_HOME/auth.json) and pulls
//     the email claim out of tokens.id_token (a JWT). Falls back to
//     tokens.account_id when the id_token cannot be parsed.
//
// Other providers (copilot, opencode, cursor, …) return (empty, "", nil)
// for now; we will fill them in as we hit them. The contract is "best
// effort, retry-safe" — adding a new provider only needs to grow the switch.
func DetectAccountIdentity(provider string) (identity, source string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve home: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude":
		return detectClaudeIdentity(home)
	case "codex":
		return detectCodexIdentity(home)
	default:
		return "", "", nil
	}
}

// claudeConfig is the subset of ~/.claude.json we read. Field names match
// the on-disk JSON exactly — Anthropic's CLI writes camelCase keys here.
type claudeConfig struct {
	OAuthAccount struct {
		EmailAddress string `json:"emailAddress"`
		AccountUUID  string `json:"accountUuid"`
	} `json:"oauthAccount"`
}

func detectClaudeIdentity(home string) (string, string, error) {
	path := filepath.Join(home, ".claude.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", path, nil
		}
		return "", path, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg claudeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", path, fmt.Errorf("parse %s: %w", path, err)
	}
	if id := strings.TrimSpace(cfg.OAuthAccount.EmailAddress); id != "" {
		return id, path, nil
	}
	if id := strings.TrimSpace(cfg.OAuthAccount.AccountUUID); id != "" {
		return id, path, nil
	}
	return "", path, nil
}

// codexAuth mirrors ~/.codex/auth.json. The id_token is a JWT whose payload
// carries the human-readable email; account_id is the stable backend
// identifier when no JWT is present.
type codexAuth struct {
	Tokens struct {
		IDToken   string `json:"id_token"`
		AccountID string `json:"account_id"`
	} `json:"tokens"`
}

func detectCodexIdentity(home string) (string, string, error) {
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	path := filepath.Join(codexHome, "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", path, nil
		}
		return "", path, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg codexAuth
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", path, fmt.Errorf("parse %s: %w", path, err)
	}
	if email := emailFromJWT(cfg.Tokens.IDToken); email != "" {
		return email, path, nil
	}
	if id := strings.TrimSpace(cfg.Tokens.AccountID); id != "" {
		return id, path, nil
	}
	return "", path, nil
}

// emailFromJWT extracts the "email" (or "preferred_username") claim from a
// JWT without verifying its signature — we only need the identity claim for
// display, not for trust decisions. Returns empty string if the token is
// malformed or has neither claim.
func emailFromJWT(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some issuers emit standard (padded) base64; tolerate either.
		payload, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	for _, key := range []string{"email", "preferred_username", "sub"} {
		if v, ok := claims[key].(string); ok {
			if id := strings.TrimSpace(v); id != "" {
				return id
			}
		}
	}
	return ""
}
