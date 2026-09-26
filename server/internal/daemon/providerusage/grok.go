package providerusage

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const (
	grokUsageURL    = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
	grokTrustedAuth = "https://auth.x.ai"
)

// GrokCollector reads the weekly Grok Build allowance from the CLI session
// in ~/.grok/auth.json. Only tokens minted by auth.x.ai are used. An expired
// token is not refreshed here.
type GrokCollector struct {
	AuthPath string
	Do       HTTPDoer
	Now      func() time.Time
}

func (c GrokCollector) Collect(ctx context.Context) Result {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	path := c.AuthPath
	if path == "" {
		path = grokAuthPath()
	}
	token, expired, found := loadGrokToken(path, now)
	if !found {
		return emptyResult(ProviderGrok, ReasonNotLoggedIn, now)
	}
	if expired {
		return Result{Upload: false}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, grokUsageURL, nil)
	if err != nil {
		return Result{Upload: false}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-XAI-Token-Auth", "xai-grok-cli")
	body, status, retryAfter, err := performVendor(c.Do, req)
	if err != nil {
		return Result{Upload: false}
	}
	reason, backoff, transient := classifyVendorStatus(status, retryAfter)
	if transient {
		return Result{Upload: false, Backoff: backoff}
	}
	if reason != "" {
		return emptyResult(ProviderGrok, reason, now)
	}
	windows, plan, ok := ParseGrokUsage(body)
	if !ok {
		return Result{Upload: false}
	}
	return Result{
		Upload: true,
		Snapshot: Snapshot{
			Provider:    ProviderGrok,
			PlanName:    plan,
			CollectedAt: now,
			Windows:     windows,
		},
	}
}

func grokAuthPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".grok", "auth.json")
}

func loadGrokToken(path string, now time.Time) (token string, expired, found bool) {
	body, err := readRegularFile(path)
	if err != nil {
		return "", false, false
	}
	root, ok := decodeObject(body)
	if !ok {
		return "", false, false
	}
	var fallback string
	fallbackExpired := false
	sawTrusted := false
	for key, value := range root {
		entry := asMap(value)
		if entry == nil || !grokTrusted(key, entry) {
			continue
		}
		raw, _ := entry["key"].(string)
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		sawTrusted = true
		expiry, hasExpiry := parseFlexibleTime(stringField(entry, "expires_at"))
		isExpired := hasExpiry && !expiry.After(now)
		if !isExpired {
			return raw, false, true
		}
		if fallback == "" {
			fallback = raw
			fallbackExpired = true
		}
	}
	if !sawTrusted {
		return "", false, false
	}
	return fallback, fallbackExpired, fallback != ""
}

func grokTrusted(key string, entry map[string]any) bool {
	issuer, _, _ := strings.Cut(key, "::")
	if issuer == grokTrustedAuth {
		return true
	}
	return stringField(entry, "oidc_issuer") == grokTrustedAuth
}

func stringField(entry map[string]any, key string) string {
	value, _ := entry[key].(string)
	return strings.TrimSpace(value)
}

// ParseGrokUsage maps the credits billing payload onto one credits window.
func ParseGrokUsage(body []byte) (windows []Window, plan string, ok bool) {
	root, parsed := decodeObject(body)
	if !parsed {
		return nil, "", false
	}
	config := asMap(root["config"])
	if config == nil {
		return nil, "", false
	}
	period := asMap(config["currentPeriod"])
	reset := parseResetValue(period["end"])
	if reset == nil {
		reset = parseResetValue(config["billingPeriodEnd"])
	}
	plan = grokProductName(config)
	if percent, hasPercent := asFloat(config["creditUsagePercent"]); hasPercent {
		value, usable := usablePercent(percent)
		if !usable {
			return nil, "", false
		}
		return []Window{{ID: "credits", PercentUsed: value, ResetsAt: reset}}, plan, true
	}
	for _, item := range asSlice(config["productUsage"]) {
		product := asMap(item)
		percent, hasPercent := asFloat(product["usagePercent"])
		if !hasPercent {
			continue
		}
		value, usable := usablePercent(percent)
		if !usable {
			continue
		}
		if plan == "" {
			plan = grokHumanize(stringField(product, "product"))
		}
		return []Window{{ID: "credits", PercentUsed: value, ResetsAt: reset}}, trimPlan(plan), true
	}
	periodType := stringField(period, "type")
	if strings.Contains(periodType, "WEEKLY") {
		return []Window{{ID: "credits", PercentUsed: 0, ResetsAt: reset}}, plan, true
	}
	return nil, "", false
}

func grokProductName(config map[string]any) string {
	products := asSlice(config["productUsage"])
	if len(products) == 0 {
		return ""
	}
	return trimPlan(grokHumanize(stringField(asMap(products[0]), "product")))
}

func grokHumanize(name string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsUpper(r) && b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
