package transportretry

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// Observer records transport-retry telemetry (metrics + structured logs).
type Observer interface {
	RecordAttempt(provider, policyID, sessionMode, outcome string)
	RecordRecovered(provider, policyID, sessionMode string)
	RecordCacheReadTokens(provider, policyID string, tokens int64)
	RecordWallSeconds(provider, policyID string, seconds float64)
}

// ShouldRetry reports whether a failed result is eligible for transport retry.
func ShouldRetry(result ResultView, tools int32) bool {
	if tools > 0 {
		return false
	}
	if result.Status != "failed" {
		return false
	}
	return strings.TrimSpace(result.Error) != ""
}

func cacheReadTotal(usage map[string]TokenUsageView) int64 {
	var total int64
	for _, u := range usage {
		total += u.CacheReadTokens
	}
	return total
}

// ExecuteWithRetry runs execute up to the policy launch budget, retrying matching transport failures.
func ExecuteWithRetry(
	ctx context.Context,
	cfg Config,
	provider string,
	priorSessionID string,
	hooks RetryHooks,
	observer Observer,
	taskLog *slog.Logger,
	execute ExecuteFunc,
	initialPrompt string,
	initialOpts ExecOptionsView,
) (ResultView, int32, Stats, string, error) {
	if !cfg.Enabled {
		if observer != nil {
			observer.RecordAttempt(provider, "", "", "skipped_disabled")
		}
		result, tools, err := execute(initialPrompt, initialOpts)
		return result, tools, Stats{}, "", err
	}

	prompt := initialPrompt
	opts := initialOpts
	var (
		stats          Stats
		retiredSession string
		extraWallStart = time.Now()
		launchIndex    int
		lastPolicy     Policy
		matchedPolicy  bool
	)

	for {
		result, tools, err := execute(prompt, opts)
		stats.Attempts = launchIndex + 1
		if launchIndex == 0 {
			stats.CacheReadTokensFirst = cacheReadTotal(result.Usage)
		}

		if err != nil {
			stats.SurfacedToServer = true
			stats.ExtraWallSeconds = time.Since(extraWallStart).Seconds()
			if observer != nil {
				observer.RecordWallSeconds(provider, stats.PolicyID, stats.ExtraWallSeconds)
			}
			return result, tools, stats, retiredSession, err
		}

		if result.Status != "failed" {
			if launchIndex > 0 && observer != nil && matchedPolicy {
				mode := sessionModeForLaunch(lastPolicy, launchIndex-1)
				observer.RecordAttempt(provider, lastPolicy.ID, mode.String(), "recovered")
				observer.RecordRecovered(provider, lastPolicy.ID, mode.String())
				observer.RecordCacheReadTokens(provider, lastPolicy.ID, cacheReadTotal(result.Usage))
			}
			if launchIndex > 0 {
				stats.RecoveredOnAttempt = launchIndex + 1
				stats.CacheReadTokensRecovered = cacheReadTotal(result.Usage)
				stats.ExtraWallSeconds = time.Since(extraWallStart).Seconds()
				if observer != nil {
					observer.RecordWallSeconds(provider, stats.PolicyID, stats.ExtraWallSeconds)
				}
			}
			return result, tools, stats, retiredSession, nil
		}

		if tools > 0 {
			if policy, ok := findPolicy(cfg, provider, result.Error); ok {
				if stats.PolicyID == "" {
					stats.PolicyID = policy.ID
				}
				if observer != nil {
					observer.RecordAttempt(provider, policy.ID, "", "skipped_tools")
				}
			}
			stats.SurfacedToServer = true
			stats.ExtraWallSeconds = time.Since(extraWallStart).Seconds()
			if observer != nil {
				observer.RecordWallSeconds(provider, stats.PolicyID, stats.ExtraWallSeconds)
			}
			return result, tools, stats, retiredSession, nil
		}

		if !ShouldRetry(result, tools) {
			stats.SurfacedToServer = true
			stats.ExtraWallSeconds = time.Since(extraWallStart).Seconds()
			if observer != nil {
				observer.RecordWallSeconds(provider, stats.PolicyID, stats.ExtraWallSeconds)
			}
			return result, tools, stats, retiredSession, nil
		}

		policy, ok := findPolicy(cfg, provider, result.Error)
		if !ok {
			stats.SurfacedToServer = true
			stats.ExtraWallSeconds = time.Since(extraWallStart).Seconds()
			if observer != nil {
				observer.RecordWallSeconds(provider, stats.PolicyID, stats.ExtraWallSeconds)
			}
			return result, tools, stats, retiredSession, nil
		}
		lastPolicy = policy
		matchedPolicy = true
		if stats.PolicyID == "" {
			stats.PolicyID = policy.ID
		}

		maxLaunches := totalLaunches(policy)
		if launchIndex+1 >= maxLaunches {
			if observer != nil {
				observer.RecordAttempt(provider, policy.ID, "", "exhausted")
				observer.RecordWallSeconds(provider, policy.ID, time.Since(extraWallStart).Seconds())
			}
			stats.SurfacedToServer = true
			stats.ExtraWallSeconds = time.Since(extraWallStart).Seconds()
			return result, tools, stats, retiredSession, nil
		}

		retryIndex := launchIndex
		mode := sessionModeForLaunch(policy, retryIndex)
		stats.SessionModes = append(stats.SessionModes, mode)

		if observer != nil {
			observer.RecordAttempt(provider, policy.ID, mode.String(), "retrying")
		}
		logTransportRetryAttempt(taskLog, launchIndex+2, policy.ID, mode, opts.ResumeSessionID, result, false)

		delay := delayForLaunch(policy, retryIndex)
		if delay > 0 || cfg.MaxRetryWallClockS > 0 {
			if err := waitForRetry(ctx, extraWallStart, cfg.MaxRetryWallClockS, delay); err != nil {
				stats.SurfacedToServer = true
				stats.ExtraWallSeconds = time.Since(extraWallStart).Seconds()
				if observer != nil {
					observer.RecordAttempt(provider, policy.ID, mode.String(), "exhausted")
					observer.RecordWallSeconds(provider, policy.ID, stats.ExtraWallSeconds)
				}
				return ResultView{Status: "failed", Error: err.Error()}, tools, stats, retiredSession, err
			}
		}

		switch mode {
		case SessionRetrySame:
			sessionID := opts.ResumeSessionID
			if sessionID == "" {
				sessionID = priorSessionID
			}
			if sessionID == "" {
				sessionID = result.SessionID
			}
			opts.ResumeSessionID = sessionID
		case SessionRetryFresh:
			if hooks.OnFreshSession != nil {
				newPrompt, retired := hooks.OnFreshSession(&opts)
				if retired != "" {
					retiredSession = retired
				}
				if newPrompt != "" {
					prompt = newPrompt
				}
			} else {
				opts.ResumeSessionID = ""
			}
		case SessionRetryCold:
			opts.ResumeSessionID = ""
		}

		launchIndex++
	}
}

func waitForRetry(ctx context.Context, extraWallStart time.Time, maxWallS, delayMs int) error {
	if delayMs <= 0 {
		if maxWallS > 0 && time.Since(extraWallStart).Seconds() >= float64(maxWallS) {
			return ctx.Err()
		}
		return nil
	}
	timer := time.NewTimer(time.Duration(delayMs) * time.Millisecond)
	defer timer.Stop()
	if maxWallS > 0 {
		deadline := extraWallStart.Add(time.Duration(maxWallS) * time.Second)
		if time.Until(deadline) <= 0 {
			return context.DeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func logTransportRetryAttempt(taskLog *slog.Logger, attempt int, policyID string, mode SessionRetryMode, sessionID string, result ResultView, recovered bool) {
	if taskLog == nil {
		return
	}
	taskLog.Info("transport_retry",
		"attempt", attempt,
		"policy", policyID,
		"session_mode", mode.String(),
		"session_id", sessionID,
		"recovered", recovered,
		"cache_read_tokens", cacheReadTotal(result.Usage),
		"event_count", eventCountFromError(result.Error),
		"last_event_type", lastEventTypeFromError(result.Error),
	)
}

func eventCountFromError(err string) int {
	const prefix = "event_count="
	idx := strings.Index(err, prefix)
	if idx < 0 {
		return 0
	}
	start := idx + len(prefix)
	end := start
	for end < len(err) && err[end] >= '0' && err[end] <= '9' {
		end++
	}
	if end == start {
		return 0
	}
	var n int
	for i := start; i < end; i++ {
		n = n*10 + int(err[i]-'0')
	}
	return n
}

func lastEventTypeFromError(err string) string {
	const prefix = "last_event_type="
	idx := strings.Index(err, prefix)
	if idx < 0 {
		return ""
	}
	start := idx + len(prefix)
	end := strings.Index(err[start:], ",")
	if end < 0 {
		end = strings.Index(err[start:], ")")
	}
	if end < 0 {
		return strings.TrimSpace(err[start:])
	}
	return strings.TrimSpace(err[start : start+end])
}

func (m SessionRetryMode) String() string {
	return string(m)
}
