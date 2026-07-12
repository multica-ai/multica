// CEREBRO-PATCH(daemon-claude-oauth-usage): FIR-3118 exact account usage
// windows. After each task run on a "claude" runtime the daemon calls
// Claude's OAuth usage endpoint (the same source usagepal reads) with the
// Claude Code access token from the local credential store, and reports the
// 5-hour / 7-day window utilization + reset times to
// /api/daemon/accounts/{id}/usage. Net-new fork file in the upstream daemon
// package — response/credential parsing lives in
// server/internal/cerebro/account (oauth_usage.go); this file only carries
// credential resolution and HTTP plumbing.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	cerebroaccount "github.com/multica-ai/multica/server/internal/cerebro/account"
)

const (
	claudeOAuthUsageURL = "https://api.anthropic.com/api/oauth/usage"
	// claudeOAuthUsageMinInterval throttles endpoint calls per account so a
	// burst of task completions doesn't hammer the provider — usage barely
	// moves in under a minute anyway. Shared by all provider fetchers.
	claudeOAuthUsageMinInterval = time.Minute
)

var claudeOAuthUsageHTTP = &http.Client{Timeout: 10 * time.Second}

// claudeOAuthUsageLastFetch tracks the last successful-or-failed fetch per
// account ID for the throttle above.
var claudeOAuthUsageLastFetch sync.Map // accountID string -> time.Time

// maybeReportProviderUsageWindows fetches the exact usage windows for the
// account behind the given runtime and reports them to the server.
// Best-effort on every path: missing credentials, keychain denial, network
// errors and non-200s are logged at debug/warn and never block the
// task-completion path. No-op for providers without a usage source
// (claude → OAuth usage endpoint; codex → ChatGPT wham/usage endpoint,
// see cerebro_account_codex_usage.go).
func (d *Daemon) maybeReportProviderUsageWindows(ctx context.Context, runtimeID, accountID string, log *slog.Logger) {
	rt := d.findRuntime(runtimeID)
	if rt == nil {
		return
	}
	provider := strings.ToLower(strings.TrimSpace(rt.Provider))
	var fetch func(context.Context) (cerebroaccount.OAuthUsageSnapshot, error)
	switch provider {
	case "claude":
		fetch = fetchClaudeUsageSnapshot
	case "codex":
		fetch = fetchCodexUsageSnapshot
	default:
		return
	}
	if last, ok := claudeOAuthUsageLastFetch.Load(accountID); ok {
		if t, ok := last.(time.Time); ok && time.Since(t) < claudeOAuthUsageMinInterval {
			return
		}
	}
	claudeOAuthUsageLastFetch.Store(accountID, time.Now())

	snap, err := fetch(ctx)
	if err != nil {
		// Missing/stale credentials are expected on machines that use an
		// API key or are not logged in — debug. Real fetch failures warn.
		if errors.Is(err, cerebroaccount.ErrOAuthTokenUnavailable) {
			log.Debug("provider usage token unavailable",
				"provider", provider, "runtime_id", runtimeID, "error", err)
		} else {
			log.Warn("fetch provider usage failed",
				"provider", provider, "runtime_id", runtimeID, "error", err)
		}
		return
	}
	if !snap.HasSignal() {
		return
	}
	if err := d.client.ReportAccountUsageWindows(ctx, accountID, snap); err != nil {
		log.Warn("report provider usage failed",
			"provider", provider, "account_id", accountID, "runtime_id", runtimeID, "error", err)
	}
}

// fetchClaudeUsageSnapshot resolves the Claude Code OAuth token and calls
// the OAuth usage endpoint.
func fetchClaudeUsageSnapshot(ctx context.Context) (cerebroaccount.OAuthUsageSnapshot, error) {
	token, err := readClaudeOAuthToken(time.Now())
	if err != nil {
		return cerebroaccount.OAuthUsageSnapshot{}, err
	}
	return fetchClaudeOAuthUsage(ctx, token)
}

// fetchClaudeOAuthUsage calls the OAuth usage endpoint with the given
// access token and parses the window utilization out of the response.
func fetchClaudeOAuthUsage(ctx context.Context, token string) (cerebroaccount.OAuthUsageSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeOAuthUsageURL, nil)
	if err != nil {
		return cerebroaccount.OAuthUsageSnapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := claudeOAuthUsageHTTP.Do(req)
	if err != nil {
		return cerebroaccount.OAuthUsageSnapshot{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return cerebroaccount.OAuthUsageSnapshot{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return cerebroaccount.OAuthUsageSnapshot{}, fmt.Errorf("oauth usage endpoint returned %d", resp.StatusCode)
	}
	return cerebroaccount.ParseOAuthUsageResponse(body)
}

// readClaudeOAuthToken resolves the Claude Code OAuth access token from the
// local credential store, in the same order Claude Code itself uses:
//
//  1. $CLAUDE_CONFIG_DIR/.credentials.json (or ~/.claude/.credentials.json)
//  2. macOS keychain item "Claude Code-credentials" (darwin only)
//
// Both sources hold the same JSON document; parsing (incl. the expiry
// check) is owned by cerebroaccount.ParseClaudeCredentials.
func readClaudeOAuthToken(now time.Time) (string, error) {
	if data, err := readClaudeCredentialsFile(); err == nil {
		if token, perr := cerebroaccount.ParseClaudeCredentials(data, now); perr == nil {
			return token, nil
		}
	}
	if runtime.GOOS == "darwin" {
		data, err := readClaudeCredentialsKeychain()
		if err != nil {
			return "", err
		}
		return cerebroaccount.ParseClaudeCredentials(data, now)
	}
	return "", cerebroaccount.ErrOAuthTokenUnavailable
}

func readClaudeCredentialsFile() ([]byte, error) {
	dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".claude")
	}
	return os.ReadFile(filepath.Join(dir, ".credentials.json"))
}

// readClaudeCredentialsKeychain shells out to `security` for the Claude Code
// keychain item. The daemon runs as the logged-in user outside the agent
// sandbox, so this is an ordinary keychain read of an item the user's own
// tooling wrote (see providerKeychainItems in sandbox.go for the
// per-provider contract). A 5s timeout guards against a blocking keychain
// UI prompt on hosts where access was not previously granted.
func readClaudeCredentialsKeychain() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
	if err != nil {
		return nil, errors.Join(cerebroaccount.ErrOAuthTokenUnavailable, err)
	}
	return []byte(strings.TrimSpace(string(out))), nil
}
