// CEREBRO-PATCH(daemon-claude-oauth-usage): FIR-3118 exact account usage
// windows for the codex provider. Sibling of
// cerebro_account_claude_usage.go — after each task run on a "codex"
// runtime the daemon calls the ChatGPT usage endpoint with the access token
// from the Codex CLI's auth.json and reports the primary (5h) / secondary
// (weekly) window utilization through the same
// /api/daemon/accounts/{id}/usage path. Net-new fork file in the upstream
// daemon package — response/credential parsing lives in
// server/internal/cerebro/account (codex_usage.go).
package daemon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	cerebroaccount "github.com/multica-ai/multica/server/internal/cerebro/account"
)

const codexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// fetchCodexUsageSnapshot resolves the Codex CLI's ChatGPT access token from
// auth.json ($CODEX_HOME/auth.json, default ~/.codex/auth.json — the same
// resolution detectCodexIdentity uses) and calls the usage endpoint. Returns
// ErrOAuthTokenUnavailable when no token is on disk; the Codex CLI owns the
// refresh flow, so a stale token simply surfaces as a non-200 here.
func fetchCodexUsageSnapshot(ctx context.Context) (cerebroaccount.OAuthUsageSnapshot, error) {
	data, err := readCodexAuthFile()
	if err != nil {
		return cerebroaccount.OAuthUsageSnapshot{}, cerebroaccount.ErrOAuthTokenUnavailable
	}
	token, accountID, err := cerebroaccount.ParseCodexCredentials(data)
	if err != nil {
		return cerebroaccount.OAuthUsageSnapshot{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, codexUsageURL, nil)
	if err != nil {
		return cerebroaccount.OAuthUsageSnapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-Id", accountID)
	}

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
		return cerebroaccount.OAuthUsageSnapshot{}, fmt.Errorf("codex usage endpoint returned %d", resp.StatusCode)
	}
	return cerebroaccount.ParseCodexUsageResponse(body, time.Now())
}

func readCodexAuthFile() ([]byte, error) {
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		codexHome = filepath.Join(home, ".codex")
	}
	return os.ReadFile(filepath.Join(codexHome, "auth.json"))
}
