package daemon

import (
	"math/rand"
	"strings"
	"time"
)

var codexRetryJitterInt63n = rand.Int63n

func isCodexTransientUpstreamError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "responses websocket returned 500 internal server error") ||
		(strings.Contains(msg, "responses_websocket") &&
			strings.Contains(msg, "500 internal server error") &&
			strings.Contains(msg, "wss://api.openai.com/v1/responses"))
}

func shouldRetryCodexTask(provider string, err error, attempt, maxAttempts int) bool {
	return provider == "codex" &&
		err != nil &&
		attempt < maxAttempts &&
		isCodexTransientUpstreamError(err)
}

func codexRetryDelay(base, jitter time.Duration, retryNumber int) time.Duration {
	delay := codexRetryBaseDelay(base, retryNumber)
	if delay <= 0 {
		return 0
	}
	return delay + codexRetryJitter(delay, jitter)
}

func codexRetryBaseDelay(base time.Duration, retryNumber int) time.Duration {
	if base <= 0 {
		return 0
	}
	if retryNumber <= 1 {
		return base
	}

	multiplier := time.Duration(1)
	for i := 1; i < retryNumber && i < 6; i++ {
		multiplier *= 2
	}
	return base * multiplier
}

func codexRetryJitter(delay, maxJitter time.Duration) time.Duration {
	if delay <= 0 || maxJitter <= 0 {
		return 0
	}

	// Keep the random spread bounded so retries do not drift too far from the
	// intended exponential schedule.
	if halfDelay := delay / 2; maxJitter > halfDelay {
		maxJitter = halfDelay
	}
	if maxJitter <= 0 {
		return 0
	}

	return time.Duration(codexRetryJitterInt63n(int64(maxJitter) + 1))
}
