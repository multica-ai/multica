package providerusage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const codexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// CodexAuthPath resolves the Codex auth file the daemon already uses for
// task homes: $CODEX_HOME/auth.json, otherwise ~/.codex/auth.json. This
// matches execenv.resolveSharedCodexHome without exporting it.
func CodexAuthPath() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			return filepath.Join(abs, "auth.json")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "auth.json")
}

// CodexCollector reads ChatGPT plan limits with the local Codex login.
// An API-key-only auth.json is an empty snapshot, not an error.
type CodexCollector struct {
	AuthPath string
	Do       HTTPDoer
	Now      func() time.Time
}

func (c CodexCollector) Collect(ctx context.Context) Result {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	path := c.AuthPath
	if path == "" {
		path = CodexAuthPath()
	}
	token, accountID, reason, err := loadCodexChatGPTAuth(path)
	if err != nil {
		if reason == "" {
			reason = ReasonNotLoggedIn
		}
		return emptyResult(ProviderCodex, reason, now)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, codexUsageURL, nil)
	if err != nil {
		return Result{Upload: false}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache, no-store")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("ChatGPT-Account-Id", accountID)
	do := c.Do
	if do == nil {
		client := &http.Client{Timeout: vendorTimeout}
		do = client.Do
	}
	resp, err := do(req)
	if err != nil {
		return Result{Upload: false}
	}
	body, err := readVendorBody(resp)
	if err != nil {
		return Result{Upload: false}
	}
	emptyReason, backoff, transient := classifyVendorStatus(resp.StatusCode, resp.Header.Get("Retry-After"))
	if transient {
		return Result{Upload: false, Backoff: backoff}
	}
	if emptyReason != "" {
		return emptyResult(ProviderCodex, emptyReason, now)
	}
	windows, plan, ok := ParseCodexUsage(body, now)
	if !ok {
		return Result{Upload: false}
	}
	return Result{
		Upload: true,
		Snapshot: Snapshot{
			Provider:    ProviderCodex,
			PlanName:    plan,
			CollectedAt: now,
			Windows:     windows,
		},
	}
}

func loadCodexChatGPTAuth(path string) (token, accountID, reason string, err error) {
	if path == "" {
		return "", "", ReasonNotLoggedIn, fmt.Errorf("codex auth path missing")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", ReasonNotLoggedIn, err
		}
		return "", "", "", err
	}
	var doc struct {
		Tokens *struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
		APIKey string `json:"OPENAI_API_KEY"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", "", ReasonNotLoggedIn, err
	}
	if doc.Tokens == nil || doc.Tokens.AccessToken == "" || doc.Tokens.AccountID == "" {
		if doc.APIKey != "" || doc.Tokens == nil {
			return "", "", ReasonAPIKeyOnly, fmt.Errorf("codex auth has no chatgpt tokens")
		}
		return "", "", ReasonAPIKeyOnly, fmt.Errorf("codex auth has no chatgpt tokens")
	}
	return doc.Tokens.AccessToken, doc.Tokens.AccountID, "", nil
}

type codexWindow struct {
	UsedPercent       *float64 `json:"used_percent"`
	ResetAt           *int64   `json:"reset_at"`
	ResetAfterSeconds *int64   `json:"reset_after_seconds"`
}

type codexUsageBody struct {
	PlanType  string `json:"plan_type"`
	RateLimit struct {
		Primary   *codexWindow `json:"primary_window"`
		Secondary *codexWindow `json:"secondary_window"`
	} `json:"rate_limit"`
}

// ParseCodexUsage maps wham usage JSON onto primary and secondary windows.
func ParseCodexUsage(body []byte, now time.Time) (windows []Window, plan string, ok bool) {
	var parsed codexUsageBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, "", false
	}
	if parsed.RateLimit.Primary == nil && parsed.RateLimit.Secondary == nil && parsed.PlanType == "" {
		return nil, "", false
	}
	if w, ok := codexWindowOf("primary", parsed.RateLimit.Primary, now); ok {
		windows = append(windows, w)
	}
	if w, ok := codexWindowOf("secondary", parsed.RateLimit.Secondary, now); ok {
		windows = append(windows, w)
	}
	return windows, parsed.PlanType, true
}

func codexWindowOf(id string, raw *codexWindow, now time.Time) (Window, bool) {
	if raw == nil || raw.UsedPercent == nil {
		return Window{}, false
	}
	w := Window{ID: id, PercentUsed: *raw.UsedPercent}
	switch {
	case raw.ResetAt != nil && *raw.ResetAt > 0:
		t := time.Unix(*raw.ResetAt, 0).UTC()
		w.ResetsAt = &t
	case raw.ResetAfterSeconds != nil && *raw.ResetAfterSeconds >= 0:
		t := now.Add(time.Duration(*raw.ResetAfterSeconds) * time.Second).UTC()
		w.ResetsAt = &t
	}
	return w, true
}
