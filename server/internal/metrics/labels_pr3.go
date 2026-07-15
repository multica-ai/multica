package metrics

import (
	"encoding/json"
	"strings"
)

// PR3 normalizers. All inputs go through fixed allow-lists so a misbehaving
// caller cannot inflate metric cardinality. Every "unknown" / "other" bucket
// keeps the series count bounded even under enum drift.

var (
	knownPlatforms = map[string]string{
		"server":  "server",
		"web":     "web",
		"desktop": "desktop",
		"cli":     "cli",
		"mobile":  "mobile",
		"ios":     "ios",
		"unknown": "unknown",
	}

	// knownSignupSources is the fixed bucket set for the signup_source
	// metric label. The PostHog event still ships the raw cookie value
	// so analytics keeps the long tail; the Prometheus side gets the
	// bucketed version so cardinality stays bounded even if a misbehaving
	// frontend writes a unique-per-visitor cookie. Empty cookie collapses
	// to "direct" (no attribution = direct visit), unknown channels to
	// "other".
	knownSignupSources = map[string]string{
		"direct":       "direct",
		"google":       "google",
		"bing":         "bing",
		"duckduckgo":   "duckduckgo",
		"twitter":      "twitter",
		"x":            "twitter",
		"linkedin":     "linkedin",
		"facebook":     "facebook",
		"instagram":    "instagram",
		"github":       "github",
		"gitlab":       "gitlab",
		"hacker_news":  "hacker_news",
		"hackernews":   "hacker_news",
		"reddit":       "reddit",
		"youtube":      "youtube",
		"discord":      "discord",
		"slack":        "slack",
		"product_hunt": "product_hunt",
		"producthunt":  "product_hunt",
		"medium":       "medium",
		"dev_to":       "dev_to",
		"devto":        "dev_to",
		"email":        "email",
		"newsletter":   "email",
		"organic":      "organic",
		"referral":     "referral",
		"partner":      "partner",
		"affiliate":    "affiliate",
		"ad":           "paid",
		"ads":          "paid",
		"paid":         "paid",
		"cpc":          "paid",
		"sem":          "paid",
		"other":        "other",
	}

	knownOnboardingPaths = map[string]string{
		"full":            "full",
		"runtime_skipped": "runtime_skipped",
		"cloud_waitlist":  "cloud_waitlist",
		"skip_existing":   "skip_existing",
		"invite_accept":   "invite_accept",
		"unknown":         "unknown",
	}

	knownAutopilotCadences = map[string]string{
		"hourly":  "hourly",
		"daily":   "daily",
		"weekly":  "weekly",
		"monthly": "monthly",
		"manual":  "manual",
		"webhook": "webhook",
		"unknown": "unknown",
	}

	knownAutopilotTriggers = map[string]string{
		"schedule": "schedule",
		"webhook":  "webhook",
		"manual":   "manual",
		"unknown":  "unknown",
	}

	knownAutopilotSkipReasons = map[string]string{
		"already_running":   "already_running",
		"recent_run":        "recent_run",
		"runtime_offline":   "runtime_offline",
		"throttled":         "throttled",
		"max_concurrency":   "max_concurrency",
		"trigger_disabled":  "trigger_disabled",
		"signature_invalid": "signature_invalid",
		"unknown":           "unknown",
		"other":             "other",
	}

	knownWebhookProviders = map[string]string{
		"github":  "github",
		"generic": "generic",
		"gitlab":  "gitlab",
		"stripe":  "stripe",
		"other":   "other",
	}

	knownWebhookDeliveryStatuses = map[string]string{
		"queued":     "queued",
		"dispatched": "dispatched",
		"failed":     "failed",
		"rejected":   "rejected",
		"ignored":    "ignored",
		"duplicate":  "duplicate",
		"other":      "other",
	}

	knownWebhookRateLimitGates = map[string]string{
		"absolute_ip":       "absolute_ip",
		"bad_credential_ip": "bad_credential_ip",
		"worker_trigger":    "worker_trigger",
		"other":             "other",
	}

	knownGithubEventKinds = map[string]string{
		"pull_request":              "pull_request",
		"pull_request_review":       "pull_request_review",
		"issues":                    "issues",
		"issue_comment":             "issue_comment",
		"push":                      "push",
		"installation":              "installation",
		"installation_repositories": "installation_repositories",
		"check_run":                 "check_run",
		"check_suite":               "check_suite",
		"ping":                      "ping",
		"other":                     "other",
	}

	knownGithubActions = map[string]string{
		"opened":      "opened",
		"closed":      "closed",
		"reopened":    "reopened",
		"merged":      "merged",
		"synchronize": "synchronize",
		"edited":      "edited",
		"submitted":   "submitted",
		"created":     "created",
		"deleted":     "deleted",
		"labeled":     "labeled",
		"unlabeled":   "unlabeled",
		"assigned":    "assigned",
		"unassigned":  "unassigned",
		"requested":   "requested",
		"completed":   "completed",
		"none":        "none",
		"other":       "other",
	}

	knownGithubPRReviewResults = map[string]string{
		"approved":          "approved",
		"changes_requested": "changes_requested",
		"commented":         "commented",
		"dismissed":         "dismissed",
		"other":             "other",
	}

	knownCloudRuntimeOps = map[string]string{
		"provision": "provision",
		"terminate": "terminate",
		"status":    "status",
		"gateway":   "gateway",
		"billing":   "billing",
		"fleet":     "fleet",
		"other":     "other",
	}

	knownCloudRuntimeStatuses = map[string]string{
		"ok":      "ok",
		"4xx":     "4xx",
		"5xx":     "5xx",
		"timeout": "timeout",
		"error":   "error",
	}

	knownDaemonWSKinds = map[string]string{
		"heartbeat":     "heartbeat",
		"task_claim":    "task_claim",
		"task_complete": "task_complete",
		"task_usage":    "task_usage",
		"task_progress": "task_progress",
		"task_messages": "task_messages",
		"log":           "log",
		"other":         "other",
	}

	knownFeedbackKinds = map[string]string{
		"bug":     "bug",
		"feature": "feature",
		"general": "general",
		"praise":  "praise",
		"other":   "other",
	}

	knownContactSalesSources = map[string]string{
		"page":        "page",
		"onboarding":  "onboarding",
		"agents_page": "agents_page",
		"unknown":     "unknown",
		"other":       "other",
	}
)

func normalizeFromAllowList(value string, allowList map[string]string, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := allowList[value]; ok {
		return normalized
	}
	return fallback
}

func NormalizePlatform(value string) string {
	return normalizeFromAllowList(value, knownPlatforms, "unknown")
}

// NormalizeSignupSource buckets the raw multica_signup_source cookie payload
// into the fixed signup channel allow-list. The cookie carries free-form
// JSON (utm_source / utm_medium / referrer) on the PostHog side; here we
// only need a bounded label, so we look at utm_source / source / referrer
// fields when present, otherwise the bare string. Empty -> "direct".
// Anything not in the allow-list -> "other".
func NormalizeSignupSource(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "direct"
	}
	// JSON shape: {"utm_source":"...","utm_medium":"...","referrer":"..."}
	if strings.HasPrefix(value, "{") {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(value), &parsed); err == nil {
			for _, key := range []string{"utm_source", "source", "referrer", "ref"} {
				if raw, ok := parsed[key]; ok {
					if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
						value = s
						break
					}
				}
			}
		}
	}
	return normalizeFromAllowList(canonicaliseSignupChannel(value), knownSignupSources, "other")
}

// canonicaliseSignupChannel collapses the raw signup-source string into a
// shape the allow-list can match: lowercase, trimmed, host-only for URL-ish
// values, and with a few obvious aliases unified ("twitter.com" -> "twitter").
// We deliberately keep this defensive — the cookie is set client-side, so any
// shape is possible.
func canonicaliseSignupChannel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	// Strip an optional URL scheme so "https://twitter.com/foo" -> "twitter.com/foo".
	for _, scheme := range []string{"https://", "http://", "//"} {
		if strings.HasPrefix(value, scheme) {
			value = strings.TrimPrefix(value, scheme)
			break
		}
	}
	// Take just the host segment.
	if i := strings.IndexAny(value, "/?#"); i >= 0 {
		value = value[:i]
	}
	value = strings.TrimPrefix(value, "www.")
	// Map well-known hostnames to their channel bucket.
	hostAliases := map[string]string{
		"google.com":           "google",
		"google.co.uk":         "google",
		"bing.com":             "bing",
		"duckduckgo.com":       "duckduckgo",
		"twitter.com":          "twitter",
		"x.com":                "twitter",
		"t.co":                 "twitter",
		"linkedin.com":         "linkedin",
		"lnkd.in":              "linkedin",
		"facebook.com":         "facebook",
		"fb.com":               "facebook",
		"instagram.com":        "instagram",
		"github.com":           "github",
		"gitlab.com":           "gitlab",
		"news.ycombinator.com": "hacker_news",
		"reddit.com":           "reddit",
		"old.reddit.com":       "reddit",
		"youtube.com":          "youtube",
		"youtu.be":             "youtube",
		"discord.com":          "discord",
		"discord.gg":           "discord",
		"slack.com":            "slack",
		"producthunt.com":      "product_hunt",
		"medium.com":           "medium",
		"dev.to":               "dev_to",
	}
	if mapped, ok := hostAliases[value]; ok {
		return mapped
	}
	return value
}

func NormalizeOnboardingPath(value string) string {
	return normalizeFromAllowList(value, knownOnboardingPaths, "unknown")
}

func NormalizeAutopilotCadence(value string) string {
	return normalizeFromAllowList(value, knownAutopilotCadences, "unknown")
}

func NormalizeAutopilotTrigger(value string) string {
	return normalizeFromAllowList(value, knownAutopilotTriggers, "unknown")
}

func NormalizeAutopilotSkipReason(value string) string {
	return normalizeFromAllowList(value, knownAutopilotSkipReasons, "other")
}

func NormalizeWebhookProvider(value string) string {
	return normalizeFromAllowList(value, knownWebhookProviders, "other")
}

func NormalizeWebhookDeliveryStatus(value string) string {
	return normalizeFromAllowList(value, knownWebhookDeliveryStatuses, "other")
}

func NormalizeWebhookRateLimitGate(value string) string {
	return normalizeFromAllowList(value, knownWebhookRateLimitGates, "other")
}

func NormalizeGithubEventKind(value string) string {
	return normalizeFromAllowList(value, knownGithubEventKinds, "other")
}

func NormalizeGithubAction(value string) string {
	if strings.TrimSpace(value) == "" {
		return "none"
	}
	return normalizeFromAllowList(value, knownGithubActions, "other")
}

func NormalizeGithubPRReviewResult(value string) string {
	return normalizeFromAllowList(value, knownGithubPRReviewResults, "other")
}

func NormalizeCloudRuntimeOp(value string) string {
	return normalizeFromAllowList(value, knownCloudRuntimeOps, "other")
}

// NormalizeCloudRuntimeStatus collapses an HTTP status code or symbolic
// outcome string into the fixed bucket set {ok, 4xx, 5xx, timeout, error}.
// Empty / unknown collapses to "error".
func NormalizeCloudRuntimeStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := knownCloudRuntimeStatuses[value]; ok {
		return normalized
	}
	if len(value) == 3 {
		switch value[0] {
		case '2':
			return "ok"
		case '4':
			return "4xx"
		case '5':
			return "5xx"
		}
	}
	return "error"
}

// CloudRuntimeStatusForCode maps an HTTP status code to its bucket label.
// Used by cloudruntime client instrumentation.
func CloudRuntimeStatusForCode(code int) string {
	switch {
	case code >= 200 && code < 400:
		return "ok"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "error"
	}
}

func NormalizeDaemonWSKind(value string) string {
	return normalizeFromAllowList(value, knownDaemonWSKinds, "other")
}

func NormalizeFeedbackKind(value string) string {
	return normalizeFromAllowList(value, knownFeedbackKinds, "other")
}

func NormalizeContactSalesSource(value string) string {
	return normalizeFromAllowList(value, knownContactSalesSources, "other")
}
